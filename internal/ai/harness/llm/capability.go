package llm

import "strings"

// Capability describes what request knobs a model supports. It is resolved from
// a small in-code table by Capabilities. The table is intentionally tiny and
// additive: unknown models fall back to a permissive default (temperature on,
// reasoning off).
type Capability struct {
	// SupportsTemperature reports whether the model accepts a temperature.
	// Reasoning models frequently reject it.
	SupportsTemperature bool
	// SupportsReasoning reports whether the model can perform reasoning/thinking.
	SupportsReasoning bool
	// ReasoningMap maps a unified ReasoningLevel to the provider-native value.
	// For OpenAI the value is the reasoning_effort string ("low"/"medium"/...).
	// For Anthropic the value is a thinking budget in tokens (e.g. "8192").
	// A level absent from the map is treated as unsupported (omitted).
	ReasoningMap map[ReasoningLevel]string
}

// openAIEffort maps unified levels to OpenAI reasoning_effort strings.
var openAIEffort = map[ReasoningLevel]string{
	ReasoningMinimal: "minimal",
	ReasoningLow:     "low",
	ReasoningMedium:  "medium",
	ReasoningHigh:    "high",
}

// anthropicBudget maps unified levels to Anthropic thinking budgets (tokens).
var anthropicBudget = map[ReasoningLevel]string{
	ReasoningMinimal: "1024",
	ReasoningLow:     "4096",
	ReasoningMedium:  "8192",
	ReasoningHigh:    "16384",
}

// capEntry is one row in the capability table.
type capEntry struct {
	substr string
	cap    Capability
}

// capTable is matched by longest substring against the lowercased model id.
// Extend this list as new model families gain (or lose) capabilities.
var capTable = []capEntry{
	// OpenAI reasoning families reject temperature.
	{"o1", Capability{SupportsReasoning: true, ReasoningMap: openAIEffort}},
	{"o3", Capability{SupportsReasoning: true, ReasoningMap: openAIEffort}},
	{"o4", Capability{SupportsReasoning: true, ReasoningMap: openAIEffort}},
	{"gpt-5", Capability{SupportsReasoning: true, ReasoningMap: openAIEffort}},
	// Anthropic reasoning models keep temperature (but not alongside thinking).
	{"claude-3-7", Capability{SupportsTemperature: true, SupportsReasoning: true, ReasoningMap: anthropicBudget}},
	{"claude-sonnet-4", Capability{SupportsTemperature: true, SupportsReasoning: true, ReasoningMap: anthropicBudget}},
	{"claude-opus-4", Capability{SupportsTemperature: true, SupportsReasoning: true, ReasoningMap: anthropicBudget}},
}

// defaultCapability is used for models absent from the table.
var defaultCapability = Capability{SupportsTemperature: true, SupportsReasoning: false}

// Capabilities resolves the capability record for a model id using
// longest-substring matching over the built-in table. Unknown models get a
// permissive default (temperature on, reasoning off).
func Capabilities(model string) Capability {
	id := strings.ToLower(model)
	best := -1
	result := defaultCapability
	for _, e := range capTable {
		if strings.Contains(id, e.substr) && len(e.substr) > best {
			best = len(e.substr)
			result = e.cap
		}
	}
	return result
}

// ClampReasoning maps the requested level to the model's provider-native
// reasoning value. It returns ("", false) when the model cannot reason, when no
// level was requested, or when the level is not mapped for that model — in all
// of which cases the adapter should omit reasoning entirely.
func ClampReasoning(model string, want ReasoningLevel) (native string, ok bool) {
	if want == ReasoningNone {
		return "", false
	}
	c := Capabilities(model)
	if !c.SupportsReasoning {
		return "", false
	}
	native, ok = c.ReasoningMap[want]
	return native, ok
}
