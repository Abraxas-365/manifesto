package openai

import (
	"context"
	"fmt"
	"os"
	"runtime"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm"
	"github.com/Abraxas-365/manifesto/internal/errx"
	gopenai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
)

// CodexAPIEndpoint is the ChatGPT backend endpoint for Codex requests.
const CodexAPIEndpoint = "https://chatgpt.com/backend-api/codex"

// CodexCredentials provides per-request credential resolution for the
// ChatGPT subscription (OAuth) flow.
type CodexCredentials interface {
	Token(ctx context.Context) (accessToken, accountID string, err error)
	HandleUnauthorized(ctx context.Context) bool
}

// CodexProvider implements llm.Provider using the ChatGPT Codex backend
// via the user's ChatGPT subscription. It wraps the standard OpenAI SDK
// pointed at the Codex backend URL, with per-request Bearer auth.
//
// The Codex backend requires: store=false, stream=true, and only supports
// certain models (gpt-5.5, gpt-5.4, gpt-5.4-mini, gpt-5.3-codex-spark).
type CodexProvider struct {
	creds  CodexCredentials
	client gopenai.Client
}

// NewCodexProvider creates a provider that talks to the ChatGPT Codex backend
// using OAuth credentials from a CodexCredentials source.
func NewCodexProvider(creds CodexCredentials) *CodexProvider {
	// The "dummy" API key satisfies the SDK constructor; real auth is injected
	// per-request via option.WithAPIKey.
	client := gopenai.NewClient(
		option.WithAPIKey("codex-oauth"),
		option.WithBaseURL(CodexAPIEndpoint),
	)
	return &CodexProvider{creds: creds, client: client}
}

// codexOpts resolves per-request auth and custom headers.
func (p *CodexProvider) codexOpts(ctx context.Context) ([]option.RequestOption, error) {
	accessToken, accountID, err := p.creds.Token(ctx)
	if err != nil {
		return nil, llm.Registry.NewWithCause(llm.ErrMissingAPIKey, err)
	}
	opts := []option.RequestOption{
		option.WithAPIKey(accessToken),
		option.WithHeader("originator", "codex_cli_go"),
		option.WithHeader("User-Agent",
			fmt.Sprintf("agent/1.0 (%s %s; %s)", runtime.GOOS, osRelease(), runtime.GOARCH)),
	}
	if accountID != "" {
		opts = append(opts, option.WithHeader("ChatGPT-Account-Id", accountID))
	}
	return opts, nil
}

// helper wraps the Provider to reuse responsesParams/convert logic.
func (p *CodexProvider) helper() *Provider {
	return &Provider{client: p.client, apiKey: "codex-oauth"}
}

// codexParams builds Responses API params with Codex-required fields.
func (p *CodexProvider) codexParams(req llm.Request) llm.Request {
	// Force max_tokens to 0 so responsesParams doesn't set max_output_tokens.
	req.MaxTokens = 0
	return req
}

// Chat implements llm.Provider. The Codex backend requires stream=true,
// so we collect the stream into a single response.
func (p *CodexProvider) Chat(ctx context.Context, req llm.Request) (*llm.Response, error) {
	stream, err := p.ChatStream(ctx, req)
	if err != nil {
		return nil, err
	}
	defer stream.Close()

	// Drain stream to completion.
	for {
		ev, err := stream.Next()
		if err != nil {
			return nil, err
		}
		if ev.Done {
			return &llm.Response{
				Message:    ev.Message,
				StopReason: ev.StopReason,
				Usage:      ev.Usage,
			}, nil
		}
	}
}

// ChatStream implements llm.Provider.
func (p *CodexProvider) ChatStream(ctx context.Context, req llm.Request) (llm.Stream, error) {
	stream, err := p.chatStreamOnce(ctx, req)
	if err != nil && isUnauthorized(err) && p.creds.HandleUnauthorized(ctx) {
		return p.chatStreamOnce(ctx, req)
	}
	return stream, err
}

func (p *CodexProvider) chatStreamOnce(ctx context.Context, req llm.Request) (llm.Stream, error) {
	opts, err := p.codexOpts(ctx)
	if err != nil {
		return nil, err
	}
	if len(req.Messages) == 0 {
		return nil, llm.Registry.New(llm.ErrEmptyMessages)
	}

	h := p.helper()
	params := h.responsesParams(p.codexParams(req))

	// Codex backend requires store=false (doesn't allow storing conversations).
	params.Store = param.NewOpt(false)

	allOpts := append(providerOpts(req), opts...)
	// NewStreaming automatically sets stream=true.
	stream := p.client.Responses.NewStreaming(ctx, params, allOpts...)
	return &responsesStream{stream: stream}, nil
}

// isUnauthorized checks if an error represents a 401 response.
func isUnauthorized(err error) bool {
	if err == nil {
		return false
	}
	var e *errx.Error
	if errx.As(err, &e) {
		return e.HTTPStatus == 401
	}
	return false
}

// osRelease returns a short OS version string.
func osRelease() string {
	if v := os.Getenv("OS_VERSION"); v != "" {
		return v
	}
	return runtime.GOOS
}
