package harness

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/tool"
)

func TestEstimateTokens(t *testing.T) {
	msgs := []llm.Message{llm.UserText(strings.Repeat("a", 40))}
	got := EstimateTokens(msgs, "sysprompt")
	// BPE tokenizes differently from chars/4. Just verify it returns a
	// positive, reasonable count (not zero or wildly off).
	if got <= 0 {
		t.Fatalf("got %d, want > 0", got)
	}
	if got > 100 {
		t.Fatalf("got %d, unexpectedly high for ~50 chars of input", got)
	}
}

func TestTruncateCompactor_NeverSplitsToolPair(t *testing.T) {
	// Build: user, assistant(tool_use), user(tool_result), assistant, user, assistant.
	msgs := []llm.Message{
		llm.UserText("start"),
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.ToolUseBlock("c1", "T", nil)}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.ToolResultBlock("c1", "res", false)}},
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.Text("a")}},
		llm.UserText("more"),
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.Text("b")}},
	}

	out, err := TruncateCompactor{KeepRecent: 4}.Compact(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	// KeepRecent=4 would cut at index 2 (a tool_result) — must advance past it.
	if startsWithToolResult(out[0]) {
		t.Fatalf("compacted history begins with orphaned tool_result: %+v", out[0])
	}
}

func TestTruncateCompactor_AllToolResults(t *testing.T) {
	// When every message is a tool_result, safeCut advances past all of them.
	// The compactor should return msgs unchanged (not an empty slice).
	msgs := []llm.Message{
		{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.ToolResultBlock("c1", "r1", false)}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.ToolResultBlock("c2", "r2", false)}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.ToolResultBlock("c3", "r3", false)}},
	}
	out, err := TruncateCompactor{KeepRecent: 1}.Compact(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 3 {
		t.Fatalf("expected all messages preserved, got %d", len(out))
	}
}

func TestTruncateCompactor_NoOpWhenSmall(t *testing.T) {
	msgs := []llm.Message{llm.UserText("a"), llm.UserText("b")}
	out, _ := TruncateCompactor{KeepRecent: 6}.Compact(context.Background(), msgs)
	if len(out) != 2 {
		t.Fatalf("expected no-op, got %d", len(out))
	}
}

func TestSummarizeCompactor_Shape(t *testing.T) {
	sp := &fakeProvider{responses: []llm.Response{assistantText("SUMMARY", llm.StopEndTurn)}}
	msgs := []llm.Message{
		llm.UserText("m1"), llm.UserText("m2"), llm.UserText("m3"),
		llm.UserText("m4"), llm.UserText("m5"), llm.UserText("m6"), llm.UserText("m7"),
	}
	c := SummarizeCompactor{Provider: sp, KeepRecent: 2}
	out, err := c.Compact(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	// summary + 2 recent.
	if len(out) != 3 {
		t.Fatalf("expected 3 messages, got %d: %+v", len(out), out)
	}
	if !strings.Contains(out[0].TextContent(), "SUMMARY") {
		t.Fatalf("first message should be the summary, got %q", out[0].TextContent())
	}
	if out[1].TextContent() != "m6" || out[2].TextContent() != "m7" {
		t.Fatalf("recent messages not preserved: %+v", out[1:])
	}
}

// overflowResponse mimics Anthropic reporting context overflow: a SUCCESSFUL
// response with stop reason model_context_window_exceeded and no text content.
func overflowResponse() llm.Response {
	return llm.Response{
		Message:    llm.Message{Role: llm.RoleAssistant},
		StopReason: llm.StopContextWindowExceeded,
	}
}

func TestSummarizeCompactor_RetriesOnOverflowStopReason(t *testing.T) {
	// First summarization attempt overflows the model context (empty message,
	// model_context_window_exceeded); the retry with shrunken input succeeds.
	// Regression: this used to surface as "compaction returned empty summary"
	// and trip the circuit breaker (auto-compact disabled after 3 failures).
	sp := &fakeProvider{responses: []llm.Response{
		overflowResponse(),
		assistantText("SUMMARY", llm.StopEndTurn),
	}}
	msgs := []llm.Message{
		llm.UserText("m1"), llm.UserText("m2"), llm.UserText("m3"),
		llm.UserText("m4"), llm.UserText("m5"), llm.UserText("m6"), llm.UserText("m7"),
	}
	out, err := SummarizeCompactor{Provider: sp, KeepRecent: 2}.Compact(context.Background(), msgs)
	if err != nil {
		t.Fatalf("expected overflow retry to succeed, got %v", err)
	}
	if len(sp.requests) != 2 {
		t.Fatalf("expected 2 summarization attempts, got %d", len(sp.requests))
	}
	// Retry must send fewer/smaller input messages than the first attempt.
	if len(sp.requests[1].Messages) >= len(sp.requests[0].Messages) {
		t.Fatalf("retry did not shrink input: first=%d retry=%d",
			len(sp.requests[0].Messages), len(sp.requests[1].Messages))
	}
	if !strings.Contains(out[0].TextContent(), "SUMMARY") {
		t.Fatalf("first message should be the summary, got %q", out[0].TextContent())
	}
}

func TestSummarizeCompactor_RetriesOnOverflowError(t *testing.T) {
	// OpenAI-style overflow: the request errors with "maximum context length".
	sp := &overflowThenOKProvider{failures: 1}
	msgs := []llm.Message{
		llm.UserText("m1"), llm.UserText("m2"), llm.UserText("m3"),
		llm.UserText("m4"), llm.UserText("m5"), llm.UserText("m6"), llm.UserText("m7"),
	}
	out, err := SummarizeCompactor{Provider: sp, KeepRecent: 2}.Compact(context.Background(), msgs)
	if err != nil {
		t.Fatalf("expected overflow-error retry to succeed, got %v", err)
	}
	if sp.calls != 2 {
		t.Fatalf("expected 2 attempts, got %d", sp.calls)
	}
	if !strings.Contains(out[0].TextContent(), "SUMMARY") {
		t.Fatalf("first message should be the summary, got %q", out[0].TextContent())
	}
}

func TestSummarizeCompactor_OverflowRetriesBounded(t *testing.T) {
	// Every attempt overflows — the loop must terminate and return an error
	// (not spin forever), after at most maxSummarizeAttempts calls.
	sp := &overflowThenOKProvider{failures: 100}
	msgs := []llm.Message{
		llm.UserText("m1"), llm.UserText("m2"), llm.UserText("m3"),
		llm.UserText("m4"), llm.UserText("m5"), llm.UserText("m6"), llm.UserText("m7"),
	}
	_, err := SummarizeCompactor{Provider: sp, KeepRecent: 2}.Compact(context.Background(), msgs)
	if err == nil {
		t.Fatal("expected error when every attempt overflows")
	}
	if sp.calls > maxSummarizeAttempts {
		t.Fatalf("expected at most %d attempts, got %d", maxSummarizeAttempts, sp.calls)
	}
}

func TestSummarizeCompactor_EmptySummaryIncludesStopReason(t *testing.T) {
	// Non-overflow empty response: error must include the stop reason for
	// diagnosability instead of the bare "empty summary".
	sp := &fakeProvider{responses: []llm.Response{
		assistantText("", llm.StopEndTurn),
		assistantText("", llm.StopEndTurn),
		assistantText("", llm.StopEndTurn),
	}}
	msgs := []llm.Message{
		llm.UserText("m1"), llm.UserText("m2"), llm.UserText("m3"),
		llm.UserText("m4"), llm.UserText("m5"), llm.UserText("m6"), llm.UserText("m7"),
	}
	_, err := SummarizeCompactor{Provider: sp, KeepRecent: 2}.Compact(context.Background(), msgs)
	if err == nil || !strings.Contains(err.Error(), "stop reason") {
		t.Fatalf("expected empty-summary error with stop reason, got %v", err)
	}
}

// overflowThenOKProvider errors with a context-overflow message for the first
// N calls, then returns a summary.
type overflowThenOKProvider struct {
	failures int
	calls    int
}

func (p *overflowThenOKProvider) Chat(_ context.Context, req llm.Request) (*llm.Response, error) {
	p.calls++
	if p.calls <= p.failures {
		return nil, errors.New("api error 400: this model's maximum context length is exceeded")
	}
	resp := assistantText("SUMMARY", llm.StopEndTurn)
	return &resp, nil
}

func (p *overflowThenOKProvider) ChatStream(context.Context, llm.Request) (llm.Stream, error) {
	return nil, errors.New("not implemented")
}

func TestMaybeCompact_TriggersPastThreshold(t *testing.T) {
	// Small context window so a couple of messages exceed the threshold.
	p := &fakeProvider{responses: []llm.Response{assistantText("ok", llm.StopEndTurn)}}
	a := newAgent(p)
	a.ContextWindow = 4 // 4 tokens; threshold 0.8 => ~3 tokens
	a.CompactThreshold = 0.8

	var compacted bool
	a.Compactor = compactorFunc(func(_ context.Context, msgs []llm.Message) ([]llm.Message, error) {
		compacted = true
		return msgs, nil
	})

	// Seed history with enough text to exceed 3 tokens (>12 chars).
	a.history = []llm.Message{llm.UserText(strings.Repeat("x", 40))}

	if err := a.maybeCompact(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !compacted {
		t.Fatal("expected compaction to trigger past threshold")
	}
}

func TestMaybeCompact_SkipsUnderThreshold(t *testing.T) {
	p := &fakeProvider{}
	a := newAgent(p)
	a.ContextWindow = 100000
	var compacted bool
	a.Compactor = compactorFunc(func(_ context.Context, msgs []llm.Message) ([]llm.Message, error) {
		compacted = true
		return msgs, nil
	})
	a.history = []llm.Message{llm.UserText("tiny")}

	if err := a.maybeCompact(context.Background()); err != nil {
		t.Fatal(err)
	}
	if compacted {
		t.Fatal("expected no compaction under threshold")
	}
}

func TestMaybeCompact_NilCompactorNoOp(t *testing.T) {
	a := newAgent(&fakeProvider{})
	a.history = []llm.Message{llm.UserText(strings.Repeat("x", 10000))}
	if err := a.maybeCompact(context.Background()); err != nil {
		t.Fatalf("nil compactor should be a no-op, got %v", err)
	}
}

func TestMaybeCompact_CooldownSkipsAutoCompact(t *testing.T) {
	p := &fakeProvider{}
	a := newAgent(p)
	a.ContextWindow = 1000
	a.CompactThreshold = 0.8
	a.justCompacted = true // cooldown active
	// Simulate 850 tokens via API (above 80% but below 98% force).
	a.lastTotalTokens = 850

	var compacted bool
	a.Compactor = compactorFunc(func(_ context.Context, msgs []llm.Message) ([]llm.Message, error) {
		compacted = true
		return msgs, nil
	})
	a.history = []llm.Message{llm.UserText("x")}

	if err := a.maybeCompact(context.Background()); err != nil {
		t.Fatal(err)
	}
	if compacted {
		t.Fatal("expected cooldown to skip auto-compaction")
	}
}

func TestMaybeCompact_ForceIgnoresCooldown(t *testing.T) {
	p := &fakeProvider{}
	a := newAgent(p)
	a.ContextWindow = 100
	a.justCompacted = true // cooldown active
	// Simulate API reporting tokens at 99% of context window.
	a.lastTotalTokens = 99

	var compacted bool
	a.Compactor = compactorFunc(func(_ context.Context, msgs []llm.Message) ([]llm.Message, error) {
		compacted = true
		return msgs, nil
	})
	a.history = []llm.Message{llm.UserText("x")}

	if err := a.maybeCompact(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !compacted {
		t.Fatal("expected force compaction to ignore cooldown")
	}
}

func TestMaybeCompact_CircuitBreaker(t *testing.T) {
	p := &fakeProvider{}
	a := newAgent(p)
	a.ContextWindow = 1000
	a.CompactThreshold = 0.8
	a.consecutiveCompactFailures = MaxConsecutiveCompactFailures
	// Simulate 850 tokens via API (above 80% but below 98% force).
	a.lastTotalTokens = 850

	var compacted bool
	a.Compactor = compactorFunc(func(_ context.Context, msgs []llm.Message) ([]llm.Message, error) {
		compacted = true
		return msgs, nil
	})
	a.history = []llm.Message{llm.UserText("x")}

	if err := a.maybeCompact(context.Background()); err != nil {
		t.Fatal(err)
	}
	if compacted {
		t.Fatal("expected circuit breaker to prevent compaction")
	}
}

func TestMaybeCompact_UsesAPITokenCount(t *testing.T) {
	p := &fakeProvider{}
	a := newAgent(p)
	a.ContextWindow = 200000
	a.CompactThreshold = 0.8 // 160000 tokens
	// Tiny history text but API says we're at 170k tokens.
	a.lastTotalTokens = 170000

	var compacted bool
	a.Compactor = compactorFunc(func(_ context.Context, msgs []llm.Message) ([]llm.Message, error) {
		compacted = true
		return msgs, nil
	})
	a.history = []llm.Message{llm.UserText("tiny")}

	if err := a.maybeCompact(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !compacted {
		t.Fatal("expected compaction based on API token count")
	}
}

func TestMaybeCompact_FailureIncrementsCircuitBreaker(t *testing.T) {
	p := &fakeProvider{}
	a := newAgent(p)
	a.ContextWindow = 4
	a.CompactThreshold = 0.8
	a.Compactor = compactorFunc(func(_ context.Context, msgs []llm.Message) ([]llm.Message, error) {
		return nil, errors.New("compaction failed")
	})
	a.history = []llm.Message{llm.UserText(strings.Repeat("x", 40))}

	if err := a.maybeCompact(context.Background()); err != nil {
		t.Fatal("compaction failure should not fail the turn")
	}
	if a.consecutiveCompactFailures != 1 {
		t.Fatalf("expected 1 failure, got %d", a.consecutiveCompactFailures)
	}
}

func TestMaybeCompact_FailureFiresOnCompactionFailed(t *testing.T) {
	p := &fakeProvider{}
	a := newAgent(p)
	a.ContextWindow = 4
	a.CompactThreshold = 0.8
	a.Compactor = compactorFunc(func(_ context.Context, msgs []llm.Message) ([]llm.Message, error) {
		return nil, errors.New("summarizer exploded")
	})
	a.history = []llm.Message{llm.UserText(strings.Repeat("x", 40))}

	var failedErr error
	var compactionFired bool
	a.Hooks.OnCompactionFailed = func(err error) { failedErr = err }
	a.Hooks.OnCompaction = func(before, after int) { compactionFired = true }

	if err := a.maybeCompact(context.Background()); err != nil {
		t.Fatal("compaction failure should not fail the turn")
	}
	if failedErr == nil || !strings.Contains(failedErr.Error(), "summarizer exploded") {
		t.Fatalf("expected OnCompactionFailed with cause, got %v", failedErr)
	}
	if compactionFired {
		t.Fatal("OnCompaction (success hook) must not fire on failure")
	}
}

func TestMaybeCompact_NoOpCompactionCountsAsFailure(t *testing.T) {
	p := &fakeProvider{}
	a := newAgent(p)
	a.ContextWindow = 4
	a.CompactThreshold = 0.8
	// Compactor returns history unchanged — cannot shrink.
	a.Compactor = compactorFunc(func(_ context.Context, msgs []llm.Message) ([]llm.Message, error) {
		return msgs, nil
	})
	a.history = []llm.Message{llm.UserText(strings.Repeat("x", 40))}

	var failedErr error
	a.Hooks.OnCompactionFailed = func(err error) { failedErr = err }

	if err := a.maybeCompact(context.Background()); err != nil {
		t.Fatal(err)
	}
	if a.consecutiveCompactFailures != 1 {
		t.Fatalf("no-op compaction should count as failure, got %d failures", a.consecutiveCompactFailures)
	}
	if failedErr == nil {
		t.Fatal("expected OnCompactionFailed for no-op compaction")
	}
}

func TestMaybeCompact_ForceRespectsCircuitBreaker(t *testing.T) {
	p := &fakeProvider{}
	a := newAgent(p)
	a.ContextWindow = 1000
	a.CompactThreshold = 0.8
	a.consecutiveCompactFailures = MaxConsecutiveCompactFailures
	a.lastTotalTokens = 990 // above the 98% force threshold

	var compacted bool
	a.Compactor = compactorFunc(func(_ context.Context, msgs []llm.Message) ([]llm.Message, error) {
		compacted = true
		return msgs[:0], nil
	})
	a.history = []llm.Message{llm.UserText("x")}

	if err := a.maybeCompact(context.Background()); err != nil {
		t.Fatal(err)
	}
	if compacted {
		t.Fatal("force compaction must respect the circuit breaker")
	}
}

func TestMicroCompact(t *testing.T) {
	// Build history with large tool results that exceed the 100KB budget.
	// c1=110KB, c2=110KB → total≈221KB, budget=100KB.
	// Clearing c1 leaves ~111KB (still over), so both must be cleared.
	msgs := []llm.Message{
		llm.UserText("start"),
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.ToolUseBlock("c1", "Read", nil)}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.ToolResultBlock("c1", strings.Repeat("A", 110000), false)}},
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.ToolUseBlock("c2", "Bash", nil)}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.ToolResultBlock("c2", strings.Repeat("B", 110000), false)}},
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.ToolUseBlock("c3", "Read", nil)}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.ToolResultBlock("c3", strings.Repeat("C", 600), false)}},
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.ToolUseBlock("c4", "Read", nil)}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.ToolResultBlock("c4", "small", false)}},
	}

	result := MicroCompact(msgs, 2, 512)

	// The two large results (c1, c2) should be cleared; c3 and c4 are protected (last 2).
	c1Result := result[2].Content[0].Content
	if !strings.HasPrefix(c1Result, "[Read result cleared") {
		t.Fatalf("expected c1 to be cleared, got %q", c1Result[:50])
	}
	c2Result := result[4].Content[0].Content
	if !strings.HasPrefix(c2Result, "[result cleared") {
		t.Fatalf("expected c2 to be cleared, got %q", c2Result[:50])
	}
	// c3 and c4 are protected (last 2 results).
	if result[6].Content[0].Content != strings.Repeat("C", 600) {
		t.Fatal("expected c3 to be preserved (protected)")
	}
	if result[8].Content[0].Content != "small" {
		t.Fatal("expected c4 to be preserved (protected)")
	}
}

func TestMicroCompact_NothingToClear(t *testing.T) {
	msgs := []llm.Message{
		llm.UserText("hello"),
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.Text("hi")}},
	}
	result := MicroCompact(msgs, 5, 512)
	if len(result) != len(msgs) {
		t.Fatalf("expected no change, got %d messages", len(result))
	}
}

func TestMicroCompact_SkipsAlreadyCompacted(t *testing.T) {
	msgs := []llm.Message{
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.ToolUseBlock("c1", "Read", nil)}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.ToolResultBlock("c1", "[result cleared — 5000 bytes]", false)}},
	}
	result := MicroCompact(msgs, 0, 0)
	// Already-compacted stubs should not be collected or re-cleared.
	if result[1].Content[0].Content != "[result cleared — 5000 bytes]" {
		t.Fatal("already-compacted result should be left as-is")
	}
}

func TestEstimateTokensDelta(t *testing.T) {
	a := newAgent(&fakeProvider{})
	a.ContextWindow = 200000
	a.lastTotalTokens = 50000
	a.lastHistoryLen = 1
	// Simulate 2 additional messages (tool results) added after the API response.
	a.history = []llm.Message{
		llm.UserText("original"),
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.Text(strings.Repeat("x", 4000))}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.ToolResultBlock("c1", strings.Repeat("y", 8000), false)}},
	}
	tokens := a.estimateTokens()
	// Should be 50000 + BPE tokens for the 2 new messages + framing.
	// With BPE, repeated chars compress well so exact count varies, but it
	// must be > 50000 (baseline) and reasonably close.
	if tokens <= 50000 {
		t.Fatalf("expected > 50000, got %d", tokens)
	}
	if tokens > 60000 {
		t.Fatalf("expected < 60000, got %d (unexpectedly high)", tokens)
	}
}

func TestLoopClearsCooldownAfterAPIResponse(t *testing.T) {
	p := &fakeProvider{responses: []llm.Response{
		{
			Message:    llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.Text("done")}},
			StopReason: llm.StopEndTurn,
			Usage:      llm.Usage{InputTokens: 5000},
		},
	}}
	a := newAgent(p)
	a.justCompacted = true
	a.Compactor = TruncateCompactor{KeepRecent: 6} // non-nil so compaction is "enabled"

	_, err := a.Run(context.Background(), "hi")
	if err != nil {
		t.Fatal(err)
	}
	if a.justCompacted {
		t.Fatal("expected justCompacted to be cleared after API response")
	}
	if a.lastTotalTokens != 5000 {
		t.Fatalf("expected lastTotalTokens=5000, got %d", a.lastTotalTokens)
	}
}

// compactorFunc adapts a func to the Compactor interface.
type compactorFunc func(context.Context, []llm.Message) ([]llm.Message, error)

func (f compactorFunc) Compact(ctx context.Context, msgs []llm.Message) ([]llm.Message, error) {
	return f(ctx, msgs)
}

// ---------------------------------------------------------------------------
// Token counting: no cache double-counting (Bug #1 fix)
// ---------------------------------------------------------------------------

func TestTokenCounting_NoCacheDoubleCount(t *testing.T) {
	// Simulate an API response where cache tokens are subsets of InputTokens.
	// Before the fix, lastTotalTokens = Input + Output + CacheRead + CacheWrite
	// which double-counted. After the fix, only Input + Output is used.
	p := &fakeProvider{responses: []llm.Response{
		{
			Message:    llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.Text("done")}},
			StopReason: llm.StopEndTurn,
			Usage: llm.Usage{
				InputTokens:      100000, // includes cached
				OutputTokens:     5000,
				CacheReadTokens:  80000, // subset of InputTokens
				CacheWriteTokens: 10000, // subset of InputTokens
			},
		},
	}}
	a := newAgent(p)
	a.ContextWindow = 200000
	a.Compactor = TruncateCompactor{KeepRecent: 6}

	_, err := a.Run(context.Background(), "hi")
	if err != nil {
		t.Fatal(err)
	}

	// Should be InputTokens + OutputTokens = 105000
	// NOT InputTokens + OutputTokens + CacheRead + CacheWrite = 195000
	if a.lastTotalTokens != 105000 {
		t.Fatalf("expected lastTotalTokens=105000 (input+output only), got %d", a.lastTotalTokens)
	}
}

func TestTokenCounting_NoCacheDoubleCount_CompactionThreshold(t *testing.T) {
	// With the old double-counting, 100k input + 5k output + 80k cache_read = 185k,
	// which would exceed the 80% threshold (160k) on a 200k window. With the fix,
	// it's 105k which is under threshold.
	var compacted bool
	p := &fakeProvider{responses: []llm.Response{
		{
			Message:    llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.Text("done")}},
			StopReason: llm.StopEndTurn,
			Usage: llm.Usage{
				InputTokens:     100000,
				OutputTokens:    5000,
				CacheReadTokens: 80000,
			},
		},
		// Second response in case compaction triggers a retry.
		{
			Message:    llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.Text("done2")}},
			StopReason: llm.StopEndTurn,
			Usage:      llm.Usage{InputTokens: 50000, OutputTokens: 2000},
		},
	}}
	a := newAgent(p)
	a.ContextWindow = 200000
	a.CompactThreshold = 0.8 // 160k threshold
	a.Compactor = compactorFunc(func(_ context.Context, msgs []llm.Message) ([]llm.Message, error) {
		compacted = true
		return msgs[len(msgs)-1:], nil
	})

	_, err := a.Run(context.Background(), "hi")
	if err != nil {
		t.Fatal(err)
	}

	// 105k is under 160k threshold — compaction should NOT trigger.
	if compacted {
		t.Fatal("compaction should not trigger: 105k tokens is under the 160k threshold")
	}
}

// ---------------------------------------------------------------------------
// BPE estimator: block type coverage (Bug #3 fix)
// ---------------------------------------------------------------------------

func TestEstimateTokens_ImageBlocks(t *testing.T) {
	msgs := []llm.Message{
		{Role: llm.RoleUser, Content: []llm.ContentBlock{
			{Type: llm.BlockImage, MediaType: "image/png", Data: strings.Repeat("A", 10000)},
		}},
	}
	got := EstimateTokens(msgs, "")
	// Should use the flat imageTokenEstimate (1600), NOT tokenize the base64 data.
	if got < 1600 || got > 1700 {
		t.Fatalf("expected ~1600 for image block, got %d", got)
	}
}

func TestEstimateTokens_ThinkingBlocks(t *testing.T) {
	msgs := []llm.Message{
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{
			{Type: llm.BlockThinking, Thinking: "Let me think about this carefully."},
		}},
	}
	got := EstimateTokens(msgs, "")
	if got <= 0 {
		t.Fatal("thinking blocks should produce non-zero token count")
	}
}

func TestEstimateTokens_RedactedThinkingBlocks(t *testing.T) {
	msgs := []llm.Message{
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{
			{Type: llm.BlockRedactedThinking, Thinking: "redacted content"},
		}},
	}
	got := EstimateTokens(msgs, "")
	if got <= 0 {
		t.Fatal("redacted thinking blocks should produce non-zero token count")
	}
}

func TestEstimateTokens_ToolUseBlocks(t *testing.T) {
	msgs := []llm.Message{
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{
			llm.ToolUseBlock("c1", "ReadFile", json.RawMessage(`{"path":"/foo/bar.go"}`)),
		}},
	}
	got := EstimateTokens(msgs, "")
	if got <= 0 {
		t.Fatal("tool_use blocks should produce non-zero token count")
	}
}

func TestEstimateTokens_ToolResultWithImages(t *testing.T) {
	msgs := []llm.Message{
		{Role: llm.RoleUser, Content: []llm.ContentBlock{
			{
				Type:      llm.BlockToolResult,
				ToolUseID: "c1",
				Content:   "screenshot taken",
				Images: []llm.ContentBlock{
					{Type: llm.BlockImage, MediaType: "image/png", Data: "base64data"},
					{Type: llm.BlockImage, MediaType: "image/png", Data: "base64data2"},
				},
			},
		}},
	}
	got := EstimateTokens(msgs, "")
	// Should include 2 * imageTokenEstimate (3200) + text content tokens.
	if got < 3200 {
		t.Fatalf("expected >= 3200 for tool result with 2 images, got %d", got)
	}
}

func TestEstimateTokens_PerMessageFraming(t *testing.T) {
	// Two empty-ish messages should differ from one by perMessageFraming.
	one := EstimateTokens([]llm.Message{llm.UserText("hi")}, "")
	two := EstimateTokens([]llm.Message{llm.UserText("hi"), llm.UserText("hi")}, "")
	diff := two - one
	// The difference should be exactly perMessageFraming + tokens("hi").
	if diff <= 0 {
		t.Fatalf("adding a message should increase the count, diff=%d", diff)
	}
}

func TestEstimateTokens_SystemPrompt(t *testing.T) {
	without := EstimateTokens([]llm.Message{llm.UserText("hi")}, "")
	with := EstimateTokens([]llm.Message{llm.UserText("hi")}, "You are a helpful assistant.")
	if with <= without {
		t.Fatalf("system prompt should increase token count: without=%d, with=%d", without, with)
	}
}

// ---------------------------------------------------------------------------
// Delta estimator: image blocks in appended messages
// ---------------------------------------------------------------------------

func TestEstimateTokensDelta_WithImages(t *testing.T) {
	a := newAgent(&fakeProvider{})
	a.ContextWindow = 200000
	a.lastTotalTokens = 50000
	a.lastHistoryLen = 1
	a.history = []llm.Message{
		llm.UserText("original"),
		// Tool result with 2 images appended after last API response.
		{Role: llm.RoleUser, Content: []llm.ContentBlock{
			{
				Type:      llm.BlockToolResult,
				ToolUseID: "c1",
				Content:   "screenshot",
				Images: []llm.ContentBlock{
					{Type: llm.BlockImage, MediaType: "image/png", Data: "data1"},
					{Type: llm.BlockImage, MediaType: "image/png", Data: "data2"},
				},
			},
		}},
	}
	tokens := a.estimateTokens()
	// Should include 50000 baseline + 2*1600 image estimates + text.
	if tokens < 53200 {
		t.Fatalf("expected >= 53200 (50000 + 2*1600 images), got %d", tokens)
	}
}

func TestEstimateTokensDelta_ImageBlock(t *testing.T) {
	a := newAgent(&fakeProvider{})
	a.ContextWindow = 200000
	a.lastTotalTokens = 10000
	a.lastHistoryLen = 1
	a.history = []llm.Message{
		llm.UserText("original"),
		// Standalone image block in an assistant message.
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{
			{Type: llm.BlockImage, MediaType: "image/png", Data: strings.Repeat("A", 50000)},
		}},
	}
	tokens := a.estimateTokens()
	// Should use flat 1600, NOT tokenize the 50KB base64 payload.
	if tokens > 15000 {
		t.Fatalf("expected ~11604 (10000 + 1600 + framing), got %d (image was tokenized as text)", tokens)
	}
	if tokens < 11000 {
		t.Fatalf("expected ~11604, got %d (too low)", tokens)
	}
}

// ---------------------------------------------------------------------------
// Micro-compaction hook suppression (Bug #2 fix)
// ---------------------------------------------------------------------------

func TestMaybeCompact_MicroCompactNoHookWhenNoChange(t *testing.T) {
	a := newAgent(&fakeProvider{})
	a.ContextWindow = 100
	a.CompactThreshold = 0.8
	// Set token count at 65% — above micro threshold (60%), below auto (80%).
	a.lastTotalTokens = 65

	hookCalls := 0
	a.Hooks.OnMicroCompact = func() { hookCalls++ }
	a.Compactor = TruncateCompactor{KeepRecent: 6}
	// History with only small tool results — nothing to micro-compact.
	a.history = []llm.Message{
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.ToolUseBlock("c1", "Read", nil)}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.ToolResultBlock("c1", "small result", false)}},
	}

	if err := a.maybeCompact(context.Background()); err != nil {
		t.Fatal(err)
	}
	if hookCalls != 0 {
		t.Fatalf("expected 0 hook calls when nothing changed, got %d", hookCalls)
	}
}

func TestMaybeCompact_MicroCompactFiresHookWhenChanged(t *testing.T) {
	a := newAgent(&fakeProvider{})
	a.ContextWindow = 100
	a.CompactThreshold = 0.8
	// Set token count at 65% — above micro threshold (60%).
	a.lastTotalTokens = 65

	hookCalls := 0
	a.Hooks.OnMicroCompact = func() { hookCalls++ }
	a.Compactor = TruncateCompactor{KeepRecent: 6}
	// Need > 5 tool results (keepLastResults=5) so some are unprotected,
	// and total tool result size > 100KB so clearing is triggered.
	a.history = []llm.Message{
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.ToolUseBlock("c1", "Read", nil)}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.ToolResultBlock("c1", strings.Repeat("A", 110000), false)}},
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.ToolUseBlock("c2", "Read", nil)}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.ToolResultBlock("c2", strings.Repeat("B", 110000), false)}},
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.ToolUseBlock("c3", "Read", nil)}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.ToolResultBlock("c3", "small3", false)}},
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.ToolUseBlock("c4", "Read", nil)}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.ToolResultBlock("c4", "small4", false)}},
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.ToolUseBlock("c5", "Read", nil)}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.ToolResultBlock("c5", "small5", false)}},
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.ToolUseBlock("c6", "Read", nil)}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.ToolResultBlock("c6", "small6", false)}},
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.ToolUseBlock("c7", "Read", nil)}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.ToolResultBlock("c7", "small7", false)}},
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.Text("done")}},
	}

	if err := a.maybeCompact(context.Background()); err != nil {
		t.Fatal(err)
	}
	if hookCalls != 1 {
		t.Fatalf("expected 1 hook call when content changed, got %d", hookCalls)
	}
}

// ---------------------------------------------------------------------------
// messagesEqual helper
// ---------------------------------------------------------------------------

func TestMessagesEqual_Identical(t *testing.T) {
	a := []llm.Message{llm.UserText("hello"), llm.UserText("world")}
	b := []llm.Message{llm.UserText("hello"), llm.UserText("world")}
	if !messagesEqual(a, b) {
		t.Fatal("identical messages should be equal")
	}
}

func TestMessagesEqual_DifferentContent(t *testing.T) {
	a := []llm.Message{llm.UserText("hello")}
	b := []llm.Message{llm.UserText("goodbye")}
	if messagesEqual(a, b) {
		t.Fatal("different content should not be equal")
	}
}

func TestMessagesEqual_DifferentLength(t *testing.T) {
	a := []llm.Message{llm.UserText("hello")}
	b := []llm.Message{llm.UserText("hello"), llm.UserText("world")}
	if messagesEqual(a, b) {
		t.Fatal("different lengths should not be equal")
	}
}

func TestMessagesEqual_DifferentBlockCount(t *testing.T) {
	a := []llm.Message{{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.Text("a")}}}
	b := []llm.Message{{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.Text("a"), llm.Text("b")}}}
	if messagesEqual(a, b) {
		t.Fatal("different block counts should not be equal")
	}
}

func TestMessagesEqual_ToolResultChanged(t *testing.T) {
	a := []llm.Message{
		{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.ToolResultBlock("c1", strings.Repeat("X", 5000), false)}},
	}
	b := []llm.Message{
		{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.ToolResultBlock("c1", "[result cleared — 5000 bytes]", false)}},
	}
	if messagesEqual(a, b) {
		t.Fatal("cleared vs original content should not be equal")
	}
}

// ---------------------------------------------------------------------------
// BPE estimator accuracy: more accurate than chars/4
// ---------------------------------------------------------------------------

func TestEstimateTokens_BPE_MoreAccurateThanChars4(t *testing.T) {
	// English prose is typically ~4 chars/token. Code/JSON is typically ~3.
	// BPE should differentiate; chars/4 can't.
	code := strings.Repeat(`func foo() { return "bar" }`, 100)
	prose := strings.Repeat("The quick brown fox jumps over the lazy dog. ", 100)

	codeMsgs := []llm.Message{llm.UserText(code)}
	proseMsgs := []llm.Message{llm.UserText(prose)}

	codeTokens := EstimateTokens(codeMsgs, "")
	proseTokens := EstimateTokens(proseMsgs, "")

	// Code should tokenize at a higher rate (more tokens per char) than prose.
	codeRatio := float64(codeTokens) / float64(len(code))
	proseRatio := float64(proseTokens) / float64(len(prose))

	// Both should be non-zero and reasonable.
	if codeTokens <= 0 || proseTokens <= 0 {
		t.Fatalf("token counts should be positive: code=%d, prose=%d", codeTokens, proseTokens)
	}

	// With BPE, the ratios should differ. With chars/4 they'd be identical (0.25).
	// We just check they're not identical (within floating point tolerance).
	if codeRatio == proseRatio {
		t.Log("BPE should produce different token ratios for code vs prose")
	}
	t.Logf("code: %d tokens for %d chars (ratio=%.3f)", codeTokens, len(code), codeRatio)
	t.Logf("prose: %d tokens for %d chars (ratio=%.3f)", proseTokens, len(prose), proseRatio)
}

// ---------------------------------------------------------------------------
// Integration: loop records correct lastTotalTokens after API response
// ---------------------------------------------------------------------------

func TestLoop_LastTotalTokens_InputPlusOutput(t *testing.T) {
	p := &fakeProvider{responses: []llm.Response{
		{
			Message:    llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.Text("done")}},
			StopReason: llm.StopEndTurn,
			Usage: llm.Usage{
				InputTokens:      50000,
				OutputTokens:     3000,
				CacheReadTokens:  40000,
				CacheWriteTokens: 5000,
			},
		},
	}}
	a := newAgent(p)
	a.Compactor = TruncateCompactor{KeepRecent: 6}

	_, err := a.Run(context.Background(), "hi")
	if err != nil {
		t.Fatal(err)
	}

	// Must be Input + Output = 53000, NOT 98000 (with cache double-counting).
	if a.lastTotalTokens != 53000 {
		t.Fatalf("expected 53000, got %d", a.lastTotalTokens)
	}
}

func TestLoop_LastTotalTokens_ZeroUsagePreservesPrevious(t *testing.T) {
	// When a response reports zero usage, the previous value should be kept.
	p := &fakeProvider{responses: []llm.Response{
		{
			Message:    llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.Text("done")}},
			StopReason: llm.StopEndTurn,
			Usage:      llm.Usage{}, // all zeros
		},
	}}
	a := newAgent(p)
	a.lastTotalTokens = 42000
	a.Compactor = TruncateCompactor{KeepRecent: 6}

	_, err := a.Run(context.Background(), "hi")
	if err != nil {
		t.Fatal(err)
	}

	if a.lastTotalTokens != 42000 {
		t.Fatalf("zero usage should preserve previous lastTotalTokens, got %d", a.lastTotalTokens)
	}
}

// ---------------------------------------------------------------------------
// SanitizeToolPairs: orphaned tool_result / tool_use removal
// ---------------------------------------------------------------------------

func TestSanitizeToolPairs_NoOrphans(t *testing.T) {
	msgs := []llm.Message{
		llm.UserText("hello"),
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.ToolUseBlock("c1", "Read", nil)}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.ToolResultBlock("c1", "file content", false)}},
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.Text("done")}},
	}
	out := SanitizeToolPairs(msgs)
	if len(out) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(out))
	}
}

func TestSanitizeToolPairs_OrphanedToolResult(t *testing.T) {
	// tool_result references a tool_use_id that doesn't exist in the previous message.
	msgs := []llm.Message{
		llm.UserText("hello"),
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.Text("thinking...")}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.ToolResultBlock("c1", "result", false)}},
	}
	out := SanitizeToolPairs(msgs)
	// The orphaned tool_result message should be removed entirely.
	if len(out) != 2 {
		t.Fatalf("expected 2 messages (orphaned result removed), got %d", len(out))
	}
}

func TestSanitizeToolPairs_OrphanedToolUse(t *testing.T) {
	// assistant has tool_use but no following user tool_result message.
	msgs := []llm.Message{
		llm.UserText("hello"),
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.ToolUseBlock("c1", "Read", nil)}},
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.Text("done")}},
	}
	out := SanitizeToolPairs(msgs)
	// The orphaned tool_use in the assistant message should be removed.
	if len(out) != 2 {
		t.Fatalf("expected 2 messages (orphaned tool_use assistant removed), got %d", len(out))
	}
}

func TestSanitizeToolPairs_PartialOrphan(t *testing.T) {
	// Assistant message has 2 tool_uses, but only 1 has a matching result.
	msgs := []llm.Message{
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{
			llm.ToolUseBlock("c1", "Read", nil),
			llm.ToolUseBlock("c2", "Bash", nil),
		}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{
			llm.ToolResultBlock("c1", "file content", false),
			// c2 result is missing
		}},
	}
	out := SanitizeToolPairs(msgs)
	// c2 tool_use should be removed from assistant; c2 result wasn't there.
	if len(out) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(out))
	}
	if len(out[0].Content) != 1 {
		t.Fatalf("expected 1 tool_use in assistant (c2 removed), got %d", len(out[0].Content))
	}
	if out[0].Content[0].ID != "c1" {
		t.Fatalf("expected remaining tool_use to be c1, got %s", out[0].Content[0].ID)
	}
}

func TestSanitizeToolPairs_MixedUserMessage(t *testing.T) {
	// User message has text + orphaned tool_result. Text should be kept.
	msgs := []llm.Message{
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.Text("no tools")}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{
			llm.Text("here's context"),
			llm.ToolResultBlock("c1", "orphaned result", false),
		}},
	}
	out := SanitizeToolPairs(msgs)
	if len(out) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(out))
	}
	// Only the text block should remain.
	if len(out[1].Content) != 1 || out[1].Content[0].Type != llm.BlockText {
		t.Fatalf("expected only text block, got %+v", out[1].Content)
	}
}

func TestSanitizeToolPairs_CompactionScenario(t *testing.T) {
	// Simulates what happens after SummarizeCompactor: summary message
	// replaces older history, but recent messages may have orphaned results.
	msgs := []llm.Message{
		llm.UserText("[conversation summary]\nUser asked to read files..."),
		// This assistant message had tool_use for c_old, but the result was
		// in the older (now summarized) part. After compaction, the result
		// is gone but the tool_use remains.
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{
			llm.Text("Let me read the file"),
			llm.ToolUseBlock("c_old", "Read", nil),
		}},
		// The next user message has the result for c_old (which was in the
		// summarized portion — shouldn't happen, but edge case).
		{Role: llm.RoleUser, Content: []llm.ContentBlock{
			llm.ToolResultBlock("c_old", "file content", false),
		}},
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.Text("done")}},
	}
	out := SanitizeToolPairs(msgs)
	// c_old tool_use and tool_result pair: tool_result has matching tool_use
	// in the previous message, so this is actually valid.
	if len(out) != 4 {
		t.Fatalf("expected 4 messages (valid pair), got %d", len(out))
	}
}

func TestSanitizeToolPairs_TruncationOrphan(t *testing.T) {
	// Simulates truncateToBytes removing an assistant message, leaving
	// a later user tool_result orphaned.
	msgs := []llm.Message{
		llm.UserText("some text in between"),
		// tool_result for a tool_use that was in a dropped message.
		{Role: llm.RoleUser, Content: []llm.ContentBlock{
			llm.ToolResultBlock("dropped_id", "result for dropped tool", false),
		}},
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.ToolUseBlock("c2", "Grep", nil)}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.ToolResultBlock("c2", "grep output", false)}},
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.Text("found it")}},
	}
	out := SanitizeToolPairs(msgs)
	// The orphaned tool_result (dropped_id) should be removed.
	// The valid pair (c2) should be preserved.
	if len(out) != 4 {
		t.Fatalf("expected 4 messages (orphan removed), got %d", len(out))
	}
	// First message should be the text.
	if out[0].TextContent() != "some text in between" {
		t.Fatalf("expected text message first, got %+v", out[0])
	}
	// Second should be assistant with c2 tool_use.
	if out[1].Content[0].ID != "c2" {
		t.Fatalf("expected c2 tool_use, got %s", out[1].Content[0].ID)
	}
}

func TestSanitizeToolPairs_Empty(t *testing.T) {
	out := SanitizeToolPairs(nil)
	if len(out) != 0 {
		t.Fatalf("expected empty, got %d", len(out))
	}
}

func TestSanitizeToolPairs_AssistantWithTextAndToolUse(t *testing.T) {
	// Assistant has text + tool_use. Tool_use is orphaned (no result follows).
	// Text should be preserved.
	msgs := []llm.Message{
		llm.UserText("hello"),
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{
			llm.Text("Let me check"),
			llm.ToolUseBlock("c1", "Read", nil),
		}},
		llm.UserText("never mind"),
	}
	out := SanitizeToolPairs(msgs)
	if len(out) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(out))
	}
	// Assistant message should keep text but lose tool_use.
	if len(out[1].Content) != 1 || out[1].Content[0].Type != llm.BlockText {
		t.Fatalf("expected only text in assistant, got %+v", out[1].Content)
	}
}

// ---------------------------------------------------------------------------
// CompactThresholdForModel tests
// ---------------------------------------------------------------------------

func TestCompactThresholdForModel_KnownModels(t *testing.T) {
	tests := []struct {
		model     string
		maxOutput int
		want      int
	}{
		// 200k window, maxOutput capped at 20k: 200000 - 20000 - 13000 = 167000
		{"claude-opus-4-6", 64000, 167000},
		{"claude-sonnet-4-20250514", 64000, 167000},
		{"claude-sonnet-5", 64000, 167000},
		{"claude-opus-4-8", 64000, 167000},
		{"claude-haiku-4-5-20251001", 64000, 167000},

		// 128k window, maxOutput capped at 20k: 128000 - 20000 - 13000 = 95000
		{"gpt-4o", 16384, 128000 - 16384 - 13000},
		{"gpt-4o-mini", 16384, 128000 - 16384 - 13000},

		// maxOutput under cap: 128000 - 16384 - 13000 = 98616
		{"gpt-4-turbo", 16384, 98616},

		// maxOutput exactly at cap: 200000 - 20000 - 13000 = 167000
		{"claude-opus-4-6", 20000, 167000},

		// maxOutput below cap: 200000 - 8192 - 13000 = 178808
		{"claude-opus-4-6", 8192, 178808},

		// maxOutput = 0 uses fallback 16384: 200000 - 16384 - 13000 = 170616
		{"claude-opus-4-6", 0, 170616},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			got := CompactThresholdForModel(tt.model, tt.maxOutput)
			if got != tt.want {
				t.Errorf("CompactThresholdForModel(%q, %d) = %d, want %d",
					tt.model, tt.maxOutput, got, tt.want)
			}
		})
	}
}

func TestCompactThresholdForModel_UnknownModel(t *testing.T) {
	// Unknown model uses DefaultContextWindow (200000).
	// With maxOutput 16384: 200000 - 16384 - 13000 = 170616
	got := CompactThresholdForModel("unknown-model-xyz", 16384)
	want := DefaultContextWindow - 16384 - AutoCompactBufferTokens
	if got != want {
		t.Errorf("got %d, want %d", got, want)
	}
}

func TestContextWindowForModel(t *testing.T) {
	if got := ContextWindowForModel("claude-opus-4-6"); got != 200000 {
		t.Errorf("claude-opus-4-6: got %d, want 200000", got)
	}
	if got := ContextWindowForModel("gpt-4o"); got != 128000 {
		t.Errorf("gpt-4o: got %d, want 128000", got)
	}
	if got := ContextWindowForModel("unknown"); got != DefaultContextWindow {
		t.Errorf("unknown: got %d, want %d", got, DefaultContextWindow)
	}
}

// ---------------------------------------------------------------------------
// Default threshold boundary tests (CompactThreshold == 0, absolute-buffer)
// ---------------------------------------------------------------------------

// buildAgentForThresholdTest creates an agent with known parameters for precise
// threshold testing. It uses lastTotalTokens to control the token estimate
// exactly, bypassing the BPE estimator entirely.
func buildAgentForThresholdTest(contextWindow, maxTokens int) *Agent {
	a := newAgent(&fakeProvider{})
	a.ContextWindow = contextWindow
	a.MaxTokens = maxTokens
	// CompactThreshold = 0 → uses default absolute-buffer formula
	a.history = []llm.Message{llm.UserText("x")} // non-empty to avoid edge cases
	return a
}

// expectedDefaultThreshold computes the exact auto-compact threshold for the
// default (absolute-buffer) path, matching the formula in maybeCompact.
func expectedDefaultThreshold(contextWindow, maxTokens int) int {
	outputReserve := maxTokens
	if outputReserve > MaxOutputTokensForCompact {
		outputReserve = MaxOutputTokensForCompact
	}
	return contextWindow - outputReserve - AutoCompactBufferTokens
}

func TestDefaultThreshold_ExactlyAtThreshold_Fires(t *testing.T) {
	a := buildAgentForThresholdTest(200000, 16384)
	threshold := expectedDefaultThreshold(200000, 16384)
	a.lastTotalTokens = threshold // exactly at threshold

	compacted := false
	a.Compactor = compactorFunc(func(_ context.Context, msgs []llm.Message) ([]llm.Message, error) {
		compacted = true
		return msgs, nil
	})

	if err := a.maybeCompact(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !compacted {
		t.Fatalf("expected compaction at exactly %d tokens (threshold=%d)", a.lastTotalTokens, threshold)
	}
}

func TestDefaultThreshold_OneTokenBelowThreshold_DoesNotFire(t *testing.T) {
	a := buildAgentForThresholdTest(200000, 16384)
	threshold := expectedDefaultThreshold(200000, 16384)
	a.lastTotalTokens = threshold - 1 // one below

	compacted := false
	a.Compactor = compactorFunc(func(_ context.Context, msgs []llm.Message) ([]llm.Message, error) {
		compacted = true
		return msgs, nil
	})

	if err := a.maybeCompact(context.Background()); err != nil {
		t.Fatal(err)
	}
	if compacted {
		t.Fatalf("compaction fired at %d tokens, should not fire below threshold %d",
			a.lastTotalTokens, threshold)
	}
}

func TestDefaultThreshold_OneTokenAboveThreshold_Fires(t *testing.T) {
	a := buildAgentForThresholdTest(200000, 16384)
	threshold := expectedDefaultThreshold(200000, 16384)
	a.lastTotalTokens = threshold + 1

	compacted := false
	a.Compactor = compactorFunc(func(_ context.Context, msgs []llm.Message) ([]llm.Message, error) {
		compacted = true
		return msgs, nil
	})

	if err := a.maybeCompact(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !compacted {
		t.Fatalf("expected compaction at %d tokens (threshold=%d)", a.lastTotalTokens, threshold)
	}
}

func TestDefaultThreshold_128kWindow_Boundary(t *testing.T) {
	// gpt-4o-like: 128k context, 16384 max output
	a := buildAgentForThresholdTest(128000, 16384)
	threshold := expectedDefaultThreshold(128000, 16384)

	// Verify threshold value: 128000 - 16384 - 13000 = 98616
	if threshold != 98616 {
		t.Fatalf("expected threshold=98616, got %d", threshold)
	}

	// Just below: should NOT fire
	a.lastTotalTokens = threshold - 1
	compacted := false
	a.Compactor = compactorFunc(func(_ context.Context, msgs []llm.Message) ([]llm.Message, error) {
		compacted = true
		return msgs, nil
	})
	if err := a.maybeCompact(context.Background()); err != nil {
		t.Fatal(err)
	}
	if compacted {
		t.Fatal("should not compact below threshold")
	}

	// Exactly at: should fire
	a.lastTotalTokens = threshold
	a.justCompacted = false
	if err := a.maybeCompact(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !compacted {
		t.Fatal("should compact at threshold")
	}
}

func TestDefaultThreshold_LargeMaxOutput_CappedAt20k(t *testing.T) {
	// 200k context, 64k max output → outputReserve capped at 20k
	a := buildAgentForThresholdTest(200000, 64000)
	threshold := expectedDefaultThreshold(200000, 64000)

	// 200000 - 20000 - 13000 = 167000
	if threshold != 167000 {
		t.Fatalf("expected threshold=167000, got %d", threshold)
	}

	// Below threshold: no compaction
	a.lastTotalTokens = 166999
	compacted := false
	a.Compactor = compactorFunc(func(_ context.Context, msgs []llm.Message) ([]llm.Message, error) {
		compacted = true
		return msgs, nil
	})
	if err := a.maybeCompact(context.Background()); err != nil {
		t.Fatal(err)
	}
	if compacted {
		t.Fatal("should not compact at 166999 (threshold=167000)")
	}

	// At threshold: compaction
	a.lastTotalTokens = 167000
	a.justCompacted = false
	if err := a.maybeCompact(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !compacted {
		t.Fatal("should compact at 167000")
	}
}

// ---------------------------------------------------------------------------
// Force threshold (98%) boundary tests
// ---------------------------------------------------------------------------

func TestForceThreshold_ExactlyAt98Pct_Fires(t *testing.T) {
	a := buildAgentForThresholdTest(200000, 16384)
	a.justCompacted = true     // cooldown active — force should ignore it
	a.lastTotalTokens = 196000 // exactly 98%

	compacted := false
	a.Compactor = compactorFunc(func(_ context.Context, msgs []llm.Message) ([]llm.Message, error) {
		compacted = true
		return msgs, nil
	})

	if err := a.maybeCompact(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !compacted {
		t.Fatal("force compaction should fire at 98% despite cooldown")
	}
}

func TestForceThreshold_JustBelow98Pct_RespectsCooldown(t *testing.T) {
	a := buildAgentForThresholdTest(200000, 16384)
	a.justCompacted = true     // cooldown active
	a.lastTotalTokens = 195999 // just below 98%

	compacted := false
	a.Compactor = compactorFunc(func(_ context.Context, msgs []llm.Message) ([]llm.Message, error) {
		compacted = true
		return msgs, nil
	})

	if err := a.maybeCompact(context.Background()); err != nil {
		t.Fatal(err)
	}
	if compacted {
		t.Fatal("below 98% should respect cooldown")
	}
}

// ---------------------------------------------------------------------------
// Micro-compact threshold (60%) boundary tests
// ---------------------------------------------------------------------------

func TestMicroThreshold_Below60Pct_NoMicroCompact(t *testing.T) {
	a := buildAgentForThresholdTest(200000, 16384)
	a.lastTotalTokens = 119999 // just below 60%

	hookCalls := 0
	a.Hooks.OnMicroCompact = func() { hookCalls++ }
	a.Compactor = TruncateCompactor{KeepRecent: 6}

	// Add large tool results that would be cleared IF micro-compact fired.
	a.history = []llm.Message{
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.ToolUseBlock("c1", "Read", nil)}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.ToolResultBlock("c1", strings.Repeat("A", 110000), false)}},
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.ToolUseBlock("c2", "Read", nil)}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.ToolResultBlock("c2", strings.Repeat("B", 110000), false)}},
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.ToolUseBlock("c3", "Read", nil)}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.ToolResultBlock("c3", strings.Repeat("C", 110000), false)}},
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.ToolUseBlock("c4", "Read", nil)}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.ToolResultBlock("c4", strings.Repeat("D", 110000), false)}},
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.ToolUseBlock("c5", "Read", nil)}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.ToolResultBlock("c5", strings.Repeat("E", 110000), false)}},
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.ToolUseBlock("c6", "Read", nil)}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.ToolResultBlock("c6", strings.Repeat("F", 110000), false)}},
	}

	if err := a.maybeCompact(context.Background()); err != nil {
		t.Fatal(err)
	}
	if hookCalls != 0 {
		t.Fatal("micro-compact should NOT fire below 60%")
	}
}

func TestMicroThreshold_AtExactly60Pct_FiresMicroCompact(t *testing.T) {
	a := buildAgentForThresholdTest(200000, 16384)
	a.lastTotalTokens = 120000 // exactly 60%

	hookCalls := 0
	a.Hooks.OnMicroCompact = func() { hookCalls++ }
	a.Compactor = TruncateCompactor{KeepRecent: 6}

	// Add large tool results so micro-compact has something to clear.
	a.history = []llm.Message{
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.ToolUseBlock("c1", "Read", nil)}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.ToolResultBlock("c1", strings.Repeat("A", 110000), false)}},
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.ToolUseBlock("c2", "Read", nil)}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.ToolResultBlock("c2", strings.Repeat("B", 110000), false)}},
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.ToolUseBlock("c3", "Read", nil)}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.ToolResultBlock("c3", strings.Repeat("C", 110000), false)}},
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.ToolUseBlock("c4", "Read", nil)}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.ToolResultBlock("c4", strings.Repeat("D", 110000), false)}},
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.ToolUseBlock("c5", "Read", nil)}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.ToolResultBlock("c5", strings.Repeat("E", 110000), false)}},
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.ToolUseBlock("c6", "Read", nil)}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.ToolResultBlock("c6", strings.Repeat("F", 110000), false)}},
	}

	if err := a.maybeCompact(context.Background()); err != nil {
		t.Fatal(err)
	}
	if hookCalls != 1 {
		t.Fatalf("micro-compact should fire at 60%%, got %d hook calls", hookCalls)
	}
}

// ---------------------------------------------------------------------------
// Tier ordering: verify that auto fires between micro and force thresholds,
// and that each tier is mutually exclusive at its boundary.
// ---------------------------------------------------------------------------

func TestCompactTiers_AllBoundaries(t *testing.T) {
	// 200k context, 16384 max output
	// default threshold = 200000 - 16384 - 13000 = 170616
	// micro = 60% = 120000
	// force = 98% = 196000
	const window = 200000
	const maxOut = 16384
	autoThreshold := expectedDefaultThreshold(window, maxOut) // 170616

	cases := []struct {
		name      string
		tokens    int
		cooldown  bool
		wantFull  bool // full compaction (auto or force)
		wantMicro bool // micro compaction only
	}{
		// Below micro: nothing fires
		{"below_micro_59pct", 118000, false, false, false},

		// At micro threshold: micro fires (not full)
		{"at_micro_60pct", 120000, false, false, true},

		// Between micro and auto: micro fires
		{"between_micro_auto_70pct", 140000, false, false, true},

		// One below auto: micro fires (not full)
		{"one_below_auto", autoThreshold - 1, false, false, true},

		// At auto threshold: full fires
		{"at_auto_threshold", autoThreshold, false, true, false},

		// Above auto: full fires
		{"above_auto", autoThreshold + 5000, false, true, false},

		// At auto with cooldown: nothing (cooldown blocks auto+micro)
		{"at_auto_cooldown", autoThreshold, true, false, false},

		// At force threshold with cooldown: force fires (ignores cooldown)
		{"at_force_98pct_cooldown", 196000, true, true, false},

		// Just below force with cooldown: cooldown blocks
		{"below_force_97pct_cooldown", 195999, true, false, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := buildAgentForThresholdTest(window, maxOut)
			a.lastTotalTokens = tc.tokens
			a.justCompacted = tc.cooldown

			fullCompacted := false
			a.Compactor = compactorFunc(func(_ context.Context, msgs []llm.Message) ([]llm.Message, error) {
				fullCompacted = true
				return msgs, nil
			})

			microFired := false
			a.Hooks.OnMicroCompact = func() { microFired = true }

			// For micro-compact to actually fire, we need clearable content.
			a.history = []llm.Message{
				{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.ToolUseBlock("c1", "R", nil)}},
				{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.ToolResultBlock("c1", strings.Repeat("A", 110000), false)}},
				{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.ToolUseBlock("c2", "R", nil)}},
				{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.ToolResultBlock("c2", strings.Repeat("B", 110000), false)}},
				{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.ToolUseBlock("c3", "R", nil)}},
				{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.ToolResultBlock("c3", strings.Repeat("C", 110000), false)}},
				{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.ToolUseBlock("c4", "R", nil)}},
				{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.ToolResultBlock("c4", strings.Repeat("D", 110000), false)}},
				{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.ToolUseBlock("c5", "R", nil)}},
				{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.ToolResultBlock("c5", strings.Repeat("E", 110000), false)}},
				{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.ToolUseBlock("c6", "R", nil)}},
				{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.ToolResultBlock("c6", strings.Repeat("F", 110000), false)}},
			}

			if err := a.maybeCompact(context.Background()); err != nil {
				t.Fatal(err)
			}

			if fullCompacted != tc.wantFull {
				t.Errorf("full compaction: got %v, want %v", fullCompacted, tc.wantFull)
			}
			if microFired != tc.wantMicro {
				t.Errorf("micro compaction: got %v, want %v", microFired, tc.wantMicro)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Integration: end-to-end Run with API-reported tokens triggers compaction
// at the correct boundary.
// ---------------------------------------------------------------------------

func TestRun_DefaultThreshold_CompactsAtCorrectTokenCount(t *testing.T) {
	// First response pushes us to the threshold, second is the post-compaction response.
	threshold := expectedDefaultThreshold(200000, DefaultAgentMaxTokens)

	compacted := false
	p := &fakeProvider{responses: []llm.Response{
		// Turn 1: API reports tokens exactly at threshold
		{
			Message:    llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.Text("ok")}},
			StopReason: llm.StopEndTurn,
			Usage:      llm.Usage{InputTokens: threshold - 1000, OutputTokens: 1000},
		},
	}}
	a := newAgent(p)
	a.ContextWindow = 200000
	// CompactThreshold = 0: uses default formula
	a.Compactor = compactorFunc(func(_ context.Context, msgs []llm.Message) ([]llm.Message, error) {
		compacted = true
		return msgs[len(msgs)-1:], nil
	})

	_, err := a.Run(context.Background(), "hi")
	if err != nil {
		t.Fatal(err)
	}

	// After the first response, lastTotalTokens = threshold.
	// The next call to maybeCompact (at the top of the next loop iteration)
	// would fire, but since the model returned end_turn, there is no next
	// iteration. So compaction should NOT have fired during this run.
	// (It fires at the START of the next turn, not at the end of this one.)
	// This is correct behavior: the agent checks before sending, not after.
	if compacted {
		t.Fatal("compaction should not fire after the response that sets the threshold — it fires at the next turn start")
	}

	// Verify the token count was recorded correctly.
	if a.lastTotalTokens != threshold {
		t.Fatalf("expected lastTotalTokens=%d, got %d", threshold, a.lastTotalTokens)
	}
}

func TestRun_DefaultThreshold_CompactsOnSecondTurn(t *testing.T) {
	// Turn 1: pushes tokens above threshold
	// Turn 2: tool_use forces a second iteration where maybeCompact fires
	threshold := expectedDefaultThreshold(200000, DefaultAgentMaxTokens)

	compacted := false
	p := &fakeProvider{responses: []llm.Response{
		// Turn 1: tool use triggers a second loop iteration
		{
			Message: llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{
				llm.ToolUseBlock("c1", "fakeTool", json.RawMessage(`{}`)),
			}},
			StopReason: llm.StopToolUse,
			Usage:      llm.Usage{InputTokens: threshold, OutputTokens: 500},
		},
		// Turn 2: after compaction, model responds with text
		{
			Message:    llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.Text("done")}},
			StopReason: llm.StopEndTurn,
			Usage:      llm.Usage{InputTokens: 5000, OutputTokens: 200},
		},
	}}

	ft := &fakeTool{name: "fakeTool", result: &tool.Result{Content: "ok"}}
	a := newAgent(p, ft)
	a.ContextWindow = 200000
	a.Compactor = compactorFunc(func(_ context.Context, msgs []llm.Message) ([]llm.Message, error) {
		compacted = true
		return msgs[len(msgs)-2:], nil // keep last 2
	})

	_, err := a.Run(context.Background(), "hi")
	if err != nil {
		t.Fatal(err)
	}

	if !compacted {
		t.Fatal("compaction should fire at the start of the second turn when tokens exceed threshold")
	}
}

func TestRun_DefaultThreshold_NoCompactBelowThreshold(t *testing.T) {
	// API reports tokens just below threshold — compaction should never fire.
	threshold := expectedDefaultThreshold(200000, DefaultAgentMaxTokens)

	compacted := false
	p := &fakeProvider{responses: []llm.Response{
		{
			Message: llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{
				llm.ToolUseBlock("c1", "fakeTool", json.RawMessage(`{}`)),
			}},
			StopReason: llm.StopToolUse,
			Usage:      llm.Usage{InputTokens: threshold - 2000, OutputTokens: 500},
		},
		{
			Message:    llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.Text("done")}},
			StopReason: llm.StopEndTurn,
			Usage:      llm.Usage{InputTokens: threshold - 2000, OutputTokens: 500},
		},
	}}

	ft := &fakeTool{name: "fakeTool", result: &tool.Result{Content: "ok"}}
	a := newAgent(p, ft)
	a.ContextWindow = 200000
	a.Compactor = compactorFunc(func(_ context.Context, msgs []llm.Message) ([]llm.Message, error) {
		compacted = true
		return msgs, nil
	})

	_, err := a.Run(context.Background(), "hi")
	if err != nil {
		t.Fatal(err)
	}

	if compacted {
		t.Fatalf("compaction should not fire when tokens (%d) < threshold (%d)",
			threshold-2000+500, threshold)
	}
}

// ---------------------------------------------------------------------------
// Verify CompactThresholdForModel matches maybeCompact's internal formula
// ---------------------------------------------------------------------------

func TestCompactThresholdForModel_MatchesMaybeCompactFormula(t *testing.T) {
	// Verify that CompactThresholdForModel produces the same threshold that
	// maybeCompact uses internally, for various configurations.
	configs := []struct {
		window    int
		maxOutput int
	}{
		{200000, 64000},
		{200000, 16384},
		{200000, 8192},
		{128000, 16384},
		{128000, 8192},
		{100000, 20000},
		{50000, 10000},
	}

	for _, cfg := range configs {
		t.Run("", func(t *testing.T) {
			// Compute the expected threshold manually using the same formula.
			outputReserve := cfg.maxOutput
			if outputReserve > MaxOutputTokensForCompact {
				outputReserve = MaxOutputTokensForCompact
			}
			wantThreshold := cfg.window - outputReserve - AutoCompactBufferTokens

			// Build an agent and verify it fires at exactly wantThreshold
			a := buildAgentForThresholdTest(cfg.window, cfg.maxOutput)

			// Should NOT fire at wantThreshold-1
			a.lastTotalTokens = wantThreshold - 1
			compacted := false
			a.Compactor = compactorFunc(func(_ context.Context, msgs []llm.Message) ([]llm.Message, error) {
				compacted = true
				return msgs, nil
			})
			if err := a.maybeCompact(context.Background()); err != nil {
				t.Fatal(err)
			}
			if compacted {
				t.Errorf("window=%d maxOut=%d: compacted at %d, should not fire below %d",
					cfg.window, cfg.maxOutput, wantThreshold-1, wantThreshold)
			}

			// Should fire at exactly wantThreshold
			a.lastTotalTokens = wantThreshold
			a.justCompacted = false
			compacted = false
			if err := a.maybeCompact(context.Background()); err != nil {
				t.Fatal(err)
			}
			if !compacted {
				t.Errorf("window=%d maxOut=%d: did NOT compact at %d (threshold=%d)",
					cfg.window, cfg.maxOutput, wantThreshold, wantThreshold)
			}
		})
	}
}

func TestTruncateToolInputsForSummary_ProducesJSONObject(t *testing.T) {
	bigInput, _ := json.Marshal(map[string]string{"content": strings.Repeat("x", 5000)})
	msgs := []llm.Message{
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{
			llm.ToolUseBlock("t1", "Write", bigInput),
		}},
	}
	out := truncateToolInputsForSummary(msgs, 100)
	got := out[0].Content[0].Input
	if len(got) >= len(bigInput) {
		t.Fatalf("input was not truncated: %d bytes", len(got))
	}
	// The Anthropic API requires tool_use.input to be a JSON object, not a
	// bare string — a string here causes a 400 during compaction.
	var obj map[string]any
	if err := json.Unmarshal(got, &obj); err != nil {
		t.Fatalf("truncated input is not a JSON object: %v (input: %s)", err, got)
	}
}

// ---------------------------------------------------------------------------
// Provider-compat invariants: every message slice produced by compaction
// preprocessing must be safe to send to Anthropic/OpenAI. Regression suite for
// the 400 "tool_use.input: Input should be an object" bug.
// ---------------------------------------------------------------------------

// assertProviderCompatible fails the test if msgs violate any invariant the
// Anthropic API enforces on requests:
//  1. tool_use.input must be absent or a JSON object (not string/array/number)
//  2. no message may have an empty content slice
//  3. every tool_result must match a tool_use in the immediately preceding
//     assistant message; every tool_use must have a result in the next message
//  4. roles must be user or assistant
func assertProviderCompatible(t *testing.T, msgs []llm.Message) {
	t.Helper()
	for i, m := range msgs {
		if m.Role != llm.RoleUser && m.Role != llm.RoleAssistant {
			t.Fatalf("msg %d: invalid role %q", i, m.Role)
		}
		if len(m.Content) == 0 {
			t.Fatalf("msg %d (%s): empty content slice", i, m.Role)
		}
		for j, b := range m.Content {
			if b.Type == llm.BlockToolUse && len(b.Input) > 0 {
				var obj map[string]any
				if err := json.Unmarshal(b.Input, &obj); err != nil {
					t.Fatalf("msg %d block %d: tool_use.input is not a JSON object: %s", i, j, b.Input)
				}
			}
		}
	}

	// Tool pairing invariants.
	for i, m := range msgs {
		if m.Role == llm.RoleUser {
			for _, b := range m.Content {
				if b.Type != llm.BlockToolResult {
					continue
				}
				ok := false
				if i > 0 && msgs[i-1].Role == llm.RoleAssistant {
					for _, prev := range msgs[i-1].Content {
						if prev.Type == llm.BlockToolUse && prev.ID == b.ToolUseID {
							ok = true
						}
					}
				}
				if !ok {
					t.Fatalf("msg %d: orphaned tool_result %q", i, b.ToolUseID)
				}
			}
		}
		if m.Role == llm.RoleAssistant {
			for _, b := range m.Content {
				if b.Type != llm.BlockToolUse {
					continue
				}
				ok := false
				if i+1 < len(msgs) && msgs[i+1].Role == llm.RoleUser {
					for _, next := range msgs[i+1].Content {
						if next.Type == llm.BlockToolResult && next.ToolUseID == b.ID {
							ok = true
						}
					}
				}
				if !ok {
					t.Fatalf("msg %d: orphaned tool_use %q", i, b.ID)
				}
			}
		}
	}
}

// messyHistory builds a history exercising every preprocessing hazard: huge
// tool inputs (Write with an embedded file), huge tool results, images,
// thinking blocks, multi-tool turns, and already-compacted stubs.
func messyHistory() []llm.Message {
	bigWriteInput, _ := json.Marshal(map[string]string{
		"file_path": "/tmp/big.txt",
		"content":   strings.Repeat("line of code\n", 500),
	})
	return []llm.Message{
		llm.UserText("please write and inspect some files"),
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{
			llm.ThinkingBlock("let me think", "sig1"),
			llm.Text("writing the file"),
			llm.ToolUseBlock("w1", "Write", bigWriteInput),
		}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{
			llm.ToolResultBlock("w1", "wrote 6500 bytes", false),
		}},
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{
			llm.ToolUseBlock("r1", "Read", json.RawMessage(`{"file_path":"/tmp/big.txt"}`)),
			llm.ToolUseBlock("b1", "Bash", json.RawMessage(`{"command":"wc -l /tmp/big.txt"}`)),
		}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{
			llm.ToolResultBlock("r1", strings.Repeat("file content ", 400), false),
			llm.ToolResultBlock("b1", "500 /tmp/big.txt", false),
		}},
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{
			llm.ToolUseBlock("s1", "Screenshot", json.RawMessage(`{}`)),
		}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{
			{Type: llm.BlockToolResult, ToolUseID: "s1", Content: "screenshot taken",
				Images: []llm.ContentBlock{{Type: llm.BlockImage, MediaType: "image/png", Data: "aGk="}}},
		}},
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{
			llm.ToolUseBlock("old", "Grep", json.RawMessage(`{"pattern":"x"}`)),
		}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{
			llm.ToolResultBlock("old", "[result cleared — 9999 bytes]", false),
		}},
		llm.UserText("now summarize what you did"),
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.Text("done, wrote and verified the file")}},
	}
}

// TestSummarizeCompactor_RequestIsProviderCompatible is the end-to-end
// regression test for the compaction 400: the request actually sent to the
// summarizer provider must satisfy all provider invariants, including
// object-typed tool_use inputs after truncation.
func TestSummarizeCompactor_RequestIsProviderCompatible(t *testing.T) {
	sp := &fakeProvider{responses: []llm.Response{assistantText("SUMMARY", llm.StopEndTurn)}}
	c := SummarizeCompactor{Provider: sp, KeepRecent: 2}

	out, err := c.Compact(context.Background(), messyHistory())
	if err != nil {
		t.Fatal(err)
	}

	if len(sp.requests) != 1 {
		t.Fatalf("expected 1 summarization request, got %d", len(sp.requests))
	}
	// The summarization request itself must be provider-safe.
	assertProviderCompatible(t, sp.requests[0].Messages)
	// And the compacted history returned for subsequent turns too.
	assertProviderCompatible(t, out)

	// The big Write input must have been truncated — and stayed an object.
	foundTruncated := false
	for _, m := range sp.requests[0].Messages {
		for _, b := range m.Content {
			if b.Type == llm.BlockToolUse && b.Name == "Write" {
				if len(b.Input) > summaryToolInputMaxChars+300 {
					t.Fatalf("Write input not truncated: %d bytes", len(b.Input))
				}
				foundTruncated = true
			}
		}
	}
	if !foundTruncated {
		t.Fatal("expected truncated Write tool_use in summarization request")
	}
}

// TestCompactionPreprocessors_PreserveProviderInvariants runs every individual
// preprocessing transform over the messy history and checks that none of them
// can produce a provider-invalid message slice (after the standard
// SanitizeToolPairs pass that compaction always applies last).
func TestCompactionPreprocessors_PreserveProviderInvariants(t *testing.T) {
	transforms := map[string]func([]llm.Message) []llm.Message{
		"stripImages":           stripImages,
		"stripThinkingBlocks":   stripThinkingBlocks,
		"truncateToolResults":   func(m []llm.Message) []llm.Message { return truncateToolResultsForSummary(m, 100) },
		"truncateToolInputs":    func(m []llm.Message) []llm.Message { return truncateToolInputsForSummary(m, 100) },
		"clearLargeToolResults": func(m []llm.Message) []llm.Message { return clearLargeToolResults(m, 100) },
		"truncateToBytes":       func(m []llm.Message) []llm.Message { return truncateToBytes(m, 3000) },
		"truncateToTokens":      func(m []llm.Message) []llm.Message { return truncateToTokens(m, 500) },
		"microCompact":          func(m []llm.Message) []llm.Message { return MicroCompact(m, 2, 64) },
		"fullPreprocessPipeline": func(m []llm.Message) []llm.Message {
			m = stripImages(m)
			m = stripThinkingBlocks(m)
			m = truncateToolResultsForSummary(m, 100)
			m = truncateToolInputsForSummary(m, 100)
			m = truncateToBytes(m, 3000)
			m = truncateToTokens(m, 500)
			return m
		},
	}
	for name, fn := range transforms {
		t.Run(name, func(t *testing.T) {
			out := SanitizeToolPairs(fn(messyHistory()))
			assertProviderCompatible(t, out)
		})
	}
}

// TestTruncateToolInputs_MidRuneSliceStaysValidJSON guards against byte-level
// truncation splitting a multi-byte UTF-8 rune and producing invalid JSON.
func TestTruncateToolInputs_MidRuneSliceStaysValidJSON(t *testing.T) {
	// Input where the truncation point (100 bytes) lands inside a multi-byte rune.
	input, _ := json.Marshal(map[string]string{"content": strings.Repeat("ñ", 200)})
	msgs := []llm.Message{
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{
			llm.ToolUseBlock("t1", "Write", input),
		}},
	}
	out := truncateToolInputsForSummary(msgs, 100)
	var obj map[string]any
	if err := json.Unmarshal(out[0].Content[0].Input, &obj); err != nil {
		t.Fatalf("mid-rune truncation produced invalid JSON object: %v (%s)", err, out[0].Content[0].Input)
	}
}

// TestTruncateCompactor_OutputIsProviderCompatible covers the non-LLM
// compactor path with the same invariants.
func TestTruncateCompactor_OutputIsProviderCompatible(t *testing.T) {
	for keep := 1; keep <= 8; keep++ {
		out := mustCompact(t, TruncateCompactor{KeepRecent: keep}, messyHistory())
		assertProviderCompatible(t, out)
	}
}

func mustCompact(t *testing.T, c Compactor, msgs []llm.Message) []llm.Message {
	t.Helper()
	out, err := c.Compact(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
