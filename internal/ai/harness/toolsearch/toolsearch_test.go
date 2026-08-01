package toolsearch

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/tool"
)

type stubTool struct {
	name string
}

func (s stubTool) Name() string                 { return s.name }
func (s stubTool) Description() string          { return "desc " + s.name }
func (s stubTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (s stubTool) IsReadOnly() bool             { return true }
func (s stubTool) Execute(context.Context, json.RawMessage) (*tool.Result, error) {
	return &tool.Result{Content: "ok"}, nil
}

func newFixture() (*tool.Registry, *tool.Discovery, *Tool) {
	r := tool.NewRegistry()
	r.Register(stubTool{name: "Read"}) // eager
	r.Register(stubTool{name: "Grep"})
	r.Register(stubTool{name: "Glob"})
	r.SetDeferred("Grep", "search file contents with regex")
	r.SetDeferred("Glob", "find files by pattern")
	d := tool.NewDiscovery()
	return r, d, &Tool{Registry: r, Discovery: d}
}

func exec(t *testing.T, tl *Tool, query string) *tool.Result {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"query": query})
	res, err := tl.Execute(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestSelectRevealsExact(t *testing.T) {
	_, d, tl := newFixture()
	res := exec(t, tl, "select:Grep")
	if res.IsError {
		t.Fatalf("unexpected error: %q", res.Content)
	}
	if !strings.Contains(res.Content, "<functions>") || !strings.Contains(res.Content, `"name":"Grep"`) {
		t.Fatalf("expected Grep schema block: %q", res.Content)
	}
	if !d.Revealed("Grep") {
		t.Fatal("Grep should be revealed")
	}
	if d.Revealed("Glob") {
		t.Fatal("Glob should not be revealed")
	}
}

func TestSelectIgnoresEagerTools(t *testing.T) {
	_, _, tl := newFixture()
	res := exec(t, tl, "select:Read")
	if !strings.Contains(res.Content, "No tools matched") {
		t.Fatalf("eager Read should not be discoverable: %q", res.Content)
	}
}

func TestKeywordSearch(t *testing.T) {
	_, d, tl := newFixture()
	res := exec(t, tl, "search regex contents")
	if !strings.Contains(res.Content, `"name":"Grep"`) {
		t.Fatalf("expected Grep match: %q", res.Content)
	}
	if !d.Revealed("Grep") {
		t.Fatal("Grep should be revealed by keyword search")
	}
}

func TestKeywordRanksNameOverHint(t *testing.T) {
	r := tool.NewRegistry()
	r.Register(stubTool{name: "Grep"})
	r.Register(stubTool{name: "Other"})
	// "Grep" name matches keyword; Other only matches via hint.
	r.SetDeferred("Grep", "unrelated")
	r.SetDeferred("Other", "grep-like search")
	tl := &Tool{Registry: r, Discovery: tool.NewDiscovery()}

	raw, _ := json.Marshal(map[string]any{"query": "grep", "max_results": 1})
	res, err := tl.Execute(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Content, `"name":"Grep"`) || strings.Contains(res.Content, `"name":"Other"`) {
		t.Fatalf("name match should outrank hint match with max_results=1: %q", res.Content)
	}
}

func TestNoMatch(t *testing.T) {
	_, _, tl := newFixture()
	res := exec(t, tl, "zzzznope")
	if !strings.Contains(res.Content, "No tools matched") {
		t.Fatalf("expected no-match message: %q", res.Content)
	}
}

func TestNilRegistry(t *testing.T) {
	tl := &Tool{}
	res := exec(t, tl, "select:Grep")
	if !res.IsError {
		t.Fatal("expected error with nil registry")
	}
}
