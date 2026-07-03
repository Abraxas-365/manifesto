package anthropic

import (
	"testing"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm"
)

func floatPtr(f float64) *float64 { return &f }

func TestBuildParamsTemperatureOmittedWhenNil(t *testing.T) {
	p := New("k")
	params, err := p.buildParams(llm.Request{Model: "claude-sonnet-4-20250514"})
	if err != nil {
		t.Fatal(err)
	}
	if params.Temperature.Valid() {
		t.Errorf("nil Temperature should be omitted")
	}
}

func TestBuildParamsTemperatureSet(t *testing.T) {
	p := New("k")
	params, err := p.buildParams(llm.Request{Model: "claude-sonnet-4-20250514", Temperature: floatPtr(0.3)})
	if err != nil {
		t.Fatal(err)
	}
	if !params.Temperature.Valid() || params.Temperature.Value != 0.3 {
		t.Errorf("temperature = %v, want 0.3", params.Temperature)
	}
}

func TestBuildParamsTopP(t *testing.T) {
	p := New("k")
	params, err := p.buildParams(llm.Request{Model: "claude-sonnet-4-20250514", TopP: floatPtr(0.8)})
	if err != nil {
		t.Fatal(err)
	}
	if !params.TopP.Valid() || params.TopP.Value != 0.8 {
		t.Errorf("top_p = %v, want 0.8", params.TopP)
	}
}

func TestBuildParamsReasoningBudget(t *testing.T) {
	p := New("k")
	params, err := p.buildParams(llm.Request{Model: "claude-sonnet-4-20250514", Reasoning: llm.ReasoningMedium})
	if err != nil {
		t.Fatal(err)
	}
	budget := params.Thinking.GetBudgetTokens()
	if budget == nil || *budget != 8192 {
		t.Errorf("thinking budget = %v, want 8192", budget)
	}
}

func TestBuildParamsTemperatureOmittedWhenThinking(t *testing.T) {
	p := New("k")
	params, err := p.buildParams(llm.Request{
		Model:       "claude-sonnet-4-20250514",
		Temperature: floatPtr(0.3),
		Reasoning:   llm.ReasoningMedium,
	})
	if err != nil {
		t.Fatal(err)
	}
	if params.Temperature.Valid() {
		t.Errorf("temperature must be omitted while thinking is enabled")
	}
}

func TestBuildParamsReasoningOmittedForNonReasoningModel(t *testing.T) {
	p := New("k")
	params, err := p.buildParams(llm.Request{Model: "claude-3-haiku-20240307", Reasoning: llm.ReasoningHigh})
	if err != nil {
		t.Fatal(err)
	}
	if params.Thinking.GetBudgetTokens() != nil {
		t.Errorf("non-reasoning model should omit thinking")
	}
}

func TestProviderOptsSortedAndScoped(t *testing.T) {
	req := llm.Request{Provider: map[string]map[string]any{
		"anthropic": {"foo": 1, "bar": 2},
		"openai":    {"ignored": true},
	}}
	opts := providerOpts(req)
	if len(opts) != 2 {
		t.Fatalf("expected 2 opts for anthropic bag, got %d", len(opts))
	}
}

func TestProviderOptsEmpty(t *testing.T) {
	if opts := providerOpts(llm.Request{}); opts != nil {
		t.Errorf("no bag should yield nil opts, got %v", opts)
	}
}
