package llm

import (
	"log"
	"strings"
	"sync"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/models"
)

// Capability describes what request knobs a model supports.
type Capability struct {
	// SupportsTemperature reports whether the model accepts a temperature.
	// Reasoning models frequently reject it.
	SupportsTemperature bool
	// SupportsReasoning reports whether the model can perform reasoning/thinking.
	SupportsReasoning bool
	// ReasoningMap maps a unified ReasoningLevel to the provider-native value.
	// For OpenAI the value is the reasoning_effort string ("low"/"medium"/...).
	// For Anthropic the value is an effort string ("low"/"medium"/"high") or a
	// thinking budget in tokens (e.g. "8192").
	// A level absent from the map is treated as unsupported (omitted).
	ReasoningMap map[ReasoningLevel]string
	// MaxOutputTokens is the model's maximum output token limit (0 = unknown).
	MaxOutputTokens int
}

// fallback reasoning maps used when a model's ThinkingLevelMap is empty.
var (
	// anthropicEffort matches the effort strings used by modern Anthropic models
	// (adaptive thinking with output_config.effort).
	// Anthropic supports: low, medium, high, xhigh (Opus 4.7+/Sonnet 5/Fable 5), max.
	anthropicEffort = map[ReasoningLevel]string{
		ReasoningMinimal: "low",
		ReasoningLow:     "low",
		ReasoningMedium:  "medium",
		ReasoningHigh:    "high",
		ReasoningXHigh:   "xhigh",
		ReasoningMax:     "max",
	}
	openAIEffort = map[ReasoningLevel]string{
		ReasoningMinimal: "minimal",
		ReasoningLow:     "low",
		ReasoningMedium:  "medium",
		ReasoningHigh:    "high",
		ReasoningXHigh:   "high",
		ReasoningMax:     "high",
	}
)

// defaultCapability is used when the model is completely unknown and no
// provider can be inferred from the name.
var defaultCapability = Capability{SupportsTemperature: true, SupportsReasoning: false}

// inferredWarnings tracks which model IDs have already logged an inference
// warning, so we only warn once per model per session.
var inferredWarnings sync.Map

// Capabilities resolves the capability record for a model from the global
// models.Cache (populated from DefaultCapabilities + models.SetGlobalCache at
// startup). When the model is not in the cache, capabilities are inferred from the
// model name prefix (e.g. "claude-*" → Anthropic reasoning, "o3-*" → OpenAI
// reasoning) so that new models work out of the box without recompiling.
func Capabilities(model string) Capability {
	// Try the models cache first (exact/substring match).
	cache := models.GetGlobalCache()
	if cache != nil {
		if cap := cache.Get(model); cap != nil {
			return fromCacheCap(model, cap)
		}
	}

	// Model not in cache — infer from name prefix.
	return inferFromName(model)
}

// fromCacheCap builds a Capability from a models.Cache entry.
func fromCacheCap(model string, cap *models.ModelCapability) Capability {
	result := Capability{
		SupportsTemperature: true,
		SupportsReasoning:   cap.SupportsThinking,
		MaxOutputTokens:     cap.MaxOutputTokens,
	}

	if !result.SupportsReasoning {
		return result
	}

	// Build ReasoningMap from ThinkingLevelMap if available.
	if len(cap.ThinkingLevelMap) > 0 {
		m := make(map[ReasoningLevel]string, len(cap.ThinkingLevelMap))
		for k, v := range cap.ThinkingLevelMap {
			m[ReasoningLevel(k)] = v
		}
		result.ReasoningMap = m
	} else {
		result.ReasoningMap = inferReasoningMap(model)
	}

	// OpenAI reasoning models reject temperature.
	if cap.Provider == "openai" && result.SupportsReasoning {
		result.SupportsTemperature = false
	}

	return result
}

// inferFromName guesses capabilities from the model name when it's not in the
// cache. This ensures new models (e.g. "claude-sonnet-6") work without
// recompiling or defining them in init.lua — matching pi's approach.
func inferFromName(model string) Capability {
	id := strings.ToLower(model)

	switch {
	case strings.Contains(id, "claude") && claudeLikelyReasoning(id):
		warnInferred(model, "Anthropic")
		return Capability{
			SupportsTemperature: true,
			SupportsReasoning:   true,
			ReasoningMap:        anthropicEffort,
			MaxOutputTokens:     64_000, // conservative default for unknown claude reasoning models
		}

	case strings.Contains(id, "claude"):
		// Known non-reasoning claude families (haiku, older claude-3).
		return Capability{SupportsTemperature: true, MaxOutputTokens: 8192}

	case strings.HasPrefix(id, "o1") || strings.HasPrefix(id, "o3") ||
		strings.HasPrefix(id, "o4") || strings.Contains(id, "gpt-5"):
		warnInferred(model, "OpenAI reasoning")
		return Capability{
			SupportsTemperature: false,
			SupportsReasoning:   true,
			ReasoningMap:        openAIEffort,
			MaxOutputTokens:     16_384,
		}

	default:
		return defaultCapability
	}
}

// claudeLikelyReasoning returns true if a claude model name suggests it supports
// reasoning. Sonnet 4+, Opus 4+, Fable 5+, and claude-3-7 support it. Haiku
// and older claude-3 (not 3-7) do not.
func claudeLikelyReasoning(id string) bool {
	// Families known to support reasoning.
	for _, sub := range []string{"sonnet-4", "sonnet-5", "sonnet-6", "opus-4", "opus-5", "opus-6", "fable-5", "fable-6", "claude-3-7", "claude-3.7"} {
		if strings.Contains(id, sub) {
			return true
		}
	}
	// Families known NOT to support reasoning.
	for _, sub := range []string{"haiku", "claude-3-opus", "claude-3-sonnet", "claude-3.0", "claude-3.5", "claude-2", "claude-1"} {
		if strings.Contains(id, sub) {
			return false
		}
	}
	// Unknown claude family — assume future models support reasoning.
	return true
}

// inferReasoningMap returns a fallback reasoning map based on the model name.
func inferReasoningMap(model string) map[ReasoningLevel]string {
	id := strings.ToLower(model)
	switch {
	case strings.Contains(id, "claude"):
		return anthropicEffort
	case strings.Contains(id, "o1") || strings.Contains(id, "o3") ||
		strings.Contains(id, "o4") || strings.Contains(id, "gpt-5"):
		return openAIEffort
	default:
		return anthropicEffort
	}
}

// warnInferred logs a one-time warning when capabilities are inferred rather
// than looked up from the cache.
func warnInferred(model, provider string) {
	if _, loaded := inferredWarnings.LoadOrStore(model, true); !loaded {
		log.Printf("[warn] model %q not found in registry; inferring %s capabilities. "+
			"Register it in the models cache for accurate settings.", model, provider)
	}
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
