package harness

import (
	"context"
	"strings"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm"
)

// DefaultContextWindow is used when a model's context window is unknown.
const DefaultContextWindow = 200000

// DefaultCompactThreshold is the fraction of the context window at which
// compaction triggers when Agent.CompactThreshold is not set.
const DefaultCompactThreshold = 0.8

// contextWindows maps known models to their context window in tokens.
var contextWindows = map[string]int{
	"claude-sonnet-4-20250514":   200000,
	"claude-3-5-sonnet-20241022": 200000,
	"gpt-4o":                     128000,
	"gpt-4o-mini":                128000,
	"gpt-4-turbo":                128000,
}

// TokenEstimator estimates the number of tokens a conversation occupies.
type TokenEstimator func(msgs []llm.Message, system string) int

// EstimateTokens is the default estimator: a ~4-chars-per-token heuristic over
// all text content plus the system prompt.
func EstimateTokens(msgs []llm.Message, system string) int {
	chars := len(system)
	for _, m := range msgs {
		for _, b := range m.Content {
			chars += len(b.Text) + len(b.Content) + len(b.Input)
		}
	}
	return chars / 4
}

// Compactor reduces a conversation's size while preserving its meaning. It is
// opt-in: a nil Agent.Compactor disables compaction entirely.
type Compactor interface {
	Compact(ctx context.Context, msgs []llm.Message) ([]llm.Message, error)
}

// TruncateCompactor drops the oldest messages, keeping the most recent
// KeepRecent messages. It never orphans a tool_result from its tool_use: the
// cut point is advanced past any leading tool-result user message.
type TruncateCompactor struct {
	KeepRecent int
}

// Compact implements Compactor.
func (c TruncateCompactor) Compact(_ context.Context, msgs []llm.Message) ([]llm.Message, error) {
	keep := c.KeepRecent
	if keep <= 0 {
		keep = 6
	}
	if len(msgs) <= keep {
		return msgs, nil
	}

	cut := len(msgs) - keep
	cut = safeCut(msgs, cut)
	return msgs[cut:], nil
}

// safeCut advances cut forward so the first kept message does not begin with an
// orphaned tool_result (whose matching tool_use would be dropped).
func safeCut(msgs []llm.Message, cut int) int {
	for cut < len(msgs) && startsWithToolResult(msgs[cut]) {
		cut++
	}
	return cut
}

// startsWithToolResult reports whether a message's first block is a tool_result.
func startsWithToolResult(m llm.Message) bool {
	return len(m.Content) > 0 && m.Content[0].Type == llm.BlockToolResult
}

// SummarizeCompactor replaces older messages with an LLM-generated summary,
// keeping the most recent KeepRecent messages. Provider must be a raw provider
// (not the agent) to avoid re-entrant compaction.
type SummarizeCompactor struct {
	Provider   llm.Provider
	Model      string
	MaxTokens  int
	KeepRecent int
	// Prompt overrides the default summarization instruction.
	Prompt string
}

// Compact implements Compactor.
func (c SummarizeCompactor) Compact(ctx context.Context, msgs []llm.Message) ([]llm.Message, error) {
	keep := c.KeepRecent
	if keep <= 0 {
		keep = 6
	}
	if len(msgs) <= keep {
		return msgs, nil
	}

	cut := safeCut(msgs, len(msgs)-keep)
	if cut == 0 {
		return msgs, nil
	}
	older := msgs[:cut]
	recent := msgs[cut:]

	prompt := c.Prompt
	if prompt == "" {
		prompt = "Summarize the conversation so far into a concise checkpoint that " +
			"preserves key facts, decisions, file paths, and open tasks. Write only the summary."
	}

	maxTokens := c.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 1024
	}

	// Copy older into a fresh slice before appending the prompt: older and
	// recent share msgs's backing array, so appending in place would clobber
	// recent's first message.
	reqMsgs := make([]llm.Message, 0, len(older)+1)
	reqMsgs = append(reqMsgs, older...)
	reqMsgs = append(reqMsgs, llm.UserText(prompt))

	req := llm.Request{
		Model:     c.Model,
		MaxTokens: maxTokens,
		Messages:  reqMsgs,
	}
	resp, err := c.Provider.Chat(ctx, req)
	if err != nil {
		return nil, err
	}

	summary := strings.TrimSpace(resp.Message.TextContent())
	summaryMsg := llm.UserText("[conversation summary]\n" + summary)

	out := make([]llm.Message, 0, len(recent)+1)
	out = append(out, summaryMsg)
	out = append(out, recent...)
	return out, nil
}

// contextWindow returns the effective context window for the agent.
func (a *Agent) contextWindow() int {
	if a.ContextWindow > 0 {
		return a.ContextWindow
	}
	if w, ok := contextWindows[a.Model]; ok {
		return w
	}
	return DefaultContextWindow
}

// maybeCompact runs the configured compactor when the estimated token count
// exceeds the threshold. It is a no-op when no compactor is configured.
func (a *Agent) maybeCompact(ctx context.Context) error {
	if a.Compactor == nil {
		return nil
	}

	estimator := a.TokenEstimator
	if estimator == nil {
		estimator = EstimateTokens
	}

	threshold := a.CompactThreshold
	if threshold <= 0 {
		threshold = DefaultCompactThreshold
	}

	before := estimator(a.history, a.System)
	if float64(before) <= threshold*float64(a.contextWindow()) {
		return nil
	}

	compacted, err := a.Compactor.Compact(ctx, a.history)
	if err != nil {
		return err
	}
	a.history = compacted

	if a.Hooks.OnCompaction != nil {
		after := estimator(a.history, a.System)
		a.Hooks.OnCompaction(before, after)
	}
	return nil
}
