package openai

import (
	"context"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/models"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/packages/ssestream"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
)

// useResponses returns true if the model should use the Responses API instead
// of Chat Completions. Checks models.Cache for api_kind, falling back to
// heuristic for known reasoning-family models.
func useResponses(model string) bool {
	if cache := models.GetGlobalCache(); cache != nil {
		if cap := cache.Get(model); cap != nil {
			return cap.APIKind == "openai-responses"
		}
	}
	return false
}

// responsesParams builds a Responses API request from the harness request.
func (p *Provider) responsesParams(req llm.Request) responses.ResponseNewParams {
	model := req.Model
	if model == "" {
		model = DefaultModel
	}

	params := responses.ResponseNewParams{
		Model: shared.ResponsesModel(model),
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: toResponsesInput(req.System, req.Messages),
		},
		// Enable storage so OpenAI's automatic prompt caching can kick in.
		Store: param.NewOpt(true),
	}

	if req.MaxTokens > 0 {
		params.MaxOutputTokens = param.NewOpt(int64(req.MaxTokens))
	}

	if req.Temperature != nil && llm.Capabilities(model).SupportsTemperature {
		params.Temperature = param.NewOpt(*req.Temperature)
	}
	if req.TopP != nil {
		params.TopP = param.NewOpt(*req.TopP)
	}
	if native, ok := llm.ClampReasoning(model, req.Reasoning); ok {
		params.Reasoning = shared.ReasoningParam{
			Effort:  shared.ReasoningEffort(native),
			Summary: "auto",
		}
	}
	if tools := toResponsesTools(req.Tools); len(tools) > 0 {
		params.Tools = tools
	}

	// Set prompt_cache_key from provider options to improve cache hit rates.
	if bag := req.Provider["openai"]; bag != nil {
		if key, ok := bag["prompt_cache_key"].(string); ok && key != "" {
			params.PromptCacheKey = param.NewOpt(key)
		}
	}

	return params
}

// chatResponses implements Chat using the Responses API.
func (p *Provider) chatResponses(ctx context.Context, req llm.Request) (*llm.Response, error) {
	params := p.responsesParams(req)
	resp, err := p.client.Responses.New(ctx, params, providerOpts(req)...)
	if err != nil {
		return nil, ParseOpenAIError(err).WithDetail("model", string(params.Model))
	}
	msg, stopReason, usage := fromResponsesOutput(resp)
	return &llm.Response{Message: msg, StopReason: stopReason, Usage: usage}, nil
}

// chatStreamResponses implements ChatStream using the Responses API.
func (p *Provider) chatStreamResponses(ctx context.Context, req llm.Request) (llm.Stream, error) {
	params := p.responsesParams(req)
	stream := p.client.Responses.NewStreaming(ctx, params, providerOpts(req)...)
	return &responsesStream{stream: stream}, nil
}

type responsesStream struct {
	stream   *ssestream.Stream[responses.ResponseStreamEventUnion]
	response *responses.Response                 // populated on completion
	items    []responses.ResponseOutputItemUnion // accumulated from output_item.done events
}

func (s *responsesStream) Next() (llm.StreamEvent, error) {
	for s.stream.Next() {
		event := s.stream.Current()

		switch event.Type {
		case "response.output_text.delta":
			if event.Delta != "" {
				return llm.StreamEvent{TextDelta: event.Delta}, nil
			}
		case "response.output_item.done":
			// Accumulate completed output items so we have them when
			// response.completed fires (the completed event's Output
			// array may be empty in streaming mode).
			s.items = append(s.items, event.Item)
		case "response.completed":
			resp := event.Response
			// Backfill output items from our accumulation if the SDK
			// didn't populate them on the completed event.
			if len(resp.Output) == 0 && len(s.items) > 0 {
				resp.Output = s.items
			}
			s.response = &resp
			msg, stopReason, usage := fromResponsesOutput(&resp)
			return llm.StreamEvent{Done: true, Message: msg, StopReason: stopReason, Usage: usage}, nil
		}
	}

	if err := s.stream.Err(); err != nil {
		return llm.StreamEvent{}, ParseOpenAIError(err)
	}

	// Stream ended without response.completed — return terminal event from
	// whatever we accumulated (unlikely but defensive).
	if s.response != nil {
		msg, stopReason, usage := fromResponsesOutput(s.response)
		return llm.StreamEvent{Done: true, Message: msg, StopReason: stopReason, Usage: usage}, nil
	}
	return llm.StreamEvent{Done: true, Message: llm.Message{
		Role:    llm.RoleAssistant,
		Content: []llm.ContentBlock{llm.Text("[stream ended without response]")},
	}}, nil
}

func (s *responsesStream) Close() error {
	return s.stream.Close()
}
