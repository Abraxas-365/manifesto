package tool

import (
	"sort"
	"strings"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm"
)

// Registry holds the tools available to an agent, preserving registration
// order.
type Registry struct {
	tools    map[string]Tool
	order    []string
	deferred map[string]string // name -> hint override
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool), deferred: make(map[string]string)}
}

// Register adds a tool, overwriting any existing tool with the same name.
func (r *Registry) Register(t Tool) {
	name := t.Name()
	if _, exists := r.tools[name]; !exists {
		r.order = append(r.order, name)
	}
	r.tools[name] = t
}

// Get returns the tool with the given name.
func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// All returns the tools in registration order.
func (r *Registry) All() []Tool {
	out := make([]Tool, 0, len(r.order))
	for _, name := range r.order {
		out = append(out, r.tools[name])
	}
	return out
}

// Names returns the tool names in registration order.
func (r *Registry) Names() []string {
	out := make([]string, len(r.order))
	copy(out, r.order)
	return out
}

// APIDefinitions builds the llm.ToolDef list for all registered tools.
func (r *Registry) APIDefinitions() []llm.ToolDef {
	defs := make([]llm.ToolDef, 0, len(r.order))
	for _, name := range r.order {
		defs = append(defs, r.toolDef(name))
	}
	return defs
}

func (r *Registry) toolDef(name string) llm.ToolDef {
	t := r.tools[name]
	return llm.ToolDef{
		Name:        t.Name(),
		Description: t.Description(),
		InputSchema: t.InputSchema(),
	}
}

// SetDeferred marks an already-registered (or future) tool as deferred with the
// given search hint. Deferred tools are advertised to the model as name + hint
// only until discovered via ToolSearch. This overrides a tool's own
// ShouldDefer/SearchHint.
//
// The name is not required to be registered yet, so SetDeferred may be called
// before Register. Use DeferredUnknown after wiring to catch typos.
func (r *Registry) SetDeferred(name, hint string) {
	r.deferred[name] = hint
}

// DeferredUnknown returns the SetDeferred names that match no registered tool —
// almost always a typo in the name passed to SetDeferred. It returns a sorted
// slice; an empty result means every deferred override targets a real tool.
func (r *Registry) DeferredUnknown() []string {
	var out []string
	for name := range r.deferred {
		if _, ok := r.tools[name]; !ok {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// deferralHint reports whether a tool is deferred and its hint. An explicit
// SetDeferred override wins; otherwise a tool implementing DeferrableTool with
// ShouldDefer()==true is deferred using its SearchHint().
func (r *Registry) deferralHint(name string) (string, bool) {
	if hint, ok := r.deferred[name]; ok {
		return hint, true
	}
	if d, ok := r.tools[name].(DeferrableTool); ok && d.ShouldDefer() {
		return d.SearchHint(), true
	}
	return "", false
}

// SearchHints returns the name->hint map of every deferred tool.
func (r *Registry) SearchHints() map[string]string {
	out := make(map[string]string)
	for _, name := range r.order {
		if hint, ok := r.deferralHint(name); ok {
			out[name] = hint
		}
	}
	return out
}

// VisibleDefinitions is like APIDefinitions but omits deferred tools that d has
// not revealed yet. A nil Discovery yields the same result as APIDefinitions.
func (r *Registry) VisibleDefinitions(d *Discovery) []llm.ToolDef {
	if d == nil {
		return r.APIDefinitions()
	}
	defs := make([]llm.ToolDef, 0, len(r.order))
	for _, name := range r.order {
		if _, deferred := r.deferralHint(name); deferred && !d.Revealed(name) {
			continue
		}
		defs = append(defs, r.toolDef(name))
	}
	return defs
}

// DeferredReminder builds a system-reminder block listing the deferred tools
// that d has not revealed yet, instructing the model to load their schema via
// ToolSearch before use. Returns "" when nothing is hidden.
func (r *Registry) DeferredReminder(d *Discovery) string {
	var names []string
	for _, name := range r.order {
		if _, deferred := r.deferralHint(name); !deferred {
			continue
		}
		if d != nil && d.Revealed(name) {
			continue
		}
		names = append(names, name)
	}
	if len(names) == 0 {
		return ""
	}
	sort.Strings(names)

	var b strings.Builder
	b.WriteString("<system-reminder>\n")
	b.WriteString("The following tools are available but their full schemas are not loaded. ")
	b.WriteString("Call the ToolSearch tool with a query to load a tool's schema before using it.\n")
	for _, name := range names {
		hint, _ := r.deferralHint(name)
		if hint != "" {
			b.WriteString("- ")
			b.WriteString(name)
			b.WriteString(": ")
			b.WriteString(hint)
			b.WriteByte('\n')
		} else {
			b.WriteString("- ")
			b.WriteString(name)
			b.WriteByte('\n')
		}
	}
	b.WriteString("</system-reminder>")
	return b.String()
}
