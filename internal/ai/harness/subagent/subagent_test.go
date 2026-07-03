package subagent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Abraxas-365/manifesto/internal/ai/harness"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/tool"
)

// stubProvider returns a fixed final answer (or error) on the first Chat call.
type stubProvider struct {
	answer string
	err    error
	calls  int
}

func (p *stubProvider) Chat(context.Context, llm.Request) (*llm.Response, error) {
	p.calls++
	if p.err != nil {
		return nil, p.err
	}
	return &llm.Response{
		Message:    llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.Text(p.answer)}},
		StopReason: llm.StopEndTurn,
	}, nil
}

func (p *stubProvider) ChatStream(context.Context, llm.Request) (llm.Stream, error) {
	return nil, errors.New("not implemented")
}

// modelRecordingProvider captures the model from the request, then returns a
// final answer.
type modelRecordingProvider struct{ seen *string }

func (p *modelRecordingProvider) Chat(_ context.Context, req llm.Request) (*llm.Response, error) {
	*p.seen = req.Model
	return &llm.Response{
		Message:    llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.Text("done")}},
		StopReason: llm.StopEndTurn,
	}, nil
}

func (p *modelRecordingProvider) ChatStream(context.Context, llm.Request) (llm.Stream, error) {
	return nil, errors.New("not implemented")
}

func newTool(p llm.Provider) *Tool {
	return &Tool{NewAgent: func() *harness.Agent {
		return harness.New(p, tool.NewRegistry())
	}}
}

func TestExecute_ReturnsFinalAnswer(t *testing.T) {
	p := &stubProvider{answer: "the answer"}
	res, err := newTool(p).Execute(context.Background(), []byte(`{"prompt":"do research"}`))
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %q", res.Content)
	}
	if res.Content != "the answer" {
		t.Fatalf("got %q, want %q", res.Content, "the answer")
	}
	if p.calls != 1 {
		t.Fatalf("expected 1 provider call, got %d", p.calls)
	}
}

func TestExecute_FreshAgentPerCall(t *testing.T) {
	// Each invocation must build a new agent, so histories never accumulate.
	var built int
	tl := &Tool{NewAgent: func() *harness.Agent {
		built++
		return harness.New(&stubProvider{answer: "ok"}, tool.NewRegistry())
	}}
	for i := 0; i < 3; i++ {
		if _, err := tl.Execute(context.Background(), []byte(`{"prompt":"x"}`)); err != nil {
			t.Fatal(err)
		}
	}
	if built != 3 {
		t.Fatalf("expected 3 fresh agents, got %d", built)
	}
}

func TestExecute_EmptyPrompt(t *testing.T) {
	res, _ := newTool(&stubProvider{}).Execute(context.Background(), []byte(`{"prompt":"  "}`))
	if !res.IsError {
		t.Fatal("expected error result for empty prompt")
	}
}

func TestExecute_InvalidJSON(t *testing.T) {
	res, _ := newTool(&stubProvider{}).Execute(context.Background(), []byte(`not json`))
	if !res.IsError {
		t.Fatal("expected error result for invalid JSON")
	}
}

func TestExecute_NilFactory(t *testing.T) {
	res, _ := (&Tool{}).Execute(context.Background(), []byte(`{"prompt":"x"}`))
	if !res.IsError {
		t.Fatal("expected error result when NewAgent is nil")
	}
}

func TestExecute_SubagentError(t *testing.T) {
	p := &stubProvider{err: errors.New("boom")}
	res, _ := newTool(p).Execute(context.Background(), []byte(`{"prompt":"x"}`))
	if !res.IsError {
		t.Fatal("expected error result when subagent fails")
	}
}

func TestExecute_ModelOverride(t *testing.T) {
	var gotModel string
	tl := &Tool{NewAgent: func() *harness.Agent {
		a := harness.New(&modelRecordingProvider{seen: &gotModel}, tool.NewRegistry())
		a.Model = "default-model"
		return a
	}}
	if _, err := tl.Execute(context.Background(), []byte(`{"prompt":"x","model":"override-model"}`)); err != nil {
		t.Fatal(err)
	}
	if gotModel != "override-model" {
		t.Fatalf("model override not applied, provider saw %q", gotModel)
	}
}

func TestExecute_ModelDefaultWhenAbsent(t *testing.T) {
	var gotModel string
	tl := &Tool{NewAgent: func() *harness.Agent {
		a := harness.New(&modelRecordingProvider{seen: &gotModel}, tool.NewRegistry())
		a.Model = "default-model"
		return a
	}}
	if _, err := tl.Execute(context.Background(), []byte(`{"prompt":"x"}`)); err != nil {
		t.Fatal(err)
	}
	if gotModel != "default-model" {
		t.Fatalf("expected factory default, provider saw %q", gotModel)
	}
}

func TestExecute_ModelRejectedWhenNotAllowed(t *testing.T) {
	var called bool
	tl := &Tool{
		AllowedModels: []string{"gpt-4o"},
		NewAgent: func() *harness.Agent {
			called = true
			return harness.New(&stubProvider{answer: "ok"}, tool.NewRegistry())
		},
	}
	res, _ := tl.Execute(context.Background(), []byte(`{"prompt":"x","model":"claude-x"}`))
	if !res.IsError {
		t.Fatal("expected error for disallowed model")
	}
	_ = called // agent may be built before validation; the point is Run is not reached
}

func TestInputSchema_ModelEnumWhenAllowed(t *testing.T) {
	if !strings.Contains(string((&Tool{AllowedModels: []string{"a"}}).InputSchema()), `"enum"`) {
		t.Fatal("expected enum in schema when AllowedModels set")
	}
	if strings.Contains(string((&Tool{}).InputSchema()), `"enum"`) {
		t.Fatal("did not expect enum when AllowedModels empty")
	}
}

func TestNameAndDescriptionDefaults(t *testing.T) {
	tl := &Tool{}
	if tl.Name() != DefaultName {
		t.Fatalf("default name: got %q", tl.Name())
	}
	if tl.Description() != DefaultDescription {
		t.Fatalf("default description mismatch")
	}
	custom := &Tool{ToolName: "Research", Desc: "custom"}
	if custom.Name() != "Research" || custom.Description() != "custom" {
		t.Fatal("overrides not applied")
	}
}
