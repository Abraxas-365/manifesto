package tool

import "context"

type useIDKey struct{}

// WithUseID stores the current tool-use block ID in ctx. The agent sets it
// before Execute so tools that spawn nested work (e.g. subagents) can tag
// their events with the parent call that produced them.
func WithUseID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, useIDKey{}, id)
}

// UseID returns the tool-use block ID stored by WithUseID ("" when absent).
func UseID(ctx context.Context) string {
	s, _ := ctx.Value(useIDKey{}).(string)
	return s
}
