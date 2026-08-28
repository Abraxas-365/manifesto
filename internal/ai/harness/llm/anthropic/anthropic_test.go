package anthropic

import (
	"encoding/json"
	"os"
	"testing"

	anthropicSdk "github.com/anthropics/anthropic-sdk-go"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm"
	"github.com/Abraxas-365/manifesto/internal/ai/models"
)

func TestMain(m *testing.M) {
	models.SetGlobalCache(models.NewDefaultCache())
	os.Exit(m.Run())
}

func floatPtr(f float64) *float64 { return &f }

func TestBuildParamsTemperatureOmittedWhenNil(t *testing.T) {
	p := New("k")
	params, err := p.buildParams(llm.Request{Model: "claude-sonnet-4-5-20250514"})
	if err != nil {
		t.Fatal(err)
	}
	if params.Temperature.Valid() {
		t.Errorf("nil Temperature should be omitted")
	}
}

func TestBuildParamsTemperatureSet(t *testing.T) {
	p := New("k")
	params, err := p.buildParams(llm.Request{Model: "claude-sonnet-4-5-20250514", Temperature: floatPtr(0.3)})
	if err != nil {
		t.Fatal(err)
	}
	if !params.Temperature.Valid() || params.Temperature.Value != 0.3 {
		t.Errorf("temperature = %v, want 0.3", params.Temperature)
	}
}

func TestBuildParamsTopP(t *testing.T) {
	p := New("k")
	params, err := p.buildParams(llm.Request{Model: "claude-sonnet-4-5-20250514", TopP: floatPtr(0.8)})
	if err != nil {
		t.Fatal(err)
	}
	if !params.TopP.Valid() || params.TopP.Value != 0.8 {
		t.Errorf("top_p = %v, want 0.8", params.TopP)
	}
}

func TestBuildParamsAdaptiveThinking(t *testing.T) {
	p := New("k")
	params, err := p.buildParams(llm.Request{Model: "claude-sonnet-4-5-20250514", Reasoning: llm.ReasoningMedium})
	if err != nil {
		t.Fatal(err)
	}
	// Modern Anthropic models use adaptive thinking + effort, not fixed budgets.
	if params.Thinking.OfAdaptive == nil {
		t.Errorf("expected adaptive thinking, got %+v", params.Thinking)
	}
	if params.OutputConfig.Effort != "medium" {
		t.Errorf("effort = %q, want \"medium\"", params.OutputConfig.Effort)
	}
}

func TestBuildParamsTemperatureOmittedWhenThinking(t *testing.T) {
	p := New("k")
	params, err := p.buildParams(llm.Request{
		Model:       "claude-sonnet-4-5-20250514",
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
	if params.Thinking.OfEnabled != nil || params.Thinking.OfAdaptive != nil {
		t.Errorf("non-reasoning model should omit thinking")
	}
}

// TestV1ParityRequestParams verifies that claudio builds API request parameters
// matching legacy's behavior for all common model/reasoning combinations.
func TestV1ParityRequestParams(t *testing.T) {
	p := New("test-key")

	tests := []struct {
		name         string
		model        string
		reasoning    llm.ReasoningLevel
		wantEffort   string
		wantAdaptive bool
		wantMaxTok   int64 // 0 = don't check (uses request-level default)
	}{
		{
			name:         "opus-4 with high reasoning",
			model:        "claude-opus-4-6",
			reasoning:    llm.ReasoningHigh,
			wantEffort:   "high",
			wantAdaptive: true,
		},
		{
			name:         "sonnet-5 with high reasoning",
			model:        "claude-sonnet-5",
			reasoning:    llm.ReasoningHigh,
			wantEffort:   "high",
			wantAdaptive: true,
		},
		{
			name:         "xhigh maps to xhigh effort",
			model:        "claude-opus-4-6",
			reasoning:    llm.ReasoningXHigh,
			wantEffort:   "xhigh",
			wantAdaptive: true,
		},
		{
			name:         "medium reasoning",
			model:        "claude-sonnet-4-5-20250514",
			reasoning:    llm.ReasoningMedium,
			wantEffort:   "medium",
			wantAdaptive: true,
		},
		{
			name:      "haiku has no reasoning",
			model:     "claude-haiku-4-5-20251001",
			reasoning: llm.ReasoningHigh,
			// haiku doesn't support reasoning — should be omitted
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			params, err := p.buildParams(llm.Request{
				Model:     tc.model,
				Reasoning: tc.reasoning,
			})
			if err != nil {
				t.Fatal(err)
			}

			if tc.wantAdaptive {
				if params.Thinking.OfAdaptive == nil {
					t.Error("expected adaptive thinking to be enabled")
				}
				if string(params.OutputConfig.Effort) != tc.wantEffort {
					t.Errorf("effort = %q, want %q", params.OutputConfig.Effort, tc.wantEffort)
				}
			} else if tc.wantEffort == "" {
				// Should have no thinking
				if params.Thinking.OfAdaptive != nil || params.Thinking.OfEnabled != nil {
					t.Error("expected no thinking config for non-reasoning model")
				}
			}
		})
	}
}

// TestV1ParityMaxOutputTokens verifies that effectiveMaxTokens returns
// model-appropriate values matching legacy (which reads cap.MaxResponseTokens).
func TestV1ParityMaxOutputTokens(t *testing.T) {
	// legacy values from models/capabilities.go MaxResponseTokens
	v1Expected := map[string]int{
		"claude-opus-4-6":            128_000, // legacy: 128000
		"claude-opus-4-8":            128_000,
		"claude-sonnet-5":            64_000, // legacy: 64000
		"claude-sonnet-4-5-20250514": 64_000,
		"claude-fable-5":             128_000,
	}

	for model, wantMin := range v1Expected {
		t.Run(model, func(t *testing.T) {
			cap := llm.Capabilities(model)
			if cap.MaxOutputTokens < wantMin {
				t.Errorf("MaxOutputTokens for %s = %d, want >= %d (legacy parity)",
					model, cap.MaxOutputTokens, wantMin)
			}
		})
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

// mockStream feeds canned MessageStreamEventUnion values for testing.
type mockStream struct {
	events []anthropicSdk.MessageStreamEventUnion
	pos    int
}

func (m *mockStream) Next() bool {
	m.pos++
	return m.pos <= len(m.events)
}
func (m *mockStream) Current() anthropicSdk.MessageStreamEventUnion {
	return m.events[m.pos-1]
}
func (m *mockStream) Err() error   { return nil }
func (m *mockStream) Close() error { return nil }

// unmarshalEvent builds a MessageStreamEventUnion from raw JSON, so the SDK's
// custom UnmarshalJSON produces properly typed inner unions that Accumulate
// can process (flat struct construction skips those codepaths).
func unmarshalEvent(t *testing.T, raw string) anthropicSdk.MessageStreamEventUnion {
	t.Helper()
	var ev anthropicSdk.MessageStreamEventUnion
	if err := json.Unmarshal([]byte(raw), &ev); err != nil {
		t.Fatalf("unmarshal event: %v\n%s", err, raw)
	}
	return ev
}

func TestStreamEmitsThinkingDelta(t *testing.T) {
	events := []anthropicSdk.MessageStreamEventUnion{
		unmarshalEvent(t, `{"type":"message_start","message":{"id":"m1","type":"message","role":"assistant","content":[],"model":"claude-sonnet-5","stop_reason":null,"usage":{"input_tokens":10,"output_tokens":0}}}`),
		unmarshalEvent(t, `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`),
		unmarshalEvent(t, `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"let me think"}}`),
		unmarshalEvent(t, `{"type":"content_block_stop","index":0}`),
		unmarshalEvent(t, `{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`),
		unmarshalEvent(t, `{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"hello"}}`),
		unmarshalEvent(t, `{"type":"content_block_stop","index":1}`),
		unmarshalEvent(t, `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":10}}`),
		unmarshalEvent(t, `{"type":"message_stop"}`),
	}
	s := &anthropicStream{stream: &mockStream{events: events}}

	ev, err := s.Next()
	if err != nil {
		t.Fatal(err)
	}
	if ev.ThinkingDelta != "let me think" {
		t.Errorf("ThinkingDelta = %q, want %q", ev.ThinkingDelta, "let me think")
	}

	ev, err = s.Next()
	if err != nil {
		t.Fatal(err)
	}
	if ev.TextDelta != "hello" {
		t.Errorf("TextDelta = %q, want %q", ev.TextDelta, "hello")
	}

	ev, err = s.Next()
	if err != nil {
		t.Fatal(err)
	}
	if !ev.Done {
		t.Error("expected Done after message_stop")
	}
}

// TestStreamEmitsThinkingDeltaAdaptive verifies that adaptive thinking
// (which sends empty thinking text) still emits a ThinkingDelta event.
func TestStreamEmitsThinkingDeltaAdaptive(t *testing.T) {
	events := []anthropicSdk.MessageStreamEventUnion{
		unmarshalEvent(t, `{"type":"message_start","message":{"id":"m1","type":"message","role":"assistant","content":[],"model":"claude-sonnet-5","stop_reason":null,"usage":{"input_tokens":10,"output_tokens":0}}}`),
		unmarshalEvent(t, `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`),
		unmarshalEvent(t, `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":""}}`),
		unmarshalEvent(t, `{"type":"content_block_stop","index":0}`),
		unmarshalEvent(t, `{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`),
		unmarshalEvent(t, `{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"hello"}}`),
		unmarshalEvent(t, `{"type":"content_block_stop","index":1}`),
		unmarshalEvent(t, `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":10}}`),
		unmarshalEvent(t, `{"type":"message_stop"}`),
	}
	s := &anthropicStream{stream: &mockStream{events: events}}

	// First event must be a ThinkingDelta even though API sent empty thinking text.
	ev, err := s.Next()
	if err != nil {
		t.Fatal(err)
	}
	if ev.ThinkingDelta == "" {
		t.Error("adaptive thinking_delta with empty text should still emit ThinkingDelta event")
	}
}

func TestStreamEmitsToolUseStreaming(t *testing.T) {
	events := []anthropicSdk.MessageStreamEventUnion{
		unmarshalEvent(t, `{"type":"message_start","message":{"id":"m2","type":"message","role":"assistant","content":[],"model":"claude-sonnet-5","stop_reason":null,"usage":{"input_tokens":10,"output_tokens":0}}}`),
		unmarshalEvent(t, `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"t1","name":"Read","input":{}}}`),
		unmarshalEvent(t, `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"path\":\"/foo"}}`),
		unmarshalEvent(t, `{"type":"content_block_stop","index":0}`),
		unmarshalEvent(t, `{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":5}}`),
		unmarshalEvent(t, `{"type":"message_stop"}`),
	}
	s := &anthropicStream{stream: &mockStream{events: events}}

	ev, err := s.Next()
	if err != nil {
		t.Fatal(err)
	}
	if ev.ToolUseStreaming == nil || ev.ToolUseStreaming.Name != "Read" {
		t.Errorf("ToolUseStreaming = %+v, want Read", ev.ToolUseStreaming)
	}

	ev, err = s.Next()
	if err != nil {
		t.Fatal(err)
	}
	if ev.InputJSONDeltaLen != len(`{"path":"/foo`) {
		t.Errorf("InputJSONDeltaLen = %d, want %d", ev.InputJSONDeltaLen, len(`{"path":"/foo`))
	}
}
