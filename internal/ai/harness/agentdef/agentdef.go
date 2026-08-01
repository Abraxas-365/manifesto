// Package agentdef holds named agent definitions (profiles) for the subagent
// (Task/Agent) tool. It ports a battle-tested agent blueprint system
// (programmatic definitions and AGENT.md files), with
// legacy's two-step contract: Define registers a blueprint, the roster controls
// which blueprints the Agent tool exposes.
package agentdef

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Definition is one agent blueprint. Zero-valued fields mean "inherit from
// the parent agent" unless noted.
type Definition struct {
	Name        string
	Description string
	// Model is "" or "inherit" (parent's model), "small" (the configured
	// small model), or a literal model id.
	Model string
	// Thinking is "", "off", "minimal", "low", "medium", "high", "xhigh".
	Thinking string
	// SystemPrompt, when set, is the subagent's system prompt.
	SystemPrompt string
	// SystemPromptMode is "replace" (default) or "append" relative to the
	// builtin subagent prompt.
	SystemPromptMode string
	// Tools is the allowlist of tool names, or ["*"] for all tools.
	// Empty means ["*"] (legacy: omitted = everything).
	Tools []string
	// ToolExclude is a denylist applied when Tools is ["*"].
	ToolExclude []string
	// AutoloadSkills lists skill names whose bodies are preloaded into the
	// subagent context.
	AutoloadSkills []string
	// Subagents, when non-nil, restricts which named agents this agent may
	// invoke through its own Task tool (legacy subagent_agents). An empty
	// non-nil list removes the Task tool from the subagent entirely.
	Subagents []string
	// MaxSubagentDepth caps Task-call nesting for this agent's descendants
	// (pi-subagents maxSubagentDepth). 0 = inherit the parent's limit.
	MaxSubagentDepth int
	// MaxTurns caps the subagent's provider round-trips. 0 = no cap.
	MaxTurns int
	// GraceTurns is the number of additional turns after MaxTurns is reached.
	// Tools remain available and a warning is injected. 0 = hard stop at MaxTurns.
	GraceTurns int
	// TimeoutMS aborts the run after this many milliseconds. 0 = none.
	TimeoutMS int
	// ReadOnly restricts the subagent to read-only tools regardless of
	// Tools/ToolExclude.
	ReadOnly bool
}

// AllowsAll reports whether the definition grants the full tool registry.
func (d *Definition) AllowsAll() bool {
	if len(d.Tools) == 0 {
		return true
	}
	for _, t := range d.Tools {
		if t == "*" {
			return true
		}
	}
	return false
}

// AllowsTool reports whether the definition permits the named tool.
func (d *Definition) AllowsTool(name string) bool {
	if d.AllowsAll() {
		for _, x := range d.ToolExclude {
			if x == name {
				return false
			}
		}
		return true
	}
	for _, t := range d.Tools {
		if t == name {
			return true
		}
	}
	return false
}

// Registry stores definitions and the roster of exposed agents. Safe for
// concurrent use.
type Registry struct {
	mu     sync.Mutex
	defs   map[string]*Definition
	order  []string // definition insertion order
	roster []string // exposed agent names, insertion order
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{defs: map[string]*Definition{}}
}

// Define registers or updates a blueprint. legacy semantics: redefining merges —
// fields left zero in def keep their previously registered value.
func (r *Registry) Define(def Definition) error {
	if strings.TrimSpace(def.Name) == "" {
		return fmt.Errorf("agentdef: name is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	existing, ok := r.defs[def.Name]
	if !ok {
		d := def
		r.defs[def.Name] = &d
		r.order = append(r.order, def.Name)
		return nil
	}
	merge(existing, def)
	return nil
}

// merge overlays non-zero fields of src onto dst (legacy partial update).
func merge(dst *Definition, src Definition) {
	if src.Description != "" {
		dst.Description = src.Description
	}
	if src.Model != "" {
		dst.Model = src.Model
	}
	if src.Thinking != "" {
		dst.Thinking = src.Thinking
	}
	if src.SystemPrompt != "" {
		dst.SystemPrompt = src.SystemPrompt
	}
	if src.SystemPromptMode != "" {
		dst.SystemPromptMode = src.SystemPromptMode
	}
	if src.Tools != nil {
		dst.Tools = src.Tools
	}
	if src.ToolExclude != nil {
		dst.ToolExclude = src.ToolExclude
	}
	if src.AutoloadSkills != nil {
		dst.AutoloadSkills = src.AutoloadSkills
	}
	if src.Subagents != nil {
		dst.Subagents = src.Subagents
	}
	if src.MaxSubagentDepth != 0 {
		dst.MaxSubagentDepth = src.MaxSubagentDepth
	}
	if src.MaxTurns != 0 {
		dst.MaxTurns = src.MaxTurns
	}
	if src.GraceTurns != 0 {
		dst.GraceTurns = src.GraceTurns
	}
	if src.TimeoutMS != 0 {
		dst.TimeoutMS = src.TimeoutMS
	}
	if src.ReadOnly {
		dst.ReadOnly = true
	}
}

// Put registers or replaces a blueprint wholesale (legacy RegisterDynamic /
// register-replace semantics — no merge).
func (r *Registry) Put(def Definition) error {
	if strings.TrimSpace(def.Name) == "" {
		return fmt.Errorf("agentdef: name is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.defs[def.Name]; !ok {
		r.order = append(r.order, def.Name)
	}
	d := def
	r.defs[def.Name] = &d
	return nil
}

// Remove deletes a definition and drops it from the roster
// (register-remove).
func (r *Registry) Remove(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.defs, name)
	for i, n := range r.order {
		if n == name {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}
	for i, n := range r.roster {
		if n == name {
			r.roster = append(r.roster[:i], r.roster[i+1:]...)
			break
		}
	}
}

// Get returns a copy of the named definition.
func (r *Registry) Get(name string) (Definition, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.defs[name]
	if !ok {
		return Definition{}, false
	}
	return *d, true
}

// All returns copies of every definition in registration order.
func (r *Registry) All() []Definition {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Definition, 0, len(r.order))
	for _, n := range r.order {
		out = append(out, *r.defs[n])
	}
	return out
}

// Expose adds an agent name to the roster (roster add).
// Lenient like legacy: the name may be defined later; Roster/RosterNames skip
// names with no definition. Duplicates are no-ops.
func (r *Registry) Expose(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, n := range r.roster {
		if n == name {
			return
		}
	}
	r.roster = append(r.roster, name)
}

// Unexpose removes an agent from the roster (roster remove).
func (r *Registry) Unexpose(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, n := range r.roster {
		if n == name {
			r.roster = append(r.roster[:i], r.roster[i+1:]...)
			return
		}
	}
}

// ClearRoster empties the roster (roster clear).
func (r *Registry) ClearRoster() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.roster = nil
}

// Roster returns copies of exposed definitions in exposure order. Names
// without a definition are skipped (legacy RosterList).
func (r *Registry) Roster() []Definition {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Definition, 0, len(r.roster))
	for _, n := range r.roster {
		if d, ok := r.defs[n]; ok {
			out = append(out, *d)
		}
	}
	return out
}

// RosterNames returns the exposed-and-defined agent names sorted (stable for
// schema enums).
func (r *Registry) RosterNames() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.roster))
	for _, n := range r.roster {
		if _, ok := r.defs[n]; ok {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}
