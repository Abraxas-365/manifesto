package aiopenai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Abraxas-365/manifesto/pkg/ai/embedding"
	"github.com/Abraxas-365/manifesto/pkg/ai/llm"
	"github.com/Abraxas-365/manifesto/pkg/ai/speech"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/shared"
	"github.com/openai/openai-go/v3/shared/constant"
)

// OpenAIProvider implements the LLM interface for OpenAI
type OpenAIProvider struct {
	client openai.Client
	apiKey string
}

// NewOpenAIProvider creates a new OpenAI provider
func NewOpenAIProvider(apiKey string, opts ...option.RequestOption) *OpenAIProvider {
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}

	options := append([]option.RequestOption{option.WithAPIKey(apiKey)}, opts...)
	client := openai.NewClient(options...)

	return &OpenAIProvider{
		client: client,
		apiKey: apiKey,
	}
}

func defaultChatOptions() *llm.ChatOptions {
	options := llm.DefaultOptions()
	options.Model = "gpt-4o"
	return options
}

// ============================================================================
// Chat Implementation
// ============================================================================

// Chat implements the LLM interface
func (p *OpenAIProvider) Chat(ctx context.Context, messages []llm.Message, opts ...llm.Option) (llm.Response, error) {
	// Validate API key
	if p.apiKey == "" {
		return llm.Response{}, errorRegistry.New(ErrMissingAPIKey)
	}

	// Validate messages
	if len(messages) == 0 {
		return llm.Response{}, errorRegistry.New(ErrEmptyMessages)
	}

	options := defaultChatOptions()
	for _, opt := range opts {
		opt(options)
	}

	// Convert messages
	openAIMessages := make([]openai.ChatCompletionMessageParamUnion, 0, len(messages))
	for i, msg := range messages {
		openAIMsg, err := convertToOpenAIMessage(msg)
		if err != nil {
			return llm.Response{}, WrapError(err, ErrInvalidMessage).
				WithDetail("message_index", i).
				WithDetail("role", msg.Role)
		}
		openAIMessages = append(openAIMessages, openAIMsg)
	}

	// Prepare params
	params := openai.ChatCompletionNewParams{
		Messages: openAIMessages,
		Model:    options.Model,
	}

	// Set optional parameters
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

	// Add LogitBias
	if len(options.LogitBias) > 0 {
		logitBias := make(map[string]int64)
		for k, v := range options.LogitBias {
			logitBias[fmt.Sprintf("%d", k)] = int64(v)
		}
		params.LogitBias = logitBias
	}

	// Set reasoning effort
	if options.ReasoningEffort != "" {
		params.ReasoningEffort = convertToOpenAIReasoningEffort(options.ReasoningEffort)
	}

	// Convert tools
	if len(options.Tools) > 0 || len(options.Functions) > 0 {
		tools, err := convertToOpenAITools(options.Tools, options.Functions)
		if err != nil {
			return llm.Response{}, WrapError(err, ErrConversionFailed).
				WithDetail("error", "failed to convert tools")
		}
		if len(tools) > 0 {
			params.Tools = tools
		}
	}

	// Set tool choice
	if options.ToolChoice != nil {
		params.ToolChoice = convertToOpenAIToolChoice(options.ToolChoice)
	}

	// Set response format
	if options.JSONMode {
		params.ResponseFormat = convertToJSONFormatParam()
	} else if options.ResponseFormat != nil {
		format, err := convertToResponseFormatParam(options.ResponseFormat)
		if err != nil {
			return llm.Response{}, WrapError(err, ErrConversionFailed).
				WithDetail("error", "failed to convert response format")
		}
		params.ResponseFormat = format
	}

	// Make the API call
	completion, err := p.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return llm.Response{}, ParseOpenAIError(err).
			WithDetail("model", options.Model).
			WithDetail("num_messages", len(messages))
	}

	// Convert the response
	response, err := convertFromOpenAIResponse(completion)
	if err != nil {
		return llm.Response{}, WrapError(err, ErrAPIResponse).
			WithDetail("error", "failed to parse response")
	}

	return response, nil
}

// ============================================================================
// Chat Stream Implementation
// ============================================================================

// ChatStream implements streaming for Chat Completions API
func (p *OpenAIProvider) ChatStream(ctx context.Context, messages []llm.Message, opts ...llm.Option) (llm.Stream, error) {
	// Validate API key
	if p.apiKey == "" {
		return nil, errorRegistry.New(ErrMissingAPIKey)
	}

	// Validate messages
	if len(messages) == 0 {
		return nil, errorRegistry.New(ErrEmptyMessages)
	}

	options := defaultChatOptions()
	for _, opt := range opts {
		opt(options)
	}

	// Convert messages
	openAIMessages := make([]openai.ChatCompletionMessageParamUnion, 0, len(messages))
	for i, msg := range messages {
		openAIMsg, err := convertToOpenAIMessage(msg)
		if err != nil {
			return nil, WrapError(err, ErrInvalidMessage).
				WithDetail("message_index", i).
				WithDetail("role", msg.Role)
		}
		openAIMessages = append(openAIMessages, openAIMsg)
	}

	// Prepare params
	params := openai.ChatCompletionNewParams{
		Messages: openAIMessages,
		Model:    options.Model,
	}

	// Set optional parameters (same as Chat)
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
	if len(options.Stop) > 0 {
		params.Stop = openai.ChatCompletionNewParamsStopUnion{
			OfStringArray: options.Stop,
		}
	}
	if options.ReasoningEffort != "" {
		params.ReasoningEffort = convertToOpenAIReasoningEffort(options.ReasoningEffort)
	}

	// Convert tools
	if len(options.Tools) > 0 || len(options.Functions) > 0 {
		tools, err := convertToOpenAITools(options.Tools, options.Functions)
		if err != nil {
			return nil, WrapError(err, ErrConversionFailed)
		}
		if len(tools) > 0 {
			params.Tools = tools
		}
	}

	if options.ToolChoice != nil {
		params.ToolChoice = convertToOpenAIToolChoice(options.ToolChoice)
	}

	if options.JSONMode {
		params.ResponseFormat = convertToJSONFormatParam()
	} else if options.ResponseFormat != nil {
		format, err := convertToResponseFormatParam(options.ResponseFormat)
		if err != nil {
			return nil, WrapError(err, ErrConversionFailed)
		}
		params.ResponseFormat = format
	}

	// Create the stream
	sseStream := p.client.Chat.Completions.NewStreaming(ctx, params)

	// Return our stream adapter
	return &openAIStream{
		stream:      sseStream,
		accumulator: openai.ChatCompletionAccumulator{},
	}, nil
}

// ============================================================================
// Embedding Implementation
// ============================================================================

func (p *OpenAIProvider) EmbedDocuments(ctx context.Context, documents []string, opts ...embedding.Option) ([]embedding.Embedding, error) {
	// Validate input
	if len(documents) == 0 {
		return nil, errorRegistry.New(ErrEmptyEmbeddingInput)
	}

	options := embedding.DefaultOptions()
	for _, opt := range opts {
		opt(options)
	}

	params := openai.EmbeddingNewParams{
		Input: openai.EmbeddingNewParamsInputUnion{
			OfArrayOfStrings: documents,
		},
	}

	if options.Model != "" {
		params.Model = options.Model
	} else {
		params.Model = "text-embedding-3-small"
	}

	if options.Dimensions > 0 {
		params.Dimensions = openai.Int(int64(options.Dimensions))
	}

	if options.User != "" {
		params.User = openai.String(options.User)
	}

	resp, err := p.client.Embeddings.New(ctx, params)
	if err != nil {
		return nil, ParseOpenAIError(err).
			WithDetail("model", params.Model).
			WithDetail("num_documents", len(documents))
	}

	if len(resp.Data) == 0 {
		return nil, errorRegistry.New(ErrNoEmbeddingReturned).
			WithDetail("num_documents", len(documents))
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

func (p *OpenAIProvider) EmbedQuery(ctx context.Context, text string, opts ...embedding.Option) (embedding.Embedding, error) {
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
// Speech Synthesis Implementation
// ============================================================================

func (p *OpenAIProvider) Synthesize(ctx context.Context, text string, opts ...speech.SynthesisOption) (speech.Audio, error) {
	if text == "" {
		return speech.Audio{}, errorRegistry.New(ErrEmptySpeechInput)
	}

	options := speech.SynthesisOptions{
		Model:       string(openai.SpeechModelTTS1),
		Voice:       "alloy",
		AudioFormat: speech.AudioFormatMP3,
		SpeechRate:  1.0,
	}

	for _, opt := range opts {
		opt(&options)
	}

	responseFormat := openai.AudioSpeechNewParamsResponseFormatMP3
	switch options.AudioFormat {
	case speech.AudioFormatMP3:
		responseFormat = openai.AudioSpeechNewParamsResponseFormatMP3
	case speech.AudioFormatPCM:
		responseFormat = openai.AudioSpeechNewParamsResponseFormatPCM
	case speech.AudioFormatOGG:
		responseFormat = openai.AudioSpeechNewParamsResponseFormatOpus
	case speech.AudioFormatWAV:
		responseFormat = openai.AudioSpeechNewParamsResponseFormatPCM
	}

	voice := openai.AudioSpeechNewParamsVoiceAlloy
	switch strings.ToLower(options.Voice) {
	case "alloy":
		voice = openai.AudioSpeechNewParamsVoiceAlloy
	case "echo":
		voice = openai.AudioSpeechNewParamsVoiceEcho
	case "fable":
	case "shimmer":
		voice = openai.AudioSpeechNewParamsVoiceShimmer
	default:
		voice = openai.AudioSpeechNewParamsVoiceAlloy
	}

	params := openai.AudioSpeechNewParams{
		Model:          options.Model,
		Input:          text,
		Voice:          voice,
		ResponseFormat: responseFormat,
	}

	if options.SpeechRate != 1.0 {
		params.Speed = param.NewOpt(float64(options.SpeechRate))
	}

	res, err := p.client.Audio.Speech.New(ctx, params)
	if err != nil {
		return speech.Audio{}, ParseOpenAIError(err).
			WithDetail("model", options.Model).
			WithDetail("voice", options.Voice)
	}

	sampleRate := 24000
	if options.SampleRate > 0 {
		sampleRate = options.SampleRate
	}

	return speech.Audio{
		Content:    res.Body,
		Format:     options.AudioFormat,
		SampleRate: sampleRate,
		Usage: speech.TTSUsage{
			InputCharacters: len(text),
		},
	}, nil
}

// ============================================================================
// Speech Transcription Implementation
// ============================================================================

func (p *OpenAIProvider) Transcribe(ctx context.Context, audio io.Reader, opts ...speech.TranscriptionOption) (speech.Transcript, error) {
	if audio == nil {
		return speech.Transcript{}, errorRegistry.New(ErrEmptySpeechInput).
			WithDetail("error", "audio reader cannot be nil")
	}

	options := speech.TranscriptionOptions{
		Model:      string(openai.AudioModelWhisper1),
		Language:   "",
		Timestamps: false,
	}

	for _, opt := range opts {
		opt(&options)
	}

	params := openai.AudioTranscriptionNewParams{
		Model: options.Model,
		File:  audio,
	}

	if options.Language != "" {
		params.Language = param.NewOpt(options.Language)
	}

	response, err := p.client.Audio.Transcriptions.New(ctx, params)
	if err != nil {
		return speech.Transcript{}, ParseOpenAIError(err).
			WithDetail("model", options.Model)
	}

	result := speech.Transcript{
		Text: response.Text,
	}

	return result, nil
}

// ============================================================================
// Stream Implementation
// ============================================================================

type openAIStream struct {
	stream interface {
		Next() bool
		Current() openai.ChatCompletionChunk
		Err() error
	}
	accumulator openai.ChatCompletionAccumulator
	lastError   error
	current     llm.Message
}

func (s *openAIStream) Next() (llm.Message, error) {
	if s.lastError != nil {
		return llm.Message{}, s.lastError
	}

	if !s.stream.Next() {
		if err := s.stream.Err(); err != nil {
			s.lastError = ParseOpenAIError(err)
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

	// ✅ Use INDEX-based accumulation, not ID-based.
	// OpenAI only sends the ID on the first delta for each tool call.
	// All subsequent argument chunks have an empty ID but carry the correct Index.
	for _, tc := range delta.ToolCalls {
		idx := int(tc.Index)

		// Grow the slice to accommodate this index
		for len(s.current.ToolCalls) <= idx {
			s.current.ToolCalls = append(s.current.ToolCalls, llm.ToolCall{Type: "function"})
		}

		// Only set ID and Name when they arrive (first chunk for this tool call)
		if tc.ID != "" {
			s.current.ToolCalls[idx].ID = tc.ID
		}
		if tc.Function.Name != "" {
			s.current.ToolCalls[idx].Function.Name += tc.Function.Name
		}
		// Arguments accumulate across ALL chunks for this tool call
		s.current.ToolCalls[idx].Function.Arguments += tc.Function.Arguments
	}

	return llm.Message{
		Role:      llm.RoleAssistant,
		Content:   delta.Content,       // delta only
		ToolCalls: s.current.ToolCalls, // full accumulated snapshot
	}, nil
}

func (s *openAIStream) Close() error {
	return nil
}

// ============================================================================
// Helper Functions
// ============================================================================

func convertToOpenAIMessage(msg llm.Message) (openai.ChatCompletionMessageParamUnion, error) {
	switch msg.Role {
	case llm.RoleSystem:
		return openai.SystemMessage(msg.Content), nil
	case llm.RoleUser:
		if msg.IsMultimodal() {
			parts, err := convertToOpenAIContentParts(msg.MultiContent)
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
	case llm.RoleFunction:
		return openai.ChatCompletionMessageParamUnion{
			OfTool: &openai.ChatCompletionToolMessageParam{
				Content: openai.ChatCompletionToolMessageParamContentUnion{
					OfString: openai.String(msg.Content),
				},
				ToolCallID: msg.Name,
			},
		}, nil
	case llm.RoleTool:
		return openai.ToolMessage(msg.Content, msg.ToolCallID), nil
	default:
		return openai.ChatCompletionMessageParamUnion{},
			errorRegistry.New(ErrUnsupportedRole).
				WithDetail("role", msg.Role)
	}
}

func convertToOpenAITools(tools []llm.Tool, functions []llm.Function) ([]openai.ChatCompletionToolUnionParam, error) {
	result := make([]openai.ChatCompletionToolUnionParam, 0)

	for _, tool := range tools {
		if tool.Type == "function" {
			paramsJSON, err := json.Marshal(tool.Function.Parameters)
			if err != nil {
				return nil, WrapError(err, ErrJSONParsing).
					WithDetail("tool", tool.Function.Name)
			}

			var parametersMap map[string]any
			if err := json.Unmarshal(paramsJSON, &parametersMap); err != nil {
				return nil, WrapError(err, ErrJSONParsing).
					WithDetail("tool", tool.Function.Name)
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
			return nil, WrapError(err, ErrJSONParsing).
				WithDetail("function", fn.Name)
		}

		var parametersMap map[string]any
		if err := json.Unmarshal(paramsJSON, &parametersMap); err != nil {
			return nil, WrapError(err, ErrJSONParsing).
				WithDetail("function", fn.Name)
		}

		result = append(result, openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{
			Name:        fn.Name,
			Description: openai.String(fn.Description),
			Parameters:  openai.FunctionParameters(parametersMap),
		}))
	}

	return result, nil
}

func convertToOpenAIToolChoice(toolChoice any) openai.ChatCompletionToolChoiceOptionUnionParam {
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

func convertToOpenAIReasoningEffort(effort string) shared.ReasoningEffort {
	switch strings.ToLower(effort) {
	case "low":
		return shared.ReasoningEffortLow
	case "medium":
		return shared.ReasoningEffortMedium
	case "high":
		return shared.ReasoningEffortHigh
	case "minimal":
		return shared.ReasoningEffortMinimal
	default:
		return shared.ReasoningEffortMedium
	}
}

func convertToJSONFormatParam() openai.ChatCompletionNewParamsResponseFormatUnion {
	return openai.ChatCompletionNewParamsResponseFormatUnion{
		OfJSONObject: &shared.ResponseFormatJSONObjectParam{},
	}
}

func convertToResponseFormatParam(format *llm.ResponseFormat) (openai.ChatCompletionNewParamsResponseFormatUnion, error) {
	switch format.Type {
	case llm.JSONObject:
		return openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONObject: &shared.ResponseFormatJSONObjectParam{},
		}, nil
	case llm.JSONSchema:
		schema, ok := format.JSONSchema.(map[string]any)
		if !ok {
			schemaBytes, err := json.Marshal(format.JSONSchema)
			if err != nil {
				return openai.ChatCompletionNewParamsResponseFormatUnion{},
					WrapError(err, ErrJSONParsing)
			}

			var schemaMap map[string]any
			if err := json.Unmarshal(schemaBytes, &schemaMap); err != nil {
				return openai.ChatCompletionNewParamsResponseFormatUnion{},
					WrapError(err, ErrJSONParsing)
			}
			schema = schemaMap
		}

		return openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &shared.ResponseFormatJSONSchemaParam{
				JSONSchema: shared.ResponseFormatJSONSchemaJSONSchemaParam{
					Name:   "schema",
					Schema: schema,
				},
			},
		}, nil
	default:
		return openai.ChatCompletionNewParamsResponseFormatUnion{
			OfText: &shared.ResponseFormatTextParam{},
		}, nil
	}
}

func convertFromOpenAIResponse(completion *openai.ChatCompletion) (llm.Response, error) {
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

func convertToOpenAIContentParts(parts []llm.ContentPart) ([]openai.ChatCompletionContentPartUnionParam, error) {
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
		case llm.ContentPartTypeInputAudio:
			if part.InputAudio == nil {
				return nil, errorRegistry.New(ErrInvalidMessage).
					WithDetail("error", "input_audio content part missing input_audio")
			}
			audioParam := openai.ChatCompletionContentPartInputAudioInputAudioParam{
				Data:   part.InputAudio.Data,
				Format: part.InputAudio.Format,
			}
			result = append(result, openai.InputAudioContentPart(audioParam))
		case llm.ContentPartTypeFile:
			if part.File == nil {
				return nil, errorRegistry.New(ErrInvalidMessage).
					WithDetail("error", "file content part missing file")
			}
			fileParam := openai.ChatCompletionContentPartFileFileParam{}
			if part.File.FileID != "" {
				fileParam.FileID = param.NewOpt(part.File.FileID)
			}
			if part.File.FileData != "" {
				fileParam.FileData = param.NewOpt(part.File.FileData)
			}
			if part.File.Filename != "" {
				fileParam.Filename = param.NewOpt(part.File.Filename)
			}
			result = append(result, openai.FileContentPart(fileParam))
		default:
			return nil, errorRegistry.New(ErrInvalidMessage).
				WithDetail("error", fmt.Sprintf("unsupported content part type: %s", part.Type))
		}
	}
	return result, nil
}

func convertToFloat32Slice(input []float64) []float32 {
	result := make([]float32, len(input))
	for i, v := range input {
		result[i] = float32(v)
	}
	return result
}
