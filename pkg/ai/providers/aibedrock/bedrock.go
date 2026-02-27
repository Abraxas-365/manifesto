package aibedrock

import (
	"context"
	"encoding/json"
	"io"

	"github.com/Abraxas-365/manifesto/pkg/ai/llm"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
)

// ProviderOption configures the Bedrock provider
type ProviderOption func(*BedrockProvider)

// WithDefaultModel sets the default model ID
func WithDefaultModel(model string) ProviderOption {
	return func(p *BedrockProvider) {
		p.defaultModel = model
	}
}

// BedrockProvider implements the LLM interface for AWS Bedrock
type BedrockProvider struct {
	client       *bedrockruntime.Client
	defaultModel string
}

// NewBedrockProvider creates a new Bedrock provider
func NewBedrockProvider(cfg aws.Config, opts ...ProviderOption) *BedrockProvider {
	p := &BedrockProvider{
		client:       bedrockruntime.NewFromConfig(cfg),
		defaultModel: "anthropic.claude-sonnet-4-20250514-v1:0",
	}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

func defaultChatOptions(model string) *llm.ChatOptions {
	options := llm.DefaultOptions()
	options.Model = model
	return options
}

// ============================================================================
// Chat Implementation
// ============================================================================

// Chat implements the LLM interface
func (p *BedrockProvider) Chat(ctx context.Context, messages []llm.Message, opts ...llm.Option) (llm.Response, error) {
	if len(messages) == 0 {
		return llm.Response{}, errorRegistry.New(ErrEmptyMessages)
	}

	options := defaultChatOptions(p.defaultModel)
	for _, opt := range opts {
		opt(options)
	}

	// Extract system prompt
	systemBlocks, nonSystemMsgs := extractSystemPrompt(messages)

	// Convert messages
	bedrockMsgs, err := convertMessages(nonSystemMsgs)
	if err != nil {
		return llm.Response{}, err
	}

	input := &bedrockruntime.ConverseInput{
		ModelId:  aws.String(options.Model),
		Messages: bedrockMsgs,
	}

	if len(systemBlocks) > 0 {
		input.System = systemBlocks
	}

	// Set inference config
	inferenceConfig := buildInferenceConfig(options)
	if inferenceConfig != nil {
		input.InferenceConfig = inferenceConfig
	}

	// Convert tools
	if len(options.Tools) > 0 || len(options.Functions) > 0 {
		toolConfig := convertToBedrockToolConfig(options)
		if toolConfig != nil {
			input.ToolConfig = toolConfig
		}
	}

	// Make the API call
	output, err := p.client.Converse(ctx, input)
	if err != nil {
		return llm.Response{}, ParseBedrockError(err).
			WithDetail("model", options.Model).
			WithDetail("num_messages", len(messages))
	}

	return convertFromBedrockResponse(output)
}

// ============================================================================
// Chat Stream Implementation
// ============================================================================

// ChatStream implements streaming for Bedrock Converse API
func (p *BedrockProvider) ChatStream(ctx context.Context, messages []llm.Message, opts ...llm.Option) (llm.Stream, error) {
	if len(messages) == 0 {
		return nil, errorRegistry.New(ErrEmptyMessages)
	}

	options := defaultChatOptions(p.defaultModel)
	for _, opt := range opts {
		opt(options)
	}

	systemBlocks, nonSystemMsgs := extractSystemPrompt(messages)

	bedrockMsgs, err := convertMessages(nonSystemMsgs)
	if err != nil {
		return nil, err
	}

	input := &bedrockruntime.ConverseStreamInput{
		ModelId:  aws.String(options.Model),
		Messages: bedrockMsgs,
	}

	if len(systemBlocks) > 0 {
		input.System = systemBlocks
	}

	inferenceConfig := buildInferenceConfig(options)
	if inferenceConfig != nil {
		input.InferenceConfig = inferenceConfig
	}

	if len(options.Tools) > 0 || len(options.Functions) > 0 {
		toolConfig := convertToBedrockToolConfig(options)
		if toolConfig != nil {
			input.ToolConfig = toolConfig
		}
	}

	output, err := p.client.ConverseStream(ctx, input)
	if err != nil {
		return nil, ParseBedrockError(err).
			WithDetail("model", options.Model)
	}

	eventStream := output.GetStream()

	return &bedrockStream{
		events: eventStream.Events(),
		stream: eventStream,
	}, nil
}

// ============================================================================
// Stream Implementation
// ============================================================================

type bedrockStream struct {
	events    <-chan types.ConverseStreamOutput
	stream    interface{ Err() error; Close() error }
	toolCalls []llm.ToolCall
	lastError error
}

func (s *bedrockStream) Next() (llm.Message, error) {
	if s.lastError != nil {
		return llm.Message{}, s.lastError
	}

	for {
		event, ok := <-s.events
		if !ok {
			// Channel closed
			if err := s.stream.Err(); err != nil {
				s.lastError = ParseBedrockError(err)
				return llm.Message{}, s.lastError
			}
			s.lastError = io.EOF
			return llm.Message{}, io.EOF
		}

		switch v := event.(type) {
		case *types.ConverseStreamOutputMemberContentBlockStart:
			start := v.Value
			if toolStart, ok := start.Start.(*types.ContentBlockStartMemberToolUse); ok {
				s.toolCalls = append(s.toolCalls, llm.ToolCall{
					ID:   aws.ToString(toolStart.Value.ToolUseId),
					Type: "function",
					Function: llm.FunctionCall{
						Name: aws.ToString(toolStart.Value.Name),
					},
				})
			}

		case *types.ConverseStreamOutputMemberContentBlockDelta:
			delta := v.Value

			switch d := delta.Delta.(type) {
			case *types.ContentBlockDeltaMemberText:
				return llm.Message{
					Role:      llm.RoleAssistant,
					Content:   d.Value,
					ToolCalls: s.toolCalls,
				}, nil

			case *types.ContentBlockDeltaMemberToolUse:
				if len(s.toolCalls) > 0 && d.Value.Input != nil {
					last := &s.toolCalls[len(s.toolCalls)-1]
					last.Function.Arguments += aws.ToString(d.Value.Input)
				}
			}

		case *types.ConverseStreamOutputMemberMessageStop:
			s.lastError = io.EOF
			return llm.Message{}, io.EOF
		}
	}
}

func (s *bedrockStream) Close() error {
	return s.stream.Close()
}

// ============================================================================
// Helper Functions
// ============================================================================

func extractSystemPrompt(messages []llm.Message) ([]types.SystemContentBlock, []llm.Message) {
	var system []types.SystemContentBlock
	var rest []llm.Message

	for _, msg := range messages {
		if msg.Role == llm.RoleSystem {
			system = append(system, &types.SystemContentBlockMemberText{
				Value: msg.TextContent(),
			})
		} else {
			rest = append(rest, msg)
		}
	}

	return system, rest
}

func convertMessages(messages []llm.Message) ([]types.Message, error) {
	var result []types.Message

	for i := 0; i < len(messages); i++ {
		msg := messages[i]

		switch msg.Role {
		case llm.RoleUser:
			content := convertUserContent(msg)
			result = append(result, types.Message{
				Role:    types.ConversationRoleUser,
				Content: content,
			})

		case llm.RoleAssistant:
			content := convertAssistantContent(msg)
			result = append(result, types.Message{
				Role:    types.ConversationRoleAssistant,
				Content: content,
			})

		case llm.RoleTool:
			// Collect consecutive tool messages into a single user message
			var content []types.ContentBlock
			content = append(content, convertToolResult(msg))

			for i+1 < len(messages) && messages[i+1].Role == llm.RoleTool {
				i++
				content = append(content, convertToolResult(messages[i]))
			}

			result = append(result, types.Message{
				Role:    types.ConversationRoleUser,
				Content: content,
			})

		case llm.RoleFunction:
			result = append(result, types.Message{
				Role: types.ConversationRoleUser,
				Content: []types.ContentBlock{
					&types.ContentBlockMemberToolResult{
						Value: types.ToolResultBlock{
							ToolUseId: aws.String(msg.Name),
							Content: []types.ToolResultContentBlock{
								&types.ToolResultContentBlockMemberText{Value: msg.Content},
							},
						},
					},
				},
			})

		default:
			return nil, errorRegistry.New(ErrUnsupportedRole).
				WithDetail("role", msg.Role)
		}
	}

	return result, nil
}

func convertUserContent(msg llm.Message) []types.ContentBlock {
	if msg.IsMultimodal() {
		var content []types.ContentBlock
		for _, part := range msg.MultiContent {
			switch part.Type {
			case llm.ContentPartTypeText:
				content = append(content, &types.ContentBlockMemberText{Value: part.Text})
			}
		}
		return content
	}

	return []types.ContentBlock{
		&types.ContentBlockMemberText{Value: msg.Content},
	}
}

func convertAssistantContent(msg llm.Message) []types.ContentBlock {
	var content []types.ContentBlock

	if msg.Content != "" {
		content = append(content, &types.ContentBlockMemberText{Value: msg.Content})
	}

	for _, tc := range msg.ToolCalls {
		var input any
		if tc.Function.Arguments != "" {
			_ = json.Unmarshal([]byte(tc.Function.Arguments), &input)
		}
		if input == nil {
			input = map[string]any{}
		}

		content = append(content, &types.ContentBlockMemberToolUse{
			Value: types.ToolUseBlock{
				ToolUseId: aws.String(tc.ID),
				Name:      aws.String(tc.Function.Name),
				Input:     document.NewLazyDocument(input),
			},
		})
	}

	return content
}

func convertToolResult(msg llm.Message) types.ContentBlock {
	return &types.ContentBlockMemberToolResult{
		Value: types.ToolResultBlock{
			ToolUseId: aws.String(msg.ToolCallID),
			Content: []types.ToolResultContentBlock{
				&types.ToolResultContentBlockMemberText{Value: msg.Content},
			},
		},
	}
}

func buildInferenceConfig(options *llm.ChatOptions) *types.InferenceConfiguration {
	config := &types.InferenceConfiguration{}
	hasConfig := false

	if options.MaxCompletionTokens > 0 {
		v := int32(options.MaxCompletionTokens)
		config.MaxTokens = &v
		hasConfig = true
	} else if options.MaxTokens > 0 {
		v := int32(options.MaxTokens)
		config.MaxTokens = &v
		hasConfig = true
	}

	if options.Temperature != 0 {
		v := float32(options.Temperature)
		config.Temperature = &v
		hasConfig = true
	}

	if options.TopP != 0 {
		v := float32(options.TopP)
		config.TopP = &v
		hasConfig = true
	}

	if len(options.Stop) > 0 {
		config.StopSequences = options.Stop
		hasConfig = true
	}

	if !hasConfig {
		return nil
	}

	return config
}

func convertToBedrockToolConfig(options *llm.ChatOptions) *types.ToolConfiguration {
	var tools []types.Tool

	for _, tool := range options.Tools {
		if tool.Type != "function" {
			continue
		}
		tools = append(tools, convertToolSpec(tool.Function))
	}

	for _, fn := range options.Functions {
		tools = append(tools, convertToolSpec(fn))
	}

	if len(tools) == 0 {
		return nil
	}

	config := &types.ToolConfiguration{
		Tools: tools,
	}

	if options.ToolChoice != nil {
		config.ToolChoice = convertBedrockToolChoice(options.ToolChoice)
	}

	return config
}

func convertToolSpec(fn llm.Function) types.Tool {
	var inputSchema types.ToolInputSchema

	if fn.Parameters != nil {
		var m map[string]any
		switch v := fn.Parameters.(type) {
		case map[string]any:
			m = v
		default:
			data, err := json.Marshal(fn.Parameters)
			if err == nil {
				_ = json.Unmarshal(data, &m)
			}
		}

		if m != nil {
			inputSchema = &types.ToolInputSchemaMemberJson{
				Value: document.NewLazyDocument(m),
			}
		}
	}

	if inputSchema == nil {
		inputSchema = &types.ToolInputSchemaMemberJson{
			Value: document.NewLazyDocument(map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			}),
		}
	}

	spec := types.ToolSpecification{
		Name:        aws.String(fn.Name),
		InputSchema: inputSchema,
	}

	if fn.Description != "" {
		spec.Description = aws.String(fn.Description)
	}

	return &types.ToolMemberToolSpec{Value: spec}
}

func convertBedrockToolChoice(toolChoice any) types.ToolChoice {
	if strChoice, ok := toolChoice.(string); ok {
		switch strChoice {
		case "auto":
			return &types.ToolChoiceMemberAuto{Value: types.AutoToolChoice{}}
		case "required":
			return &types.ToolChoiceMemberAny{Value: types.AnyToolChoice{}}
		case "none":
			// Bedrock doesn't have "none" — return auto and let the caller omit tools
			return &types.ToolChoiceMemberAuto{Value: types.AutoToolChoice{}}
		}
	}

	return &types.ToolChoiceMemberAuto{Value: types.AutoToolChoice{}}
}

func convertFromBedrockResponse(output *bedrockruntime.ConverseOutput) (llm.Response, error) {
	msgOutput, ok := output.Output.(*types.ConverseOutputMemberMessage)
	if !ok {
		return llm.Response{}, errorRegistry.New(ErrAPIResponse).
			WithDetail("error", "unexpected output type")
	}

	var content string
	var toolCalls []llm.ToolCall

	for _, block := range msgOutput.Value.Content {
		switch v := block.(type) {
		case *types.ContentBlockMemberText:
			content += v.Value

		case *types.ContentBlockMemberToolUse:
			args := ""
			if v.Value.Input != nil {
				data, _ := json.Marshal(v.Value.Input)
				args = string(data)
			}
			toolCalls = append(toolCalls, llm.ToolCall{
				ID:   aws.ToString(v.Value.ToolUseId),
				Type: "function",
				Function: llm.FunctionCall{
					Name:      aws.ToString(v.Value.Name),
					Arguments: args,
				},
			})
		}
	}

	usage := llm.Usage{}
	if output.Usage != nil {
		if output.Usage.InputTokens != nil {
			usage.PromptTokens = int(*output.Usage.InputTokens)
		}
		if output.Usage.OutputTokens != nil {
			usage.CompletionTokens = int(*output.Usage.OutputTokens)
		}
		if output.Usage.TotalTokens != nil {
			usage.TotalTokens = int(*output.Usage.TotalTokens)
		}
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
