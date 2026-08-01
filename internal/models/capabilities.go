// Package models provides model capabilities caching and lookup.
package models

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
)

// ModelCapability describes a model's capabilities and limits.
type ModelCapability struct {
	ID               string `json:"id"`
	MaxInputTokens   int    `json:"max_input_tokens"`
	MaxOutputTokens  int    `json:"max_output_tokens"`
	SupportsThinking bool   `json:"supports_thinking"`
	SupportsEffort   bool   `json:"supports_effort"`
	Source           string `json:"source,omitempty"` // "default", "lua", "cache"
	CompactFunc      any    `json:"-"`                // Lua function ref, nil = use default compaction

	// Provider/wire metadata (all omitempty so existing cache JSON + Lua overrides stay valid).
	Provider string   `json:"provider,omitempty"` // "anthropic","google","bedrock","vertex","openai","groq",...
	APIKind  string   `json:"api_kind,omitempty"` // "anthropic-messages","openai-completions","openai-responses","gemini","bedrock-converse"
	Input    []string `json:"input,omitempty"`    // modalities accepted: ["text"] or ["text","image"] (vision flag)

	// Cost merges what used to live in api/usage.go's modelPricing map so there is one
	// declarative source of truth. nil = fall back to the hardcoded pricing table.
	Cost *ModelCost `json:"cost,omitempty"`

	// ThinkingLevelMap maps a unified reasoning level (off/minimal/low/medium/high/xhigh)
	// to this provider's mechanism: an effort name ("low"/"high"/"max"), a Gemini/Anthropic
	// token budget ("8192"), or "null"/"" meaning the level is unsupported (clamp downward).
	ThinkingLevelMap map[string]string `json:"thinking_level_map,omitempty"`
}

// ModelCost is the per-million-token price of a model in USD.
type ModelCost struct {
	InputPerMillion      float64 `json:"input"`
	OutputPerMillion     float64 `json:"output"`
	CacheReadPerMillion  float64 `json:"cache_read"`
	CacheWritePerMillion float64 `json:"cache_write"`
}

// Cache holds cached model capabilities loaded from disk.
type Cache struct {
	mu     sync.RWMutex
	models []ModelCapability
}

// global cache instance
var (
	globalCache   *Cache
	globalCacheMu sync.RWMutex
)

// SetGlobalCache sets the global model capabilities cache.
func SetGlobalCache(c *Cache) {
	globalCacheMu.Lock()
	globalCache = c
	globalCacheMu.Unlock()
}

// GetGlobalCache returns the global cache, lazily initializing it with the
// hardcoded defaults on first use.
func GetGlobalCache() *Cache {
	globalCacheMu.RLock()
	c := globalCache
	globalCacheMu.RUnlock()
	if c != nil {
		return c
	}
	globalCacheMu.Lock()
	defer globalCacheMu.Unlock()
	if globalCache == nil {
		globalCache = NewDefaultCache()
	}
	return globalCache
}

// LoadCache reads model capabilities from a JSON file.
// Returns an empty cache if the file doesn't exist or is invalid.
func LoadCache(path string) *Cache {
	c := &Cache{}

	data, err := os.ReadFile(path)
	if err != nil {
		return c
	}

	var models []ModelCapability
	if err := json.Unmarshal(data, &models); err != nil {
		return c
	}

	c.models = models
	return c
}

// MaxContext returns the maximum input tokens for the given model.
// Uses longest-ID-match strategy. Returns 0 if not found.
func (c *Cache) MaxContext(model string) int {
	if c == nil {
		return 0
	}
	c.mu.RLock()
	defer c.mu.RUnlock()

	cap := c.findBestMatch(model)
	if cap != nil {
		return cap.MaxInputTokens
	}
	return 0
}

// MaxOutput returns the maximum output tokens for the given model.
// Returns 0 if not found.
func (c *Cache) MaxOutput(model string) int {
	if c == nil {
		return 0
	}
	c.mu.RLock()
	defer c.mu.RUnlock()

	cap := c.findBestMatch(model)
	if cap != nil {
		return cap.MaxOutputTokens
	}
	return 0
}

// SupportsThinking returns whether the model supports extended thinking.
func (c *Cache) SupportsThinking(model string) bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()

	cap := c.findBestMatch(model)
	if cap != nil {
		return cap.SupportsThinking
	}
	return false
}

// SupportsVision returns whether the model accepts image input.
func (c *Cache) SupportsVision(model string) bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if cap := c.findBestMatch(model); cap != nil {
		for _, m := range cap.Input {
			if m == "image" {
				return true
			}
		}
	}
	return false
}

// ProviderFor returns the provider key for the model (e.g. "anthropic","google"), or "".
func (c *Cache) ProviderFor(model string) string {
	if c == nil {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if cap := c.findBestMatch(model); cap != nil {
		return cap.Provider
	}
	return ""
}

// APIKindFor returns the wire-format kind for the model, or "".
func (c *Cache) APIKindFor(model string) string {
	if c == nil {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if cap := c.findBestMatch(model); cap != nil {
		return cap.APIKind
	}
	return ""
}

// ThinkingMap returns the model's unified-level → provider-mechanism map, or nil.
func (c *Cache) ThinkingMap(model string) map[string]string {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if cap := c.findBestMatch(model); cap != nil {
		return cap.ThinkingLevelMap
	}
	return nil
}

// CostFor returns the model's declared cost, or nil if none is registered.
func (c *Cache) CostFor(model string) *ModelCost {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if cap := c.findBestMatch(model); cap != nil {
		return cap.Cost
	}
	return nil
}

// Upsert inserts or merges a model capability. Merge semantics: only non-zero
// fields in cap overwrite existing values. Source "lua" always wins over "default".
// CompactFunc is always overwritten if non-nil.
func (c *Cache) Upsert(cap ModelCapability) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for i := range c.models {
		if strings.EqualFold(c.models[i].ID, cap.ID) {
			c.mergeInto(&c.models[i], cap)
			return
		}
	}
	// Not found — insert as-is
	c.models = append(c.models, cap)
}

// mergeInto applies non-zero fields from src onto dst.
func (c *Cache) mergeInto(dst *ModelCapability, src ModelCapability) {
	luaOverride := src.Source == "lua" && dst.Source != "lua"

	if src.MaxInputTokens != 0 || luaOverride {
		if src.MaxInputTokens != 0 {
			dst.MaxInputTokens = src.MaxInputTokens
		}
	}
	if src.MaxOutputTokens != 0 || luaOverride {
		if src.MaxOutputTokens != 0 {
			dst.MaxOutputTokens = src.MaxOutputTokens
		}
	}
	if src.SupportsThinking || luaOverride {
		dst.SupportsThinking = src.SupportsThinking
	}
	if src.SupportsEffort || luaOverride {
		dst.SupportsEffort = src.SupportsEffort
	}
	if src.Source != "" {
		dst.Source = src.Source
	}
	if src.CompactFunc != nil {
		dst.CompactFunc = src.CompactFunc
	}
	if src.Provider != "" || luaOverride {
		if src.Provider != "" {
			dst.Provider = src.Provider
		}
	}
	if src.APIKind != "" || luaOverride {
		if src.APIKind != "" {
			dst.APIKind = src.APIKind
		}
	}
	if len(src.Input) > 0 || luaOverride {
		if len(src.Input) > 0 {
			dst.Input = src.Input
		}
	}
	if src.Cost != nil {
		dst.Cost = src.Cost
	}
	if len(src.ThinkingLevelMap) > 0 || luaOverride {
		if len(src.ThinkingLevelMap) > 0 {
			dst.ThinkingLevelMap = src.ThinkingLevelMap
		}
	}
}

// Get returns the full capability for a model, or nil if not found.
// Uses the same longest-match strategy as MaxContext.
func (c *Cache) Get(model string) *ModelCapability {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()

	match := c.findBestMatch(model)
	if match == nil {
		return nil
	}
	// Return a copy to avoid callers mutating internal state.
	cp := *match
	return &cp
}

// List returns all cached model capabilities.
func (c *Cache) List() []ModelCapability {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make([]ModelCapability, len(c.models))
	copy(result, c.models)
	return result
}

// findBestMatch finds the cached model with the longest matching ID.
// Uses substring matching: model "claude-opus-4-8" matches cache entry "opus-4-6".
func (c *Cache) findBestMatch(model string) *ModelCapability {
	modelLower := strings.ToLower(model)

	var best *ModelCapability
	bestLen := 0

	for i := range c.models {
		idLower := strings.ToLower(c.models[i].ID)
		if strings.Contains(modelLower, idLower) || strings.Contains(idLower, modelLower) {
			if len(c.models[i].ID) > bestLen {
				best = &c.models[i]
				bestLen = len(c.models[i].ID)
			}
		}
	}

	return best
}

// Reasoning-level maps shared across model families. Keys are the unified levels
// (off/minimal/low/medium/high/xhigh); "" means the level is unsupported (clamp down).
var (
	// anthropicEffortMap: claude models with adaptive effort (Opus 4.7+).
	// output_config.effort accepts low/medium/high/xhigh/max.
	anthropicEffortMap = map[string]string{
		"off": "", "minimal": "low", "low": "low", "medium": "medium", "high": "high", "xhigh": "xhigh", "max": "max",
	}
	// openAIEffortMap: OpenAI reasoning_effort (o-series, gpt-5).
	openAIEffortMap = map[string]string{
		"off": "", "minimal": "minimal", "low": "low", "medium": "medium", "high": "high", "xhigh": "high", "max": "high",
	}
	// gpt56EffortMap: GPT-5.6 family (Sol/Terra/Luna) reasoning_effort — unlike
	// earlier GPT-5.x, these natively support distinct xhigh and max tiers.
	gpt56EffortMap = map[string]string{
		"off": "", "minimal": "low", "low": "low", "medium": "medium", "high": "high", "xhigh": "xhigh", "max": "max",
	}
	// geminiBudgetMap: Gemini thinkingBudget tokens (-1 = dynamic).
	geminiBudgetMap = map[string]string{
		"off": "0", "minimal": "1024", "low": "4096", "medium": "8192", "high": "16384", "xhigh": "24576", "max": "32768",
	}
)

func textOnly() []string  { return []string{"text"} }
func textImage() []string { return []string{"text", "image"} }

// DefaultCapabilities returns hardcoded defaults for known models.
func DefaultCapabilities() []ModelCapability {
	return []ModelCapability{
		// Anthropic
		{ID: "claude-opus-5", MaxInputTokens: 200_000, MaxOutputTokens: 128_000, SupportsThinking: true, SupportsEffort: true, Source: "default", Provider: "anthropic", APIKind: "anthropic-messages", Input: textImage(), ThinkingLevelMap: anthropicEffortMap, Cost: &ModelCost{5.0, 25.0, 0.5, 6.25}},
		{ID: "claude-opus-4-8", MaxInputTokens: 200_000, MaxOutputTokens: 128_000, SupportsThinking: true, SupportsEffort: true, Source: "default", Provider: "anthropic", APIKind: "anthropic-messages", Input: textImage(), ThinkingLevelMap: anthropicEffortMap, Cost: &ModelCost{15.0, 75.0, 1.5, 18.75}},
		{ID: "claude-opus-4-7", MaxInputTokens: 200_000, MaxOutputTokens: 128_000, SupportsThinking: true, SupportsEffort: true, Source: "default", Provider: "anthropic", APIKind: "anthropic-messages", Input: textImage(), ThinkingLevelMap: anthropicEffortMap, Cost: &ModelCost{15.0, 75.0, 1.5, 18.75}},
		{ID: "claude-opus-4-6", MaxInputTokens: 200_000, MaxOutputTokens: 128_000, SupportsThinking: true, SupportsEffort: true, Source: "default", Provider: "anthropic", APIKind: "anthropic-messages", Input: textImage(), ThinkingLevelMap: anthropicEffortMap, Cost: &ModelCost{15.0, 75.0, 1.5, 18.75}},
		{ID: "claude-opus-4-5", MaxInputTokens: 200_000, MaxOutputTokens: 64_000, SupportsThinking: true, SupportsEffort: true, Source: "default", Provider: "anthropic", APIKind: "anthropic-messages", Input: textImage(), ThinkingLevelMap: anthropicEffortMap, Cost: &ModelCost{15.0, 75.0, 1.5, 18.75}},
		{ID: "claude-sonnet-5", MaxInputTokens: 200_000, MaxOutputTokens: 128_000, SupportsThinking: true, SupportsEffort: true, Source: "default", Provider: "anthropic", APIKind: "anthropic-messages", Input: textImage(), ThinkingLevelMap: anthropicEffortMap, Cost: &ModelCost{3.0, 15.0, 0.3, 3.75}},
		{ID: "claude-sonnet-4-5", MaxInputTokens: 200_000, MaxOutputTokens: 64_000, SupportsThinking: true, SupportsEffort: true, Source: "default", Provider: "anthropic", APIKind: "anthropic-messages", Input: textImage(), ThinkingLevelMap: anthropicEffortMap, Cost: &ModelCost{3.0, 15.0, 0.3, 3.75}},
		{ID: "claude-sonnet-4", MaxInputTokens: 200_000, MaxOutputTokens: 64_000, SupportsThinking: true, SupportsEffort: true, Source: "default", Provider: "anthropic", APIKind: "anthropic-messages", Input: textImage(), ThinkingLevelMap: anthropicEffortMap, Cost: &ModelCost{3.0, 15.0, 0.3, 3.75}},
		{ID: "claude-fable-5", MaxInputTokens: 200_000, MaxOutputTokens: 128_000, SupportsThinking: true, SupportsEffort: true, Source: "default", Provider: "anthropic", APIKind: "anthropic-messages", Input: textImage(), ThinkingLevelMap: anthropicEffortMap, Cost: &ModelCost{3.0, 15.0, 0.3, 3.75}},
		{ID: "claude-opus-4", MaxInputTokens: 200_000, MaxOutputTokens: 128_000, SupportsThinking: true, SupportsEffort: true, Source: "default", Provider: "anthropic", APIKind: "anthropic-messages", Input: textImage(), ThinkingLevelMap: anthropicEffortMap, Cost: &ModelCost{15.0, 75.0, 1.5, 18.75}},
		{ID: "claude-haiku-4-5", MaxInputTokens: 200_000, MaxOutputTokens: 64_000, SupportsThinking: false, SupportsEffort: false, Source: "default", Provider: "anthropic", APIKind: "anthropic-messages", Input: textImage(), Cost: &ModelCost{0.25, 1.25, 0.025, 0.3125}},
		// Google Gemini (native)
		{ID: "gemini-2.5-pro", MaxInputTokens: 200_000, MaxOutputTokens: 65_536, SupportsThinking: true, SupportsEffort: false, Source: "default", Provider: "google", APIKind: "gemini", Input: textImage(), ThinkingLevelMap: geminiBudgetMap, Cost: &ModelCost{1.25, 10.0, 0.31, 0}},
		{ID: "gemini-2.5-flash", MaxInputTokens: 200_000, MaxOutputTokens: 65_536, SupportsThinking: true, SupportsEffort: false, Source: "default", Provider: "google", APIKind: "gemini", Input: textImage(), ThinkingLevelMap: geminiBudgetMap, Cost: &ModelCost{0.30, 2.50, 0.075, 0}},
		// Groq (Llama)
		{ID: "llama-3.3-70b-versatile", MaxInputTokens: 128_000, MaxOutputTokens: 32_768, SupportsThinking: false, SupportsEffort: false, Source: "default", Provider: "groq", APIKind: "openai-completions", Input: textOnly()},
		{ID: "llama-3.1-8b-instant", MaxInputTokens: 128_000, MaxOutputTokens: 8_192, SupportsThinking: false, SupportsEffort: false, Source: "default", Provider: "groq", APIKind: "openai-completions", Input: textOnly()},
		{ID: "llama-3.2-90b-vision-preview", MaxInputTokens: 128_000, MaxOutputTokens: 8_192, SupportsThinking: false, SupportsEffort: false, Source: "default", Provider: "groq", APIKind: "openai-completions", Input: textImage()},
		// Groq (Mixtral)
		{ID: "mixtral-8x7b-32768", MaxInputTokens: 32_768, MaxOutputTokens: 32_768, SupportsThinking: false, SupportsEffort: false, Source: "default", Provider: "groq", APIKind: "openai-completions", Input: textOnly()},
		// Groq (Gemma)
		{ID: "gemma2-9b-it", MaxInputTokens: 8_192, MaxOutputTokens: 8_192, SupportsThinking: false, SupportsEffort: false, Source: "default", Provider: "groq", APIKind: "openai-completions", Input: textOnly()},
		// OpenAI
		{ID: "gpt-4o", MaxInputTokens: 128_000, MaxOutputTokens: 16_384, SupportsThinking: false, SupportsEffort: false, Source: "default", Provider: "openai", APIKind: "openai-completions", Input: textImage(), Cost: &ModelCost{2.5, 10.0, 1.25, 0}},
		{ID: "gpt-4o-mini", MaxInputTokens: 128_000, MaxOutputTokens: 16_384, SupportsThinking: false, SupportsEffort: false, Source: "default", Provider: "openai", APIKind: "openai-completions", Input: textImage(), Cost: &ModelCost{0.15, 0.60, 0.075, 0}},
		{ID: "gpt-4-turbo", MaxInputTokens: 128_000, MaxOutputTokens: 4_096, SupportsThinking: false, SupportsEffort: false, Source: "default", Provider: "openai", APIKind: "openai-completions", Input: textImage()},
		{ID: "gpt-5", MaxInputTokens: 200_000, MaxOutputTokens: 128_000, SupportsThinking: true, SupportsEffort: true, Source: "default", Provider: "openai", APIKind: "openai-responses", Input: textImage(), ThinkingLevelMap: openAIEffortMap, Cost: &ModelCost{1.25, 10.0, 0.125, 0}},
		// GPT-5.6 family (Sol/Terra/Luna), released July 9, 2026. "gpt-5.6" is an
		// alias for the flagship Sol model. Pricing/output specs from
		// platform.openai.com/docs/pricing; context window capped at 200k (results
		// degrade past that in practice) instead of the raw 1.05M window.
		{ID: "gpt-5.6-sol", MaxInputTokens: 200_000, MaxOutputTokens: 128_000, SupportsThinking: true, SupportsEffort: true, Source: "default", Provider: "openai", APIKind: "openai-responses", Input: textImage(), ThinkingLevelMap: gpt56EffortMap, Cost: &ModelCost{5.0, 30.0, 0.5, 6.25}},
		{ID: "gpt-5.6", MaxInputTokens: 200_000, MaxOutputTokens: 128_000, SupportsThinking: true, SupportsEffort: true, Source: "default", Provider: "openai", APIKind: "openai-responses", Input: textImage(), ThinkingLevelMap: gpt56EffortMap, Cost: &ModelCost{5.0, 30.0, 0.5, 6.25}},
		{ID: "gpt-5.6-terra", MaxInputTokens: 200_000, MaxOutputTokens: 128_000, SupportsThinking: true, SupportsEffort: true, Source: "default", Provider: "openai", APIKind: "openai-responses", Input: textImage(), ThinkingLevelMap: gpt56EffortMap, Cost: &ModelCost{2.5, 15.0, 0.25, 3.125}},
		{ID: "gpt-5.6-luna", MaxInputTokens: 200_000, MaxOutputTokens: 128_000, SupportsThinking: true, SupportsEffort: true, Source: "default", Provider: "openai", APIKind: "openai-responses", Input: textImage(), ThinkingLevelMap: gpt56EffortMap, Cost: &ModelCost{1.0, 6.0, 0.1, 1.25}},
		{ID: "o3", MaxInputTokens: 200_000, MaxOutputTokens: 100_000, SupportsThinking: true, SupportsEffort: true, Source: "default", Provider: "openai", APIKind: "openai-responses", Input: textImage(), ThinkingLevelMap: openAIEffortMap, Cost: &ModelCost{2.0, 8.0, 0.5, 0}},
		{ID: "o1", MaxInputTokens: 200_000, MaxOutputTokens: 100_000, SupportsThinking: true, SupportsEffort: true, Source: "default", Provider: "openai", APIKind: "openai-responses", Input: textImage(), ThinkingLevelMap: openAIEffortMap, Cost: &ModelCost{15.0, 60.0, 7.5, 0}},
		{ID: "o1-mini", MaxInputTokens: 128_000, MaxOutputTokens: 65_536, SupportsThinking: false, SupportsEffort: false, Source: "default", Provider: "openai", APIKind: "openai-completions", Input: textOnly()},
	}
}

// NewDefaultCache creates a cache pre-populated with hardcoded defaults.
func NewDefaultCache() *Cache {
	return &Cache{models: DefaultCapabilities()}
}
