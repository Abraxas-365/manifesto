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
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/tool"
	"github.com/Abraxas-365/manifesto/internal/errx"
)

// DefaultMaxTurns is unused but kept for backwards compatibility with tests.
// Agent.MaxTurns 0 (default) means unlimited — matching legacy's behaviour where
// interactive sessions and subagents have no turn cap.
const DefaultMaxTurns = 0

// DefaultAgentMaxTokens is the output token limit sent to the API when
// Agent.MaxTokens is 0. Kept low (8192) to maximise input capacity
// (context_window − max_tokens). Escalated automatically on retry when
// the model hits this limit on a text-only response (legacy parity).
const DefaultAgentMaxTokens = 8192

// FallbackEscalatedMax is the escalated output token limit used when the
// model's actual max output capacity is unknown.
const FallbackEscalatedMax = 64_000

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

// Approver decides whether a tool call may proceed. The application (not the
// tool) decides which tools need gating — matching the pi-subagents philosophy.
// Return true to allow execution. ctx is the turn context: implementations
// that block on user input must respect its cancellation.
type Approver func(ctx context.Context, name string, input json.RawMessage) bool

// Agent runs a minimal LLM tool-calling loop against a swappable tool set.
type Agent struct {
	Provider  llm.Provider
	Registry  *tool.Registry
	System    string
	Model     string
	MaxTokens int
	// MaxTurns caps LLM calls per Run. 0 = DefaultMaxTurns, negative = unlimited.
	MaxTurns int
	// GraceTurns is the number of additional turns after MaxTurns. Tools remain
	// available but a warning is injected. 0 = hard stop at MaxTurns.
	GraceTurns int

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

	// ContextMessage, when set, is prepended to the first user message of a
	// fresh conversation (project instructions + date, legacy contract). It rides
	// in the user turn so it stays out of the cacheable system prompt prefix.
	ContextMessage string

	// Approver gates tool calls. The application decides which tools need
	// approval (e.g. via permission mode config). Nil = auto-approve all.
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

	// Compaction state — managed internally by maybeCompact / loop.
	lastTotalTokens            int       // total tokens (input+output) from most recent API response
	lastHistoryLen             int       // len(history) when lastTotalTokens was recorded
	justCompacted              bool      // cooldown: skip auto-compact until next API response
	consecutiveCompactFailures int       // circuit breaker counter
	lastAPICallAt              time.Time // used for cache-expiry micro-compaction

	// maxTokensOverride persists the escalated max_tokens across loop
	// iterations. Reset to 0 after tool execution so escalation can fire
	// fresh on the next text-only max_tokens hit.
	maxTokensOverride int

	// frozenReminder caches the deferred-tool system-reminder text. It is
	// rebuilt only when the set of deferred tool names changes (e.g. a lazy
	// plugin loads new tools). This keeps the dynamic system prompt suffix
	// byte-stable across turns, avoiding unnecessary re-processing of ~200
	// tokens on every API call. Matches legacy's frozenDeferredReminder approach.
	frozenReminder     string
	frozenReminderHash string // sorted deferred names key
}

// deferredReminder returns the frozen deferred-tool system-reminder. It
// rebuilds the reminder only when the set of deferred tool names changes
// (hash mismatch), keeping the system prompt byte-stable across turns.
func (a *Agent) deferredReminder() string {
	hints := a.Registry.SearchHints()
	names := make([]string, 0, len(hints))
	for n := range hints {
		names = append(names, n)
	}
	sort.Strings(names)
	key := strings.Join(names, ",")

	if key != a.frozenReminderHash {
		a.frozenReminderHash = key
		a.frozenReminder = a.Registry.DeferredReminder(nil) // nil = ignore reveals, list all
	}
	return a.frozenReminder
}

// effectiveMaxTokens returns the output token limit for the current request:
// user-configured > escalated override > model capability > DefaultAgentMaxTokens.
func (a *Agent) effectiveMaxTokens() int {
	if a.MaxTokens > 0 {
		return a.MaxTokens
	}
	if a.maxTokensOverride > 0 {
		return a.maxTokensOverride
	}
	// Use the model's advertised max output tokens from the capability cache
	// (legacy parity: legacy reads cap.MaxResponseTokens and sends it as max_tokens).
	if cap := llm.Capabilities(a.Model); cap.MaxOutputTokens > 0 {
		return cap.MaxOutputTokens
	}
	return DefaultAgentMaxTokens
}

// New creates an Agent with the given provider and tool registry.
func New(provider llm.Provider, registry *tool.Registry) *Agent {
	return &Agent{Provider: provider, Registry: registry}
}

// History returns the accumulated conversation.
func (a *Agent) History() []llm.Message { return a.history }

// SetHistory replaces the conversation history (used to resume a persisted
// session).
func (a *Agent) SetHistory(msgs []llm.Message) { a.history = msgs }

// SetLastTotalTokens seeds the authoritative total token count from the
// previous turn's API response so that compaction thresholds are evaluated
// correctly on the very first iteration of the new turn. Without this, a fresh
// agent (created per-turn by the session service) starts with lastTotalTokens=0,
// falls back to the chars/4 heuristic, and may underestimate — causing
// compaction to never trigger.
func (a *Agent) SetLastTotalTokens(tokens int) {
	a.lastTotalTokens = tokens
	a.lastHistoryLen = len(a.history)
}

// TotalUsage returns the token usage accumulated across all Run calls.
func (a *Agent) TotalUsage() llm.Usage { return a.totalUsage }

// Run drives the agent loop for a single user input and returns the final
// assistant text. Conversation state persists across calls via history.
func (a *Agent) Run(ctx context.Context, userInput string) (string, error) {
	if a.ContextMessage != "" && len(a.history) == 0 {
		userInput = a.ContextMessage + "\n\n" + userInput
	}
	a.history = append(a.history, llm.UserText(userInput))
	return a.loop(ctx)
}

// RunBlocks drives the agent loop for a user message composed of multiple
// content blocks (text + images). This is the multimodal equivalent of Run.
func (a *Agent) RunBlocks(ctx context.Context, blocks []llm.ContentBlock) (string, error) {
	if a.ContextMessage != "" && len(a.history) == 0 {
		blocks = append([]llm.ContentBlock{llm.Text("<context>\n" + a.ContextMessage + "\n</context>")}, blocks...)
	}
	a.history = append(a.history, llm.Message{Role: llm.RoleUser, Content: blocks})
	return a.loop(ctx)
}

// Continue drives the agent loop on the existing history without appending a
// new user message. Used to process messages already injected into history
// (e.g. a background-run completion reminder) so the agent reacts to them
// autonomously instead of waiting for the next user turn.
func (a *Agent) Continue(ctx context.Context) (string, error) {
	return a.loop(ctx)
}

// maxOverflowRetries bounds how many consecutive compact-and-retry attempts a
// single turn may make after a provider context-overflow rejection.
const maxOverflowRetries = 2

// loop runs the turn loop over the current history until the model stops
// calling tools (or the turn budget is exhausted).
func (a *Agent) loop(ctx context.Context) (string, error) {
	maxTurns := a.MaxTurns
	// 0 = unlimited (legacy parity). Only positive values cap turns.
	totalLimit := maxTurns + a.GraceTurns
	overflowRetries := 0
	for turn := 0; maxTurns <= 0 || turn < totalLimit; turn++ {
		// Check for context cancellation between turns so killed background
		// runs (or other cancellations) stop promptly instead of spinning
		// until max turns.
		if err := ctx.Err(); err != nil {
			return "", err
		}

		// Grace-turn warning: when past the soft limit, inject a user
		// message telling the agent to wrap up (tools still available).
		if maxTurns > 0 && a.GraceTurns > 0 && turn >= maxTurns {
			remaining := totalLimit - turn
			graceMsg := fmt.Sprintf(
				"You have %d turn(s) remaining before hard stop. "+
					"Stop starting new work and provide your final output.",
				remaining,
			)
			a.history = append(a.history, llm.Message{
				Role:    llm.RoleUser,
				Content: []llm.ContentBlock{llm.Text(graceMsg)},
			})
		}

		if a.Hooks.OnTurnStart != nil {
			a.Hooks.OnTurnStart(turn)
		}

		if err := a.maybeCompact(ctx); err != nil {
			return "", err
		}

		// Guard against assistant-trailing history. Some providers (Anthropic
		// Claude Opus 4+) reject requests where the last message has role
		// "assistant" ("does not support assistant message prefill"). This can
		// happen when Continue() is called on history that ends with an
		// assistant response, or after compaction trims a trailing user
		// message. Append a lightweight user nudge so the request is valid.
		if n := len(a.history); n > 0 && a.history[n-1].Role == llm.RoleAssistant {
			a.history = append(a.history, llm.Message{
				Role:    llm.RoleUser,
				Content: []llm.ContentBlock{llm.Text("[continue]")},
			})
		}

		tools := a.Registry.APIDefinitions()
		system := a.System
		if a.discovery != nil {
			tools = a.Registry.VisibleDefinitions(a.discovery)
			if rem := a.deferredReminder(); rem != "" {
				system = a.System + "\n\n" + rem
			}
		}

		maxTok := a.effectiveMaxTokens()
		req := llm.Request{
			System:      system,
			Model:       a.Model,
			MaxTokens:   maxTok,
			Temperature: a.Temperature,
			TopP:        a.TopP,
			Reasoning:   a.Reasoning,
			Provider:    a.ProviderOptions,
			Messages:    a.history,
			Tools:       tools,
		}
		resp, err := a.complete(ctx, req)
		if err != nil {
			// Providers that reject oversized requests with an error (OpenAI's
			// context_length_exceeded 400) instead of a stop reason get the
			// same compact-and-retry recovery as StopContextWindowExceeded
			// below. Bounded per turn: if compaction can't shrink the request
			// the second failure propagates.
			if llm.IsContextOverflow(err) && a.Compactor != nil && overflowRetries < maxOverflowRetries {
				overflowRetries++
				a.lastTotalTokens = a.contextWindow()
				a.lastHistoryLen = 0
				a.justCompacted = false
				turn-- // retry this turn after compaction
				continue
			}
			return "", err
		}
		overflowRetries = 0

		a.totalUsage = a.totalUsage.Add(resp.Usage)
		if a.Hooks.OnUsage != nil {
			a.Hooks.OnUsage(turn, resp.Usage, a.totalUsage)
		}

		// Update compaction state with authoritative token count from the API.
		// InputTokens is normalized to include cached tokens (total context),
		// so InputTokens + OutputTokens approximates the next turn's context size.
		totalTokens := resp.Usage.InputTokens + resp.Usage.OutputTokens
		if totalTokens > 0 {
			a.lastTotalTokens = totalTokens
			a.lastHistoryLen = len(a.history)
			// Clear cooldown — real token count is now known. If context is
			// still above threshold, compaction can fire again on the next
			// iteration. When the provider reports no usage the cooldown is
			// kept, so the (over-)estimator can't re-trigger compaction right
			// after one just ran.
			a.justCompacted = false
		}
		a.lastAPICallAt = time.Now()

		// model_context_window_exceeded: the request was too large. Force
		// compaction and retry instead of failing. Matches legacy behaviour.
		if resp.StopReason == llm.StopContextWindowExceeded {
			if a.Compactor != nil {
				a.lastTotalTokens = a.contextWindow()
				a.lastHistoryLen = 0
				a.justCompacted = false
				turn-- // retry this turn after compaction
				continue
			}
			return "", agentRegistry.New(ErrMaxTokens).
				WithDetail("reason", "model_context_window_exceeded")
		}

		a.history = append(a.history, resp.Message)

		toolUses := resp.Message.ToolUses()
		if len(toolUses) == 0 {
			text := resp.Message.TextContent()
			if a.Hooks.OnAssistantText != nil {
				a.Hooks.OnAssistantText(text)
			}
			if resp.StopReason == llm.StopMaxTokens {
				// Text-only response hit the output limit. If the user
				// hasn't configured a fixed limit and we haven't already
				// escalated, retry at the model's full output capacity
				// (legacy parity: 8k → 64k cycling). When tool_use blocks
				// are present the truncated inputs will error and the
				// model self-corrects on the next turn, so we only
				// escalate for pure-text responses.
				if a.MaxTokens == 0 && a.maxTokensOverride == 0 {
					// Use model's full capacity or the fallback
					cap := llm.Capabilities(a.Model)
					escalated := cap.MaxOutputTokens
					if escalated <= 0 {
						escalated = FallbackEscalatedMax
					}
					a.maxTokensOverride = escalated
					// Remove the truncated assistant message — we'll
					// redo the request at the higher limit.
					a.history = a.history[:len(a.history)-1]
					continue
				}
				return text, agentRegistry.New(ErrMaxTokens).
					WithDetail("partial", text)
			}
			return text, nil
		}

		// Reset escalation override after tool execution so the next
		// text-only response uses the model's default capacity.
		a.maxTokensOverride = 0

		resultBlocks := make([]llm.ContentBlock, len(toolUses))
		if len(toolUses) == 1 {
			resultBlocks[0] = a.dispatch(ctx, toolUses[0])
		} else {
			var wg sync.WaitGroup
			wg.Add(len(toolUses))
			for i, tu := range toolUses {
				go func(i int, tu llm.ContentBlock) {
					defer wg.Done()
					resultBlocks[i] = a.dispatch(ctx, tu)
				}(i, tu)
			}
			wg.Wait()
		}
		a.history = append(a.history, llm.Message{Role: llm.RoleUser, Content: resultBlocks})
	}

	return "", agentRegistry.New(ErrMaxTurns).WithDetail("max_turns", maxTurns)
}

// complete performs one model call. When OnTextDelta is set it uses the
// provider's streaming API and forwards text chunks; otherwise it uses the
// plain Chat call.
func (a *Agent) complete(ctx context.Context, req llm.Request) (*llm.Response, error) {
	if a.Hooks.OnTextDelta == nil {
		return a.Provider.Chat(ctx, req)
	}
	stream, err := a.Provider.ChatStream(ctx, req)
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	for {
		ev, err := stream.Next()
		if err != nil {
			return nil, err
		}
		if ev.TextDelta != "" {
			a.Hooks.OnTextDelta(ev.TextDelta)
		}
		if ev.ThinkingDelta != "" && a.Hooks.OnThinkingDelta != nil {
			a.Hooks.OnThinkingDelta(ev.ThinkingDelta)
		}
		if ev.ToolUseStreaming != nil && a.Hooks.OnToolUseStreaming != nil {
			a.Hooks.OnToolUseStreaming(ev.ToolUseStreaming.ID, ev.ToolUseStreaming.Name)
		}
		if ev.InputJSONDeltaLen > 0 && a.Hooks.OnInputJSONDelta != nil {
			a.Hooks.OnInputJSONDelta(ev.InputJSONDeltaLen)
		}
		if ev.Done {
			return &llm.Response{Message: ev.Message, StopReason: ev.StopReason, Usage: ev.Usage}, nil
		}
	}
}

// dispatch executes a single tool use and returns its result block.
func (a *Agent) dispatch(ctx context.Context, tu llm.ContentBlock) llm.ContentBlock {
	// Fast-path: if context is already cancelled (e.g. Kill on a background
	// run), skip the tool entirely instead of running it and only noticing
	// the cancellation on the next loop iteration.
	if err := ctx.Err(); err != nil {
		return llm.ToolResultBlock(tu.ID, fmt.Sprintf("Cancelled: %v", err), true)
	}

	t, ok := a.Registry.Get(tu.Name)
	if !ok {
		return llm.ToolResultBlock(tu.ID, fmt.Sprintf("Unknown tool: %s", tu.Name), true)
	}

	if a.Approver != nil && !a.Approver(ctx, tu.Name, tu.Input) {
		return llm.ToolResultBlock(tu.ID, "Tool execution denied by approver", true)
	}

	if a.Hooks.OnToolStart != nil {
		if intercept := a.Hooks.OnToolStart(tu.ID, tu.Name, tu.Input); intercept != nil {
			if intercept.Cancel {
				msg := intercept.ErrorMessage
				if msg == "" {
					msg = "Tool call blocked by hook"
				}
				block := llm.ToolResultBlock(tu.ID, msg, true)
				if a.Hooks.OnToolEnd != nil {
					if r := a.Hooks.OnToolEnd(tu.Name, block); r != nil {
						block = *r
					}
				}
				return block
			}
			if intercept.ModifiedInput != nil {
				tu.Input = intercept.ModifiedInput
			}
		}
	}

	res, err := t.Execute(tool.WithUseID(ctx, tu.ID), tu.Input)
	if err != nil {
		block := llm.ToolResultBlock(tu.ID, fmt.Sprintf("Tool error: %v", err), true)
		if a.Hooks.OnToolEnd != nil {
			if r := a.Hooks.OnToolEnd(tu.Name, block); r != nil {
				block = *r
			}
		}
		return block
	}

	content := res.Content
	block := llm.ToolResultBlock(tu.ID, content, res.IsError)
	for _, img := range res.Images {
		block.Images = append(block.Images, llm.ImageBlock(img.MediaType, img.Data))
	}
	if a.Hooks.OnToolEnd != nil {
		if r := a.Hooks.OnToolEnd(tu.Name, block); r != nil {
			block = *r
		}
	}
	return block
}
