package subagent

// Agent-profile support for the Task tool: when a Defs registry is attached,
// the tool accepts an "agent" parameter selecting a named blueprint
// (agentdef.Definition) that controls the subagent's system prompt, model,
// thinking level, tool subset, and turn budget. legacy heritage:
// dynamic registration; markdown agents come from
// agentdef.LoadDir.

import (
	"fmt"
	"strings"

	agent "github.com/Abraxas-365/manifesto/internal/ai/harness"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/agentdef"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/tool"
)

// ApplyProfile configures ag according to def. smallModel is the configured
// small/fast model id ("" = none; "small" then falls back to the parent's
// model).
func ApplyProfile(ag *agent.Agent, def agentdef.Definition, smallModel string) {
	switch def.Model {
	case "", "inherit":
		// keep the factory's model
	case "small":
		if smallModel != "" {
			ag.Model = smallModel
		}
	default:
		ag.Model = def.Model
	}
	if def.Thinking != "" && def.Thinking != "off" {
		ag.Reasoning = llm.ReasoningLevel(def.Thinking)
	}
	if def.SystemPrompt != "" {
		if def.SystemPromptMode == "append" && ag.System != "" {
			ag.System = ag.System + "\n\n" + def.SystemPrompt
		} else {
			ag.System = def.SystemPrompt
		}
	}
	if def.MaxTurns > 0 {
		ag.MaxTurns = def.MaxTurns
	}
	if def.GraceTurns > 0 {
		ag.GraceTurns = def.GraceTurns
	}
	if ag.Registry != nil {
		ag.Registry = SubsetRegistry(ag.Registry, def)
	}
}

// SubsetRegistry returns a registry containing only the tools def permits.
// ReadOnly definitions additionally drop mutating tools.
func SubsetRegistry(full *tool.Registry, def agentdef.Definition) *tool.Registry {
	sub := tool.NewRegistry()
	for _, t := range full.All() {
		if !def.AllowsTool(t.Name()) {
			continue
		}
		if def.ReadOnly && !t.IsReadOnly() {
			continue
		}
		sub.Register(t)
	}
	return sub
}

// rosterDescription renders the exposed agents for the tool description,
// mirroring the Skill tool's "Available skills" listing. names filters the
// roster (per-agent subagent allowlist); nil-safe via Tool.rosterNames.
func rosterDescription(defs *agentdef.Registry, names []string) string {
	allowed := map[string]bool{}
	for _, n := range names {
		allowed[n] = true
	}
	roster := make([]agentdef.Definition, 0, len(names))
	for _, d := range defs.Roster() {
		if allowed[d.Name] {
			roster = append(roster, d)
		}
	}
	if len(roster) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\nAvailable agent types and the tools they have access to:\n")
	for _, d := range roster {
		desc := d.Description
		if desc == "" {
			desc = "(no description)"
		}
		fmt.Fprintf(&b, "- %s: %s\n", d.Name, desc)
	}
	b.WriteString("\nWhen using this tool, specify the agent parameter to select the agent type.")
	return b.String()
}
