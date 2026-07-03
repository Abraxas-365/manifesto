package openai

import (
	"context"
	"os"
	"sort"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"
)

// DefaultModel is used when a request does not specify a model.
const DefaultModel = "gpt-4o"

// DefaultMaxTokens is used when a request does not specify a token limit.
const DefaultMaxTokens = 4096

// Provider implements llm.Provider using the OpenAI Chat Completions API.
type Provider struct {
	client openai.Client
	apiKey string
}

// New creates an OpenAI provider. If apiKey is empty it falls back to the
// OPENAI_API_KEY environment variable.
func New(apiKey string, opts ...option.RequestOption) *Provider {
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}

	options := append([]option.RequestOption{option.WithAPIKey(apiKey)}, opts...)
	client := openai.NewClient(options...)

	return &Provider{client: client, apiKey: apiKey}
}

func (p *Provider) buildParams(req llm.Request) (openai.ChatCompletionNewParams, error) {
	msgs, err := toOpenAIMessages(req.System, req.Messages)
	if err != nil {
		return openai.ChatCompletionNewParams{}, err
	}

	model := req.Model
	if model == "" {
		model = DefaultModel
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = DefaultMaxTokens
	}

	params := openai.ChatCompletionNewParams{
		Model:               shared.ChatModel(model),
		Messages:            msgs,
		MaxCompletionTokens: openai.Int(int64(maxTokens)),
	}
	if req.Temperature != nil && llm.Capabilities(model).SupportsTemperature {
		params.Temperature = openai.Float(*req.Temperature)
	}
	if req.TopP != nil {
		params.TopP = openai.Float(*req.TopP)
	}
	if native, ok := llm.ClampReasoning(model, req.Reasoning); ok {
		params.ReasoningEffort = shared.ReasoningEffort(native)
	}
	if tools := toOpenAITools(req.Tools); len(tools) > 0 {
		params.Tools = tools
	}

	return params, nil
}

// providerOpts translates the request's provider-specific option bag for
// OpenAI into per-request SDK options. Keys are applied in sorted order for
// deterministic request bodies.
func providerOpts(req llm.Request) []option.RequestOption {
	bag := req.Provider["openai"]
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

	completion, err := p.client.Chat.Completions.New(ctx, params, providerOpts(req)...)
	if err != nil {
		return nil, ParseOpenAIError(err).WithDetail("model", string(params.Model))
	}

	msg, stopReason, usage := fromOpenAIResponse(completion)
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

	stream := p.client.Chat.Completions.NewStreaming(ctx, params, providerOpts(req)...)
	return &openaiStream{stream: stream}, nil
}

type sdkStream interface {
	Next() bool
	Current() openai.ChatCompletionChunk
	Err() error
	Close() error
}

type openaiStream struct {
	stream sdkStream
	acc    openai.ChatCompletionAccumulator
}

func (s *openaiStream) Next() (llm.StreamEvent, error) {
	for s.stream.Next() {
		chunk := s.stream.Current()
		s.acc.AddChunk(chunk)

		if len(chunk.Choices) > 0 {
			if delta := chunk.Choices[0].Delta.Content; delta != "" {
				return llm.StreamEvent{TextDelta: delta}, nil
			}
		}
	}

	if err := s.stream.Err(); err != nil {
		return llm.StreamEvent{}, ParseOpenAIError(err)
	}

	msg, stopReason, usage := fromOpenAIResponse(&s.acc.ChatCompletion)
	return llm.StreamEvent{Done: true, Message: msg, StopReason: stopReason, Usage: usage}, nil
}

func (s *openaiStream) Close() error {
	return s.stream.Close()
}
