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
	// OnToolStart fires before a tool executes.
	OnToolStart func(name string, input json.RawMessage)
	// OnToolEnd fires after a tool executes, with the result block sent back to
	// the model.
	OnToolEnd func(name string, result llm.ContentBlock)
	// OnRetry fires before a retryable provider call is retried. It is invoked by
	// the retry decorator provider when wired via Agent.
	OnRetry func(attempt int, err error, delay time.Duration)
	// OnCompaction fires after history compaction, reporting the estimated token
	// counts before and after.
	OnCompaction func(before, after int)
	// OnUsage fires after each provider response with that turn's usage and the
	// running total.
	OnUsage func(turn int, turnUsage, total llm.Usage)
}
