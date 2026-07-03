package tool

import "sync"

// DeferrableTool is a Tool that can be withheld from the model until requested
// via ToolSearch. Implement it (or embed Deferrable) to defer a tool by default.
//
// A deferred tool is advertised to the model as name + hint only (in a
// system-reminder). The model loads its full schema on demand with the
// ToolSearch tool, at which point it becomes visible for the rest of the run.
type DeferrableTool interface {
	Tool
	// ShouldDefer reports whether the tool should start hidden.
	ShouldDefer() bool
	// SearchHint returns short keywords/description shown in the reminder and
	// used for ToolSearch keyword matching.
	SearchHint() string
}

// Deferrable is an embeddable helper: embed tool.Deferrable{Hint: "..."} in a
// tool to make it deferred by default.
type Deferrable struct{ Hint string }

// ShouldDefer implements DeferrableTool.
func (d Deferrable) ShouldDefer() bool { return true }

// SearchHint implements DeferrableTool.
func (d Deferrable) SearchHint() string { return d.Hint }

// Discovery tracks which deferred tools have been revealed during a run. It is
// shared (by pointer) between an Agent and its ToolSearch tool. Safe for
// concurrent use.
type Discovery struct {
	mu       sync.Mutex
	revealed map[string]bool
}

// NewDiscovery returns an empty discovery set.
func NewDiscovery() *Discovery { return &Discovery{revealed: make(map[string]bool)} }

// Reveal marks a deferred tool as discovered.
func (d *Discovery) Reveal(name string) {
	d.mu.Lock()
	d.revealed[name] = true
	d.mu.Unlock()
}

// Revealed reports whether a tool has been discovered.
func (d *Discovery) Revealed(name string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.revealed[name]
}
