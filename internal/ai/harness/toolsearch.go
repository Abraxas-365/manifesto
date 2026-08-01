package harness

import (
	"fmt"
	"strings"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/tool"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/toolsearch"
)

// EnableToolSearch turns on deferred-tool discovery. It creates a shared
// Discovery, registers a ToolSearch tool bound to this agent's registry, and
// makes Run send deferred tools to the model as name+hint (in a system-reminder)
// until the model loads their schema via ToolSearch.
//
// Defer specific tools with a.Registry.SetDeferred(name, hint), or by having a
// tool embed tool.Deferrable. When no tools are deferred this is a no-op beyond
// registering the (unused) ToolSearch tool. Returns the agent for chaining.
func (a *Agent) EnableToolSearch() *Agent {
	if unknown := a.Registry.DeferredUnknown(); len(unknown) > 0 {
		panic(fmt.Sprintf("harness: SetDeferred targets unregistered tool(s): %s "+
			"(check for a typo or register the tool first)", strings.Join(unknown, ", ")))
	}
	a.discovery = tool.NewDiscovery()
	a.Registry.Register(&toolsearch.Tool{Registry: a.Registry, Discovery: a.discovery})
	return a
}

// UseDiscovery attaches an existing Discovery without registering a new
// ToolSearch tool. Use it when many agents share one registry (session
// factories): register a single toolsearch.Tool bound to the shared Discovery
// once, then attach the same Discovery to each agent here.
func (a *Agent) UseDiscovery(d *tool.Discovery) *Agent {
	a.discovery = d
	return a
}
