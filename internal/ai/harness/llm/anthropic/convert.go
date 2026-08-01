package anthropic

import (
	"encoding/json"
	"strings"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm"
	"github.com/anthropics/anthropic-sdk-go"
)

// toAnthropicMessages converts harness messages (excluding system) into SDK
// message params. Unsigned thinking blocks are stripped before conversion (the
// API rejects them). When cc is non-nil, cache_control breakpoints are placed
// on the last content block of strategic messages to enable multi-turn caching:
//   - messages[len-2] (second-to-last): most recent context, highest value
//   - messages[len/3] (midpoint): for long histories (>= 10 messages)
func toAnthropicMessages(messages []llm.Message, cc *anthropic.CacheControlEphemeralParam) ([]anthropic.MessageParam, error) {
	messages = stripUnsignedThinkingBlocks(messages)
	var result []anthropic.MessageParam

	for _, msg := range messages {
		switch msg.Role {
		case llm.RoleUser:
			blocks := toContentBlocks(msg)
			if len(blocks) == 0 {
				blocks = []anthropic.ContentBlockParamUnion{anthropic.NewTextBlock("[empty]")}
			}
			result = append(result, anthropic.NewUserMessage(blocks...))
		case llm.RoleAssistant:
			blocks := toContentBlocks(msg)
			if len(blocks) == 0 {
				// Guard against empty assistant messages (e.g. cross-provider
				// model switch where the original response had no convertible
				// blocks). Anthropic rejects messages without content.
				blocks = []anthropic.ContentBlockParamUnion{anthropic.NewTextBlock("[no content]")}
			}
			result = append(result, anthropic.NewAssistantMessage(blocks...))
		default:
			return nil, llm.Registry.New(llm.ErrUnsupportedRole).
				WithDetail("role", string(msg.Role))
		}
	}

	if cc != nil && len(result) >= 2 {
		// Priority 1: second-to-last message (most recent context, highest value).
		markMessageCacheControl(&result[len(result)-2], *cc)

		// Priority 2: mid-conversation breakpoint for long histories.
		if len(result) >= 10 {
			if midIdx := len(result) / 3; midIdx > 0 {
				markMessageCacheControl(&result[midIdx], *cc)
			}
		}
	}

	return result, nil
}

// markMessageCacheControl sets cache_control on the last cacheable content
// block of a message. Skips thinking blocks (API rejects cache_control on them
// — GetCacheControl returns nil for them) and empty text blocks.
func markMessageCacheControl(msg *anthropic.MessageParam, cc anthropic.CacheControlEphemeralParam) {
	blocks := msg.Content
	for i := len(blocks) - 1; i >= 0; i-- {
		ptr := blocks[i].GetCacheControl()
		if ptr == nil {
			continue
		}
		*ptr = cc
		return
	}
}

// toContentBlocks converts a message's content blocks into SDK block params.
func toContentBlocks(msg llm.Message) []anthropic.ContentBlockParamUnion {
	var blocks []anthropic.ContentBlockParamUnion

	for _, b := range msg.Content {
		switch b.Type {
		case llm.BlockText:
			if b.Text == "" {
				continue // Anthropic rejects empty text content blocks.
			}
			blocks = append(blocks, anthropic.NewTextBlock(b.Text))

		case llm.BlockImage:
			blocks = append(blocks, anthropic.NewImageBlockBase64(b.MediaType, b.Data))

		case llm.BlockToolUse:
			var input any
			if len(b.Input) > 0 {
				_ = json.Unmarshal(b.Input, &input)
			}
			// The API requires tool_use.input to be a JSON object. Wrap any
			// non-object value (string/array/number from truncation or bad
			// persistence) instead of sending it as-is and getting a 400.
			if _, ok := input.(map[string]any); !ok {
				if input == nil {
					input = map[string]any{}
				} else {
					input = map[string]any{"value": input}
				}
			}
			blocks = append(blocks, anthropic.NewToolUseBlock(b.ID, input, b.Name))

		case llm.BlockThinking:
			if b.Thinking != "" && b.Signature != "" {
				blocks = append(blocks, anthropic.NewThinkingBlock(b.Signature, b.Thinking))
			}

		case llm.BlockRedactedThinking:
			blocks = append(blocks, anthropic.NewRedactedThinkingBlock(b.Data))

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

// systemPromptDynamicBoundary separates static (cacheable) content from dynamic
// session-specific content. Must match prompts.SystemPromptDynamicBoundary.
const systemPromptDynamicBoundary = "__SYSTEM_PROMPT_DYNAMIC_BOUNDARY__"

// extractSystem returns the system prompt as SDK text block params. When cc is
// non-nil and the system prompt contains the dynamic boundary marker, the prompt
// is split into two blocks: a static block (with cache_control) and a dynamic
// block (no caching). This ensures the static prefix stays cached across turns
// even as the dynamic suffix changes (deferred tools, etc.).
func extractSystem(system string, cc *anthropic.CacheControlEphemeralParam) []anthropic.TextBlockParam {
	if system == "" {
		return nil
	}
	if cc == nil {
		return []anthropic.TextBlockParam{{Text: system}}
	}
	if idx := strings.Index(system, systemPromptDynamicBoundary); idx >= 0 {
		staticPart := strings.TrimRight(system[:idx], "\n")
		dynamicPart := strings.TrimLeft(system[idx+len(systemPromptDynamicBoundary):], "\n")
		blocks := []anthropic.TextBlockParam{{Text: staticPart, CacheControl: *cc}}
		if dynamicPart != "" {
			blocks = append(blocks, anthropic.TextBlockParam{Text: dynamicPart})
		}
		return blocks
	}
	return []anthropic.TextBlockParam{{Text: system, CacheControl: *cc}}
}

// toAnthropicTools converts harness tool defs into SDK tool params. When cc is
// non-nil, a cache_control breakpoint is placed on the last tool so that the
// entire tool definition prefix is cached.
func toAnthropicTools(tools []llm.ToolDef, cc *anthropic.CacheControlEphemeralParam) []anthropic.ToolUnionParam {
	var result []anthropic.ToolUnionParam

	for i, tool := range tools {
		schema := toToolSchema(tool.InputSchema)
		t := anthropic.ToolUnionParamOfTool(schema, tool.Name)
		if tool.Description != "" {
			t.OfTool.Description = anthropic.String(tool.Description)
		}
		if cc != nil && i == len(tools)-1 {
			t.OfTool.CacheControl = *cc
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
		case "thinking":
			blocks = append(blocks, llm.ThinkingBlock(block.Thinking, block.Signature))
		case "redacted_thinking":
			blocks = append(blocks, llm.RedactedThinkingBlock(block.Data))
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
		// Normalize InputTokens to include cached tokens so it always
		// represents total input context (matching OpenAI's PromptTokens
		// which already includes cached tokens). The individual cache
		// fields remain set for cost-calculation breakdown.
		InputTokens:      int(msg.Usage.InputTokens) + int(msg.Usage.CacheReadInputTokens) + int(msg.Usage.CacheCreationInputTokens),
		OutputTokens:     int(msg.Usage.OutputTokens),
		CacheReadTokens:  int(msg.Usage.CacheReadInputTokens),
		CacheWriteTokens: int(msg.Usage.CacheCreationInputTokens),
	}
	reason := llm.NormalizeStopReason(mapStopReason(string(msg.StopReason)), out)
	return out, reason, usage
}

// stripAllThinkingBlocks removes ALL thinking/redacted_thinking blocks from
// messages. Used when the current request has thinking disabled — the API
// rejects thinking blocks in history when thinking is not enabled.
func stripAllThinkingBlocks(msgs []llm.Message) []llm.Message {
	out := make([]llm.Message, 0, len(msgs))
	for _, m := range msgs {
		if m.Role != llm.RoleAssistant {
			out = append(out, m)
			continue
		}
		var filtered []llm.ContentBlock
		for _, b := range m.Content {
			if b.Type == llm.BlockThinking || b.Type == llm.BlockRedactedThinking {
				continue
			}
			filtered = append(filtered, b)
		}
		if len(filtered) == 0 {
			filtered = []llm.ContentBlock{llm.Text("[thinking only]")}
		}
		out = append(out, llm.Message{Role: m.Role, Content: filtered})
	}
	return out
}

// stripUnsignedThinkingBlocks removes thinking blocks without a valid signature
// from assistant messages. The API requires a signature on every thinking block
// sent back; blocks from non-Anthropic providers or corrupted sessions lack one.
func stripUnsignedThinkingBlocks(msgs []llm.Message) []llm.Message {
	out := make([]llm.Message, 0, len(msgs))
	for _, m := range msgs {
		if m.Role != llm.RoleAssistant {
			out = append(out, m)
			continue
		}
		hasThinking := false
		for _, b := range m.Content {
			if b.Type == llm.BlockThinking || b.Type == llm.BlockRedactedThinking {
				hasThinking = true
				break
			}
		}
		if !hasThinking {
			out = append(out, m)
			continue
		}
		var filtered []llm.ContentBlock
		for _, b := range m.Content {
			if b.Type == llm.BlockThinking && (b.Signature == "" || strings.TrimSpace(b.Thinking) == "") {
				continue // drop unsigned or empty thinking
			}
			filtered = append(filtered, b)
		}
		if len(filtered) == 0 {
			// All blocks were thinking — keep a minimal text block so the
			// message isn't empty (API rejects empty assistant messages).
			filtered = []llm.ContentBlock{llm.Text("[thinking only]")}
		}
		out = append(out, llm.Message{Role: m.Role, Content: filtered})
	}
	return out
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
	case "model_context_window_exceeded":
		return llm.StopContextWindowExceeded
	case "":
		return llm.StopEndTurn
	default:
		return llm.StopUnknown
	}
}
