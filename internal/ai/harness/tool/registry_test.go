package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm"
)

// stubTool is a minimal Tool for registry tests.
type stubTool struct {
	name string
}

func (s stubTool) Name() string                 { return s.name }
func (s stubTool) Description() string          { return "desc " + s.name }
func (s stubTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (s stubTool) IsReadOnly() bool             { return true }
func (s stubTool) Execute(context.Context, json.RawMessage) (*Result, error) {
	return &Result{Content: "ok"}, nil
}

// deferStub is a Tool that defers itself via embedded Deferrable.
type deferStub struct {
	stubTool
	Deferrable
}

func TestSetDeferredAndSearchHints(t *testing.T) {
	r := NewRegistry()
	r.Register(stubTool{name: "Read"})
	r.Register(stubTool{name: "Grep"})
	r.Register(deferStub{stubTool: stubTool{name: "Fancy"}, Deferrable: Deferrable{Hint: "fancy stuff"}})

	r.SetDeferred("Grep", "search contents")

	hints := r.SearchHints()
	if len(hints) != 2 {
		t.Fatalf("want 2 deferred, got %d: %v", len(hints), hints)
	}
	if hints["Grep"] != "search contents" {
		t.Fatalf("override hint wrong: %q", hints["Grep"])
	}
	if hints["Fancy"] != "fancy stuff" {
		t.Fatalf("interface hint wrong: %q", hints["Fancy"])
	}
	if _, ok := hints["Read"]; ok {
		t.Fatal("Read should not be deferred")
	}
}

func TestDeferredUnknown(t *testing.T) {
	r := NewRegistry()
	r.Register(stubTool{name: "Grep"})
	r.SetDeferred("Grep", "search")  // valid
	r.SetDeferred("Grpe", "typo")    // typo: no such tool
	r.SetDeferred("Blob", "another") // typo: no such tool

	unknown := r.DeferredUnknown()
	if len(unknown) != 2 || unknown[0] != "Blob" || unknown[1] != "Grpe" {
		t.Fatalf("want sorted [Blob Grpe], got %v", unknown)
	}

	// Registering the missing tool clears it.
	r.Register(stubTool{name: "Grpe"})
	r.Register(stubTool{name: "Blob"})
	if u := r.DeferredUnknown(); len(u) != 0 {
		t.Fatalf("want none unknown, got %v", u)
	}
}

func TestVisibleDefinitionsNilEqualsAll(t *testing.T) {
	r := NewRegistry()
	r.Register(stubTool{name: "Read"})
	r.Register(stubTool{name: "Grep"})
	r.SetDeferred("Grep", "search")

	all := r.APIDefinitions()
	vis := r.VisibleDefinitions(nil)
	if len(all) != len(vis) || len(vis) != 2 {
		t.Fatalf("nil discovery should equal APIDefinitions: %d vs %d", len(all), len(vis))
	}
}

func TestVisibleDefinitionsHidesUntilRevealed(t *testing.T) {
	r := NewRegistry()
	r.Register(stubTool{name: "Read"})
	r.Register(stubTool{name: "Grep"})
	r.SetDeferred("Grep", "search")

	d := NewDiscovery()
	vis := r.VisibleDefinitions(d)
	if len(vis) != 1 || vis[0].Name != "Read" {
		t.Fatalf("deferred Grep should be hidden, got %v", defNames(vis))
	}

	d.Reveal("Grep")
	vis = r.VisibleDefinitions(d)
	if len(vis) != 2 {
		t.Fatalf("Grep should be visible after reveal, got %v", defNames(vis))
	}
}

func TestDeferredReminder(t *testing.T) {
	r := NewRegistry()
	r.Register(stubTool{name: "Read"})
	r.Register(stubTool{name: "Grep"})
	r.Register(stubTool{name: "Glob"})
	r.SetDeferred("Grep", "search contents")
	r.SetDeferred("Glob", "find files")

	d := NewDiscovery()
	rem := r.DeferredReminder(d)
	if !strings.Contains(rem, "Grep: search contents") || !strings.Contains(rem, "Glob: find files") {
		t.Fatalf("reminder missing entries: %q", rem)
	}
	if !strings.Contains(rem, "ToolSearch") {
		t.Fatalf("reminder should mention ToolSearch: %q", rem)
	}

	d.Reveal("Grep")
	d.Reveal("Glob")
	if rem := r.DeferredReminder(d); rem != "" {
		t.Fatalf("reminder should be empty once all revealed: %q", rem)
	}
}

func defNames(defs []llm.ToolDef) []string {
	out := make([]string, len(defs))
	for i, d := range defs {
		out[i] = d.Name
	}
	return out
}
