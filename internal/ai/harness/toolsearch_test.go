package harness

import (
	"context"
	"strings"
	"testing"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm"
)

// containsToolDef reports whether defs includes a tool with the given name.
func containsToolDef(defs []llm.ToolDef, name string) bool {
	for _, d := range defs {
		if d.Name == name {
			return true
		}
	}
	return false
}

func TestEnableToolSearch_DefersUntilRevealed(t *testing.T) {
	// Turn 1: model calls ToolSearch to load the deferred Secret tool.
	// Turn 2: model uses Secret. Turn 3: model answers.
	p := &fakeProvider{responses: []llm.Response{
		assistantToolUse(toolUse("t1", "ToolSearch", `{"query":"select:Secret"}`)),
		assistantToolUse(toolUse("t2", "Secret", `{}`)),
		assistantText("done", llm.StopEndTurn),
	}}

	secret := &fakeTool{name: "Secret", readOnly: true}
	a := newAgent(p, secret)
	a.Registry.SetDeferred("Secret", "does secret things")
	a.EnableToolSearch()

	out, err := a.Run(context.Background(), "go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "done" {
		t.Fatalf("got %q", out)
	}

	// Turn 1 request: Secret hidden from tools, reminder present in system.
	req1 := p.requests[0]
	if containsToolDef(req1.Tools, "Secret") {
		t.Fatalf("turn 1 should hide deferred Secret: %v", req1.Tools)
	}
	if !containsToolDef(req1.Tools, "ToolSearch") {
		t.Fatal("turn 1 should include eager ToolSearch")
	}
	if !strings.Contains(req1.System, "Secret: does secret things") {
		t.Fatalf("turn 1 system should list deferred Secret: %q", req1.System)
	}

	// Turn 2 request (after ToolSearch revealed it): Secret now visible in tools.
	// The frozen deferred reminder still lists it (by design — freezing keeps the
	// system prompt byte-stable for cache efficiency, matching legacy behavior).
	req2 := p.requests[1]
	if !containsToolDef(req2.Tools, "Secret") {
		t.Fatalf("turn 2 should expose revealed Secret: %v", req2.Tools)
	}
	if secret.executed != 1 {
		t.Fatalf("Secret should have executed once, got %d", secret.executed)
	}
}

func TestEnableToolSearch_PanicsOnUnknownDeferred(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for unknown deferred tool")
		}
		if msg, ok := r.(string); !ok || !strings.Contains(msg, "Grpe") {
			t.Fatalf("panic should name the typo, got %v", r)
		}
	}()

	p := &fakeProvider{}
	a := newAgent(p, &fakeTool{name: "Grep", readOnly: true})
	a.Registry.SetDeferred("Grpe", "typo") // misspelled
	a.EnableToolSearch()
}

func TestNoToolSearch_SendsAllToolsAndPlainSystem(t *testing.T) {
	p := &fakeProvider{responses: []llm.Response{assistantText("hi", llm.StopEndTurn)}}
	secret := &fakeTool{name: "Secret", readOnly: true}
	a := newAgent(p, secret)
	a.System = "base system"
	a.Registry.SetDeferred("Secret", "hidden") // deferred but no EnableToolSearch

	if _, err := a.Run(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	req := p.requests[0]
	if !containsToolDef(req.Tools, "Secret") {
		t.Fatal("without EnableToolSearch all tools should be sent")
	}
	if req.System != "base system" {
		t.Fatalf("system should be unchanged: %q", req.System)
	}
}
