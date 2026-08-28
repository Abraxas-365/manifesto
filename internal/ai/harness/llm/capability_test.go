package llm

import (
	"os"
	"testing"

	"github.com/Abraxas-365/manifesto/internal/ai/models"
)

func TestMain(m *testing.M) {
	models.SetGlobalCache(models.NewDefaultCache())
	os.Exit(m.Run())
}

func TestCapabilitiesUnknownModelPermissiveDefault(t *testing.T) {
	c := Capabilities("some-random-model")
	if !c.SupportsTemperature {
		t.Errorf("unknown model should support temperature by default")
	}
	if c.SupportsReasoning {
		t.Errorf("unknown model should not support reasoning by default")
	}
}

func TestCapabilitiesReasoningModel(t *testing.T) {
	c := Capabilities("claude-sonnet-5")
	if !c.SupportsReasoning {
		t.Errorf("claude-sonnet-5 should support reasoning")
	}
	if !c.SupportsTemperature {
		t.Errorf("claude-sonnet-5 should support temperature")
	}
}

func TestCapabilitiesInferredClaude(t *testing.T) {
	// A claude model not in the cache should be inferred as reasoning-capable.
	c := Capabilities("claude-sonnet-99-turbo")
	if !c.SupportsReasoning {
		t.Errorf("inferred claude model should support reasoning")
	}
	if !c.SupportsTemperature {
		t.Errorf("inferred claude model should support temperature")
	}
	if c.ReasoningMap[ReasoningHigh] != "high" {
		t.Errorf("inferred claude should use effort map, got %q", c.ReasoningMap[ReasoningHigh])
	}
}

func TestCapabilitiesInferredOpenAI(t *testing.T) {
	c := Capabilities("o3-mega-2026")
	if !c.SupportsReasoning {
		t.Errorf("inferred o3 model should support reasoning")
	}
	if c.SupportsTemperature {
		t.Errorf("inferred OpenAI reasoning model should not support temperature")
	}
}

func TestClampReasoningAnthropicEffort(t *testing.T) {
	native, ok := ClampReasoning("claude-sonnet-4-5-20250514", ReasoningHigh)
	if !ok || native != "high" {
		t.Errorf("claude-sonnet-4-5 high => (%q,%v), want (\"high\",true)", native, ok)
	}
}

func TestClampReasoningOpenAIEffort(t *testing.T) {
	native, ok := ClampReasoning("o3", ReasoningMedium)
	if !ok || native != "medium" {
		t.Errorf("o3 medium => (%q,%v), want (\"medium\",true)", native, ok)
	}
}

func TestClampReasoningNonReasoningModel(t *testing.T) {
	if _, ok := ClampReasoning("gpt-4o", ReasoningHigh); ok {
		t.Errorf("non-reasoning model should not resolve a reasoning value")
	}
}

func TestClampReasoningNoneRequested(t *testing.T) {
	if _, ok := ClampReasoning("o3", ReasoningNone); ok {
		t.Errorf("ReasoningNone should never resolve")
	}
}

func TestClampReasoningInferredModel(t *testing.T) {
	// Unknown claude model — should infer reasoning and return effort string.
	native, ok := ClampReasoning("claude-opus-99", ReasoningMedium)
	if !ok || native != "medium" {
		t.Errorf("inferred claude-opus-99 medium => (%q,%v), want (\"medium\",true)", native, ok)
	}
}
