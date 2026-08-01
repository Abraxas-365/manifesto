package harness

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/tool"
)

func TestHooks_FireInOrder(t *testing.T) {
	ft := &fakeTool{name: "Echo", result: &tool.Result{Content: "out"}}
	p := &fakeProvider{responses: []llm.Response{
		assistantToolUse(toolUse("c1", "Echo", `{"x":1}`)),
		assistantText("final", llm.StopEndTurn),
	}}
	a := newAgent(p, ft)

	var events []string
	a.Hooks = Hooks{
		OnTurnStart: func(turn int) { events = append(events, "turn") },
		OnToolStart: func(_, name string, _ json.RawMessage) *ToolIntercept {
			events = append(events, "tool_start:"+name)
			return nil
		},
		OnToolEnd: func(name string, _ llm.ContentBlock) *llm.ContentBlock {
			events = append(events, "tool_end:"+name)
			return nil
		},
		OnAssistantText: func(text string) { events = append(events, "text:"+text) },
	}

	if _, err := a.Run(context.Background(), "go"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{"turn", "tool_start:Echo", "tool_end:Echo", "turn", "text:final"}
	if len(events) != len(want) {
		t.Fatalf("got %v, want %v", events, want)
	}
	for i := range want {
		if events[i] != want[i] {
			t.Fatalf("event %d: got %q want %q (all=%v)", i, events[i], want[i], events)
		}
	}
}

func TestHooks_OnUsageAndTotalUsage(t *testing.T) {
	p := &fakeProvider{responses: []llm.Response{
		{
			Message:    llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.Text("hi")}},
			StopReason: llm.StopEndTurn,
			Usage:      llm.Usage{InputTokens: 10, OutputTokens: 5, CacheReadTokens: 3},
		},
	}}
	a := newAgent(p)

	var seenTotal llm.Usage
	a.Hooks.OnUsage = func(_ int, _, total llm.Usage) { seenTotal = total }

	if _, err := a.Run(context.Background(), "go"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := a.TotalUsage(); got.InputTokens != 10 || got.OutputTokens != 5 || got.CacheReadTokens != 3 {
		t.Fatalf("unexpected total usage: %+v", got)
	}
	if seenTotal != a.TotalUsage() {
		t.Fatalf("OnUsage total %+v != TotalUsage %+v", seenTotal, a.TotalUsage())
	}
}

func TestHooks_NilSafe(t *testing.T) {
	p := &fakeProvider{responses: []llm.Response{assistantText("ok", llm.StopEndTurn)}}
	a := newAgent(p)
	// No hooks set: must not panic.
	if _, err := a.Run(context.Background(), "go"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHooks_OnToolEndReplacesResult(t *testing.T) {
	ft := &fakeTool{name: "Echo", result: &tool.Result{Content: "original"}}
	p := &fakeProvider{
		responses: []llm.Response{
			assistantToolUse(toolUse("c1", "Echo", `{}`)),
			assistantText("done", llm.StopEndTurn),
		},
	}
	a := newAgent(p, ft)

	a.Hooks.OnToolEnd = func(name string, result llm.ContentBlock) *llm.ContentBlock {
		modified := result
		modified.Content = "replaced"
		return &modified
	}

	if _, err := a.Run(context.Background(), "go"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The second provider call should see the replaced content in the tool
	// result message (the last user message before the second call).
	if len(p.requests) < 2 {
		t.Fatalf("expected at least 2 requests, got %d", len(p.requests))
	}
	msgs := p.requests[1].Messages
	toolResultMsg := msgs[len(msgs)-1] // last message is the tool result
	if toolResultMsg.Role != llm.RoleUser {
		t.Fatalf("expected user role, got %s", toolResultMsg.Role)
	}
	found := false
	for _, b := range toolResultMsg.Content {
		if b.Type == "tool_result" && b.Content == "replaced" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected replaced tool result content, got %+v", toolResultMsg.Content)
	}
}
