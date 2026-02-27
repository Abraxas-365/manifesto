package aigemini

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/Abraxas-365/manifesto/pkg/ai/embedding"
	"github.com/Abraxas-365/manifesto/pkg/ai/llm"
	"google.golang.org/genai"
)

// ProviderOption configures the Gemini provider
type ProviderOption func(*GeminiProvider)

// WithVertexAI configures the provider to use Vertex AI backend
func WithVertexAI(project, location string) ProviderOption {
	return func(p *GeminiProvider) {
		p.project = project
		p.location = location
		p.useVertexAI = true
	}
}

// WithEmbeddingModel sets the default embedding model
func WithEmbeddingModel(model string) ProviderOption {
	return func(p *GeminiProvider) {
		p.embeddingModel = model
	}
}

// GeminiProvider implements the LLM and Embedder interfaces for Google Gemini
type GeminiProvider struct {
	client         *genai.Client
	apiKey         string
	project        string
	location       string
	useVertexAI    bool
	embeddingModel string
}

// NewGeminiProvider creates a new Gemini provider
func NewGeminiProvider(ctx context.Context, apiKey string, opts ...ProviderOption) (*GeminiProvider, error) {
	p := &GeminiProvider{
		apiKey:         apiKey,
		embeddingModel: "text-embedding-004",
	}

	for _, opt := range opts {
		opt(p)
	}

	if p.apiKey == "" {
		p.apiKey = os.Getenv("GEMINI_API_KEY")
	}

	config := &genai.ClientConfig{}

	if p.useVertexAI {
		config.Backend = genai.BackendVertexAI
		config.Project = p.project
		config.Location = p.location
	} else {
		config.APIKey = p.apiKey
		config.Backend = genai.BackendGeminiAPI
	}

	client, err := genai.NewClient(ctx, config)
	if err != nil {
		return nil, WrapError(err, ErrMissingAPIKey).
			WithDetail("error", "failed to create Gemini client")
	}

	p.client = client
	return p, nil
}

func defaultChatOptions() *llm.ChatOptions {
	options := llm.DefaultOptions()
	options.Model = "gemini-2.0-flash"
	return options
}

// ============================================================================
// Chat Implementation
// ============================================================================

// Chat implements the LLM interface
func (p *GeminiProvider) Chat(ctx context.Context, messages []llm.Message, opts ...llm.Option) (llm.Response, error) {
	if len(messages) == 0 {
		return llm.Response{}, errorRegistry.New(ErrEmptyMessages)
	}

	options := defaultChatOptions()
	for _, opt := range opts {
		opt(options)
	}

	// Extract system instruction and convert messages
	systemContent, contents := convertMessages(messages)

	config := buildGenerateConfig(options, systemContent)

	result, err := p.client.Models.GenerateContent(ctx, options.Model, contents, config)
	if err != nil {
		return llm.Response{}, ParseGeminiError(err).
			WithDetail("model", options.Model).
			WithDetail("num_messages", len(messages))
	}

	return convertFromGeminiResponse(result)
}

// ============================================================================
// Chat Stream Implementation
// ============================================================================

// ChatStream implements streaming for Gemini
func (p *GeminiProvider) ChatStream(ctx context.Context, messages []llm.Message, opts ...llm.Option) (llm.Stream, error) {
	if len(messages) == 0 {
		return nil, errorRegistry.New(ErrEmptyMessages)
	}

	options := defaultChatOptions()
	for _, opt := range opts {
		opt(options)
	}

	systemContent, contents := convertMessages(messages)
	config := buildGenerateConfig(options, systemContent)

	iter := p.client.Models.GenerateContentStream(ctx, options.Model, contents, config)

	// Convert push-based iterator to pull-based using a channel
	ch := make(chan streamResult, 1)
	done := make(chan struct{})

	go func() {
		defer close(ch)
		iter(func(resp *genai.GenerateContentResponse, err error) bool {
			select {
			case ch <- streamResult{resp: resp, err: err}:
				return true
			case <-done:
				return false
			}
		})
	}()

	return &geminiStream{
		ch:   ch,
		done: done,
	}, nil
}

type streamResult struct {
	resp *genai.GenerateContentResponse
	err  error
}

// ============================================================================
// Embedding Implementation
// ============================================================================

// EmbedDocuments converts documents to embeddings
func (p *GeminiProvider) EmbedDocuments(ctx context.Context, documents []string, opts ...embedding.Option) ([]embedding.Embedding, error) {
	if len(documents) == 0 {
		return nil, errorRegistry.New(ErrEmptyEmbeddingInput)
	}

	options := embedding.DefaultOptions()
	for _, opt := range opts {
		opt(options)
	}

	model := p.embeddingModel
	if options.Model != "" {
		model = options.Model
	}

	// Embed each document
	var contents []*genai.Content
	for _, doc := range documents {
		contents = append(contents, &genai.Content{
			Parts: []*genai.Part{genai.NewPartFromText(doc)},
		})
	}

	config := &genai.EmbedContentConfig{}
	if options.Dimensions > 0 {
		dim := int32(options.Dimensions)
		config.OutputDimensionality = &dim
	}

	resp, err := p.client.Models.EmbedContent(ctx, model, contents, config)
	if err != nil {
		return nil, ParseGeminiError(err).
			WithDetail("model", model).
			WithDetail("num_documents", len(documents))
	}

	if resp == nil || len(resp.Embeddings) == 0 {
		return nil, errorRegistry.New(ErrNoEmbeddingReturned)
	}

	embeddings := make([]embedding.Embedding, len(resp.Embeddings))
	for i, emb := range resp.Embeddings {
		embeddings[i] = embedding.Embedding{
			Vector: emb.Values,
		}
	}

	return embeddings, nil
}

// EmbedQuery converts a single query to an embedding
func (p *GeminiProvider) EmbedQuery(ctx context.Context, text string, opts ...embedding.Option) (embedding.Embedding, error) {
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

type geminiStream struct {
	ch        chan streamResult
	done      chan struct{}
	toolCalls []llm.ToolCall
	lastError error
}

func (s *geminiStream) Next() (llm.Message, error) {
	if s.lastError != nil {
		return llm.Message{}, s.lastError
	}

	result, ok := <-s.ch
	if !ok {
		s.lastError = io.EOF
		return llm.Message{}, io.EOF
	}

	if result.err != nil {
		s.lastError = ParseGeminiError(result.err)
		return llm.Message{}, s.lastError
	}

	if result.resp == nil || len(result.resp.Candidates) == 0 {
		return llm.Message{Role: llm.RoleAssistant}, nil
	}

	candidate := result.resp.Candidates[0]
	if candidate.Content == nil {
		return llm.Message{Role: llm.RoleAssistant}, nil
	}

	var textContent string
	for _, part := range candidate.Content.Parts {
		if part.Text != "" {
			textContent += part.Text
		}
		if part.FunctionCall != nil {
			args, _ := json.Marshal(part.FunctionCall.Args)
			id := part.FunctionCall.ID
			if id == "" {
				id = fmt.Sprintf("call_%s_%d", part.FunctionCall.Name, len(s.toolCalls))
			}
			s.toolCalls = append(s.toolCalls, llm.ToolCall{
				ID:   id,
				Type: "function",
				Function: llm.FunctionCall{
					Name:      part.FunctionCall.Name,
					Arguments: string(args),
				},
			})
		}
	}

	return llm.Message{
		Role:      llm.RoleAssistant,
		Content:   textContent,
		ToolCalls: s.toolCalls,
	}, nil
}

func (s *geminiStream) Close() error {
	close(s.done)
	return nil
}

// ============================================================================
// Helper Functions
// ============================================================================

func convertMessages(messages []llm.Message) (*genai.Content, []*genai.Content) {
	var systemContent *genai.Content
	var contents []*genai.Content

	for _, msg := range messages {
		switch msg.Role {
		case llm.RoleSystem:
			if systemContent == nil {
				systemContent = &genai.Content{
					Parts: []*genai.Part{genai.NewPartFromText(msg.TextContent())},
				}
			} else {
				systemContent.Parts = append(systemContent.Parts,
					genai.NewPartFromText(msg.TextContent()))
			}

		case llm.RoleUser:
			parts := convertUserParts(msg)
			contents = append(contents, &genai.Content{
				Role:  "user",
				Parts: parts,
			})

		case llm.RoleAssistant:
			parts := convertAssistantParts(msg)
			contents = append(contents, &genai.Content{
				Role:  "model",
				Parts: parts,
			})

		case llm.RoleTool:
			contents = append(contents, &genai.Content{
				Role: "user",
				Parts: []*genai.Part{
					genai.NewPartFromFunctionResponse(msg.ToolCallID, map[string]any{
						"output": msg.Content,
					}),
				},
			})

		case llm.RoleFunction:
			contents = append(contents, &genai.Content{
				Role: "user",
				Parts: []*genai.Part{
					genai.NewPartFromFunctionResponse(msg.Name, map[string]any{
						"output": msg.Content,
					}),
				},
			})
		}
	}

	return systemContent, contents
}

func convertUserParts(msg llm.Message) []*genai.Part {
	if msg.IsMultimodal() {
		var parts []*genai.Part
		for _, p := range msg.MultiContent {
			switch p.Type {
			case llm.ContentPartTypeText:
				parts = append(parts, genai.NewPartFromText(p.Text))
			case llm.ContentPartTypeImageURL:
				if p.ImageURL != nil {
					parts = append(parts, genai.NewPartFromURI(p.ImageURL.URL, "image/jpeg"))
				}
			}
		}
		return parts
	}

	return []*genai.Part{genai.NewPartFromText(msg.Content)}
}

func convertAssistantParts(msg llm.Message) []*genai.Part {
	var parts []*genai.Part

	if msg.Content != "" {
		parts = append(parts, genai.NewPartFromText(msg.Content))
	}

	for _, tc := range msg.ToolCalls {
		var args map[string]any
		if tc.Function.Arguments != "" {
			_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
		}
		if args == nil {
			args = map[string]any{}
		}
		parts = append(parts, genai.NewPartFromFunctionCall(tc.Function.Name, args))
	}

	return parts
}

func buildGenerateConfig(options *llm.ChatOptions, systemContent *genai.Content) *genai.GenerateContentConfig {
	config := &genai.GenerateContentConfig{}

	if systemContent != nil {
		config.SystemInstruction = systemContent
	}

	if options.Temperature != 0 {
		config.Temperature = genai.Ptr(options.Temperature)
	}
	if options.TopP != 0 {
		config.TopP = genai.Ptr(options.TopP)
	}
	if options.MaxCompletionTokens > 0 {
		config.MaxOutputTokens = int32(options.MaxCompletionTokens)
	} else if options.MaxTokens > 0 {
		config.MaxOutputTokens = int32(options.MaxTokens)
	}
	if len(options.Stop) > 0 {
		config.StopSequences = options.Stop
	}
	if options.PresencePenalty != 0 {
		config.PresencePenalty = genai.Ptr(options.PresencePenalty)
	}
	if options.FrequencyPenalty != 0 {
		config.FrequencyPenalty = genai.Ptr(options.FrequencyPenalty)
	}
	if options.Seed != 0 {
		seed := int32(options.Seed)
		config.Seed = &seed
	}

	// Response format
	if options.JSONMode {
		config.ResponseMIMEType = "application/json"
	} else if options.ResponseFormat != nil {
		switch options.ResponseFormat.Type {
		case llm.JSONObject:
			config.ResponseMIMEType = "application/json"
		case llm.JSONSchema:
			config.ResponseMIMEType = "application/json"
			if options.ResponseFormat.JSONSchema != nil {
				config.ResponseSchema = convertToGeminiSchema(options.ResponseFormat.JSONSchema)
			}
		}
	}

	// Convert tools
	if len(options.Tools) > 0 || len(options.Functions) > 0 {
		tools := convertToGeminiTools(options.Tools, options.Functions)
		if len(tools) > 0 {
			config.Tools = tools
		}

		// Tool choice
		if options.ToolChoice != nil {
			config.ToolConfig = convertGeminiToolConfig(options.ToolChoice)
		}
	}

	return config
}

func convertToGeminiTools(tools []llm.Tool, functions []llm.Function) []*genai.Tool {
	var declarations []*genai.FunctionDeclaration

	for _, tool := range tools {
		if tool.Type != "function" {
			continue
		}
		decl := &genai.FunctionDeclaration{
			Name:        tool.Function.Name,
			Description: tool.Function.Description,
		}
		if tool.Function.Parameters != nil {
			decl.Parameters = convertToGeminiSchema(tool.Function.Parameters)
		}
		declarations = append(declarations, decl)
	}

	for _, fn := range functions {
		decl := &genai.FunctionDeclaration{
			Name:        fn.Name,
			Description: fn.Description,
		}
		if fn.Parameters != nil {
			decl.Parameters = convertToGeminiSchema(fn.Parameters)
		}
		declarations = append(declarations, decl)
	}

	if len(declarations) == 0 {
		return nil
	}

	return []*genai.Tool{{
		FunctionDeclarations: declarations,
	}}
}

func convertToGeminiSchema(params any) *genai.Schema {
	var m map[string]any
	switch v := params.(type) {
	case map[string]any:
		m = v
	default:
		data, err := json.Marshal(params)
		if err != nil {
			return nil
		}
		_ = json.Unmarshal(data, &m)
	}

	return mapToGeminiSchema(m)
}

func mapToGeminiSchema(m map[string]any) *genai.Schema {
	if m == nil {
		return nil
	}

	schema := &genai.Schema{}

	if t, ok := m["type"].(string); ok {
		switch t {
		case "object":
			schema.Type = genai.TypeObject
		case "array":
			schema.Type = genai.TypeArray
		case "string":
			schema.Type = genai.TypeString
		case "number":
			schema.Type = genai.TypeNumber
		case "integer":
			schema.Type = genai.TypeInteger
		case "boolean":
			schema.Type = genai.TypeBoolean
		}
	}

	if desc, ok := m["description"].(string); ok {
		schema.Description = desc
	}

	if enum, ok := m["enum"].([]any); ok {
		for _, e := range enum {
			if s, ok := e.(string); ok {
				schema.Enum = append(schema.Enum, s)
			}
		}
	}

	if props, ok := m["properties"].(map[string]any); ok {
		schema.Properties = make(map[string]*genai.Schema)
		for key, val := range props {
			if propMap, ok := val.(map[string]any); ok {
				schema.Properties[key] = mapToGeminiSchema(propMap)
			}
		}
	}

	if items, ok := m["items"].(map[string]any); ok {
		schema.Items = mapToGeminiSchema(items)
	}

	if required, ok := m["required"].([]any); ok {
		for _, r := range required {
			if s, ok := r.(string); ok {
				schema.Required = append(schema.Required, s)
			}
		}
	}

	return schema
}

func convertGeminiToolConfig(toolChoice any) *genai.ToolConfig {
	if strChoice, ok := toolChoice.(string); ok {
		switch strChoice {
		case "auto":
			return &genai.ToolConfig{
				FunctionCallingConfig: &genai.FunctionCallingConfig{
					Mode: genai.FunctionCallingConfigModeAuto,
				},
			}
		case "none":
			return &genai.ToolConfig{
				FunctionCallingConfig: &genai.FunctionCallingConfig{
					Mode: genai.FunctionCallingConfigModeNone,
				},
			}
		case "required":
			return &genai.ToolConfig{
				FunctionCallingConfig: &genai.FunctionCallingConfig{
					Mode: genai.FunctionCallingConfigModeAny,
				},
			}
		}
	}

	return nil
}

func convertFromGeminiResponse(result *genai.GenerateContentResponse) (llm.Response, error) {
	if result == nil || len(result.Candidates) == 0 {
		return llm.Response{}, errorRegistry.New(ErrAPIResponse).
			WithDetail("error", "no candidates in response")
	}

	candidate := result.Candidates[0]
	if candidate.Content == nil {
		return llm.Response{
			Message: llm.Message{Role: llm.RoleAssistant},
		}, nil
	}

	var content string
	var toolCalls []llm.ToolCall

	for _, part := range candidate.Content.Parts {
		if part.Text != "" {
			content += part.Text
		}
		if part.FunctionCall != nil {
			args, _ := json.Marshal(part.FunctionCall.Args)
			id := part.FunctionCall.ID
			if id == "" {
				id = fmt.Sprintf("call_%s_%d", part.FunctionCall.Name, len(toolCalls))
			}
			toolCalls = append(toolCalls, llm.ToolCall{
				ID:   id,
				Type: "function",
				Function: llm.FunctionCall{
					Name:      part.FunctionCall.Name,
					Arguments: string(args),
				},
			})
		}
	}

	usage := llm.Usage{}
	if result.UsageMetadata != nil {
		usage.PromptTokens = int(result.UsageMetadata.PromptTokenCount)
		usage.CompletionTokens = int(result.UsageMetadata.CandidatesTokenCount)
		usage.TotalTokens = int(result.UsageMetadata.TotalTokenCount)
	}

	return llm.Response{
		Message: llm.Message{
			Role:      llm.RoleAssistant,
			Content:   content,
			ToolCalls: toolCalls,
		},
		Usage: usage,
	}, nil
}
