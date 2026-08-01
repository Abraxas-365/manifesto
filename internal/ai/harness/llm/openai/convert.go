package openai

import (
	"encoding/json"
	"fmt"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

// toOpenAIMessages converts harness messages into OpenAI chat messages. The
// system prompt is prepended separately by the caller.
func toOpenAIMessages(system string, messages []llm.Message) ([]openai.ChatCompletionMessageParamUnion, error) {
	var out []openai.ChatCompletionMessageParamUnion

	if system != "" {
		out = append(out, openai.SystemMessage(system))
	}

	for _, msg := range messages {
		switch msg.Role {
		case llm.RoleUser:
			out = append(out, userMessages(msg)...)
		case llm.RoleAssistant:
			out = append(out, assistantMessage(msg))
		default:
			return nil, llm.Registry.New(llm.ErrUnsupportedRole).
				WithDetail("role", string(msg.Role))
		}
	}

	return out, nil
}

// userMessages converts a harness user message. Tool-result blocks become
// separate OpenAI tool messages; text/image blocks become one user message.
func userMessages(msg llm.Message) []openai.ChatCompletionMessageParamUnion {
	var out []openai.ChatCompletionMessageParamUnion
	var parts []openai.ChatCompletionContentPartUnionParam

	for _, b := range msg.Content {
		switch b.Type {
		case llm.BlockText:
			if b.Text == "" {
				continue
			}
			parts = append(parts, openai.TextContentPart(b.Text))
		case llm.BlockImage:
			parts = append(parts, openai.ImageContentPart(
				openai.ChatCompletionContentPartImageImageURLParam{
					URL: fmt.Sprintf("data:%s;base64,%s", b.MediaType, b.Data),
				},
			))
		case llm.BlockToolResult:
			content := b.Content
			if b.IsError && content == "" {
				content = "(tool reported an error)"
			}
			out = append(out, openai.ToolMessage(content, b.ToolUseID))
		}
	}

	if len(parts) > 0 {
		out = append(out, openai.UserMessage(parts))
	}
	return out
}

// assistantMessage converts a harness assistant message, including tool calls.
func assistantMessage(msg llm.Message) openai.ChatCompletionMessageParamUnion {
	var am openai.ChatCompletionAssistantMessageParam

	if text := msg.TextContent(); text != "" {
		am.Content.OfString = openai.String(text)
	}

	for _, b := range msg.Content {
		if b.Type != llm.BlockToolUse {
			continue
		}
		args := string(b.Input)
		if args == "" {
			args = "{}"
		}
		am.ToolCalls = append(am.ToolCalls, openai.ChatCompletionMessageToolCallUnionParam{
			OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
				ID: b.ID,
				Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
					Name:      b.Name,
					Arguments: args,
				},
			},
		})
	}

	return openai.ChatCompletionMessageParamUnion{OfAssistant: &am}
}

// toOpenAITools converts harness tool defs into OpenAI tool params.
func toOpenAITools(tools []llm.ToolDef) []openai.ChatCompletionToolUnionParam {
	var out []openai.ChatCompletionToolUnionParam

	for _, t := range tools {
		def := shared.FunctionDefinitionParam{Name: t.Name}
		if t.Description != "" {
			def.Description = openai.String(t.Description)
		}
		if len(t.InputSchema) > 0 {
			var params map[string]any
			if err := json.Unmarshal(t.InputSchema, &params); err == nil {
				def.Parameters = shared.FunctionParameters(params)
			}
		}
		out = append(out, openai.ChatCompletionFunctionTool(def))
	}

	return out
}

// fromOpenAIResponse converts an OpenAI completion into a harness message.
func fromOpenAIResponse(resp *openai.ChatCompletion) (llm.Message, llm.StopReason, llm.Usage) {
	usage := llm.Usage{
		InputTokens:     int(resp.Usage.PromptTokens),
		OutputTokens:    int(resp.Usage.CompletionTokens),
		CacheReadTokens: int(resp.Usage.PromptTokensDetails.CachedTokens),
	}

	if len(resp.Choices) == 0 {
		return llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.Text("")}}, llm.StopEndTurn, usage
	}

	choice := resp.Choices[0]
	var blocks []llm.ContentBlock

	if choice.Message.Content != "" {
		blocks = append(blocks, llm.Text(choice.Message.Content))
	}
	for _, tc := range choice.Message.ToolCalls {
		blocks = append(blocks, llm.ToolUseBlock(
			tc.ID, tc.Function.Name, []byte(tc.Function.Arguments),
		))
	}

	msg := llm.Message{Role: llm.RoleAssistant, Content: blocks}
	// NormalizeStopReason coerces to StopToolUse when tool calls are present,
	// which fixes OpenAI reporting finish_reason "stop" alongside tool_calls.
	reason := llm.NormalizeStopReason(mapFinishReason(choice.FinishReason), msg)
	return msg, reason, usage
}

// mapFinishReason maps an OpenAI finish_reason onto the harness enum.
func mapFinishReason(reason string) llm.StopReason {
	switch reason {
	case "stop":
		return llm.StopEndTurn
	case "tool_calls", "function_call":
		return llm.StopToolUse
	case "length":
		return llm.StopMaxTokens
	case "content_filter":
		return llm.StopError
	case "":
		return llm.StopEndTurn
	default:
		return llm.StopUnknown
	}
}
