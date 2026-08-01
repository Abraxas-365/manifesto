package harness

import (
	"encoding/json"
	"time"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm"
)

// Hooks holds optional observability callbacks fired during Run. Every field is
// a nilable func: a nil field is a no-op, so the whole struct is zero-cost when
// unused. Set only the callbacks you care about.
type Hooks struct {
	// OnTurnStart fires at the top of each loop turn (0-based).
	OnTurnStart func(turn int)
	// OnAssistantText fires when the model returns a final text answer.
	OnAssistantText func(text string)
	// OnTextDelta fires for each incremental chunk of assistant text. Setting it
	// switches the agent to the provider's streaming API (ChatStream).
	OnTextDelta func(delta string)
	// OnThinkingDelta fires for each incremental chunk of thinking/reasoning text.
	OnThinkingDelta func(delta string)
	// OnToolUseStreaming fires when a tool_use block starts streaming (name known,
	// input not yet complete).
	OnToolUseStreaming func(id, name string)
	// OnInputJSONDelta fires for each input_json_delta chunk with its byte length.
	OnInputJSONDelta func(deltaLen int)
	// OnToolStart fires before a tool executes. id is the tool-use block ID,
	// letting observers correlate the matching OnToolEnd (result.ToolUseID)
	// and any nested events tagged with tool.UseID. Return nil to allow
	// execution, or a ToolIntercept to block/modify it.
	OnToolStart func(id, name string, input json.RawMessage) *ToolIntercept
	// OnToolEnd fires after a tool executes, with the result block sent back to
	// the model. Returning a non-nil *ContentBlock replaces the result that is
	// added to history (pi-style tool_result modification).
	OnToolEnd func(name string, result llm.ContentBlock) *llm.ContentBlock
	// OnRetry fires before a retryable provider call is retried. It is invoked by
	// the retry decorator provider when wired via Agent.
	OnRetry func(attempt int, err error, delay time.Duration)
	// OnCompactionStart fires before history compaction begins. Observers can
	// use this to show a progress indicator.
	OnCompactionStart func()
	// OnCompaction fires after history compaction succeeds, reporting the
	// estimated token counts before and after.
	OnCompaction func(before, after int)
	// OnCompactionFailed fires when a compaction attempt fails (summarizer
	// error, empty summary, or a no-op that couldn't shrink the history). The
	// turn continues with the uncompacted history; observers should surface a
	// warning to the user.
	OnCompactionFailed func(err error)
	// OnUsage fires after each provider response with that turn's usage and the
	// running total.
	OnUsage func(turn int, turnUsage, total llm.Usage)
	// OnMicroCompact fires after MicroCompact or cache-expiry compact clears
	// old tool results. Observers should invalidate any dedup caches (e.g.
	// ReadCache) that reference the now-cleared content.
	OnMicroCompact func()
}

// ToolIntercept allows OnToolStart hooks to block or modify a tool call.
type ToolIntercept struct {
	// Cancel blocks the tool call and returns ErrorMessage as the result.
	Cancel bool
	// ErrorMessage is returned to the model when Cancel is true.
	ErrorMessage string
	// ModifiedInput replaces the tool's input. If nil, the original input is used.
	ModifiedInput json.RawMessage
}
