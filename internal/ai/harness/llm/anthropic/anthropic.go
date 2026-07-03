package anthropic

import (
	"context"
	"os"
	"sort"
	"strconv"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm"
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// DefaultModel is used when a request does not specify a model.
const DefaultModel = "claude-sonnet-4-20250514"

// DefaultMaxTokens is used when a request does not specify a token limit.
const DefaultMaxTokens = 4096

// Provider implements llm.Provider using the Anthropic Messages API.
type Provider struct {
	client        anthropic.Client
	apiKey        string
	promptCaching bool
}

// Option configures a Provider.
type Option func(*Provider)

// WithPromptCaching enables Anthropic prompt caching by injecting cache_control
// breakpoints on the system prompt and tool definitions.
func WithPromptCaching() Option {
	return func(p *Provider) { p.promptCaching = true }
}

// New creates an Anthropic provider. If apiKey is empty it falls back to the
// ANTHROPIC_API_KEY environment variable.
func New(apiKey string, opts ...option.RequestOption) *Provider {
	return NewWithOptions(apiKey, nil, opts...)
}

// NewWithOptions creates an Anthropic provider with harness-level options (such
// as prompt caching) in addition to SDK request options. If apiKey is empty it
// falls back to the ANTHROPIC_API_KEY environment variable.
func NewWithOptions(apiKey string, cacheOpts []Option, sdkOpts ...option.RequestOption) *Provider {
	if apiKey == "" {
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
	}

	options := append([]option.RequestOption{option.WithAPIKey(apiKey)}, sdkOpts...)
	client := anthropic.NewClient(options...)

	p := &Provider{client: client, apiKey: apiKey}
	for _, opt := range cacheOpts {
		opt(p)
	}
	return p
}

func (p *Provider) buildParams(req llm.Request) (anthropic.MessageNewParams, error) {
	msgs, err := toAnthropicMessages(req.Messages)
	if err != nil {
		return anthropic.MessageNewParams{}, err
	}

	model := req.Model
	if model == "" {
		model = DefaultModel
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = DefaultMaxTokens
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: int64(maxTokens),
		Messages:  msgs,
	}

	if system := extractSystem(req.System, p.promptCaching); len(system) > 0 {
		params.System = system
	}

	// Reasoning maps to a thinking budget. Anthropic rejects temperature while
	// thinking is enabled, so track whether we applied it.
	thinking := false
	if native, ok := llm.ClampReasoning(model, req.Reasoning); ok {
		if budget, err := strconv.Atoi(native); err == nil {
			params.Thinking = anthropic.ThinkingConfigParamOfEnabled(int64(budget))
			thinking = true
		}
	}
	if req.Temperature != nil && !thinking && llm.Capabilities(model).SupportsTemperature {
		params.Temperature = anthropic.Float(*req.Temperature)
	}
	if req.TopP != nil {
		params.TopP = anthropic.Float(*req.TopP)
	}
	if tools := toAnthropicTools(req.Tools, p.promptCaching); len(tools) > 0 {
		params.Tools = tools
	}

	return params, nil
}

// providerOpts translates the request's provider-specific option bag for
// Anthropic into per-request SDK options. Keys are applied in sorted order for
// deterministic request bodies.
func providerOpts(req llm.Request) []option.RequestOption {
	bag := req.Provider["anthropic"]
	if len(bag) == 0 {
		return nil
	}
	keys := make([]string, 0, len(bag))
	for k := range bag {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	opts := make([]option.RequestOption, 0, len(keys))
	for _, k := range keys {
		opts = append(opts, option.WithJSONSet(k, bag[k]))
	}
	return opts
}

// Chat implements llm.Provider.
func (p *Provider) Chat(ctx context.Context, req llm.Request) (*llm.Response, error) {
	if p.apiKey == "" {
		return nil, llm.Registry.New(llm.ErrMissingAPIKey)
	}
	if len(req.Messages) == 0 {
		return nil, llm.Registry.New(llm.ErrEmptyMessages)
	}

	params, err := p.buildParams(req)
	if err != nil {
		return nil, err
	}

	message, err := p.client.Messages.New(ctx, params, providerOpts(req)...)
	if err != nil {
		return nil, ParseAnthropicError(err).
			WithDetail("model", string(params.Model))
	}

	msg, stopReason, usage := fromAnthropicResponse(message)
	return &llm.Response{Message: msg, StopReason: stopReason, Usage: usage}, nil
}

// ChatStream implements llm.Provider.
func (p *Provider) ChatStream(ctx context.Context, req llm.Request) (llm.Stream, error) {
	if p.apiKey == "" {
		return nil, llm.Registry.New(llm.ErrMissingAPIKey)
	}
	if len(req.Messages) == 0 {
		return nil, llm.Registry.New(llm.ErrEmptyMessages)
	}

	params, err := p.buildParams(req)
	if err != nil {
		return nil, err
	}

	stream := p.client.Messages.NewStreaming(ctx, params, providerOpts(req)...)
	return &anthropicStream{stream: stream}, nil
}

type sdkStream interface {
	Next() bool
	Current() anthropic.MessageStreamEventUnion
	Err() error
	Close() error
}

type anthropicStream struct {
	stream sdkStream
	acc    anthropic.Message
}

func (s *anthropicStream) Next() (llm.StreamEvent, error) {
	for s.stream.Next() {
		event := s.stream.Current()
		if err := s.acc.Accumulate(event); err != nil {
			return llm.StreamEvent{}, ParseAnthropicError(err)
		}

		switch event.Type {
		case "content_block_delta":
			if event.Delta.Type == "text_delta" {
				return llm.StreamEvent{TextDelta: event.Delta.Text}, nil
			}
		case "message_stop":
			msg, stopReason, usage := fromAnthropicResponse(&s.acc)
			return llm.StreamEvent{
				Done:       true,
				Message:    msg,
				StopReason: stopReason,
				Usage:      usage,
			}, nil
		}
	}

	if err := s.stream.Err(); err != nil {
		return llm.StreamEvent{}, ParseAnthropicError(err)
	}

	// Stream ended without an explicit message_stop; return terminal event.
	msg, stopReason, usage := fromAnthropicResponse(&s.acc)
	return llm.StreamEvent{Done: true, Message: msg, StopReason: stopReason, Usage: usage}, nil
}

func (s *anthropicStream) Close() error {
	return s.stream.Close()
}
