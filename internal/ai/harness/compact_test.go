package harness

import (
	"context"
	"strings"
	"testing"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm"
)

func TestEstimateTokens(t *testing.T) {
	msgs := []llm.Message{llm.UserText(strings.Repeat("a", 40))}
	// 40 chars text + 8 chars system = 48 / 4 = 12.
	if got := EstimateTokens(msgs, "sysprompt"); got != 12 {
		t.Fatalf("got %d, want 12", got)
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

// compactorFunc adapts a func to the Compactor interface.
type compactorFunc func(context.Context, []llm.Message) ([]llm.Message, error)

func (f compactorFunc) Compact(ctx context.Context, msgs []llm.Message) ([]llm.Message, error) {
	return f(ctx, msgs)
}
