package harness

import (
	"context"
	"testing"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm"
)

func TestAgentPropagatesProviderOptions(t *testing.T) {
	p := &fakeProvider{responses: []llm.Response{assistantText("done", llm.StopEndTurn)}}
	a := newAgent(p)
	temp := 0.25
	a.Temperature = &temp
	topP := 0.9
	a.TopP = &topP
	a.Reasoning = llm.ReasoningMedium
	a.ProviderOptions = map[string]map[string]any{"openai": {"service_tier": "flex"}}

	if _, err := a.Run(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	if len(p.requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(p.requests))
	}
	req := p.requests[0]
	if req.Temperature == nil || *req.Temperature != 0.25 {
		t.Errorf("Temperature not propagated: %v", req.Temperature)
	}
	if req.TopP == nil || *req.TopP != 0.9 {
		t.Errorf("TopP not propagated: %v", req.TopP)
	}
	if req.Reasoning != llm.ReasoningMedium {
		t.Errorf("Reasoning not propagated: %q", req.Reasoning)
	}
	if req.Provider["openai"]["service_tier"] != "flex" {
		t.Errorf("Provider options not propagated: %v", req.Provider)
	}
}
