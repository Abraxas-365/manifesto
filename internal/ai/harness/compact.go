package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/tokenizer"
)

// DefaultContextWindow is used when a model's context window is unknown.
const DefaultContextWindow = 200000

// AutoCompactBufferTokens is the absolute headroom kept before triggering
// full compaction — matching the legacy approach. The effective threshold is:
//
//	contextWindow - maxOutputReserve - AutoCompactBufferTokens
//
// For a 200k context with a 20k output reserve this gives 167k (~92.8%).
const AutoCompactBufferTokens = 13_000

// MaxOutputTokensForCompact caps the output-token reservation subtracted from
// the context window when computing the compaction threshold.
const MaxOutputTokensForCompact = 20_000

// DefaultCompactThreshold is used ONLY when the caller explicitly sets
// Agent.CompactThreshold to a non-zero value; the default path uses the
// absolute-buffer formula above instead.
const DefaultCompactThreshold = 0.8

// DefaultMicroCompactThreshold is the fraction of the context window at which
// micro-compaction (clearing large old tool results) triggers.
const DefaultMicroCompactThreshold = 0.6

// MaxConsecutiveCompactFailures is the circuit breaker limit — after this many
// consecutive compaction failures, auto-compact is disabled until a manual
// compact or session reset.
const MaxConsecutiveCompactFailures = 3

// perMessageFraming approximates the role/structure tokens the API adds around
// each message (role marker, content-block wrappers).
const perMessageFraming = 4

// imageTokenEstimate is the flat token count for image blocks.
const imageTokenEstimate = 1600

// contextWindows maps known models to their context window in tokens.
var contextWindows = map[string]int{
	"claude-sonnet-4-20250514":   200000,
	"claude-sonnet-5":            200000,
	"claude-opus-4-6":            200000,
	"claude-opus-4-8":            200000,
	"claude-3-5-sonnet-20241022": 200000,
	"claude-haiku-4-5-20251001":  200000,
	"gpt-4o":                     128000,
	"gpt-4o-mini":                128000,
	"gpt-4-turbo":                128000,
}

// ContextWindowForModel returns the context window size for the given model,
// falling back to DefaultContextWindow for unknown models.
func ContextWindowForModel(model string) int {
	if w, ok := contextWindows[model]; ok {
		return w
	}
	return DefaultContextWindow
}

// CompactThresholdForModel returns the absolute token threshold at which
// auto-compaction fires for the given model, using the legacy-style formula:
//
//	contextWindow - min(maxOutput, 20k) - 13k
//
// maxOutputTokens should be the model's max output; pass 0 to use 16384.
func CompactThresholdForModel(model string, maxOutputTokens int) int {
	window := ContextWindowForModel(model)
	if maxOutputTokens <= 0 {
		maxOutputTokens = 16384
	}
	outputReserve := maxOutputTokens
	if outputReserve > MaxOutputTokensForCompact {
		outputReserve = MaxOutputTokensForCompact
	}
	threshold := window - outputReserve - AutoCompactBufferTokens
	if threshold < 0 {
		threshold = 0
	}
	return threshold
}

// TokenEstimator estimates the number of tokens a conversation occupies.
type TokenEstimator func(msgs []llm.Message, system string) int

// EstimateTokens is the default estimator: uses BPE token counting (o200k_base)
// over all text content plus the system prompt. Falls back to chars/4 if the
// BPE encoder is unavailable.
func EstimateTokens(msgs []llm.Message, system string) int {
	total := tokenizer.Count(system)
	for _, m := range msgs {
		total += perMessageFraming
		for _, b := range m.Content {
			switch b.Type {
			case llm.BlockImage:
				total += imageTokenEstimate
			case llm.BlockThinking, llm.BlockRedactedThinking:
				total += tokenizer.Count(b.Thinking)
			case llm.BlockToolUse:
				total += tokenizer.Count(b.Name) + tokenizer.Count(string(b.Input))
			case llm.BlockToolResult:
				total += tokenizer.Count(b.Content)
				for range b.Images {
					total += imageTokenEstimate
				}
			default:
				total += tokenizer.Count(b.Text)
			}
		}
	}
	return total
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
	if cut >= len(msgs) {
		return msgs, nil // all messages are orphaned tool_results; keep everything
	}
	return SanitizeToolPairs(msgs[cut:]), nil
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

// maxSummarizationBytes is a coarse upper bound on total content size sent to
// the summarizer, applied before the token-based budget (legacy parity: 600KB).
const maxSummarizationBytes = 600_000

// summaryToolInputMaxChars caps tool_use input JSON sent to the summarizer.
// Write/Edit inputs can embed entire files; the summarizer only needs the gist.
const summaryToolInputMaxChars = 2000

// summaryPromptMarginTokens reserves room for the summarization instruction,
// system preamble, and message framing when computing the input token budget.
const summaryPromptMarginTokens = 4_000

// compactKeepBudgetTokens is the token budget for the "recent" slice kept
// after compaction. legacy uses 15 000 — if the last N messages exceed this,
// fewer are kept to avoid double-compaction on image-heavy turns.
const compactKeepBudgetTokens = 15_000

// Compact implements Compactor.
func (c SummarizeCompactor) Compact(ctx context.Context, msgs []llm.Message) ([]llm.Message, error) {
	keep := c.KeepRecent
	if keep <= 0 {
		keep = 10
	}
	if len(msgs) <= 1 {
		return msgs, nil
	}

	// Token-budgeted keep: walk backward from the end, accumulating up to
	// compactKeepBudgetTokens. If we exceed the budget before reaching
	// KeepRecent messages, stop early to prevent the post-compaction context
	// from immediately re-triggering compaction (legacy parity).
	budgetUsed := 0
	keptCount := 0
	for i := len(msgs) - 1; i >= 0 && keptCount < keep; i-- {
		msgTokens := 0
		for _, b := range msgs[i].Content {
			switch b.Type {
			case llm.BlockImage:
				msgTokens += imageTokenEstimate
			case llm.BlockThinking, llm.BlockRedactedThinking:
				msgTokens += tokenizer.Count(b.Thinking)
			case llm.BlockToolUse:
				msgTokens += tokenizer.Count(b.Name) + tokenizer.Count(string(b.Input))
			case llm.BlockToolResult:
				msgTokens += tokenizer.Count(b.Content)
				for range b.Images {
					msgTokens += imageTokenEstimate
				}
			default:
				msgTokens += tokenizer.Count(b.Text)
			}
		}
		if keptCount > 0 && budgetUsed+msgTokens > compactKeepBudgetTokens {
			break // exceeds budget — stop here (always keep at least 1)
		}
		budgetUsed += msgTokens
		keptCount++
	}
	if keptCount < 1 {
		keptCount = 1
	}

	cut := safeCut(msgs, len(msgs)-keptCount)
	if cut == 0 || cut >= len(msgs) {
		return msgs, nil
	}
	older := msgs[:cut]
	recent := msgs[cut:]

	maxTokens := c.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 8192
	}

	// --- Preprocess older messages before summarization (legacy parity) ---
	// 1. Strip images and thinking blocks (can't summarize, waste tokens).
	older = stripImages(older)
	older = stripThinkingBlocks(older)
	// 2. Truncate tool results AND tool inputs to 2000 chars for the
	//    summarizer (Pi-style). Write/Edit inputs can embed whole files.
	older = truncateToolResultsForSummary(older, 2000)
	older = truncateToolInputsForSummary(older, summaryToolInputMaxChars)
	// 3. Coarse byte cap, then a token-based budget so the summarization
	//    request itself cannot overflow the model's context window (bytes/4
	//    underestimates code-heavy content, which tokenizes at ~3 chars/token).
	older = truncateToBytes(older, maxSummarizationBytes)
	inputBudget := ContextWindowForModel(c.Model) - maxTokens - summaryPromptMarginTokens
	if inputBudget > 0 {
		older = truncateToTokens(older, inputBudget)
	}
	// 4. Remove orphaned tool_result/tool_use blocks left by preprocessing.
	older = SanitizeToolPairs(older)

	if len(older) == 0 {
		return nil, fmt.Errorf("nothing left to summarize after preprocessing (%d messages kept)", len(recent))
	}

	prompt := c.Prompt
	if prompt == "" {
		prompt = defaultCompactionPrompt
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
		System: `CRITICAL: Respond with TEXT ONLY. Do NOT call any tools.

- Do NOT use Read, Bash, Grep, Glob, Edit, Write, or ANY other tool.
- You already have all the context you need in the conversation above.
- Tool calls will be REJECTED and will waste your only turn — you will fail the task.
- Your entire response must be plain text: an <analysis> block followed by a <summary> block.

`,
	}
	resp, err := c.Provider.Chat(ctx, req)
	if err != nil {
		return nil, err
	}

	summary := strings.TrimSpace(resp.Message.TextContent())
	if summary == "" {
		return nil, fmt.Errorf("compaction returned empty summary")
	}
	summaryMsg := llm.UserText("[conversation summary]\n" + summary)

	out := make([]llm.Message, 0, len(recent)+1)
	out = append(out, summaryMsg)
	out = append(out, recent...)
	return SanitizeToolPairs(out), nil
}

// noToolsTrailer is appended at the end of every compaction prompt as a final
// reminder (adaptive-thinking models sometimes attempt tool calls despite the
// system-level preamble).
const noToolsTrailer = `

REMINDER: Do NOT call any tools. Respond with plain text only — an <analysis> block followed by a <summary> block. Tool calls will be rejected and you will fail the task.`

// defaultCompactionPrompt is the summarization instruction sent to the LLM.
// Ported from legacy's baseCompactPrompt for full parity.
const defaultCompactionPrompt = `Your task is to create a detailed summary of the conversation so far, paying close attention to the user's explicit requests and your previous actions.
This summary should be thorough in capturing technical details, code patterns, and architectural decisions that would be essential for continuing development work without losing context.

Before providing your final summary, wrap your analysis in <analysis> tags to organize your thoughts and ensure you've covered all necessary points. In your analysis process:

1. Chronologically analyze each message and section of the conversation. For each section thoroughly identify:
   - The user's explicit requests and intents
   - Your approach to addressing the user's requests
   - Key decisions, technical concepts and code patterns
   - Specific details like:
     - file names
     - full code snippets
     - function signatures
     - file edits
   - Errors that you ran into and how you fixed them
   - Pay special attention to specific user feedback that you received, especially if the user told you to do something differently.
2. Double-check for technical accuracy and completeness, addressing each required element thoroughly.

Your summary should include the following sections:

1. Primary Request and Intent: Capture all of the user's explicit requests and intents in detail
2. Key Technical Concepts: List all important technical concepts, technologies, and frameworks discussed.
3. Files and Code Sections: Enumerate specific files and code sections examined, modified, or created. Pay special attention to the most recent messages and include full code snippets where applicable and include a summary of why this file read or edit is important.
4. Errors and fixes: List all errors that you ran into, and how you fixed them. Pay special attention to specific user feedback that you received, especially if the user told you to do something differently.
5. Problem Solving: Document problems solved and any ongoing troubleshooting efforts.
6. All user messages: List ALL user messages that are not tool results. These are critical for understanding the users' feedback and changing intent.
7. Pending Tasks: Outline any pending tasks that you have explicitly been asked to work on.
8. Current Work: Describe in detail precisely what was being worked on immediately before this summary request, paying special attention to the most recent messages from both user and assistant. Include file names and code snippets where applicable.
9. Optional Next Step: List the next step that you will take that is related to the most recent work you were doing. IMPORTANT: ensure that this step is DIRECTLY in line with the user's most recent explicit requests, and the task you were working on immediately before this summary request. If your last task was concluded, then only list next steps if they are explicitly in line with the users request. Do not start on tangential requests or really old requests that were already completed without confirming with the user first.
                       If there is a next step, include direct quotes from the most recent conversation showing exactly what task you were working on and where you left off. This should be verbatim to ensure there's no drift in task interpretation.

Here's an example of how your output should be structured:

<example>
<analysis>
[Your thought process, ensuring all points are covered thoroughly and accurately]
</analysis>

<summary>
1. Primary Request and Intent:
   [Detailed description]

2. Key Technical Concepts:
   - [Concept 1]
   - [Concept 2]
   - [...]

3. Files and Code Sections:
   - [File Name 1]
      - [Summary of why this file is important]
      - [Summary of the changes made to this file, if any]
      - [Important Code Snippet]
   - [File Name 2]
      - [Important Code Snippet]
   - [...]

4. Errors and fixes:
    - [Detailed description of error 1]:
      - [How you fixed the error]
      - [User feedback on the error if any]
    - [...]

5. Problem Solving:
   [Description of solved problems and ongoing troubleshooting]

6. All user messages: 
    - [Detailed non tool use user message]
    - [...]

7. Pending Tasks:
   - [Task 1]
   - [Task 2]
   - [...]

8. Current Work:
   [Precise description of current work]

9. Optional Next Step:
   [Optional Next step to take]

</summary>
</example>

Please provide your summary based on the conversation so far, following this structure and ensuring precision and thoroughness in your response.` + noToolsTrailer

// contextWindow returns the effective context window for the agent.
func (a *Agent) contextWindow() int {
	if a.ContextWindow > 0 {
		return a.ContextWindow
	}
	if w := ContextWindowForModel(a.Model); w > 0 {
		return w
	}
	return DefaultContextWindow
}

// estimateTokens returns the best-available token estimate. When the API has
// reported total tokens (lastTotalTokens), it uses that as the baseline and
// adds BPE-estimated tokens for any messages appended since (tool results, etc.).
func (a *Agent) estimateTokens() int {
	if a.lastTotalTokens > 0 {
		tokens := a.lastTotalTokens
		// Add estimated tokens for messages appended after the last API
		// response (assistant message + tool results). Without this, large
		// tool results wouldn't be counted until the next API call — too late.
		if a.lastHistoryLen > 0 && len(a.history) > a.lastHistoryLen {
			for _, m := range a.history[a.lastHistoryLen:] {
				tokens += perMessageFraming
				for _, b := range m.Content {
					switch b.Type {
					case llm.BlockImage:
						tokens += imageTokenEstimate
					case llm.BlockThinking, llm.BlockRedactedThinking:
						tokens += tokenizer.Count(b.Thinking)
					case llm.BlockToolUse:
						tokens += tokenizer.Count(b.Name) + tokenizer.Count(string(b.Input))
					case llm.BlockToolResult:
						tokens += tokenizer.Count(b.Content)
						for range b.Images {
							tokens += imageTokenEstimate
						}
					default:
						tokens += tokenizer.Count(b.Text)
					}
				}
			}
		}
		return tokens
	}
	estimator := a.TokenEstimator
	if estimator == nil {
		estimator = EstimateTokens
	}
	return estimator(a.history, a.System)
}

// maybeCompact runs compaction when needed. It supports three tiers matching
// legacy's behavior:
//  1. Force compact: above ~98% of context — always fires, ignores cooldown.
//  2. Auto compact: above CompactThreshold (~80%) — runs LLM summarization.
//  3. Micro compact: above MicroCompactThreshold (~60%) — clears large old
//     tool results without an LLM call.
//
// Compaction is skipped (except force) when justCompacted is true — the
// cooldown is cleared after the next API response reports real token usage.
func (a *Agent) maybeCompact(ctx context.Context) error {
	if a.Compactor == nil {
		return nil
	}

	tokens := a.estimateTokens()
	window := a.contextWindow()

	compactThreshold := a.CompactThreshold
	if compactThreshold <= 0 {
		// legacy-style absolute buffer: effective = window - outputReserve - 13k.
		// For a 200k/20k-output model this gives 167k (~92.8%).
		outputReserve := a.effectiveMaxTokens()
		if outputReserve > MaxOutputTokensForCompact {
			outputReserve = MaxOutputTokensForCompact
		}
		effective := window - outputReserve
		autoThreshold := effective - AutoCompactBufferTokens
		if autoThreshold < 0 {
			autoThreshold = 0
		}
		compactThreshold = float64(autoThreshold) / float64(window)
	}
	microThreshold := DefaultMicroCompactThreshold

	// Force compaction: context nearly full (98%). Ignores cooldown but not
	// the circuit breaker — if compaction keeps failing, retrying it on every
	// iteration would just spam failing LLM calls; let the provider's own
	// overflow error surface through the normal error path instead.
	forceThreshold := 0.98
	if float64(tokens) >= forceThreshold*float64(window) {
		if a.consecutiveCompactFailures >= MaxConsecutiveCompactFailures {
			return nil
		}
		return a.runFullCompact(ctx, tokens)
	}

	// Skip auto/micro compaction during cooldown — wait for the next API
	// response to report the real post-compaction token count.
	if a.justCompacted {
		return nil
	}

	// Circuit breaker: too many consecutive failures, don't auto-compact.
	if a.consecutiveCompactFailures >= MaxConsecutiveCompactFailures {
		return nil
	}

	// Auto compaction: above the configured threshold.
	if float64(tokens) >= compactThreshold*float64(window) {
		return a.runFullCompact(ctx, tokens)
	}

	// Micro compaction: above 60%, clear large old tool results.
	if float64(tokens) >= microThreshold*float64(window) {
		compacted := MicroCompact(a.history, 5, 512)
		if len(compacted) != len(a.history) || !messagesEqual(compacted, a.history) {
			a.history = compacted
			a.fireMicroCompact()
		}
		return nil
	}

	// Cache-expiry micro-compaction: when prompt cache has expired (~5 min),
	// old tool results will be re-processed at full cost. Clear them
	// proactively to avoid paying for stale content (especially images in
	// tool results like screenshots). V1 calls this TimeBasedMicroCompact.
	if !a.lastAPICallAt.IsZero() && time.Since(a.lastAPICallAt) > 5*time.Minute {
		compacted := MicroCompact(a.history, 5, 512)
		if len(compacted) != len(a.history) || !messagesEqual(compacted, a.history) {
			a.history = compacted
			a.fireMicroCompact()
		}
	}

	return nil
}

// fireMicroCompact notifies observers that MicroCompact cleared tool results.
func (a *Agent) fireMicroCompact() {
	if a.Hooks.OnMicroCompact != nil {
		a.Hooks.OnMicroCompact()
	}
}

// runFullCompact executes the full Compactor and handles cooldown/circuit
// breaker state. Failures (including no-op compactions that couldn't shrink
// the history) never fail the turn: they increment the circuit breaker and
// fire OnCompactionFailed so frontends can warn the user.
func (a *Agent) runFullCompact(ctx context.Context, tokensBefore int) error {
	if a.Hooks.OnCompactionStart != nil {
		a.Hooks.OnCompactionStart()
	}
	compacted, err := a.Compactor.Compact(ctx, a.history)
	if err == nil && len(compacted) >= len(a.history) {
		// No-op compaction: the compactor had nothing it could shrink (e.g.
		// everything fits in the keep window). Treating this as success would
		// re-trigger compaction on every iteration without ever making
		// progress, so count it as a failure toward the circuit breaker.
		err = fmt.Errorf("compaction could not shrink history (%d messages, ~%d tokens)", len(a.history), tokensBefore)
	}
	if err != nil {
		a.consecutiveCompactFailures++
		// Don't fail the turn — warn and continue with uncompacted history.
		if a.consecutiveCompactFailures >= MaxConsecutiveCompactFailures {
			err = fmt.Errorf("%w (auto-compact disabled after %d consecutive failures)", err, a.consecutiveCompactFailures)
		}
		if a.Hooks.OnCompactionFailed != nil {
			a.Hooks.OnCompactionFailed(err)
		}
		return nil
	}

	a.history = compacted
	a.justCompacted = true
	a.consecutiveCompactFailures = 0
	// Reset the API token counter — it's stale after compaction. The next
	// API response will set the real post-compaction count.
	a.lastTotalTokens = 0
	a.lastHistoryLen = 0

	if a.Hooks.OnCompaction != nil {
		estimator := a.TokenEstimator
		if estimator == nil {
			estimator = EstimateTokens
		}
		after := estimator(a.history, a.System)
		a.Hooks.OnCompaction(tokensBefore, after)
	}
	return nil
}

// MicroCompact clears large tool-result content blocks from older messages,
// keeping the most recent keepLastResults tool results intact. Only results
// larger than minSizeBytes are cleared. This frees token space without an LLM
// call — the model can always re-read files or re-run tools if needed.
func MicroCompact(msgs []llm.Message, keepLastResults int, minSizeBytes int) []llm.Message {
	if len(msgs) == 0 {
		return msgs
	}

	// Collect all tool_result blocks with their size and position.
	type resultInfo struct {
		msgIdx   int
		blockIdx int
		size     int
		isError  bool
	}
	var all []resultInfo
	for i, msg := range msgs {
		if msg.Role != llm.RoleUser {
			continue
		}
		for j, b := range msg.Content {
			if b.Type != llm.BlockToolResult {
				continue
			}
			if isAlreadyCompacted(b.Content) {
				continue
			}
			all = append(all, resultInfo{
				msgIdx: i, blockIdx: j,
				size: len(b.Content), isError: b.IsError,
			})
		}
	}

	if len(all) <= keepLastResults {
		return msgs
	}

	// Protect the last keepLastResults.
	protectedStart := len(all) - keepLastResults
	protected := make(map[int]bool, keepLastResults)
	for i := protectedStart; i < len(all); i++ {
		protected[i] = true
	}

	// Calculate total and find clearable candidates.
	const targetBytes = 100_000
	totalSize := 0
	type candidate struct {
		idx  int
		size int
	}
	var candidates []candidate
	for i, r := range all {
		totalSize += r.size
		if !protected[i] && !r.isError && r.size >= minSizeBytes {
			candidates = append(candidates, candidate{idx: i, size: r.size})
		}
	}

	if totalSize <= targetBytes {
		return msgs // already under budget
	}

	// Sort candidates by size descending — clear biggest first.
	sort.Slice(candidates, func(a, b int) bool {
		return candidates[a].size > candidates[b].size
	})

	// Select candidates to clear until under budget.
	type clearKey struct{ msgIdx, blockIdx int }
	toClear := make(map[clearKey]bool)
	remaining := totalSize
	for _, c := range candidates {
		if remaining <= targetBytes {
			break
		}
		r := all[c.idx]
		toClear[clearKey{r.msgIdx, r.blockIdx}] = true
		remaining -= r.size
	}

	if len(toClear) == 0 {
		return msgs
	}

	// Apply clears — create new message slices to avoid mutating the originals.
	result := make([]llm.Message, len(msgs))
	copy(result, msgs)
	for i, msg := range result {
		if msg.Role != llm.RoleUser {
			continue
		}
		modified := false
		for j, b := range msg.Content {
			if !toClear[clearKey{i, j}] {
				continue
			}
			if !modified {
				// Copy the content slice on first modification.
				newContent := make([]llm.ContentBlock, len(msg.Content))
				copy(newContent, msg.Content)
				result[i].Content = newContent
				modified = true
			}

			toolName := toolNameForUseID(msgs, b.ToolUseID)
			stub := fmt.Sprintf("[result cleared — %d bytes]", len(b.Content))
			if toolName == "Read" {
				stub = fmt.Sprintf("[Read result cleared (%d bytes) — re-read the file or use Grep for specific content]", len(b.Content))
			}
			result[i].Content[j] = llm.ContentBlock{
				Type:      llm.BlockToolResult,
				ToolUseID: b.ToolUseID,
				Content:   stub,
			}
		}
	}

	return result
}

// toolNameForUseID finds the tool name for a given tool_use ID by scanning
// assistant messages.
func toolNameForUseID(msgs []llm.Message, useID string) string {
	for _, m := range msgs {
		if m.Role != llm.RoleAssistant {
			continue
		}
		for _, b := range m.Content {
			if b.Type == llm.BlockToolUse && b.ID == useID {
				return b.Name
			}
		}
	}
	return ""
}

// messagesEqual does a shallow comparison of two message slices by checking
// whether each message's content blocks point to the same underlying data.
// MicroCompact creates new content slices when it modifies a message, so
// pointer equality is sufficient to detect changes.
func messagesEqual(a, b []llm.Message) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if len(a[i].Content) != len(b[i].Content) {
			return false
		}
		for j := range a[i].Content {
			ab, bb := a[i].Content[j], b[i].Content[j]
			if ab.Content != bb.Content || ab.Text != bb.Text {
				return false
			}
		}
	}
	return true
}

// isAlreadyCompacted returns true if a tool result has already been cleared by
// a prior MicroCompact or compaction pass.
func isAlreadyCompacted(content string) bool {
	if len(content) > 300 {
		return false
	}
	return strings.HasPrefix(content, "[Tool output too large") ||
		strings.HasPrefix(content, "[result cleared") ||
		strings.HasPrefix(content, "[Read result") ||
		strings.HasPrefix(content, "[Old tool result") ||
		strings.HasPrefix(content, "[content cleared") ||
		strings.HasPrefix(content, "[tool result persisted")
}

// SanitizeToolPairs removes orphaned tool_result blocks whose tool_use_id does
// not appear in the immediately preceding assistant message. The Anthropic API
// requires every tool_result to have a matching tool_use in the previous
// message; compaction, truncation, or preprocessing can break this invariant.
//
// It also removes orphaned tool_use-only assistant messages (where all
// corresponding tool_results were in a removed user message).
func SanitizeToolPairs(msgs []llm.Message) []llm.Message {
	if len(msgs) == 0 {
		return msgs
	}

	out := make([]llm.Message, 0, len(msgs))
	for i, m := range msgs {
		if m.Role == llm.RoleUser && hasToolResult(m) {
			// Collect tool_use IDs from the previous assistant message.
			validIDs := make(map[string]bool)
			if i > 0 && msgs[i-1].Role == llm.RoleAssistant {
				for _, b := range msgs[i-1].Content {
					if b.Type == llm.BlockToolUse && b.ID != "" {
						validIDs[b.ID] = true
					}
				}
			}

			// Filter out orphaned tool_result blocks.
			var filtered []llm.ContentBlock
			for _, b := range m.Content {
				if b.Type == llm.BlockToolResult {
					if !validIDs[b.ToolUseID] {
						continue // orphaned — drop it
					}
				}
				filtered = append(filtered, b)
			}
			if len(filtered) == 0 {
				continue // entire message was orphaned
			}
			out = append(out, llm.Message{Role: m.Role, Content: filtered})
			continue
		}

		// For assistant messages with tool_use blocks, check that at least
		// one tool_result in the next user message references them.
		// If the next message is missing or has no matching results,
		// the tool_use blocks are orphaned (the result message was removed).
		if m.Role == llm.RoleAssistant && hasToolUse(m) {
			nextResultIDs := make(map[string]bool)
			if i+1 < len(msgs) && msgs[i+1].Role == llm.RoleUser {
				for _, b := range msgs[i+1].Content {
					if b.Type == llm.BlockToolResult {
						nextResultIDs[b.ToolUseID] = true
					}
				}
			}

			// Keep non-tool-use blocks + tool_use blocks with a result.
			var filtered []llm.ContentBlock
			for _, b := range m.Content {
				if b.Type == llm.BlockToolUse {
					if !nextResultIDs[b.ID] {
						continue // orphaned tool_use — drop it
					}
				}
				filtered = append(filtered, b)
			}
			if len(filtered) == 0 {
				continue // entire message was orphaned
			}
			out = append(out, llm.Message{Role: m.Role, Content: filtered})
			continue
		}

		out = append(out, m)
	}
	return out
}

// hasToolResult reports whether a message contains at least one tool_result block.
func hasToolResult(m llm.Message) bool {
	for _, b := range m.Content {
		if b.Type == llm.BlockToolResult {
			return true
		}
	}
	return false
}

// hasToolUse reports whether a message contains at least one tool_use block.
func hasToolUse(m llm.Message) bool {
	for _, b := range m.Content {
		if b.Type == llm.BlockToolUse {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Pre-summarization preprocessing (legacy parity)
// ---------------------------------------------------------------------------

// stripImages removes all image content blocks from messages. Images can't be
// meaningfully summarized and waste tokens in the compaction request.
func stripImages(msgs []llm.Message) []llm.Message {
	out := make([]llm.Message, 0, len(msgs))
	for _, m := range msgs {
		hasImage := false
		for _, b := range m.Content {
			if b.Type == llm.BlockImage || len(b.Images) > 0 {
				hasImage = true
				break
			}
		}
		if !hasImage {
			out = append(out, m)
			continue
		}
		// Filter out image blocks; clear Images on tool_result blocks.
		var filtered []llm.ContentBlock
		for _, b := range m.Content {
			if b.Type == llm.BlockImage {
				continue
			}
			if len(b.Images) > 0 {
				b2 := b
				b2.Images = nil
				filtered = append(filtered, b2)
			} else {
				filtered = append(filtered, b)
			}
		}
		if len(filtered) > 0 {
			out = append(out, llm.Message{Role: m.Role, Content: filtered})
		}
	}
	return out
}

// stripThinkingBlocks removes all thinking and redacted_thinking blocks from
// messages. Thinking content can't be meaningfully summarized and wastes tokens
// in the compaction request.
func stripThinkingBlocks(msgs []llm.Message) []llm.Message {
	out := make([]llm.Message, 0, len(msgs))
	for _, m := range msgs {
		if m.Role != llm.RoleAssistant {
			out = append(out, m)
			continue
		}
		hasThinking := false
		for _, b := range m.Content {
			if b.Type == llm.BlockThinking || b.Type == llm.BlockRedactedThinking {
				hasThinking = true
				break
			}
		}
		if !hasThinking {
			out = append(out, m)
			continue
		}
		var filtered []llm.ContentBlock
		for _, b := range m.Content {
			if b.Type == llm.BlockThinking || b.Type == llm.BlockRedactedThinking {
				continue
			}
			filtered = append(filtered, b)
		}
		if len(filtered) > 0 {
			out = append(out, llm.Message{Role: m.Role, Content: filtered})
		}
	}
	return out
}

// truncateToolResultsForSummary truncates tool_result content blocks to
// maxChars characters for the compaction summary prompt (Pi-style). Unlike
// clearLargeToolResults which replaces the entire content with a stub, this
// keeps the first maxChars characters so the summarizer has meaningful context.
func truncateToolResultsForSummary(msgs []llm.Message, maxChars int) []llm.Message {
	out := make([]llm.Message, len(msgs))
	copy(out, msgs)
	for i, m := range out {
		if m.Role != llm.RoleUser {
			continue
		}
		modified := false
		for j, b := range m.Content {
			if b.Type != llm.BlockToolResult {
				continue
			}
			if len(b.Content) <= maxChars {
				continue
			}
			if isAlreadyCompacted(b.Content) {
				continue
			}
			if !modified {
				newContent := make([]llm.ContentBlock, len(m.Content))
				copy(newContent, m.Content)
				out[i].Content = newContent
				modified = true
			}
			out[i].Content[j] = llm.ContentBlock{
				Type:      llm.BlockToolResult,
				ToolUseID: b.ToolUseID,
				Content:   b.Content[:maxChars] + fmt.Sprintf("\n[truncated — %d bytes total]", len(b.Content)),
			}
		}
	}
	return out
}

// clearLargeToolResults replaces tool_result content blocks larger than
// maxBytes with a short stub. This is the pre-summarization variant of
// MicroCompact — more aggressive (clears ALL large results, not just old
// ones) because the summarizer only needs the gist, not the full output.
func clearLargeToolResults(msgs []llm.Message, maxBytes int) []llm.Message {
	out := make([]llm.Message, len(msgs))
	copy(out, msgs)
	for i, m := range out {
		if m.Role != llm.RoleUser {
			continue
		}
		modified := false
		for j, b := range m.Content {
			if b.Type != llm.BlockToolResult {
				continue
			}
			if len(b.Content) <= maxBytes {
				continue
			}
			if isAlreadyCompacted(b.Content) {
				continue
			}
			if !modified {
				newContent := make([]llm.ContentBlock, len(m.Content))
				copy(newContent, m.Content)
				out[i].Content = newContent
				modified = true
			}
			out[i].Content[j] = llm.ContentBlock{
				Type:      llm.BlockToolResult,
				ToolUseID: b.ToolUseID,
				Content:   fmt.Sprintf("[content cleared — %d bytes]", len(b.Content)),
			}
		}
	}
	return out
}

// truncateToolInputsForSummary truncates tool_use input JSON to maxChars for
// the compaction summary prompt. Write/Edit tool inputs can embed entire
// files; the summarizer only needs to know which tool ran and roughly on what.
func truncateToolInputsForSummary(msgs []llm.Message, maxChars int) []llm.Message {
	out := make([]llm.Message, len(msgs))
	copy(out, msgs)
	for i, m := range out {
		if m.Role != llm.RoleAssistant {
			continue
		}
		modified := false
		for j, b := range m.Content {
			if b.Type != llm.BlockToolUse || len(b.Input) <= maxChars {
				continue
			}
			if !modified {
				newContent := make([]llm.ContentBlock, len(m.Content))
				copy(newContent, m.Content)
				out[i].Content = newContent
				modified = true
			}
			b2 := b
			// Re-encode as a JSON OBJECT: the block is marshaled into the
			// summarization request, and providers (Anthropic) require
			// tool_use.input to be an object, not a string.
			truncated := fmt.Sprintf("%s…[truncated — %d bytes total]", b.Input[:maxChars], len(b.Input))
			obj, qerr := json.Marshal(map[string]string{"_truncated_input": truncated})
			if qerr != nil {
				obj = []byte(`{"_truncated_input":"[tool input truncated]"}`)
			}
			b2.Input = obj
			out[i].Content[j] = b2
		}
	}
	return out
}

// truncateToTokens drops the oldest messages until the BPE-estimated token
// count is under maxTokens. This is the final guard preventing the
// summarization request itself from overflowing the model's context window
// (byte-based caps underestimate code-heavy content).
func truncateToTokens(msgs []llm.Message, maxTokens int) []llm.Message {
	total := EstimateTokens(msgs, "")
	dropped := 0
	for len(msgs) > 1 && total > maxTokens {
		total -= EstimateTokens(msgs[:1], "")
		msgs = msgs[1:]
		dropped++
		for len(msgs) > 0 && startsWithToolResult(msgs[0]) {
			total -= EstimateTokens(msgs[:1], "")
			msgs = msgs[1:]
			dropped++
		}
	}
	if dropped > 0 && len(msgs) > 0 {
		note := fmt.Sprintf("[%d oldest messages were dropped to fit the summarization window]", dropped)
		msgs = append([]llm.Message{llm.UserText(note)}, msgs...)
	}
	return msgs
}

// truncateToBytes drops the oldest messages until the total content size is
// under maxBytes. This prevents sending enormous histories to the summarizer.
func truncateToBytes(msgs []llm.Message, maxBytes int) []llm.Message {
	total := 0
	for _, m := range msgs {
		for _, b := range m.Content {
			total += len(b.Text) + len(b.Content) + len(b.Input) + len(b.Thinking)
		}
	}
	dropped := 0
	for len(msgs) > 1 && total > maxBytes {
		// Measure the first message's size.
		msgSize := 0
		for _, b := range msgs[0].Content {
			msgSize += len(b.Text) + len(b.Content) + len(b.Input) + len(b.Thinking)
		}
		total -= msgSize
		msgs = msgs[1:]
		dropped++
		// Skip past orphaned tool_result messages.
		for len(msgs) > 0 && startsWithToolResult(msgs[0]) {
			msgSize = 0
			for _, b := range msgs[0].Content {
				msgSize += len(b.Text) + len(b.Content) + len(b.Input) + len(b.Thinking)
			}
			total -= msgSize
			msgs = msgs[1:]
			dropped++
		}
	}
	if dropped > 0 && len(msgs) > 0 {
		note := fmt.Sprintf("[%d oldest messages were dropped to fit the summarization window]", dropped)
		msgs = append([]llm.Message{llm.UserText(note)}, msgs...)
	}
	return msgs
}
