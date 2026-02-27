package aianthropic

import (
	"context"
	"encoding/json"
	"io"
	"os"

	"github.com/Abraxas-365/manifesto/pkg/ai/llm"
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// AnthropicProvider implements the LLM interface for Anthropic Claude
type AnthropicProvider struct {
	client anthropic.Client
	apiKey string
}

// NewAnthropicProvider creates a new Anthropic provider
func NewAnthropicProvider(apiKey string, opts ...option.RequestOption) *AnthropicProvider {
	if apiKey == "" {
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
	}

	options := append([]option.RequestOption{option.WithAPIKey(apiKey)}, opts...)
	client := anthropic.NewClient(options...)

	return &AnthropicProvider{
		client: client,
		apiKey: apiKey,
	}
}

func defaultChatOptions() *llm.ChatOptions {
	options := llm.DefaultOptions()
	options.Model = "claude-sonnet-4-20250514"
	return options
}

// ============================================================================
// Chat Implementation
// ============================================================================

// Chat implements the LLM interface
func (p *AnthropicProvider) Chat(ctx context.Context, messages []llm.Message, opts ...llm.Option) (llm.Response, error) {
	if p.apiKey == "" {
		return llm.Response{}, errorRegistry.New(ErrMissingAPIKey)
	}

	if len(messages) == 0 {
		return llm.Response{}, errorRegistry.New(ErrEmptyMessages)
	}

	options := defaultChatOptions()
	for _, opt := range opts {
		opt(options)
	}

	// Extract system prompt from messages
	systemBlocks, nonSystemMsgs := extractSystemPrompt(messages)

	// Convert messages
	anthropicMsgs, err := convertMessages(nonSystemMsgs)
	if err != nil {
		return llm.Response{}, err
	}

	// Build params
	maxTokens := int64(4096)
	if options.MaxCompletionTokens > 0 {
		maxTokens = int64(options.MaxCompletionTokens)
	} else if options.MaxTokens > 0 {
		maxTokens = int64(options.MaxTokens)
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(options.Model),
		MaxTokens: maxTokens,
		Messages:  anthropicMsgs,
	}

	if len(systemBlocks) > 0 {
		params.System = systemBlocks
	}

	if options.Temperature != 0 {
		params.Temperature = anthropic.Float(float64(options.Temperature))
	}
	if options.TopP != 0 {
		params.TopP = anthropic.Float(float64(options.TopP))
	}
	if len(options.Stop) > 0 {
		params.StopSequences = options.Stop
	}

	// Convert tools
	if len(options.Tools) > 0 || len(options.Functions) > 0 {
		tools := convertToAnthropicTools(options.Tools, options.Functions)
		if len(tools) > 0 {
			params.Tools = tools
		}
	}

	// Set tool choice
	if options.ToolChoice != nil {
		params.ToolChoice = convertToolChoice(options.ToolChoice)
	}

	// Make the API call
	message, err := p.client.Messages.New(ctx, params)
	if err != nil {
		return llm.Response{}, ParseAnthropicError(err).
			WithDetail("model", options.Model).
			WithDetail("num_messages", len(messages))
	}

	// Convert response
	response := convertFromAnthropicResponse(message)
	return response, nil
}

// ============================================================================
// Chat Stream Implementation
// ============================================================================

// ChatStream implements streaming for Anthropic Messages API
func (p *AnthropicProvider) ChatStream(ctx context.Context, messages []llm.Message, opts ...llm.Option) (llm.Stream, error) {
	if p.apiKey == "" {
		return nil, errorRegistry.New(ErrMissingAPIKey)
	}

	if len(messages) == 0 {
		return nil, errorRegistry.New(ErrEmptyMessages)
	}

	options := defaultChatOptions()
	for _, opt := range opts {
		opt(options)
	}

	systemBlocks, nonSystemMsgs := extractSystemPrompt(messages)

	anthropicMsgs, err := convertMessages(nonSystemMsgs)
	if err != nil {
		return nil, err
	}

	maxTokens := int64(4096)
	if options.MaxCompletionTokens > 0 {
		maxTokens = int64(options.MaxCompletionTokens)
	} else if options.MaxTokens > 0 {
		maxTokens = int64(options.MaxTokens)
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(options.Model),
		MaxTokens: maxTokens,
		Messages:  anthropicMsgs,
	}

	if len(systemBlocks) > 0 {
		params.System = systemBlocks
	}

	if options.Temperature != 0 {
		params.Temperature = anthropic.Float(float64(options.Temperature))
	}
	if options.TopP != 0 {
		params.TopP = anthropic.Float(float64(options.TopP))
	}
	if len(options.Stop) > 0 {
		params.StopSequences = options.Stop
	}

	if len(options.Tools) > 0 || len(options.Functions) > 0 {
		tools := convertToAnthropicTools(options.Tools, options.Functions)
		if len(tools) > 0 {
			params.Tools = tools
		}
	}

	if options.ToolChoice != nil {
		params.ToolChoice = convertToolChoice(options.ToolChoice)
	}

	stream := p.client.Messages.NewStreaming(ctx, params)

	return &anthropicStream{
		stream: stream,
	}, nil
}

// ============================================================================
// Stream Implementation
// ============================================================================

type anthropicStream struct {
	stream interface {
		Next() bool
		Current() anthropic.MessageStreamEventUnion
		Err() error
		Close() error
	}
	toolCalls []llm.ToolCall
	lastError error
}

func (s *anthropicStream) Next() (llm.Message, error) {
	if s.lastError != nil {
		return llm.Message{}, s.lastError
	}

	for s.stream.Next() {
		event := s.stream.Current()

		switch event.Type {
		case "content_block_start":
			cb := event.ContentBlock
			if cb.Type == "tool_use" {
				s.toolCalls = append(s.toolCalls, llm.ToolCall{
					ID:   cb.ID,
					Type: "function",
					Function: llm.FunctionCall{
						Name: cb.Name,
					},
				})
			}

		case "content_block_delta":
			delta := event.Delta

			switch delta.Type {
			case "text_delta":
				return llm.Message{
					Role:      llm.RoleAssistant,
					Content:   delta.Text,
					ToolCalls: s.toolCalls,
				}, nil

			case "input_json_delta":
				if len(s.toolCalls) > 0 {
					last := &s.toolCalls[len(s.toolCalls)-1]
					last.Function.Arguments += delta.PartialJSON
				}
			}

		case "message_stop":
			s.lastError = io.EOF
			return llm.Message{}, io.EOF
		}
	}

	if err := s.stream.Err(); err != nil {
		s.lastError = ParseAnthropicError(err)
		return llm.Message{}, s.lastError
	}

	s.lastError = io.EOF
	return llm.Message{}, io.EOF
}

func (s *anthropicStream) Close() error {
	if closer, ok := s.stream.(interface{ Close() error }); ok {
		return closer.Close()
	}
	return nil
}

// ============================================================================
// Helper Functions
// ============================================================================

// extractSystemPrompt separates system messages into TextBlockParams
func extractSystemPrompt(messages []llm.Message) ([]anthropic.TextBlockParam, []llm.Message) {
	var system []anthropic.TextBlockParam
	var rest []llm.Message

	for _, msg := range messages {
		if msg.Role == llm.RoleSystem {
			system = append(system, anthropic.TextBlockParam{
				Text: msg.TextContent(),
			})
		} else {
			rest = append(rest, msg)
		}
	}

	return system, rest
}

// convertMessages converts llm.Message slice to Anthropic MessageParams
func convertMessages(messages []llm.Message) ([]anthropic.MessageParam, error) {
	var result []anthropic.MessageParam

	for i := 0; i < len(messages); i++ {
		msg := messages[i]

		switch msg.Role {
		case llm.RoleUser:
			blocks := convertUserContentBlocks(msg)
			result = append(result, anthropic.NewUserMessage(blocks...))

		case llm.RoleAssistant:
			blocks := convertAssistantContentBlocks(msg)
			result = append(result, anthropic.NewAssistantMessage(blocks...))

		case llm.RoleTool:
			// Collect consecutive tool messages into a single user message
			var toolBlocks []anthropic.ContentBlockParamUnion
			toolBlocks = append(toolBlocks, anthropic.NewToolResultBlock(
				msg.ToolCallID, msg.Content, false,
			))

			for i+1 < len(messages) && messages[i+1].Role == llm.RoleTool {
				i++
				toolBlocks = append(toolBlocks, anthropic.NewToolResultBlock(
					messages[i].ToolCallID, messages[i].Content, false,
				))
			}
			result = append(result, anthropic.NewUserMessage(toolBlocks...))

		case llm.RoleFunction:
			// Treat function messages as tool results
			result = append(result, anthropic.NewUserMessage(
				anthropic.NewToolResultBlock(msg.Name, msg.Content, false),
			))

		default:
			return nil, errorRegistry.New(ErrUnsupportedRole).
				WithDetail("role", msg.Role)
		}
	}

	return result, nil
}

func convertUserContentBlocks(msg llm.Message) []anthropic.ContentBlockParamUnion {
	if msg.IsMultimodal() {
		var blocks []anthropic.ContentBlockParamUnion
		for _, part := range msg.MultiContent {
			switch part.Type {
			case llm.ContentPartTypeText:
				blocks = append(blocks, anthropic.NewTextBlock(part.Text))
			case llm.ContentPartTypeImageURL:
				if part.ImageURL != nil {
					blocks = append(blocks, anthropic.NewImageBlockBase64(
						"image/jpeg", // Default; URL-based images handled differently
						part.ImageURL.URL,
					))
				}
			}
		}
		return blocks
	}

	return []anthropic.ContentBlockParamUnion{
		anthropic.NewTextBlock(msg.Content),
	}
}

func convertAssistantContentBlocks(msg llm.Message) []anthropic.ContentBlockParamUnion {
	var blocks []anthropic.ContentBlockParamUnion

	if msg.Content != "" {
		blocks = append(blocks, anthropic.NewTextBlock(msg.Content))
	}

	for _, tc := range msg.ToolCalls {
		var input any
		if tc.Function.Arguments != "" {
			_ = json.Unmarshal([]byte(tc.Function.Arguments), &input)
		}
		if input == nil {
			input = map[string]any{}
		}
		blocks = append(blocks, anthropic.NewToolUseBlock(
			tc.ID, input, tc.Function.Name,
		))
	}

	return blocks
}

// convertToAnthropicTools converts tools and functions to Anthropic tool params
func convertToAnthropicTools(tools []llm.Tool, functions []llm.Function) []anthropic.ToolUnionParam {
	var result []anthropic.ToolUnionParam

	for _, tool := range tools {
		if tool.Type != "function" {
			continue
		}
		schema := convertToolSchema(tool.Function.Parameters)
		t := anthropic.ToolUnionParamOfTool(schema, tool.Function.Name)
		if tool.Function.Description != "" {
			t.OfTool.Description = anthropic.String(tool.Function.Description)
		}
		result = append(result, t)
	}

	for _, fn := range functions {
		schema := convertToolSchema(fn.Parameters)
		t := anthropic.ToolUnionParamOfTool(schema, fn.Name)
		if fn.Description != "" {
			t.OfTool.Description = anthropic.String(fn.Description)
		}
		result = append(result, t)
	}

	return result
}

func convertToolSchema(params any) anthropic.ToolInputSchemaParam {
	schema := anthropic.ToolInputSchemaParam{}

	if params == nil {
		return schema
	}

	// Convert params to map
	var m map[string]any
	switch v := params.(type) {
	case map[string]any:
		m = v
	default:
		data, err := json.Marshal(params)
		if err != nil {
			return schema
		}
		_ = json.Unmarshal(data, &m)
	}

	if props, ok := m["properties"]; ok {
		schema.Properties = props
	}
	if req, ok := m["required"].([]any); ok {
		for _, r := range req {
			if s, ok := r.(string); ok {
				schema.Required = append(schema.Required, s)
			}
		}
	}

	return schema
}

func convertToolChoice(toolChoice any) anthropic.ToolChoiceUnionParam {
	if strChoice, ok := toolChoice.(string); ok {
		switch strChoice {
		case "auto":
			return anthropic.ToolChoiceUnionParam{
				OfAuto: &anthropic.ToolChoiceAutoParam{},
			}
		case "required":
			return anthropic.ToolChoiceUnionParam{
				OfAny: &anthropic.ToolChoiceAnyParam{},
			}
		case "none":
			return anthropic.ToolChoiceUnionParam{
				OfNone: &anthropic.ToolChoiceNoneParam{},
			}
		}
	}

	return anthropic.ToolChoiceUnionParam{
		OfAuto: &anthropic.ToolChoiceAutoParam{},
	}
}

func convertFromAnthropicResponse(msg *anthropic.Message) llm.Response {
	var content string
	var toolCalls []llm.ToolCall

	for _, block := range msg.Content {
		switch block.Type {
		case "text":
			content += block.Text
		case "tool_use":
			args := ""
			if block.Input != nil {
				data, _ := json.Marshal(block.Input)
				args = string(data)
			}
			toolCalls = append(toolCalls, llm.ToolCall{
				ID:   block.ID,
				Type: "function",
				Function: llm.FunctionCall{
					Name:      block.Name,
					Arguments: args,
				},
			})
		}
	}

	return llm.Response{
		Message: llm.Message{
			Role:      llm.RoleAssistant,
			Content:   content,
			ToolCalls: toolCalls,
		},
		Usage: llm.Usage{
			PromptTokens:     int(msg.Usage.InputTokens),
			CompletionTokens: int(msg.Usage.OutputTokens),
			TotalTokens:      int(msg.Usage.InputTokens) + int(msg.Usage.OutputTokens),
		},
	}
}
