package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// fakeCreds implements Credentials.
type fakeCreds struct {
	token     string
	isOAuth   bool
	refreshed atomic.Bool
	onRefresh func() // rotate token
}

func (c *fakeCreds) Token(ctx context.Context) (string, bool, error) {
	return c.token, c.isOAuth, nil
}

func (c *fakeCreds) HandleUnauthorized(ctx context.Context) bool {
	c.refreshed.Store(true)
	if c.onRefresh != nil {
		c.onRefresh()
	}
	return true
}

func messagesOK(text string) map[string]any {
	return map[string]any{
		"id": "msg_1", "type": "message", "role": "assistant",
		"model":       "claude-sonnet-4-20250514",
		"content":     []map[string]any{{"type": "text", "text": text}},
		"stop_reason": "end_turn",
		"usage":       map[string]any{"input_tokens": 1, "output_tokens": 1},
	}
}

// captureServer records the last request headers/body and replies scripted.
func captureServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		handler(w, r)
	}))
	t.Cleanup(ts.Close)
	return ts
}

func req() llm.Request {
	return llm.Request{
		Model:    "claude-sonnet-4-20250514",
		System:   "be helpful",
		Messages: []llm.Message{{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.Text("hi")}}},
	}
}

func TestOAuthHeadersAndBillingBlock(t *testing.T) {
	var gotAuth, gotBeta, gotAPIKey string
	var gotBody map[string]any
	ts := captureServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotBeta = r.Header.Get("anthropic-beta")
		gotAPIKey = r.Header.Get("x-api-key")
		json.NewDecoder(r.Body).Decode(&gotBody)
		json.NewEncoder(w).Encode(messagesOK("ok"))
	})

	creds := &fakeCreds{token: "oat-123", isOAuth: true}
	p := NewWithCredentials(creds, option.WithBaseURL(ts.URL))

	resp, err := p.Chat(context.Background(), req())
	if err != nil {
		t.Fatal(err)
	}
	if resp.Message.TextContent() != "ok" {
		t.Fatalf("text: %q", resp.Message.TextContent())
	}
	if gotAuth != "Bearer oat-123" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotAPIKey != "" {
		t.Errorf("x-api-key must be empty for OAuth, got %q", gotAPIKey)
	}
	if !strings.Contains(gotBeta, "oauth-2025-04-20") || !strings.Contains(gotBeta, "claude-code-20250219") {
		t.Errorf("beta headers: %q", gotBeta)
	}
	// Billing block is the first system block, original prompt follows.
	system, _ := gotBody["system"].([]any)
	if len(system) != 2 {
		t.Fatalf("system blocks: %v", gotBody["system"])
	}
	first := system[0].(map[string]any)
	if !strings.Contains(first["text"].(string), "x-anthropic-billing-header") {
		t.Errorf("first system block: %v", first)
	}
	if second := system[1].(map[string]any); second["text"] != "be helpful" {
		t.Errorf("second system block: %v", second)
	}
}

func TestAPIKeyCredsUseXAPIKeyHeader(t *testing.T) {
	var gotAuth, gotAPIKey string
	var gotBody map[string]any
	ts := captureServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAPIKey = r.Header.Get("x-api-key")
		json.NewDecoder(r.Body).Decode(&gotBody)
		json.NewEncoder(w).Encode(messagesOK("ok"))
	})

	creds := &fakeCreds{token: "sk-ant-xyz", isOAuth: false}
	p := NewWithCredentials(creds, option.WithBaseURL(ts.URL))

	if _, err := p.Chat(context.Background(), req()); err != nil {
		t.Fatal(err)
	}
	if gotAPIKey != "sk-ant-xyz" {
		t.Errorf("x-api-key = %q", gotAPIKey)
	}
	if gotAuth != "" {
		t.Errorf("Authorization must be empty for api key, got %q", gotAuth)
	}
	// No billing block for API keys.
	if system, _ := gotBody["system"].([]any); len(system) != 1 {
		t.Errorf("system blocks: %v", gotBody["system"])
	}
}

func TestChatRetriesOnceAfter401Refresh(t *testing.T) {
	var calls atomic.Int32
	var lastAuth string
	ts := captureServer(t, func(w http.ResponseWriter, r *http.Request) {
		lastAuth = r.Header.Get("Authorization")
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]any{"type": "error", "error": map[string]any{"type": "authentication_error", "message": "expired"}})
			return
		}
		json.NewEncoder(w).Encode(messagesOK("after refresh"))
	})

	creds := &fakeCreds{token: "stale", isOAuth: true}
	creds.onRefresh = func() { creds.token = "fresh" }
	p := NewWithCredentials(creds, option.WithBaseURL(ts.URL), option.WithMaxRetries(0))

	resp, err := p.Chat(context.Background(), req())
	if err != nil {
		t.Fatal(err)
	}
	if resp.Message.TextContent() != "after refresh" {
		t.Fatalf("text: %q", resp.Message.TextContent())
	}
	if !creds.refreshed.Load() {
		t.Error("HandleUnauthorized not called")
	}
	if calls.Load() != 2 {
		t.Errorf("calls = %d, want 2", calls.Load())
	}
	if lastAuth != "Bearer fresh" {
		t.Errorf("retry auth = %q", lastAuth)
	}
}

func TestNoCredsAndNoKeyFails(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	p := New("")
	if _, err := p.Chat(context.Background(), req()); err == nil {
		t.Fatal("want missing-key error")
	}
}
