package openai

import (
	"testing"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm"
)

func floatPtr(f float64) *float64 { return &f }

func TestBuildParamsTemperatureOmittedWhenNil(t *testing.T) {
	p := New("k")
	params, err := p.buildParams(llm.Request{Model: "gpt-4o"})
	if err != nil {
		t.Fatal(err)
	}
	if params.Temperature.Valid() {
		t.Errorf("nil Temperature should be omitted")
	}
}

func TestBuildParamsTemperatureSetWhenSupported(t *testing.T) {
	p := New("k")
	params, err := p.buildParams(llm.Request{Model: "gpt-4o", Temperature: floatPtr(0.5)})
	if err != nil {
		t.Fatal(err)
	}
	if !params.Temperature.Valid() || params.Temperature.Value != 0.5 {
		t.Errorf("temperature = %v, want 0.5", params.Temperature)
	}
}

func TestBuildParamsTemperatureOmittedForReasoningModel(t *testing.T) {
	p := New("k")
	params, err := p.buildParams(llm.Request{Model: "o3-mini", Temperature: floatPtr(0.5)})
	if err != nil {
		t.Fatal(err)
	}
	if params.Temperature.Valid() {
		t.Errorf("reasoning model should not accept temperature")
	}
}

func TestBuildParamsTopP(t *testing.T) {
	p := New("k")
	params, err := p.buildParams(llm.Request{Model: "gpt-4o", TopP: floatPtr(0.9)})
	if err != nil {
		t.Fatal(err)
	}
	if !params.TopP.Valid() || params.TopP.Value != 0.9 {
		t.Errorf("top_p = %v, want 0.9", params.TopP)
	}
}

func TestBuildParamsReasoningEffort(t *testing.T) {
	p := New("k")
	params, err := p.buildParams(llm.Request{Model: "o3-mini", Reasoning: llm.ReasoningHigh})
	if err != nil {
		t.Fatal(err)
	}
	if string(params.ReasoningEffort) != "high" {
		t.Errorf("reasoning_effort = %q, want high", params.ReasoningEffort)
	}
}

func TestBuildParamsReasoningOmittedForNonReasoningModel(t *testing.T) {
	p := New("k")
	params, err := p.buildParams(llm.Request{Model: "gpt-4o", Reasoning: llm.ReasoningHigh})
	if err != nil {
		t.Fatal(err)
	}
	if params.ReasoningEffort != "" {
		t.Errorf("non-reasoning model should omit reasoning_effort, got %q", params.ReasoningEffort)
	}
}

func TestProviderOptsSortedAndScoped(t *testing.T) {
	req := llm.Request{Provider: map[string]map[string]any{
		"openai":    {"service_tier": "flex", "store": false},
		"anthropic": {"ignored": true},
	}}
	opts := providerOpts(req)
	if len(opts) != 2 {
		t.Fatalf("expected 2 opts for openai bag, got %d", len(opts))
	}
}

func TestProviderOptsEmpty(t *testing.T) {
	if opts := providerOpts(llm.Request{}); opts != nil {
		t.Errorf("no bag should yield nil opts, got %v", opts)
	}
}
