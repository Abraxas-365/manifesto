package harness

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/tool"
	"github.com/Abraxas-365/manifesto/internal/errx"
	"github.com/Abraxas-365/manifesto/internal/models"
)

// fakeProvider returns a scripted sequence of responses, one per Chat call, and
// records every request it received so tests can assert on loop behaviour.
type fakeProvider struct {
	responses []llm.Response
	err       error
	calls     int
	requests  []llm.Request
}

func (p *fakeProvider) Chat(_ context.Context, req llm.Request) (*llm.Response, error) {
	p.requests = append(p.requests, req)
	if p.err != nil {
		return nil, p.err
	}
	if p.calls >= len(p.responses) {
		return nil, errors.New("fakeProvider: no scripted response for call")
	}
	resp := p.responses[p.calls]
	p.calls++
	return &resp, nil
}

func (p *fakeProvider) ChatStream(context.Context, llm.Request) (llm.Stream, error) {
	return nil, errors.New("not implemented")
}

// fakeTool is a configurable tool for exercising dispatch paths.
type fakeTool struct {
	name      string
	readOnly  bool
	result    *tool.Result
	execErr   error
	executed  int
	lastInput json.RawMessage
}

func (t *fakeTool) Name() string                 { return t.name }
func (t *fakeTool) Description() string          { return "fake tool " + t.name }
func (t *fakeTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (t *fakeTool) IsReadOnly() bool             { return t.readOnly }

func (t *fakeTool) Execute(_ context.Context, input json.RawMessage) (*tool.Result, error) {
	t.executed++
	t.lastInput = input
	if t.execErr != nil {
		return nil, t.execErr
	}
	if t.result != nil {
		return t.result, nil
	}
	return &tool.Result{Content: "ok"}, nil
}

// assistantText builds a scripted assistant response with plain text.
func assistantText(text string, reason llm.StopReason) llm.Response {
	return llm.Response{
		Message:    llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.Text(text)}},
		StopReason: reason,
	}
}

// assistantToolUse builds a scripted assistant response with one or more tool_use blocks.
func assistantToolUse(blocks ...llm.ContentBlock) llm.Response {
	return llm.Response{
		Message:    llm.Message{Role: llm.RoleAssistant, Content: blocks},
		StopReason: llm.StopToolUse,
	}
}

func toolUse(id, name, input string) llm.ContentBlock {
	return llm.ToolUseBlock(id, name, json.RawMessage(input))
}

func newAgent(p llm.Provider, tools ...tool.Tool) *Agent {
	reg := tool.NewRegistry()
	for _, t := range tools {
		reg.Register(t)
	}
	return New(p, reg)
}

func errCode(t *testing.T, err error) string {
	t.Helper()
	var e *errx.Error
	if !errors.As(err, &e) {
		t.Fatalf("expected *errx.Error, got %T: %v", err, err)
	}
	return e.Code
}

// TestV1Parity_MaxTokensFromCapabilities verifies that the agent sends
// model-appropriate max_tokens on the first request (legacy parity: legacy reads
// cap.MaxResponseTokens, not a hardcoded 8192).
func TestV1Parity_MaxTokensFromCapabilities(t *testing.T) {
	models.SetGlobalCache(models.NewDefaultCache())

	tests := []struct {
		model   string
		wantMin int // minimum expected max_tokens (legacy reference)
	}{
		{"claude-opus-4-6", 128_000},
		{"claude-sonnet-5", 64_000},
		{"claude-sonnet-4-5-20250514", 64_000},
		{"claude-fable-5", 128_000},
	}

	for _, tc := range tests {
		t.Run(tc.model, func(t *testing.T) {
			p := &fakeProvider{responses: []llm.Response{
				assistantText("ok", llm.StopEndTurn),
			}}
			a := newAgent(p)
			a.Model = tc.model
			a.System = "test"
			a.Reasoning = llm.ReasoningHigh
			_, _ = a.Run(context.Background(), "hello")

			if len(p.requests) == 0 {
				t.Fatal("no API calls recorded")
			}
			if p.requests[0].MaxTokens < tc.wantMin {
				t.Errorf("max_tokens = %d, want >= %d (legacy sends cap.MaxResponseTokens)",
					p.requests[0].MaxTokens, tc.wantMin)
			}
		})
	}
}

func TestRun_ContextMessageInjectedOnFirstTurnOnly(t *testing.T) {
	p := &fakeProvider{responses: []llm.Response{
		assistantText("one", llm.StopEndTurn),
		assistantText("two", llm.StopEndTurn),
	}}
	a := newAgent(p)
	a.ContextMessage = "<system-reminder>ctx</system-reminder>"

	if _, err := a.Run(context.Background(), "first"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Run(context.Background(), "second"); err != nil {
		t.Fatal(err)
	}

	first := p.requests[0].Messages[0].Content[0].Text
	if !strings.HasPrefix(first, "<system-reminder>ctx</system-reminder>\n\nfirst") {
		t.Fatalf("first message missing context prefix: %q", first)
	}
	// Second turn: history is non-empty, no re-injection.
	second := p.requests[1].Messages[2].Content[0].Text
	if second != "second" {
		t.Fatalf("second message should be bare, got %q", second)
	}
}

func TestRun_ContextMessageSkippedOnResumedHistory(t *testing.T) {
	p := &fakeProvider{responses: []llm.Response{assistantText("ok", llm.StopEndTurn)}}
	a := newAgent(p)
	a.ContextMessage = "ctx"
	a.SetHistory([]llm.Message{
		llm.UserText("old"),
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.Text("old answer")}},
	})

	if _, err := a.Run(context.Background(), "resumed"); err != nil {
		t.Fatal(err)
	}
	got := p.requests[0].Messages[2].Content[0].Text
	if got != "resumed" {
		t.Fatalf("resumed session should not re-inject context, got %q", got)
	}
}

func TestRun_NoToolUse(t *testing.T) {
	p := &fakeProvider{responses: []llm.Response{assistantText("hello there", llm.StopEndTurn)}}
	a := newAgent(p)

	out, err := a.Run(context.Background(), "hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "hello there" {
		t.Fatalf("got %q, want %q", out, "hello there")
	}
	if p.calls != 1 {
		t.Fatalf("expected 1 provider call, got %d", p.calls)
	}
}

func TestRun_SingleToolCall(t *testing.T) {
	ft := &fakeTool{name: "Echo", readOnly: true, result: &tool.Result{Content: "tool-output"}}
	p := &fakeProvider{responses: []llm.Response{
		assistantToolUse(toolUse("call-1", "Echo", `{"x":1}`)),
		assistantText("done", llm.StopEndTurn),
	}}
	a := newAgent(p, ft)

	out, err := a.Run(context.Background(), "run echo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "done" {
		t.Fatalf("got %q, want %q", out, "done")
	}
	if ft.executed != 1 {
		t.Fatalf("expected tool executed once, got %d", ft.executed)
	}
	if string(ft.lastInput) != `{"x":1}` {
		t.Fatalf("tool received wrong input: %s", ft.lastInput)
	}

	// Second provider request must include the tool result as a user message.
	if len(p.requests) != 2 {
		t.Fatalf("expected 2 provider requests, got %d", len(p.requests))
	}
	last := p.requests[1].Messages
	toolResultMsg := last[len(last)-1]
	if toolResultMsg.Role != llm.RoleUser {
		t.Fatalf("expected tool result carried in user message, got role %q", toolResultMsg.Role)
	}
	block := toolResultMsg.Content[0]
	if block.Type != llm.BlockToolResult || block.ToolUseID != "call-1" || block.Content != "tool-output" {
		t.Fatalf("unexpected tool result block: %+v", block)
	}
}

func TestRun_ParallelToolCalls(t *testing.T) {
	a1 := &fakeTool{name: "A", result: &tool.Result{Content: "ra"}}
	b1 := &fakeTool{name: "B", result: &tool.Result{Content: "rb"}}
	p := &fakeProvider{responses: []llm.Response{
		assistantToolUse(
			toolUse("c1", "A", `{}`),
			toolUse("c2", "B", `{}`),
		),
		assistantText("both done", llm.StopEndTurn),
	}}
	a := newAgent(p, a1, b1)

	out, err := a.Run(context.Background(), "run both")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "both done" {
		t.Fatalf("got %q", out)
	}
	if a1.executed != 1 || b1.executed != 1 {
		t.Fatalf("expected both tools executed once, got A=%d B=%d", a1.executed, b1.executed)
	}

	results := p.requests[1].Messages
	blocks := results[len(results)-1].Content
	if len(blocks) != 2 || blocks[0].ToolUseID != "c1" || blocks[1].ToolUseID != "c2" {
		t.Fatalf("expected two ordered tool results, got %+v", blocks)
	}
}

func TestRun_UnknownTool(t *testing.T) {
	p := &fakeProvider{responses: []llm.Response{
		assistantToolUse(toolUse("call-1", "Missing", `{}`)),
		assistantText("recovered", llm.StopEndTurn),
	}}
	a := newAgent(p) // no tools registered

	out, err := a.Run(context.Background(), "go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "recovered" {
		t.Fatalf("got %q", out)
	}
	block := p.requests[1].Messages[len(p.requests[1].Messages)-1].Content[0]
	if !block.IsError || block.ToolUseID != "call-1" {
		t.Fatalf("expected error result block for unknown tool, got %+v", block)
	}
}

func TestRun_ApprovalDenied(t *testing.T) {
	ft := &fakeTool{name: "Write"}
	p := &fakeProvider{responses: []llm.Response{
		assistantToolUse(toolUse("call-1", "Write", `{}`)),
		assistantText("ok", llm.StopEndTurn),
	}}
	a := newAgent(p, ft)
	a.Approver = func(context.Context, string, json.RawMessage) bool { return false }

	if _, err := a.Run(context.Background(), "write"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ft.executed != 0 {
		t.Fatalf("expected tool NOT executed when denied, got %d", ft.executed)
	}
	block := p.requests[1].Messages[len(p.requests[1].Messages)-1].Content[0]
	if !block.IsError {
		t.Fatalf("expected denied result to be an error block, got %+v", block)
	}
}

func TestRun_ApprovalGranted(t *testing.T) {
	ft := &fakeTool{name: "Write", result: &tool.Result{Content: "written"}}
	p := &fakeProvider{responses: []llm.Response{
		assistantToolUse(toolUse("call-1", "Write", `{}`)),
		assistantText("ok", llm.StopEndTurn),
	}}
	a := newAgent(p, ft)
	a.Approver = func(context.Context, string, json.RawMessage) bool { return true }

	if _, err := a.Run(context.Background(), "write"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ft.executed != 1 {
		t.Fatalf("expected tool executed when approved, got %d", ft.executed)
	}
}

func TestRun_ToolExecError(t *testing.T) {
	ft := &fakeTool{name: "Boom", execErr: errors.New("kaboom")}
	p := &fakeProvider{responses: []llm.Response{
		assistantToolUse(toolUse("call-1", "Boom", `{}`)),
		assistantText("handled", llm.StopEndTurn),
	}}
	a := newAgent(p, ft)

	out, err := a.Run(context.Background(), "go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "handled" {
		t.Fatalf("got %q", out)
	}
	block := p.requests[1].Messages[len(p.requests[1].Messages)-1].Content[0]
	if !block.IsError {
		t.Fatalf("expected error result block after exec error, got %+v", block)
	}
}

func TestRun_MaxTurns(t *testing.T) {
	ft := &fakeTool{name: "Loop"}
	// Always return a tool use so the loop never terminates naturally.
	resps := make([]llm.Response, 5)
	for i := range resps {
		resps[i] = assistantToolUse(toolUse("c", "Loop", `{}`))
	}
	p := &fakeProvider{responses: resps}
	a := newAgent(p, ft)
	a.MaxTurns = 3

	_, err := a.Run(context.Background(), "go")
	if err == nil {
		t.Fatal("expected ErrMaxTurns")
	}
	if code := errCode(t, err); code != ErrMaxTurns.Code {
		t.Fatalf("expected %s, got %s", ErrMaxTurns.Code, code)
	}
	if p.calls != 3 {
		t.Fatalf("expected exactly MaxTurns=3 provider calls, got %d", p.calls)
	}
}

func TestRun_MaxTokens(t *testing.T) {
	p := &fakeProvider{responses: []llm.Response{assistantText("partial ans", llm.StopMaxTokens)}}
	a := newAgent(p)
	a.MaxTokens = 4096 // user-configured: prevents auto-escalation

	out, err := a.Run(context.Background(), "go")
	if err == nil {
		t.Fatal("expected ErrMaxTokens")
	}
	if code := errCode(t, err); code != ErrMaxTokens.Code {
		t.Fatalf("expected %s, got %s", ErrMaxTokens.Code, code)
	}
	// Partial text is returned alongside the error.
	if out != "partial ans" {
		t.Fatalf("expected partial text returned, got %q", out)
	}
	var e *errx.Error
	errors.As(err, &e)
	if e.Details["partial"] != "partial ans" {
		t.Fatalf("expected partial detail, got %v", e.Details["partial"])
	}
}

func TestRun_ProviderError(t *testing.T) {
	p := &fakeProvider{err: errors.New("network down")}
	a := newAgent(p)

	if _, err := a.Run(context.Background(), "go"); err == nil {
		t.Fatal("expected provider error to propagate")
	}
}

func TestRun_RequestCarriesToolsAndHistory(t *testing.T) {
	ft := &fakeTool{name: "Echo"}
	p := &fakeProvider{responses: []llm.Response{assistantText("hi", llm.StopEndTurn)}}
	a := newAgent(p, ft)
	a.System = "be helpful"
	a.Model = "test-model"

	if _, err := a.Run(context.Background(), "hello"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	req := p.requests[0]
	if req.System != "be helpful" || req.Model != "test-model" {
		t.Fatalf("request missing system/model: %+v", req)
	}
	if len(req.Tools) != 1 || req.Tools[0].Name != "Echo" {
		t.Fatalf("request missing tool definitions: %+v", req.Tools)
	}
	if len(req.Messages) != 1 || req.Messages[0].TextContent() != "hello" {
		t.Fatalf("request missing user message: %+v", req.Messages)
	}
}

func TestRun_HistoryPersistsAcrossCalls(t *testing.T) {
	p := &fakeProvider{responses: []llm.Response{
		assistantText("first", llm.StopEndTurn),
		assistantText("second", llm.StopEndTurn),
	}}
	a := newAgent(p)

	if _, err := a.Run(context.Background(), "one"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Run(context.Background(), "two"); err != nil {
		t.Fatal(err)
	}
	// Second call's request should carry the full prior conversation:
	// user(one), assistant(first), user(two).
	msgs := p.requests[1].Messages
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages in second request, got %d: %+v", len(msgs), msgs)
	}
	if msgs[0].TextContent() != "one" || msgs[1].TextContent() != "first" || msgs[2].TextContent() != "two" {
		t.Fatalf("unexpected history: %+v", msgs)
	}
}

// streamProvider serves scripted responses via ChatStream, splitting text into
// per-character deltas. Chat panics to prove the streaming path is used.
type streamProvider struct {
	responses []llm.Response
	calls     int
}

func (p *streamProvider) Chat(context.Context, llm.Request) (*llm.Response, error) {
	panic("Chat called; expected ChatStream when OnTextDelta is set")
}

func (p *streamProvider) ChatStream(_ context.Context, req llm.Request) (llm.Stream, error) {
	if p.calls >= len(p.responses) {
		return nil, errors.New("streamProvider: no scripted response")
	}
	resp := p.responses[p.calls]
	p.calls++
	var deltas []string
	for _, r := range resp.Message.TextContent() {
		deltas = append(deltas, string(r))
	}
	return &fakeStream{deltas: deltas, final: resp}, nil
}

type fakeStream struct {
	deltas []string
	i      int
	final  llm.Response
	closed bool
}

func (s *fakeStream) Next() (llm.StreamEvent, error) {
	if s.i < len(s.deltas) {
		d := s.deltas[s.i]
		s.i++
		return llm.StreamEvent{TextDelta: d}, nil
	}
	return llm.StreamEvent{Done: true, Message: s.final.Message, StopReason: s.final.StopReason, Usage: s.final.Usage}, nil
}

func (s *fakeStream) Close() error { s.closed = true; return nil }

func TestRun_StreamingDeltas(t *testing.T) {
	p := &streamProvider{responses: []llm.Response{assistantText("hola", llm.StopEndTurn)}}
	a := newAgent(p)

	var got []string
	a.Hooks.OnTextDelta = func(d string) { got = append(got, d) }

	out, err := a.Run(context.Background(), "hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "hola" {
		t.Fatalf("final text %q", out)
	}
	if strings.Join(got, "") != "hola" {
		t.Fatalf("deltas %v", got)
	}
}

func TestRun_StreamingWithToolCalls(t *testing.T) {
	ft := &fakeTool{name: "Echo"}
	p := &streamProvider{responses: []llm.Response{
		assistantToolUse(toolUse("tu1", "Echo", `{}`)),
		assistantText("done", llm.StopEndTurn),
	}}
	a := newAgent(p, ft)
	a.Hooks.OnTextDelta = func(string) {}

	out, err := a.Run(context.Background(), "go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "done" || ft.executed != 1 {
		t.Fatalf("out=%q executed=%d", out, ft.executed)
	}
}

func TestRun_NoDeltaHookUsesChat(t *testing.T) {
	// fakeProvider.ChatStream errors, so success proves Chat was used.
	p := &fakeProvider{responses: []llm.Response{assistantText("plain", llm.StopEndTurn)}}
	a := newAgent(p)
	out, err := a.Run(context.Background(), "hi")
	if err != nil || out != "plain" {
		t.Fatalf("out=%q err=%v", out, err)
	}
}

func TestRun_MaxTokensEscalation(t *testing.T) {
	// First response hits max_tokens with text only → agent should escalate
	// and retry at model's full output capacity. Second response succeeds.
	p := &fakeProvider{responses: []llm.Response{
		assistantText("partial...", llm.StopMaxTokens),
		assistantText("full response", llm.StopEndTurn),
	}}
	a := newAgent(p)
	a.Model = "claude-sonnet-4-20250514"

	out, err := a.Run(context.Background(), "write something long")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "full response" {
		t.Fatalf("expected full response, got %q", out)
	}
	if len(p.requests) != 2 {
		t.Fatalf("expected 2 API calls (original + escalated), got %d", len(p.requests))
	}
	// First call should use the model's max output tokens from capabilities.
	firstExpected := llm.Capabilities("claude-sonnet-4-20250514").MaxOutputTokens
	if firstExpected == 0 {
		firstExpected = DefaultAgentMaxTokens
	}
	if p.requests[0].MaxTokens != firstExpected {
		t.Fatalf("first call max_tokens = %d, want %d", p.requests[0].MaxTokens, firstExpected)
	}
	// Second call should use escalated value (same or FallbackEscalatedMax).
	if p.requests[1].MaxTokens < firstExpected {
		t.Fatalf("second call max_tokens = %d, should be >= %d", p.requests[1].MaxTokens, firstExpected)
	}
}

func TestRun_MaxTokensNoEscalationWhenUserConfigured(t *testing.T) {
	// When the user explicitly sets MaxTokens, don't escalate — return error.
	p := &fakeProvider{responses: []llm.Response{
		assistantText("partial...", llm.StopMaxTokens),
	}}
	a := newAgent(p)
	a.MaxTokens = 1024 // user-configured

	_, err := a.Run(context.Background(), "write")
	if err == nil {
		t.Fatal("expected error when user-configured MaxTokens is hit")
	}
	if !strings.Contains(err.Error(), "MAX_TOKENS") {
		t.Fatalf("expected MAX_TOKENS error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Context cancellation / Kill tests
// ---------------------------------------------------------------------------

func TestRun_CancelledContextReturnsImmediately(t *testing.T) {
	// If the context is already cancelled before Run is called, it should
	// return an error without calling the provider at all.
	p := &fakeProvider{responses: []llm.Response{assistantText("nope", llm.StopEndTurn)}}
	a := newAgent(p)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before calling Run

	_, err := a.Run(ctx, "hi")
	if err == nil {
		t.Fatal("expected context-cancelled error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if p.calls != 0 {
		t.Fatalf("provider should not have been called, got %d calls", p.calls)
	}
}

func TestRun_CancelBetweenTurnsStopsLoop(t *testing.T) {
	// Simulate Kill: cancel the context after the first tool response so the
	// agent stops instead of making a second provider call.
	ctx, cancel := context.WithCancel(context.Background())

	ft := &fakeTool{name: "Slow", result: &tool.Result{Content: "ok"}}
	p := &fakeProvider{responses: []llm.Response{
		assistantToolUse(toolUse("c1", "Slow", `{}`)),
		assistantText("should not reach", llm.StopEndTurn),
	}}
	a := newAgent(p, ft)

	// Cancel after the first provider call returns (tool call turn).
	origChat := p.Chat
	_ = origChat
	// We hook into the tool itself: after executing, cancel the context.
	ft.result = nil
	origExec := ft.Execute
	_ = origExec
	ft2 := &cancellingTool{name: "Slow", cancel: cancel}

	reg := tool.NewRegistry()
	reg.Register(ft2)
	a.Registry = reg

	_, err := a.Run(ctx, "go")
	if err == nil {
		t.Fatal("expected context-cancelled error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	// Only one provider call should have been made (the tool_use turn).
	// The second call (which returns text) should NOT happen because the
	// loop checks ctx.Err() before calling the provider.
	if p.calls != 1 {
		t.Fatalf("expected 1 provider call, got %d", p.calls)
	}
	if ft2.executed != 1 {
		t.Fatalf("expected tool executed once, got %d", ft2.executed)
	}
}

func TestDispatch_CancelledContextSkipsTool(t *testing.T) {
	// When the context is cancelled, dispatch should return a "Cancelled"
	// error result without executing the tool.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ft := &fakeTool{name: "Echo", result: &tool.Result{Content: "nope"}}
	a := newAgent(nil, ft) // provider unused

	block := a.dispatch(ctx, toolUse("c1", "Echo", `{}`))
	if !block.IsError {
		t.Fatal("expected error block for cancelled context")
	}
	if !strings.Contains(block.Content, "Cancelled") {
		t.Fatalf("expected 'Cancelled' in content, got %q", block.Content)
	}
	if ft.executed != 0 {
		t.Fatalf("tool should not have been executed, got %d", ft.executed)
	}
}

func TestDispatch_CancelDuringParallelToolsSkipsRemaining(t *testing.T) {
	// Model returns 3 tool calls. The first tool cancels the context.
	// With parallel dispatch all tools start concurrently, so the
	// cancelling tool may or may not beat the others to the ctx.Err()
	// check. We assert only that: (a) the cancelling tool ran, (b) the
	// agent propagated the cancellation, (c) result order is preserved.
	ctx, cancel := context.WithCancel(context.Background())

	canceller := &cancellingTool{name: "First", cancel: cancel}
	second := &fakeTool{name: "Second", result: &tool.Result{Content: "s"}}
	third := &fakeTool{name: "Third", result: &tool.Result{Content: "t"}}

	p := &fakeProvider{responses: []llm.Response{
		assistantToolUse(
			toolUse("c1", "First", `{}`),
			toolUse("c2", "Second", `{}`),
			toolUse("c3", "Third", `{}`),
		),
		// After all tools return, the loop checks ctx → exits.
		assistantText("never", llm.StopEndTurn),
	}}
	a := newAgent(p, canceller, second, third)

	_, err := a.Run(ctx, "go")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if canceller.executed != 1 {
		t.Fatalf("First should have executed once, got %d", canceller.executed)
	}
}

func TestContinue_CancelledContextReturnsImmediately(t *testing.T) {
	// Continue (used by background agent wake) should also respect cancellation.
	p := &fakeProvider{responses: []llm.Response{assistantText("nope", llm.StopEndTurn)}}
	a := newAgent(p)
	// Seed history so Continue has something to work with.
	a.SetHistory([]llm.Message{
		llm.UserText("hello"),
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := a.Continue(ctx)
	if err == nil {
		t.Fatal("expected context-cancelled error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if p.calls != 0 {
		t.Fatalf("provider should not have been called, got %d calls", p.calls)
	}
}

// cancellingTool executes once then cancels its context. Used to simulate Kill
// arriving mid-turn.
type cancellingTool struct {
	name     string
	cancel   context.CancelFunc
	executed int
}

func (t *cancellingTool) Name() string                 { return t.name }
func (t *cancellingTool) Description() string          { return "cancel trigger" }
func (t *cancellingTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (t *cancellingTool) IsReadOnly() bool             { return true }

func (t *cancellingTool) Execute(_ context.Context, _ json.RawMessage) (*tool.Result, error) {
	t.executed++
	t.cancel()
	return &tool.Result{Content: "done"}, nil
}

// slowTool sleeps on a channel until signalled, recording its start time.
// Used to verify tools run concurrently rather than sequentially.
type slowTool struct {
	name     string
	gate     chan struct{} // close to unblock Execute
	started  chan struct{} // closed when Execute begins
	executed int
}

func (t *slowTool) Name() string                 { return t.name }
func (t *slowTool) Description() string          { return "slow " + t.name }
func (t *slowTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (t *slowTool) IsReadOnly() bool             { return true }

func (t *slowTool) Execute(_ context.Context, _ json.RawMessage) (*tool.Result, error) {
	t.executed++
	close(t.started)
	<-t.gate
	return &tool.Result{Content: t.name + "-done"}, nil
}

func TestRun_ParallelToolCallsExecuteConcurrently(t *testing.T) {
	// Verify that when the model returns multiple tool_use blocks, the tools
	// actually execute concurrently (not sequentially).
	//
	// Strategy: two tools each block on a gate channel. If dispatch were
	// sequential, the second tool's started channel would never be signalled
	// because the first tool is still blocked. With parallel dispatch both
	// tools start, we observe both started channels, then unblock both gates.

	toolA := &slowTool{name: "A", gate: make(chan struct{}), started: make(chan struct{})}
	toolB := &slowTool{name: "B", gate: make(chan struct{}), started: make(chan struct{})}

	p := &fakeProvider{responses: []llm.Response{
		assistantToolUse(
			toolUse("c1", "A", `{}`),
			toolUse("c2", "B", `{}`),
		),
		assistantText("both done", llm.StopEndTurn),
	}}
	a := newAgent(p, toolA, toolB)

	done := make(chan struct{})
	var runErr error
	var runOut string
	go func() {
		runOut, runErr = a.Run(context.Background(), "go")
		close(done)
	}()

	// Both tools must have started before we unblock them.
	// If dispatch is sequential, this will deadlock → test timeout.
	<-toolA.started
	<-toolB.started

	// Unblock both tools.
	close(toolA.gate)
	close(toolB.gate)

	<-done
	if runErr != nil {
		t.Fatalf("unexpected error: %v", runErr)
	}
	if runOut != "both done" {
		t.Fatalf("got %q, want %q", runOut, "both done")
	}
	if toolA.executed != 1 || toolB.executed != 1 {
		t.Fatalf("expected each tool executed once, got A=%d B=%d", toolA.executed, toolB.executed)
	}

	// Result order must match tool_use order regardless of completion order.
	results := p.requests[1].Messages
	blocks := results[len(results)-1].Content
	if len(blocks) != 2 {
		t.Fatalf("expected 2 result blocks, got %d", len(blocks))
	}
	if blocks[0].ToolUseID != "c1" || blocks[1].ToolUseID != "c2" {
		t.Fatalf("result order not preserved: %+v", blocks)
	}
}

func TestRun_MaxTokensResetAfterToolExecution(t *testing.T) {
	// After escalation, if the model calls a tool, the override should reset
	// so the next text-only response can escalate fresh.
	ft := &fakeTool{name: "Bash"}
	p := &fakeProvider{responses: []llm.Response{
		assistantText("partial", llm.StopMaxTokens),   // 1: triggers escalation
		assistantToolUse(toolUse("c1", "Bash", `{}`)), // 2: tool call at escalated
		assistantText("done", llm.StopEndTurn),        // 3: after tool, back to default
	}}
	a := newAgent(p, ft)
	a.Model = "claude-sonnet-4-20250514"

	out, err := a.Run(context.Background(), "go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "done" {
		t.Fatalf("expected 'done', got %q", out)
	}
	// 3 API calls: model default → escalated → reset to model default
	if len(p.requests) != 3 {
		t.Fatalf("expected 3 API calls, got %d", len(p.requests))
	}
	modelMax := llm.Capabilities("claude-sonnet-4-20250514").MaxOutputTokens
	if modelMax == 0 {
		modelMax = DefaultAgentMaxTokens
	}
	if p.requests[0].MaxTokens != modelMax {
		t.Fatalf("call 0: got %d, want %d", p.requests[0].MaxTokens, modelMax)
	}
	if p.requests[1].MaxTokens < modelMax {
		t.Fatalf("call 1: got %d, should be >= %d", p.requests[1].MaxTokens, modelMax)
	}
	// After tool execution, override resets → back to model default.
	if p.requests[2].MaxTokens != modelMax {
		t.Fatalf("call 2: got %d, want %d (should reset after tool exec)", p.requests[2].MaxTokens, modelMax)
	}
}

// ---------------------------------------------------------------------------
// Context-overflow recovery (OpenAI-style error path) and tool result capping
// ---------------------------------------------------------------------------

// overflowProvider fails the first failN Chat calls with a context-overflow
// error, then delegates to the scripted responses.
type overflowProvider struct {
	fakeProvider
	failN int
}

func (p *overflowProvider) Chat(ctx context.Context, req llm.Request) (*llm.Response, error) {
	if p.failN > 0 {
		p.failN--
		p.requests = append(p.requests, req)
		return nil, errors.New(`received error while streaming: {"type":"invalid_request_error","code":"context_length_exceeded","message":"Your input exceeds the context window of this model."}`)
	}
	return p.fakeProvider.Chat(ctx, req)
}

// countingCompactor records Compact calls and truncates to the last message.
type countingCompactor struct{ calls int }

func (c *countingCompactor) Compact(_ context.Context, msgs []llm.Message) ([]llm.Message, error) {
	c.calls++
	if len(msgs) == 0 {
		return msgs, nil
	}
	return msgs[len(msgs)-1:], nil
}

func TestIsContextOverflow(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{errors.New("network down"), false},
		{errors.New(`{"code":"context_length_exceeded","message":"..."}`), true},
		{errors.New("Your input exceeds the context window of this model"), true},
		{errors.New("This model's maximum context length is 128000 tokens"), true},
		{errors.New("input length and `max_tokens` exceed context limit: 195018 + 8192"), true},
	}
	for _, c := range cases {
		if got := llm.IsContextOverflow(c.err); got != c.want {
			t.Errorf("IsContextOverflow(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}

func TestRun_ContextOverflowErrorCompactsAndRetries(t *testing.T) {
	// First Chat call fails with an OpenAI-style overflow error; recovery must
	// compact and retry the same turn, then succeed.
	p := &overflowProvider{
		fakeProvider: fakeProvider{responses: []llm.Response{assistantText("recovered", llm.StopEndTurn)}},
		failN:        1,
	}
	comp := &countingCompactor{}
	ag := newAgent(p)
	ag.Compactor = comp
	ag.MaxTurns = 3

	out, err := ag.Run(context.Background(), "hi")
	if err != nil {
		t.Fatalf("expected recovery, got error: %v", err)
	}
	if out != "recovered" {
		t.Fatalf("got %q", out)
	}
	if comp.calls == 0 {
		t.Fatal("compactor was never invoked")
	}
	// 2 API calls: failed + retried.
	if len(p.requests) != 2 {
		t.Fatalf("expected 2 API calls, got %d", len(p.requests))
	}
}

func TestRun_ContextOverflowErrorGivesUpAfterRetries(t *testing.T) {
	// If compaction cannot shrink the request the loop must not spin forever.
	p := &overflowProvider{failN: 100}
	comp := &countingCompactor{}
	ag := newAgent(p)
	ag.Compactor = comp
	ag.MaxTurns = 5

	_, err := ag.Run(context.Background(), "hi")
	if err == nil {
		t.Fatal("expected error after exhausting overflow retries")
	}
	if !llm.IsContextOverflow(err) {
		t.Fatalf("expected the overflow error to propagate, got: %v", err)
	}
	// maxOverflowRetries retries + the original = bounded call count.
	if len(p.requests) > maxOverflowRetries+1 {
		t.Fatalf("unbounded retry loop: %d calls", len(p.requests))
	}
}

func TestRun_ContextOverflowErrorNoCompactorFailsFast(t *testing.T) {
	p := &overflowProvider{failN: 1}
	ag := newAgent(p) // no Compactor
	ag.MaxTurns = 3

	_, err := ag.Run(context.Background(), "hi")
	if err == nil {
		t.Fatal("expected error without compactor")
	}
	if len(p.requests) != 1 {
		t.Fatalf("must fail fast without compactor, got %d calls", len(p.requests))
	}
}
