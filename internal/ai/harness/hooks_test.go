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
		OnTurnStart:     func(turn int) { events = append(events, "turn") },
		OnToolStart:     func(name string, _ json.RawMessage) { events = append(events, "tool_start:"+name) },
		OnToolEnd:       func(name string, _ llm.ContentBlock) { events = append(events, "tool_end:"+name) },
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
