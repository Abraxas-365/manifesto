package harness

import (
	"context"
	"errors"
	"testing"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm"
)

// ---------------------------------------------------------------------------
// Regression tests for the "assistant message prefill" fix.
//
// Anthropic models (Claude Opus 4+) reject requests where the messages array
// ends with an assistant message. The agent loop now injects a "[continue]"
// user message when history is assistant-trailing, preventing the error.
// ---------------------------------------------------------------------------

// prefillRejectProvider wraps a fakeProvider and rejects any Chat request whose
// messages array ends with an assistant role, simulating Anthropic's behaviour.
type prefillRejectProvider struct {
	fakeProvider
	prefillCount int // number of rejected calls
}

var errPrefill = errors.New(
	`{"type":"error","error":{"type":"invalid_request_error","message":"This model does not support assistant message prefill. The conversation must end with a user message."}}`,
)

func (p *prefillRejectProvider) Chat(ctx context.Context, req llm.Request) (*llm.Response, error) {
	if len(req.Messages) > 0 && req.Messages[len(req.Messages)-1].Role == llm.RoleAssistant {
		p.prefillCount++
		p.requests = append(p.requests, req)
		return nil, errPrefill
	}
	return p.fakeProvider.Chat(ctx, req)
}

// assistantMsg is a shorthand for building an assistant message.
func assistantMsg(text string) llm.Message {
	return llm.Message{
		Role:    llm.RoleAssistant,
		Content: []llm.ContentBlock{llm.Text(text)},
	}
}

// sequenceProvider lets us script arbitrary per-call behavior.
type sequenceProvider struct {
	handler     func(context.Context, llm.Request) (*llm.Response, error)
	calls       []llm.Request
	prefillSeen bool
}

func (p *sequenceProvider) Chat(ctx context.Context, req llm.Request) (*llm.Response, error) {
	return p.handler(ctx, req)
}

func (p *sequenceProvider) ChatStream(context.Context, llm.Request) (llm.Stream, error) {
	return nil, errors.New("not implemented")
}

// ---------------------------------------------------------------------------
// Continue() with assistant-trailing history must not send prefill
// ---------------------------------------------------------------------------

func TestContinue_AssistantTrailingHistory_InjectsUserMessage(t *testing.T) {
	p := &prefillRejectProvider{
		fakeProvider: fakeProvider{
			responses: []llm.Response{
				assistantText("continued", llm.StopEndTurn),
			},
		},
	}

	ag := newAgent(p)
	ag.SetHistory([]llm.Message{
		llm.UserText("initial prompt"),
		assistantMsg("I answered"),
	})

	out, err := ag.Continue(context.Background())
	if err != nil {
		t.Fatalf("Continue() should succeed, got: %v", err)
	}
	if out != "continued" {
		t.Fatalf("got %q, want %q", out, "continued")
	}
	if p.prefillCount > 0 {
		t.Fatalf("provider received %d assistant-trailing requests", p.prefillCount)
	}

	// The request should end with a user message.
	msgs := p.requests[0].Messages
	last := msgs[len(msgs)-1]
	if last.Role != llm.RoleUser {
		t.Fatalf("last message role=%s, want user", last.Role)
	}
}

// ---------------------------------------------------------------------------
// Continue() after compaction that produces assistant-trailing output
// ---------------------------------------------------------------------------

func TestContinue_AfterCompaction_NoPrefill(t *testing.T) {
	p := &prefillRejectProvider{
		fakeProvider: fakeProvider{
			responses: []llm.Response{
				assistantText("woke up", llm.StopEndTurn),
			},
		},
	}

	ag := newAgent(p)
	ag.SetHistory([]llm.Message{
		llm.UserText("old user message 1"),
		assistantMsg("old answer 1"),
		llm.UserText("old user message 2"),
		assistantMsg("old answer 2"),
		llm.UserText("wake message"),
		assistantMsg("last assistant"),
	})

	// Compactor keeps only last 2 messages → ends in assistant.
	ag.Compactor = compactorFunc(func(_ context.Context, msgs []llm.Message) ([]llm.Message, error) {
		if len(msgs) <= 2 {
			return msgs, nil
		}
		return msgs[len(msgs)-2:], nil
	})
	ag.ContextWindow = 4
	ag.CompactThreshold = 0

	out, err := ag.Continue(context.Background())
	if err != nil {
		t.Fatalf("Continue() should succeed after compaction, got: %v", err)
	}
	if out != "woke up" {
		t.Fatalf("got %q, want %q", out, "woke up")
	}
	if p.prefillCount > 0 {
		t.Fatalf("provider received %d assistant-trailing requests after compaction", p.prefillCount)
	}
}

// ---------------------------------------------------------------------------
// StopContextWindowExceeded retry after compaction
// ---------------------------------------------------------------------------

func TestStopContextWindowExceeded_CompactRetry_NoPrefill(t *testing.T) {
	callCount := 0
	cp := &sequenceProvider{calls: make([]llm.Request, 0)}
	cp.handler = func(_ context.Context, req llm.Request) (*llm.Response, error) {
		cp.calls = append(cp.calls, req)
		callCount++
		if callCount == 1 {
			return &llm.Response{
				Message:    llm.Message{Role: llm.RoleAssistant, Content: nil},
				StopReason: llm.StopContextWindowExceeded,
			}, nil
		}
		// Reject prefill if it slips through.
		if len(req.Messages) > 0 && req.Messages[len(req.Messages)-1].Role == llm.RoleAssistant {
			cp.prefillSeen = true
			return nil, errPrefill
		}
		resp := assistantText("recovered", llm.StopEndTurn)
		return &resp, nil
	}

	ag := newAgent(cp)
	ag.MaxTurns = 5
	// Compactor returns assistant-trailing output.
	ag.Compactor = compactorFunc(func(_ context.Context, msgs []llm.Message) ([]llm.Message, error) {
		return []llm.Message{
			llm.UserText("[summary]"),
			assistantMsg("last response"),
		}, nil
	})

	ag.SetHistory([]llm.Message{
		llm.UserText("hello"),
		assistantMsg("first"),
		llm.UserText("followup"),
		assistantMsg("second"),
		llm.UserText("wake"),
	})

	out, err := ag.Continue(context.Background())
	if err != nil {
		t.Fatalf("expected recovery, got: %v", err)
	}
	if out != "recovered" {
		t.Fatalf("got %q, want %q", out, "recovered")
	}
	if cp.prefillSeen {
		t.Fatal("retry after compaction sent assistant-trailing messages")
	}
}

// ---------------------------------------------------------------------------
// Context overflow error retry after compaction
// ---------------------------------------------------------------------------

func TestOverflowRetry_NoPrefill(t *testing.T) {
	callCount := 0
	cp := &sequenceProvider{calls: make([]llm.Request, 0)}
	cp.handler = func(_ context.Context, req llm.Request) (*llm.Response, error) {
		cp.calls = append(cp.calls, req)
		callCount++
		if callCount == 1 {
			return nil, errors.New("context_length_exceeded: input too long")
		}
		if len(req.Messages) > 0 && req.Messages[len(req.Messages)-1].Role == llm.RoleAssistant {
			cp.prefillSeen = true
			return nil, errPrefill
		}
		resp := assistantText("recovered", llm.StopEndTurn)
		return &resp, nil
	}

	ag := newAgent(cp)
	ag.MaxTurns = 5
	ag.Compactor = compactorFunc(func(_ context.Context, msgs []llm.Message) ([]llm.Message, error) {
		if len(msgs) <= 2 {
			return msgs, nil
		}
		return msgs[len(msgs)-2:], nil
	})

	ag.SetHistory([]llm.Message{
		llm.UserText("old"),
		assistantMsg("old answer"),
		llm.UserText("wake"),
		assistantMsg("pre-overflow"),
		llm.UserText("another wake"),
	})

	out, err := ag.Continue(context.Background())
	if err != nil {
		t.Fatalf("expected recovery, got: %v", err)
	}
	if out != "recovered" {
		t.Fatalf("got %q, want %q", out, "recovered")
	}
	if cp.prefillSeen {
		t.Fatal("overflow retry sent assistant-trailing messages")
	}
}

// ---------------------------------------------------------------------------
// Run() then Continue() (wake-turn lifecycle)
// ---------------------------------------------------------------------------

func TestRunThenContinue_NoPrefill(t *testing.T) {
	p := &prefillRejectProvider{
		fakeProvider: fakeProvider{
			responses: []llm.Response{
				assistantText("hello back", llm.StopEndTurn),
				assistantText("continued", llm.StopEndTurn),
			},
		},
	}

	ag := newAgent(p)

	// Normal run — history becomes [user, assistant].
	out, err := ag.Run(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "hello back" {
		t.Fatalf("Run got %q", out)
	}

	// Continue() on assistant-trailing history.
	out, err = ag.Continue(context.Background())
	if err != nil {
		t.Fatalf("Continue: %v", err)
	}
	if out != "continued" {
		t.Fatalf("Continue got %q", out)
	}
	if p.prefillCount > 0 {
		t.Fatalf("provider received %d prefill requests", p.prefillCount)
	}
}

// ---------------------------------------------------------------------------
// Verify the request messages directly end with user
// ---------------------------------------------------------------------------

func TestContinue_RequestMessagesEndWithUser(t *testing.T) {
	p := &fakeProvider{
		responses: []llm.Response{
			assistantText("ok", llm.StopEndTurn),
		},
	}

	ag := newAgent(p)
	ag.SetHistory([]llm.Message{
		llm.UserText("hello"),
		assistantMsg("I responded"),
	})

	if _, err := ag.Continue(context.Background()); err != nil {
		t.Fatal(err)
	}

	msgs := p.requests[0].Messages
	last := msgs[len(msgs)-1]
	if last.Role != llm.RoleUser {
		t.Errorf("last message role=%s, want user", last.Role)
	}
}

// ---------------------------------------------------------------------------
// Multiple Continue() calls must all be safe
// ---------------------------------------------------------------------------

func TestContinue_MultipleWakes_NoPrefill(t *testing.T) {
	p := &fakeProvider{
		responses: []llm.Response{
			assistantText("wake 1", llm.StopEndTurn),
			assistantText("wake 2", llm.StopEndTurn),
			assistantText("wake 3", llm.StopEndTurn),
		},
	}

	ag := newAgent(p)
	ag.SetHistory([]llm.Message{
		llm.UserText("initial"),
		assistantMsg("initial response"),
	})

	for i := 0; i < 3; i++ {
		if _, err := ag.Continue(context.Background()); err != nil {
			t.Fatalf("Continue() %d: %v", i+1, err)
		}

		msgs := p.requests[i].Messages
		last := msgs[len(msgs)-1]
		if last.Role != llm.RoleUser {
			t.Errorf("Continue() %d: last message role=%s, want user", i+1, last.Role)
		}
	}
}

// ---------------------------------------------------------------------------
// Continue() on user-trailing history must NOT inject extra message
// ---------------------------------------------------------------------------

func TestContinue_UserTrailingHistory_NoExtraInjection(t *testing.T) {
	p := &fakeProvider{
		responses: []llm.Response{
			assistantText("ok", llm.StopEndTurn),
		},
	}

	ag := newAgent(p)
	ag.SetHistory([]llm.Message{
		llm.UserText("hello"),
		assistantMsg("response"),
		llm.UserText("followup"),
	})

	if _, err := ag.Continue(context.Background()); err != nil {
		t.Fatal(err)
	}

	msgs := p.requests[0].Messages
	// Should be exactly the 3 original messages — no "[continue]" injected.
	if len(msgs) != 3 {
		t.Errorf("expected 3 messages, got %d", len(msgs))
	}
	if msgs[len(msgs)-1].TextContent() != "followup" {
		t.Errorf("last message text=%q, want %q", msgs[len(msgs)-1].TextContent(), "followup")
	}
}

// ---------------------------------------------------------------------------
// Compactors can produce assistant-trailing output (informational)
// ---------------------------------------------------------------------------

func TestTruncateCompactor_CanProduceAssistantTrailing(t *testing.T) {
	msgs := []llm.Message{
		llm.UserText("msg 1"),
		assistantMsg("reply 1"),
		llm.UserText("msg 2"),
		assistantMsg("reply 2"),
		llm.UserText("msg 3"),
		assistantMsg("reply 3"),
	}

	out, err := TruncateCompactor{KeepRecent: 2}.Compact(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}

	// Compactor output can end in assistant — the loop guard handles this.
	last := out[len(out)-1]
	if last.Role != llm.RoleAssistant {
		t.Errorf("expected assistant-trailing compaction, got role=%s", last.Role)
	}
}

func TestSummarizeCompactor_CanProduceAssistantTrailing(t *testing.T) {
	sp := &fakeProvider{
		responses: []llm.Response{
			assistantText("SUMMARY", llm.StopEndTurn),
		},
	}

	msgs := []llm.Message{
		llm.UserText("msg 1"),
		assistantMsg("reply 1"),
		llm.UserText("msg 2"),
		assistantMsg("reply 2"),
		llm.UserText("msg 3"),
		assistantMsg("reply 3"),
	}

	out, err := SummarizeCompactor{Provider: sp, KeepRecent: 2}.
		Compact(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}

	last := out[len(out)-1]
	if last.Role != llm.RoleAssistant {
		t.Errorf("expected assistant-trailing compaction, got role=%s", last.Role)
	}
}
