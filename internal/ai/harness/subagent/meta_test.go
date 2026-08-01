package subagent

import (
	"context"
	"testing"
)

// ---------------------------------------------------------------------------
// Group 1: Subagent context metadata (Meta)
// ---------------------------------------------------------------------------

func TestMeta_RoundTrip(t *testing.T) {
	m := Meta{
		SessionID:       "sess-1",
		RunID:           "t3",
		AgentName:       "researcher",
		Depth:           2,
		ParentToolUseID: "toolu_abc",
		Workdir:         "/tmp/claudio-worktree-task-1",
	}
	ctx := WithMeta(context.Background(), m)
	got := MetaFromContext(ctx)

	if got != m {
		t.Fatalf("round trip mismatch:\n got: %+v\nwant: %+v", got, m)
	}
}

func TestMeta_Missing_ReturnsZero(t *testing.T) {
	got := MetaFromContext(context.Background())
	if got != (Meta{}) {
		t.Fatalf("expected zero Meta, got %+v", got)
	}
}

func TestMeta_NestedContexts_InnerWins(t *testing.T) {
	outer := WithMeta(context.Background(), Meta{AgentName: "parent", Depth: 1})
	inner := WithMeta(outer, Meta{AgentName: "child", Depth: 2})

	got := MetaFromContext(inner)
	if got.AgentName != "child" || got.Depth != 2 {
		t.Fatalf("expected inner Meta, got %+v", got)
	}
}

func TestMeta_PreservesOtherContextValues(t *testing.T) {
	type customKey struct{}
	ctx := context.WithValue(context.Background(), customKey{}, "hello")
	ctx = WithMeta(ctx, Meta{SessionID: "sess-1"})

	if v := ctx.Value(customKey{}).(string); v != "hello" {
		t.Fatalf("lost custom context value: %q", v)
	}
	if m := MetaFromContext(ctx); m.SessionID != "sess-1" {
		t.Fatalf("lost Meta: %+v", m)
	}
}

func TestMeta_IsSubagent(t *testing.T) {
	// Depth > 0 means subagent; depth 0 is top-level.
	tests := []struct {
		depth int
		want  bool
	}{
		{0, false},
		{1, true},
		{3, true},
	}
	for _, tt := range tests {
		m := Meta{Depth: tt.depth}
		got := m.Depth > 0
		if got != tt.want {
			t.Errorf("depth=%d: isSubagent=%v, want %v", tt.depth, got, tt.want)
		}
	}
}
