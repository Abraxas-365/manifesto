package anthropic

import (
	"encoding/json"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm"
	"github.com/anthropics/anthropic-sdk-go"
)

// toAnthropicMessages converts harness messages (excluding system) into SDK
// message params.
func toAnthropicMessages(messages []llm.Message) ([]anthropic.MessageParam, error) {
	var result []anthropic.MessageParam

	for _, msg := range messages {
		switch msg.Role {
		case llm.RoleUser:
			blocks := toContentBlocks(msg)
			result = append(result, anthropic.NewUserMessage(blocks...))
		case llm.RoleAssistant:
			blocks := toContentBlocks(msg)
			result = append(result, anthropic.NewAssistantMessage(blocks...))
		default:
			return nil, llm.Registry.New(llm.ErrUnsupportedRole).
				WithDetail("role", string(msg.Role))
		}
	}

	return result, nil
}

// toContentBlocks converts a message's content blocks into SDK block params.
func toContentBlocks(msg llm.Message) []anthropic.ContentBlockParamUnion {
	var blocks []anthropic.ContentBlockParamUnion

	for _, b := range msg.Content {
		switch b.Type {
		case llm.BlockText:
			blocks = append(blocks, anthropic.NewTextBlock(b.Text))

		case llm.BlockImage:
			blocks = append(blocks, anthropic.NewImageBlockBase64(b.MediaType, b.Data))

		case llm.BlockToolUse:
			var input any
			if len(b.Input) > 0 {
				_ = json.Unmarshal(b.Input, &input)
			}
			if input == nil {
				input = map[string]any{}
			}
			blocks = append(blocks, anthropic.NewToolUseBlock(b.ID, input, b.Name))

		case llm.BlockToolResult:
			blocks = append(blocks, toolResultBlock(b))
		}
	}

	return blocks
}

// toolResultBlock builds a tool_result block, attaching any images.
func toolResultBlock(b llm.ContentBlock) anthropic.ContentBlockParamUnion {
	if len(b.Images) == 0 {
		return anthropic.NewToolResultBlock(b.ToolUseID, b.Content, b.IsError)
	}

	var parts []anthropic.ToolResultBlockParamContentUnion
	if b.Content != "" {
		parts = append(parts, anthropic.ToolResultBlockParamContentUnion{
			OfText: &anthropic.TextBlockParam{Text: b.Content},
		})
	}
	for _, img := range b.Images {
		imgBlock := anthropic.NewImageBlockBase64(img.MediaType, img.Data)
		parts = append(parts, anthropic.ToolResultBlockParamContentUnion{
			OfImage: imgBlock.OfImage,
		})
	}

	return anthropic.ContentBlockParamUnion{
		OfToolResult: &anthropic.ToolResultBlockParam{
			ToolUseID: b.ToolUseID,
			Content:   parts,
			IsError:   anthropic.Bool(b.IsError),
		},
	}
}

// extractSystem returns the system prompt as SDK text block params. When
// caching is enabled a cache_control breakpoint is placed on the system block.
func extractSystem(system string, caching bool) []anthropic.TextBlockParam {
	if system == "" {
		return nil
	}
	block := anthropic.TextBlockParam{Text: system}
	if caching {
		block.CacheControl = anthropic.NewCacheControlEphemeralParam()
	}
	return []anthropic.TextBlockParam{block}
}

// toAnthropicTools converts harness tool defs into SDK tool params. When caching
// is enabled a cache_control breakpoint is placed on the last tool so that the
// entire tool definition prefix is cached.
func toAnthropicTools(tools []llm.ToolDef, caching bool) []anthropic.ToolUnionParam {
	var result []anthropic.ToolUnionParam

	for i, tool := range tools {
		schema := toToolSchema(tool.InputSchema)
		t := anthropic.ToolUnionParamOfTool(schema, tool.Name)
		if tool.Description != "" {
			t.OfTool.Description = anthropic.String(tool.Description)
		}
		if caching && i == len(tools)-1 {
			t.OfTool.CacheControl = anthropic.NewCacheControlEphemeralParam()
		}
		result = append(result, t)
	}

	return result
}

func toToolSchema(raw json.RawMessage) anthropic.ToolInputSchemaParam {
	schema := anthropic.ToolInputSchemaParam{}
	if len(raw) == 0 {
		return schema
	}

	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return schema
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

// fromAnthropicResponse converts an SDK message into a harness message.
func fromAnthropicResponse(msg *anthropic.Message) (llm.Message, llm.StopReason, llm.Usage) {
	var blocks []llm.ContentBlock

	for _, block := range msg.Content {
		switch block.Type {
		case "text":
			blocks = append(blocks, llm.Text(block.Text))
		case "tool_use":
			var input json.RawMessage
			if block.Input != nil {
				if data, err := json.Marshal(block.Input); err == nil {
					input = data
				}
			}
			blocks = append(blocks, llm.ToolUseBlock(block.ID, block.Name, input))
		}
	}

	out := llm.Message{Role: llm.RoleAssistant, Content: blocks}
	usage := llm.Usage{
		InputTokens:      int(msg.Usage.InputTokens),
		OutputTokens:     int(msg.Usage.OutputTokens),
		CacheReadTokens:  int(msg.Usage.CacheReadInputTokens),
		CacheWriteTokens: int(msg.Usage.CacheCreationInputTokens),
	}
	reason := llm.NormalizeStopReason(mapStopReason(string(msg.StopReason)), out)
	return out, reason, usage
}

// mapStopReason maps an Anthropic stop_reason onto the harness enum.
func mapStopReason(reason string) llm.StopReason {
	switch reason {
	case "end_turn", "stop_sequence", "pause_turn":
		return llm.StopEndTurn
	case "tool_use":
		return llm.StopToolUse
	case "max_tokens":
		return llm.StopMaxTokens
	case "refusal":
		return llm.StopError
	case "":
		return llm.StopEndTurn
	default:
		return llm.StopUnknown
	}
}
