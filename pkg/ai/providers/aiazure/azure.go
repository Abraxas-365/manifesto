package aiazure

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/Abraxas-365/manifesto/pkg/ai/embedding"
	"github.com/Abraxas-365/manifesto/pkg/ai/llm"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/azure"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared/constant"
)

// ProviderOption configures the Azure OpenAI provider
type ProviderOption func(*AzureOpenAIProvider)

// WithAPIVersion sets the Azure OpenAI API version
func WithAPIVersion(version string) ProviderOption {
	return func(p *AzureOpenAIProvider) {
		p.apiVersion = version
	}
}

// WithAzureADCredential configures Azure AD authentication
func WithAzureADCredential(cred azcore.TokenCredential) ProviderOption {
	return func(p *AzureOpenAIProvider) {
		p.tokenCredential = cred
	}
}

// AzureOpenAIProvider implements the LLM and Embedder interfaces for Azure OpenAI
type AzureOpenAIProvider struct {
	client          openai.Client
	endpoint        string
	apiKey          string
	apiVersion      string
	tokenCredential azcore.TokenCredential
}

// NewAzureOpenAIProvider creates a new Azure OpenAI provider
func NewAzureOpenAIProvider(endpoint, apiKey string, opts ...ProviderOption) *AzureOpenAIProvider {
	p := &AzureOpenAIProvider{
		endpoint:   endpoint,
		apiKey:     apiKey,
		apiVersion: "2024-06-01",
	}

	for _, opt := range opts {
		opt(p)
	}

	if p.apiKey == "" {
		p.apiKey = os.Getenv("AZURE_OPENAI_API_KEY")
	}

	var clientOpts []option.RequestOption
	clientOpts = append(clientOpts, azure.WithEndpoint(p.endpoint, p.apiVersion))

	if p.tokenCredential != nil {
		clientOpts = append(clientOpts, azure.WithTokenCredential(p.tokenCredential))
	} else {
		clientOpts = append(clientOpts, azure.WithAPIKey(p.apiKey))
	}

	p.client = openai.NewClient(clientOpts...)
	return p
}

// ============================================================================
// Chat Implementation
// ============================================================================

// Chat implements the LLM interface
func (p *AzureOpenAIProvider) Chat(ctx context.Context, messages []llm.Message, opts ...llm.Option) (llm.Response, error) {
	if p.endpoint == "" {
		return llm.Response{}, errorRegistry.New(ErrMissingEndpoint)
	}

	if len(messages) == 0 {
		return llm.Response{}, errorRegistry.New(ErrEmptyMessages)
	}

	options := llm.DefaultOptions()
	for _, opt := range opts {
		opt(options)
	}

	if options.Model == "" {
		return llm.Response{}, errorRegistry.New(ErrMissingEndpoint).
			WithDetail("error", "model/deployment name is required for Azure OpenAI")
	}

	openAIMessages, err := convertMessages(messages)
	if err != nil {
		return llm.Response{}, err
	}

	params := openai.ChatCompletionNewParams{
		Messages: openAIMessages,
		Model:    options.Model,
	}

	applyOptions(&params, options)

	completion, err := p.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return llm.Response{}, ParseAzureError(err).
			WithDetail("model", options.Model).
			WithDetail("num_messages", len(messages))
	}

	return convertFromResponse(completion)
}

// ============================================================================
// Chat Stream Implementation
// ============================================================================

// ChatStream implements streaming
func (p *AzureOpenAIProvider) ChatStream(ctx context.Context, messages []llm.Message, opts ...llm.Option) (llm.Stream, error) {
	if p.endpoint == "" {
		return nil, errorRegistry.New(ErrMissingEndpoint)
	}

	if len(messages) == 0 {
		return nil, errorRegistry.New(ErrEmptyMessages)
	}

	options := llm.DefaultOptions()
	for _, opt := range opts {
		opt(options)
	}

	if options.Model == "" {
		return nil, errorRegistry.New(ErrMissingEndpoint).
			WithDetail("error", "model/deployment name is required for Azure OpenAI")
	}

	openAIMessages, err := convertMessages(messages)
	if err != nil {
		return nil, err
	}

	params := openai.ChatCompletionNewParams{
		Messages: openAIMessages,
		Model:    options.Model,
	}

	applyOptions(&params, options)

	sseStream := p.client.Chat.Completions.NewStreaming(ctx, params)

	return &azureStream{
		stream:      sseStream,
		accumulator: openai.ChatCompletionAccumulator{},
	}, nil
}

// ============================================================================
// Embedding Implementation
// ============================================================================

// EmbedDocuments converts documents to embeddings
func (p *AzureOpenAIProvider) EmbedDocuments(ctx context.Context, documents []string, opts ...embedding.Option) ([]embedding.Embedding, error) {
	if len(documents) == 0 {
		return nil, errorRegistry.New(ErrEmptyEmbeddingInput)
	}

	options := embedding.DefaultOptions()
	for _, opt := range opts {
		opt(options)
	}

	if options.Model == "" {
		return nil, errorRegistry.New(ErrMissingEndpoint).
			WithDetail("error", "model/deployment name is required for Azure OpenAI embeddings")
	}

	params := openai.EmbeddingNewParams{
		Input: openai.EmbeddingNewParamsInputUnion{
			OfArrayOfStrings: documents,
		},
		Model: options.Model,
	}

	if options.Dimensions > 0 {
		params.Dimensions = openai.Int(int64(options.Dimensions))
	}

	resp, err := p.client.Embeddings.New(ctx, params)
	if err != nil {
		return nil, ParseAzureError(err).
			WithDetail("model", options.Model).
			WithDetail("num_documents", len(documents))
	}

	if len(resp.Data) == 0 {
		return nil, errorRegistry.New(ErrNoEmbeddingReturned)
	}

	embeddings := make([]embedding.Embedding, len(resp.Data))
	for i, data := range resp.Data {
		embeddings[i] = embedding.Embedding{
			Vector: convertToFloat32Slice(data.Embedding),
			Usage: embedding.Usage{
				PromptTokens: int(resp.Usage.PromptTokens),
				TotalTokens:  int(resp.Usage.TotalTokens),
			},
		}
	}

	return embeddings, nil
}

// EmbedQuery converts a single query to an embedding
func (p *AzureOpenAIProvider) EmbedQuery(ctx context.Context, text string, opts ...embedding.Option) (embedding.Embedding, error) {
	if text == "" {
		return embedding.Embedding{}, errorRegistry.New(ErrEmptyEmbeddingInput)
	}

	embeddings, err := p.EmbedDocuments(ctx, []string{text}, opts...)
	if err != nil {
		return embedding.Embedding{}, err
	}

	if len(embeddings) == 0 {
		return embedding.Embedding{}, errorRegistry.New(ErrNoEmbeddingReturned)
	}

	return embeddings[0], nil
}

// ============================================================================
// Stream Implementation
// ============================================================================

type azureStream struct {
	stream interface {
		Next() bool
		Current() openai.ChatCompletionChunk
		Err() error
	}
	accumulator openai.ChatCompletionAccumulator
	lastError   error
	current     llm.Message
}

func (s *azureStream) Next() (llm.Message, error) {
	if s.lastError != nil {
		return llm.Message{}, s.lastError
	}

	if !s.stream.Next() {
		if err := s.stream.Err(); err != nil {
			s.lastError = ParseAzureError(err)
			return llm.Message{}, s.lastError
		}
		s.lastError = io.EOF
		return llm.Message{}, io.EOF
	}

	chunk := s.stream.Current()
	s.accumulator.AddChunk(chunk)

	if len(chunk.Choices) == 0 {
		return llm.Message{Role: llm.RoleAssistant}, nil
	}

	delta := chunk.Choices[0].Delta

	for _, tc := range delta.ToolCalls {
		idx := int(tc.Index)
		for len(s.current.ToolCalls) <= idx {
			s.current.ToolCalls = append(s.current.ToolCalls, llm.ToolCall{Type: "function"})
		}
		if tc.ID != "" {
			s.current.ToolCalls[idx].ID = tc.ID
		}
		if tc.Function.Name != "" {
			s.current.ToolCalls[idx].Function.Name += tc.Function.Name
		}
		s.current.ToolCalls[idx].Function.Arguments += tc.Function.Arguments
	}

	return llm.Message{
		Role:      llm.RoleAssistant,
		Content:   delta.Content,
		ToolCalls: s.current.ToolCalls,
	}, nil
}

func (s *azureStream) Close() error {
	return nil
}

// ============================================================================
// Helper Functions
// ============================================================================

func convertMessages(messages []llm.Message) ([]openai.ChatCompletionMessageParamUnion, error) {
	result := make([]openai.ChatCompletionMessageParamUnion, 0, len(messages))

	for i, msg := range messages {
		converted, err := convertMessage(msg)
		if err != nil {
			return nil, WrapError(err, ErrInvalidMessage).
				WithDetail("message_index", i).
				WithDetail("role", msg.Role)
		}
		result = append(result, converted)
	}

	return result, nil
}

func convertMessage(msg llm.Message) (openai.ChatCompletionMessageParamUnion, error) {
	switch msg.Role {
	case llm.RoleSystem:
		return openai.SystemMessage(msg.Content), nil

	case llm.RoleUser:
		if msg.IsMultimodal() {
			parts, err := convertContentParts(msg.MultiContent)
			if err != nil {
				return openai.ChatCompletionMessageParamUnion{}, err
			}
			return openai.UserMessage(parts), nil
		}
		return openai.UserMessage(msg.Content), nil

	case llm.RoleAssistant:
		if len(msg.ToolCalls) > 0 {
			toolCalls := make([]openai.ChatCompletionMessageToolCallUnionParam, 0, len(msg.ToolCalls))
			for _, tc := range msg.ToolCalls {
				toolCalls = append(toolCalls, openai.ChatCompletionMessageToolCallUnionParam{
					OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
						ID:   tc.ID,
						Type: constant.Function("function"),
						Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
							Name:      tc.Function.Name,
							Arguments: tc.Function.Arguments,
						},
					},
				})
			}
			return openai.ChatCompletionMessageParamUnion{
				OfAssistant: &openai.ChatCompletionAssistantMessageParam{
					Role: constant.Assistant("assistant"),
					Content: openai.ChatCompletionAssistantMessageParamContentUnion{
						OfString: openai.String(msg.Content),
					},
					ToolCalls: toolCalls,
				},
			}, nil
		}
		return openai.AssistantMessage(msg.Content), nil

	case llm.RoleTool:
		return openai.ToolMessage(msg.Content, msg.ToolCallID), nil

	case llm.RoleFunction:
		return openai.ChatCompletionMessageParamUnion{
			OfTool: &openai.ChatCompletionToolMessageParam{
				Content: openai.ChatCompletionToolMessageParamContentUnion{
					OfString: openai.String(msg.Content),
				},
				ToolCallID: msg.Name,
			},
		}, nil

	default:
		return openai.ChatCompletionMessageParamUnion{},
			errorRegistry.New(ErrUnsupportedRole).WithDetail("role", msg.Role)
	}
}

func convertContentParts(parts []llm.ContentPart) ([]openai.ChatCompletionContentPartUnionParam, error) {
	result := make([]openai.ChatCompletionContentPartUnionParam, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case llm.ContentPartTypeText:
			result = append(result, openai.TextContentPart(part.Text))
		case llm.ContentPartTypeImageURL:
			if part.ImageURL == nil {
				return nil, errorRegistry.New(ErrInvalidMessage).
					WithDetail("error", "image_url content part missing image_url")
			}
			imgParam := openai.ChatCompletionContentPartImageImageURLParam{
				URL:    part.ImageURL.URL,
				Detail: string(part.ImageURL.Detail),
			}
			result = append(result, openai.ImageContentPart(imgParam))
		default:
			return nil, errorRegistry.New(ErrInvalidMessage).
				WithDetail("error", fmt.Sprintf("unsupported content part type: %s", part.Type))
		}
	}
	return result, nil
}

func applyOptions(params *openai.ChatCompletionNewParams, options *llm.ChatOptions) {
	if options.Temperature != 0 {
		params.Temperature = openai.Float(float64(options.Temperature))
	}
	if options.TopP != 0 {
		params.TopP = openai.Float(float64(options.TopP))
	}
	if options.MaxCompletionTokens > 0 {
		params.MaxCompletionTokens = openai.Int(int64(options.MaxCompletionTokens))
	} else if options.MaxTokens > 0 {
		params.MaxTokens = openai.Int(int64(options.MaxTokens))
	}
	if options.PresencePenalty != 0 {
		params.PresencePenalty = openai.Float(float64(options.PresencePenalty))
	}
	if options.FrequencyPenalty != 0 {
		params.FrequencyPenalty = openai.Float(float64(options.FrequencyPenalty))
	}
	if len(options.Stop) > 0 {
		params.Stop = openai.ChatCompletionNewParamsStopUnion{
			OfStringArray: options.Stop,
		}
	}
	if options.Seed != 0 {
		params.Seed = openai.Int(options.Seed)
	}
	if options.User != "" {
		params.User = openai.String(options.User)
	}

	// Convert tools
	if len(options.Tools) > 0 || len(options.Functions) > 0 {
		tools := convertTools(options.Tools, options.Functions)
		if len(tools) > 0 {
			params.Tools = tools
		}
	}

	if options.ToolChoice != nil {
		params.ToolChoice = convertToolChoice(options.ToolChoice)
	}
}

func convertTools(tools []llm.Tool, functions []llm.Function) []openai.ChatCompletionToolUnionParam {
	var result []openai.ChatCompletionToolUnionParam

	for _, tool := range tools {
		if tool.Type == "function" {
			paramsJSON, err := json.Marshal(tool.Function.Parameters)
			if err != nil {
				continue
			}
			var parametersMap map[string]any
			if err := json.Unmarshal(paramsJSON, &parametersMap); err != nil {
				continue
			}
			result = append(result, openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{
				Name:        tool.Function.Name,
				Description: openai.String(tool.Function.Description),
				Parameters:  openai.FunctionParameters(parametersMap),
			}))
		}
	}

	for _, fn := range functions {
		paramsJSON, err := json.Marshal(fn.Parameters)
		if err != nil {
			continue
		}
		var parametersMap map[string]any
		if err := json.Unmarshal(paramsJSON, &parametersMap); err != nil {
			continue
		}
		result = append(result, openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{
			Name:        fn.Name,
			Description: openai.String(fn.Description),
			Parameters:  openai.FunctionParameters(parametersMap),
		}))
	}

	return result
}

func convertToolChoice(toolChoice any) openai.ChatCompletionToolChoiceOptionUnionParam {
	if strChoice, ok := toolChoice.(string); ok {
		switch strChoice {
		case "auto":
			return openai.ChatCompletionToolChoiceOptionUnionParam{
				OfAuto: openai.String("auto"),
			}
		case "none":
			return openai.ChatCompletionToolChoiceOptionUnionParam{
				OfAuto: openai.String("none"),
			}
		case "required":
			return openai.ChatCompletionToolChoiceOptionUnionParam{
				OfAuto: openai.String("required"),
			}
		}
	}

	return openai.ChatCompletionToolChoiceOptionUnionParam{
		OfAuto: openai.String("auto"),
	}
}

func convertFromResponse(completion *openai.ChatCompletion) (llm.Response, error) {
	if len(completion.Choices) == 0 {
		return llm.Response{}, errorRegistry.New(ErrNoChoicesInResponse)
	}

	choice := completion.Choices[0]

	message := llm.Message{
		Role:    string(choice.Message.Role),
		Content: choice.Message.Content,
	}

	if len(choice.Message.ToolCalls) > 0 {
		toolCalls := make([]llm.ToolCall, 0, len(choice.Message.ToolCalls))
		for _, tc := range choice.Message.ToolCalls {
			toolCalls = append(toolCalls, llm.ToolCall{
				ID:   tc.ID,
				Type: "function",
				Function: llm.FunctionCall{
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
				},
			})
		}
		message.ToolCalls = toolCalls
	}

	usage := llm.Usage{
		PromptTokens:     int(completion.Usage.PromptTokens),
		CompletionTokens: int(completion.Usage.CompletionTokens),
		TotalTokens:      int(completion.Usage.TotalTokens),
	}

	return llm.Response{
		Message: message,
		Usage:   usage,
	}, nil
}

func convertToFloat32Slice(input []float64) []float32 {
	result := make([]float32, len(input))
	for i, v := range input {
		result[i] = float32(v)
	}
	return result
}
