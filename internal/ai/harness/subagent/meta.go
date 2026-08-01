package subagent

import "context"

// Meta carries subagent identity and lineage through context so that tools
// can inspect their execution environment.
type Meta struct {
	// SessionID is the owning principal session (the top-level session that
	// ultimately spawned this agent chain). Empty for agents without a session.
	SessionID string
	// RunID is the background run identifier ("t1", "t2", …). Empty for
	// foreground (blocking) subagent executions.
	RunID string
	// AgentName is the named profile (e.g. "researcher"). Empty when using
	// the default profile.
	AgentName string
	// Depth is the nesting level: 0 = top-level session agent, 1 = first
	// child, 2 = grandchild, etc.
	Depth int
	// ParentToolUseID is the model-generated tool-use ID of the SubAgent call
	// that spawned this agent. Empty at the top level.
	ParentToolUseID string
	// Workdir is the isolated working directory (worktree path) when
	// isolation is active. Empty when running in the shared working tree.
	Workdir string
}

type metaCtxKey struct{}

// WithMeta attaches subagent metadata to ctx.
func WithMeta(ctx context.Context, m Meta) context.Context {
	return context.WithValue(ctx, metaCtxKey{}, m)
}

// MetaFromContext retrieves subagent metadata from ctx.
// Returns a zero Meta if none was set.
func MetaFromContext(ctx context.Context) Meta {
	m, _ := ctx.Value(metaCtxKey{}).(Meta)
	return m
}
