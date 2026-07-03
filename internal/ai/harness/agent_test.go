package harness

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/tool"
	"github.com/Abraxas-365/manifesto/internal/errx"
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
	name          string
	readOnly      bool
	needsApproval bool
	result        *tool.Result
	execErr       error
	executed      int
	lastInput     json.RawMessage
}

func (t *fakeTool) Name() string                          { return t.name }
func (t *fakeTool) Description() string                   { return "fake tool " + t.name }
func (t *fakeTool) InputSchema() json.RawMessage          { return json.RawMessage(`{"type":"object"}`) }
func (t *fakeTool) IsReadOnly() bool                      { return t.readOnly }
func (t *fakeTool) RequiresApproval(json.RawMessage) bool { return t.needsApproval }

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
	ft := &fakeTool{name: "Write", needsApproval: true}
	p := &fakeProvider{responses: []llm.Response{
		assistantToolUse(toolUse("call-1", "Write", `{}`)),
		assistantText("ok", llm.StopEndTurn),
	}}
	a := newAgent(p, ft)
	a.Approver = func(tool.Tool, json.RawMessage) bool { return false }

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
	ft := &fakeTool{name: "Write", needsApproval: true, result: &tool.Result{Content: "written"}}
	p := &fakeProvider{responses: []llm.Response{
		assistantToolUse(toolUse("call-1", "Write", `{}`)),
		assistantText("ok", llm.StopEndTurn),
	}}
	a := newAgent(p, ft)
	a.Approver = func(tool.Tool, json.RawMessage) bool { return true }

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
