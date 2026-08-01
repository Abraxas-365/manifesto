package models

// ThinkingLevel is a provider-agnostic reasoning effort knob. A single level is
// mapped per-model (via ModelCapability.ThinkingLevelMap) to each provider's own
// mechanism: Anthropic effort/budget, OpenAI reasoning_effort, Gemini thinkingBudget.
type ThinkingLevel string

const (
	ThinkingOff     ThinkingLevel = "off"
	ThinkingMinimal ThinkingLevel = "minimal"
	ThinkingLow     ThinkingLevel = "low"
	ThinkingMedium  ThinkingLevel = "medium"
	ThinkingHigh    ThinkingLevel = "high"
	ThinkingXHigh   ThinkingLevel = "xhigh"
	ThinkingMax     ThinkingLevel = "max"
)

// thinkingOrder ranks levels from least to most reasoning. Used to clamp a
// requested level down to the nearest level the model actually supports.
var thinkingOrder = []ThinkingLevel{
	ThinkingOff, ThinkingMinimal, ThinkingLow, ThinkingMedium, ThinkingHigh, ThinkingXHigh, ThinkingMax,
}

func levelRank(l ThinkingLevel) int {
	for i, v := range thinkingOrder {
		if v == l {
			return i
		}
	}
	return 0 // unknown → off
}

// ClampThinkingLevel returns the highest level <= the requested one that the model
// supports. A model supports a level if its ThinkingLevelMap has a non-empty mapping
// for it. A model with no ThinkingLevelMap supports nothing → clamps to "off".
func ClampThinkingLevel(c *Cache, model string, requested ThinkingLevel) ThinkingLevel {
	m := c.ThinkingMap(model)
	if len(m) == 0 {
		return ThinkingOff
	}
	for i := levelRank(requested); i >= 0; i-- {
		lvl := thinkingOrder[i]
		if lvl == ThinkingOff {
			return ThinkingOff
		}
		if v, ok := m[string(lvl)]; ok && v != "" {
			return lvl
		}
	}
	return ThinkingOff
}

// ResolveProviderReasoning clamps the requested level for the model and returns the
// provider-specific value it maps to (an effort name like "high"/"max", an OpenAI
// reasoning_effort, or a Gemini thinkingBudget token count as a string). The mapped
// value is "" when reasoning is off or unsupported. The clamped level is also returned
// so callers can display/persist what was actually applied.
func ResolveProviderReasoning(c *Cache, model string, requested ThinkingLevel) (mapped string, clamped ThinkingLevel) {
	clamped = ClampThinkingLevel(c, model, requested)
	if clamped == ThinkingOff {
		return "", ThinkingOff
	}
	m := c.ThinkingMap(model)
	return m[string(clamped)], clamped
}
