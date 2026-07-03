// Package harness provides reusable primitives for building AI coding agents
// whose execution environment is swappable: file tools run over an
// fsx.FileSystem (local disk, S3, ...) and shell tools run over an
// exec.Executor (local shell, remote worker, ...).
package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/tool"
	"github.com/Abraxas-365/manifesto/internal/errx"
)

// DefaultMaxTurns caps the number of LLM turns per Run when Agent.MaxTurns is
// not set.
const DefaultMaxTurns = 25

var agentRegistry = errx.NewRegistry("HARNESS_AGENT")

// ErrMaxTurns is returned when Run exhausts its turn budget without the model
// producing a final answer.
var ErrMaxTurns = agentRegistry.Register(
	"MAX_TURNS_EXCEEDED",
	errx.TypeBusiness,
	http.StatusInternalServerError,
	"Agent exceeded maximum turns without completing",
)

// ErrMaxTokens is returned when the model stops because it hit the output token
// limit before completing its response (and did not request a tool). The
// partial text is attached under the "partial" detail.
var ErrMaxTokens = agentRegistry.Register(
	"MAX_TOKENS_EXCEEDED",
	errx.TypeBusiness,
	http.StatusInternalServerError,
	"Model hit the output token limit before completing its response",
)

// Approver decides whether a tool call that RequiresApproval may run. It
// returns true to allow execution.
type Approver func(t tool.Tool, input json.RawMessage) bool

// Agent runs a minimal LLM tool-calling loop against a swappable tool set.
type Agent struct {
	Provider  llm.Provider
	Registry  *tool.Registry
	System    string
	Model     string
	MaxTokens int
	MaxTurns  int

	// Temperature, when non-nil, is passed to the provider each turn (applied
	// only when the model supports it).
	Temperature *float64
	// TopP, when non-nil, is passed to the provider each turn.
	TopP *float64
	// Reasoning requests reasoning/thinking effort in a provider-agnostic way.
	// Empty = no reasoning. Adapters clamp/omit it for models that can't reason.
	Reasoning llm.ReasoningLevel
	// ProviderOptions carries provider-specific request options keyed by provider
	// id ("openai", "anthropic"). Passed through on every turn.
	ProviderOptions map[string]map[string]any

	// Approver gates tools whose RequiresApproval returns true. If nil, such
	// tools are auto-approved (headless operation).
	Approver Approver

	// Hooks holds optional observability callbacks. Zero value = no-op.
	Hooks Hooks

	// Compactor, if set, compacts history before a turn once the estimated
	// token count exceeds CompactThreshold of the context window. Nil disables
	// compaction.
	Compactor Compactor
	// CompactThreshold is the fraction of the context window at which compaction
	// triggers. 0 uses DefaultCompactThreshold.
	CompactThreshold float64
	// ContextWindow overrides the model's context window in tokens. 0 looks it
	// up by Model, falling back to DefaultContextWindow.
	ContextWindow int
	// TokenEstimator overrides the default token estimator.
	TokenEstimator TokenEstimator

	// discovery, when non-nil, enables deferred-tool discovery: deferred tools
	// are sent to the model as name+hint until revealed via ToolSearch. Set by
	// EnableToolSearch. Nil disables it (all tools sent eagerly).
	discovery *tool.Discovery

	history    []llm.Message
	totalUsage llm.Usage
}

// New creates an Agent with the given provider and tool registry.
func New(provider llm.Provider, registry *tool.Registry) *Agent {
	return &Agent{Provider: provider, Registry: registry}
}

// History returns the accumulated conversation.
func (a *Agent) History() []llm.Message { return a.history }

// TotalUsage returns the token usage accumulated across all Run calls.
func (a *Agent) TotalUsage() llm.Usage { return a.totalUsage }

// Run drives the agent loop for a single user input and returns the final
// assistant text. Conversation state persists across calls via history.
func (a *Agent) Run(ctx context.Context, userInput string) (string, error) {
	maxTurns := a.MaxTurns
	if maxTurns <= 0 {
		maxTurns = DefaultMaxTurns
	}

	a.history = append(a.history, llm.UserText(userInput))

	for turn := 0; turn < maxTurns; turn++ {
		if a.Hooks.OnTurnStart != nil {
			a.Hooks.OnTurnStart(turn)
		}

		if err := a.maybeCompact(ctx); err != nil {
			return "", err
		}

		tools := a.Registry.APIDefinitions()
		system := a.System
		if a.discovery != nil {
			tools = a.Registry.VisibleDefinitions(a.discovery)
			if rem := a.Registry.DeferredReminder(a.discovery); rem != "" {
				system = a.System + "\n\n" + rem
			}
		}

		resp, err := a.Provider.Chat(ctx, llm.Request{
			System:      system,
			Model:       a.Model,
			MaxTokens:   a.MaxTokens,
			Temperature: a.Temperature,
			TopP:        a.TopP,
			Reasoning:   a.Reasoning,
			Provider:    a.ProviderOptions,
			Messages:    a.history,
			Tools:       tools,
		})
		if err != nil {
			return "", err
		}

		a.totalUsage = a.totalUsage.Add(resp.Usage)
		if a.Hooks.OnUsage != nil {
			a.Hooks.OnUsage(turn, resp.Usage, a.totalUsage)
		}

		a.history = append(a.history, resp.Message)

		toolUses := resp.Message.ToolUses()
		if len(toolUses) == 0 {
			text := resp.Message.TextContent()
			if a.Hooks.OnAssistantText != nil {
				a.Hooks.OnAssistantText(text)
			}
			if resp.StopReason == llm.StopMaxTokens {
				return text, agentRegistry.New(ErrMaxTokens).
					WithDetail("partial", text)
			}
			return text, nil
		}

		resultBlocks := make([]llm.ContentBlock, 0, len(toolUses))
		for _, tu := range toolUses {
			resultBlocks = append(resultBlocks, a.dispatch(ctx, tu))
		}
		a.history = append(a.history, llm.Message{Role: llm.RoleUser, Content: resultBlocks})
	}

	return "", agentRegistry.New(ErrMaxTurns).WithDetail("max_turns", maxTurns)
}

// dispatch executes a single tool use and returns its result block.
func (a *Agent) dispatch(ctx context.Context, tu llm.ContentBlock) llm.ContentBlock {
	t, ok := a.Registry.Get(tu.Name)
	if !ok {
		return llm.ToolResultBlock(tu.ID, fmt.Sprintf("Unknown tool: %s", tu.Name), true)
	}

	if t.RequiresApproval(tu.Input) && a.Approver != nil && !a.Approver(t, tu.Input) {
		return llm.ToolResultBlock(tu.ID, "Tool execution denied by approver", true)
	}

	if a.Hooks.OnToolStart != nil {
		a.Hooks.OnToolStart(tu.Name, tu.Input)
	}

	res, err := t.Execute(ctx, tu.Input)
	if err != nil {
		block := llm.ToolResultBlock(tu.ID, fmt.Sprintf("Tool error: %v", err), true)
		if a.Hooks.OnToolEnd != nil {
			a.Hooks.OnToolEnd(tu.Name, block)
		}
		return block
	}

	block := llm.ToolResultBlock(tu.ID, res.Content, res.IsError)
	for _, img := range res.Images {
		block.Images = append(block.Images, llm.ImageBlock(img.MediaType, img.Data))
	}
	if a.Hooks.OnToolEnd != nil {
		a.Hooks.OnToolEnd(tu.Name, block)
	}
	return block
}
