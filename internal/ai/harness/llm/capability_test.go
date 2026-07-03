package llm

import "testing"

func TestCapabilitiesUnknownModelPermissiveDefault(t *testing.T) {
	c := Capabilities("some-random-model")
	if !c.SupportsTemperature {
		t.Errorf("unknown model should support temperature by default")
	}
	if c.SupportsReasoning {
		t.Errorf("unknown model should not support reasoning by default")
	}
}

func TestCapabilitiesLongestSubstringWins(t *testing.T) {
	// "gpt-5" is a reasoning family; ensure it matches within a fuller id.
	c := Capabilities("gpt-5-mini-2025")
	if !c.SupportsReasoning {
		t.Errorf("gpt-5 family should support reasoning")
	}
	if c.SupportsTemperature {
		t.Errorf("gpt-5 reasoning family should not support temperature")
	}
}

func TestClampReasoningOpenAIEffort(t *testing.T) {
	native, ok := ClampReasoning("o3-mini", ReasoningMedium)
	if !ok || native != "medium" {
		t.Errorf("o3 medium => (%q,%v), want (\"medium\",true)", native, ok)
	}
}

func TestClampReasoningAnthropicBudget(t *testing.T) {
	native, ok := ClampReasoning("claude-sonnet-4-20250514", ReasoningHigh)
	if !ok || native != "16384" {
		t.Errorf("claude-sonnet-4 high => (%q,%v), want (\"16384\",true)", native, ok)
	}
}

func TestClampReasoningNonReasoningModel(t *testing.T) {
	if _, ok := ClampReasoning("gpt-4o", ReasoningHigh); ok {
		t.Errorf("non-reasoning model should not resolve a reasoning value")
	}
}

func TestClampReasoningNoneRequested(t *testing.T) {
	if _, ok := ClampReasoning("o3-mini", ReasoningNone); ok {
		t.Errorf("ReasoningNone should never resolve")
	}
}
