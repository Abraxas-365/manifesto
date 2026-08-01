package openai

import (
	"encoding/json"
	"fmt"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
)

// toResponsesInput converts harness messages into Responses API input items.
// The system prompt becomes a "system" role message; user/assistant messages
// map to their respective roles. Tool results become function_call_output items.
func toResponsesInput(system string, messages []llm.Message) responses.ResponseInputParam {
	var out responses.ResponseInputParam

	if system != "" {
		out = append(out, responses.ResponseInputItemUnionParam{
			OfMessage: &responses.EasyInputMessageParam{
				Role:    "system",
				Content: responses.EasyInputMessageContentUnionParam{OfString: param.NewOpt(system)},
			},
		})
	}

	for _, msg := range messages {
		switch msg.Role {
		case llm.RoleUser:
			out = append(out, userResponseItems(msg)...)
		case llm.RoleAssistant:
			out = append(out, assistantResponseItems(msg)...)
		}
	}

	return out
}

// userResponseItems converts a harness user message. Tool-result blocks become
// function_call_output items; text/image blocks become a user message.
func userResponseItems(msg llm.Message) []responses.ResponseInputItemUnionParam {
	var out []responses.ResponseInputItemUnionParam
	var content responses.ResponseInputMessageContentListParam

	for _, b := range msg.Content {
		switch b.Type {
		case llm.BlockText:
			if b.Text != "" {
				content = append(content, responses.ResponseInputContentParamOfInputText(b.Text))
			}
		case llm.BlockImage:
			image := responses.ResponseInputContentParamOfInputImage(responses.ResponseInputImageDetailAuto)
			image.OfInputImage.ImageURL = param.NewOpt(fmt.Sprintf("data:%s;base64,%s", b.MediaType, b.Data))
			content = append(content, image)
		case llm.BlockToolResult:
			result := b.Content
			if b.IsError && result == "" {
				result = "(tool reported an error)"
			}
			out = append(out, responses.ResponseInputItemUnionParam{
				OfFunctionCallOutput: &responses.ResponseInputItemFunctionCallOutputParam{
					CallID: b.ToolUseID,
					Output: responses.ResponseInputItemFunctionCallOutputOutputUnionParam{
						OfString: param.NewOpt(result),
					},
				},
			})
		}
	}

	if len(content) > 0 {
		out = append(out, responses.ResponseInputItemUnionParam{
			OfMessage: &responses.EasyInputMessageParam{
				Role: "user",
				Content: responses.EasyInputMessageContentUnionParam{
					OfInputItemContentList: content,
				},
			},
		})
	}
	return out
}

// assistantResponseItems converts a harness assistant message. Text blocks
// become an output_message item; tool_use blocks become function_call items.
func assistantResponseItems(msg llm.Message) []responses.ResponseInputItemUnionParam {
	var out []responses.ResponseInputItemUnionParam

	// Collect text content for output message.
	var textContent []responses.ResponseOutputMessageContentUnionParam
	for _, b := range msg.Content {
		if b.Type == llm.BlockText && b.Text != "" {
			textContent = append(textContent, responses.ResponseOutputMessageContentUnionParam{
				OfOutputText: &responses.ResponseOutputTextParam{
					Text: b.Text,
				},
			})
		}
	}
	if len(textContent) > 0 {
		out = append(out, responses.ResponseInputItemUnionParam{
			OfOutputMessage: &responses.ResponseOutputMessageParam{
				Content: textContent,
				Status:  "completed",
			},
		})
	}

	// Tool calls become separate function_call items.
	for _, b := range msg.Content {
		if b.Type != llm.BlockToolUse {
			continue
		}
		args := string(b.Input)
		if args == "" {
			args = "{}"
		}
		out = append(out, responses.ResponseInputItemUnionParam{
			OfFunctionCall: &responses.ResponseFunctionToolCallParam{
				CallID:    b.ID,
				Name:      b.Name,
				Arguments: args,
			},
		})
	}

	return out
}

// toResponsesTools converts harness tool defs into Responses API tool params.
func toResponsesTools(tools []llm.ToolDef) []responses.ToolUnionParam {
	var out []responses.ToolUnionParam
	for _, t := range tools {
		tool := responses.FunctionToolParam{
			Name: t.Name,
		}
		if t.Description != "" {
			tool.Description = param.NewOpt(t.Description)
		}
		if len(t.InputSchema) > 0 {
			var params map[string]any
			if err := json.Unmarshal(t.InputSchema, &params); err == nil {
				tool.Parameters = shared.FunctionParameters(params)
			}
		}
		out = append(out, responses.ToolUnionParam{OfFunction: &tool})
	}
	return out
}

// fromResponsesOutput converts a Responses API response into a harness message.
func fromResponsesOutput(resp *responses.Response) (llm.Message, llm.StopReason, llm.Usage) {
	usage := llm.Usage{
		InputTokens:     int(resp.Usage.InputTokens),
		OutputTokens:    int(resp.Usage.OutputTokens),
		CacheReadTokens: int(resp.Usage.InputTokensDetails.CachedTokens),
	}

	var blocks []llm.ContentBlock
	for _, item := range resp.Output {
		switch item.Type {
		case "message":
			for _, c := range item.Content {
				switch c.Type {
				case "output_text":
					if c.Text != "" {
						blocks = append(blocks, llm.Text(c.Text))
					}
				case "refusal":
					if c.Refusal != "" {
						blocks = append(blocks, llm.Text("[Refused] "+c.Refusal))
					}
				}
			}
		case "function_call":
			input := []byte(item.Arguments)
			if len(input) == 0 {
				input = []byte("{}")
			}
			blocks = append(blocks, llm.ToolUseBlock(item.CallID, item.Name, input))
		}
	}

	msg := llm.Message{Role: llm.RoleAssistant, Content: blocks}
	if len(blocks) == 0 {
		// All output items were unrecognized types (e.g. reasoning-only
		// response) — ensure the message has at least one content block so
		// it doesn't produce empty-content errors on cross-provider replay.
		msg.Content = []llm.ContentBlock{llm.Text("")}
	}
	reason := mapResponseStatus(resp.Status)
	reason = llm.NormalizeStopReason(reason, msg)
	return msg, reason, usage
}

// mapResponseStatus maps a Responses API status to the harness stop reason.
func mapResponseStatus(status responses.ResponseStatus) llm.StopReason {
	switch status {
	case "completed":
		return llm.StopEndTurn
	case "incomplete":
		return llm.StopMaxTokens
	case "failed":
		return llm.StopError
	default:
		return llm.StopEndTurn
	}
}
