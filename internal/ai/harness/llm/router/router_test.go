package router

import (
	"context"
	"errors"
	"testing"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm"
	"github.com/Abraxas-365/manifesto/internal/errx"
)

// tagProvider returns its tag as the response text, so tests can tell which
// backend handled a request.
type tagProvider struct {
	tag   string
	calls int
}

func (p *tagProvider) Chat(_ context.Context, _ llm.Request) (*llm.Response, error) {
	p.calls++
	return &llm.Response{
		Message:    llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.Text(p.tag)}},
		StopReason: llm.StopEndTurn,
	}, nil
}

func (p *tagProvider) ChatStream(context.Context, llm.Request) (llm.Stream, error) {
	return nil, errors.New("not implemented")
}

func chatModel(t *testing.T, r *Router, model string) string {
	t.Helper()
	resp, err := r.Chat(context.Background(), llm.Request{Model: model})
	if err != nil {
		t.Fatalf("Chat(%q): %v", model, err)
	}
	return resp.Message.TextContent()
}

func TestRouter_ExactMatch(t *testing.T) {
	oa := &tagProvider{tag: "openai"}
	an := &tagProvider{tag: "anthropic"}
	r := New().Handle("gpt-4o", oa).Handle("claude-sonnet-4", an)

	if got := chatModel(t, r, "gpt-4o"); got != "openai" {
		t.Fatalf("got %q", got)
	}
	if got := chatModel(t, r, "claude-sonnet-4"); got != "anthropic" {
		t.Fatalf("got %q", got)
	}
}

func TestRouter_GlobPattern(t *testing.T) {
	oa := &tagProvider{tag: "openai"}
	an := &tagProvider{tag: "anthropic"}
	r := New().HandlePattern("gpt-*", oa).HandlePattern("claude-*", an)

	if got := chatModel(t, r, "gpt-4o-mini"); got != "openai" {
		t.Fatalf("got %q", got)
	}
	if got := chatModel(t, r, "claude-3-5-haiku"); got != "anthropic" {
		t.Fatalf("got %q", got)
	}
}

func TestRouter_LastMatchWins(t *testing.T) {
	a := &tagProvider{tag: "first"}
	b := &tagProvider{tag: "second"}
	r := New().HandlePattern("gpt-*", a).HandlePattern("gpt-4o", b)
	if got := chatModel(t, r, "gpt-4o"); got != "second" {
		t.Fatalf("expected most-recent route to win, got %q", got)
	}
}

func TestRouter_Fallback(t *testing.T) {
	def := &tagProvider{tag: "default"}
	r := New(WithDefault(def)).Handle("gpt-4o", &tagProvider{tag: "openai"})
	if got := chatModel(t, r, "some-unknown-model"); got != "default" {
		t.Fatalf("got %q", got)
	}
}

func TestRouter_NoRouteError(t *testing.T) {
	r := New().Handle("gpt-4o", &tagProvider{tag: "openai"})
	_, err := r.Chat(context.Background(), llm.Request{Model: "mystery"})
	if err == nil {
		t.Fatal("expected ErrNoRoute")
	}
	var e *errx.Error
	if !errors.As(err, &e) || e.Code != "HARNESS_ROUTER_NO_ROUTE" {
		t.Fatalf("expected HARNESS_ROUTER_NO_ROUTE, got %v", err)
	}
}

func TestRouter_OSeriesModels(t *testing.T) {
	oa := &tagProvider{tag: "openai"}
	an := &tagProvider{tag: "anthropic"}
	r := New().
		HandlePattern("claude-*", an).
		HandlePattern("gpt-*", oa).
		HandlePattern("o1-*", oa).
		HandlePattern("o3-*", oa).
		HandlePattern("o4-*", oa).
		HandlePattern("chatgpt-*", oa)

	for _, tc := range []struct {
		model string
		want  string
	}{
		{"o1-mini", "openai"},
		{"o1-preview", "openai"},
		{"o3-mini", "openai"},
		{"o3-mega-2026", "openai"},
		{"o4-mini", "openai"},
		{"chatgpt-4o-latest", "openai"},
		{"claude-sonnet-4-20250514", "anthropic"},
		{"gpt-4o", "openai"},
	} {
		if got := chatModel(t, r, tc.model); got != tc.want {
			t.Errorf("model %q: got %q, want %q", tc.model, got, tc.want)
		}
	}
}
