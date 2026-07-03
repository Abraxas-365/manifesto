// Package router provides an llm.Provider that dispatches each request to a
// registered backend provider based on the request's Model. This enables
// cross-provider model switching (e.g. an agent moving between an Anthropic and
// an OpenAI model mid-conversation) without the agent loop knowing about
// providers.
//
// The router is unopinionated: it does not hardcode any model→provider mapping.
// The caller registers routes explicitly, either by exact model name or by glob
// pattern (matched with path/filepath.Match, e.g. "gpt-*", "claude-*").
package router

import (
	"context"
	"net/http"
	"path/filepath"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm"
	"github.com/Abraxas-365/manifesto/internal/errx"
)

var registry = errx.NewRegistry("HARNESS_ROUTER")

// ErrNoRoute is returned when no route matches a request's model and no default
// provider is configured.
var ErrNoRoute = registry.Register(
	"NO_ROUTE",
	errx.TypeValidation,
	http.StatusBadRequest,
	"no provider registered for model",
)

type route struct {
	pattern  string
	provider llm.Provider
}

// Router is an llm.Provider that routes by model name. The zero value is not
// usable; construct one with New.
type Router struct {
	routes   []route
	fallback llm.Provider
}

// Option configures a Router.
type Option func(*Router)

// WithDefault sets the provider used when no route matches. Without it, an
// unmatched model yields ErrNoRoute.
func WithDefault(p llm.Provider) Option {
	return func(r *Router) { r.fallback = p }
}

// New creates a Router with the given options.
func New(opts ...Option) *Router {
	r := &Router{}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Handle registers a provider for an exact model name. Later registrations for
// the same pattern take precedence (checked first).
func (r *Router) Handle(model string, p llm.Provider) *Router {
	return r.HandlePattern(model, p)
}

// HandlePattern registers a provider for a glob pattern matched against the
// model name via filepath.Match (e.g. "gpt-*", "claude-*"). Routes are matched
// in reverse registration order, so the most recently added wins on overlap.
func (r *Router) HandlePattern(pattern string, p llm.Provider) *Router {
	r.routes = append(r.routes, route{pattern: pattern, provider: p})
	return r
}

// provider resolves the backend for a model: the last matching route wins, else
// the fallback (which may be nil).
func (r *Router) provider(model string) llm.Provider {
	for i := len(r.routes) - 1; i >= 0; i-- {
		if ok, _ := filepath.Match(r.routes[i].pattern, model); ok {
			return r.routes[i].provider
		}
	}
	return r.fallback
}

// Chat implements llm.Provider.
func (r *Router) Chat(ctx context.Context, req llm.Request) (*llm.Response, error) {
	p := r.provider(req.Model)
	if p == nil {
		return nil, registry.New(ErrNoRoute).WithDetail("model", req.Model)
	}
	return p.Chat(ctx, req)
}

// ChatStream implements llm.Provider.
func (r *Router) ChatStream(ctx context.Context, req llm.Request) (llm.Stream, error) {
	p := r.provider(req.Model)
	if p == nil {
		return nil, registry.New(ErrNoRoute).WithDetail("model", req.Model)
	}
	return p.ChatStream(ctx, req)
}
