package llm

import "encoding/json"

// Role identifies the author of a message.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// BlockType identifies the kind of content block.
type BlockType string

const (
	BlockText             BlockType = "text"
	BlockToolUse          BlockType = "tool_use"
	BlockToolResult       BlockType = "tool_result"
	BlockImage            BlockType = "image"
	BlockThinking         BlockType = "thinking"
	BlockRedactedThinking BlockType = "redacted_thinking"
)

// ContentBlock is a single piece of message content. Which fields are populated
// depends on Type:
//   - BlockText:       Text
//   - BlockToolUse:    ID, Name, Input
//   - BlockToolResult: ToolUseID, Content, IsError, Images
//   - BlockImage:      MediaType, Data
type ContentBlock struct {
	Type BlockType `json:"type"`

	// Text block.
	Text string `json:"text,omitempty"`

	// Tool-use block.
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`

	// Tool-result block.
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`

	// Thinking block.
	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`

	// Image block (also used to attach images to a tool result).
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`

	// Images holds images attached to a tool-result block.
	Images []ContentBlock `json:"images,omitempty"`
}

// Message is a single turn in the conversation.
type Message struct {
	Role    Role           `json:"role"`
	Content []ContentBlock `json:"content"`
}

// ToolDef describes a tool exposed to the model.
type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// Usage reports token consumption for a response.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	// CacheReadTokens is the number of input tokens served from a prompt cache.
	CacheReadTokens int `json:"cache_read_tokens,omitempty"`
	// CacheWriteTokens is the number of input tokens written to a prompt cache.
	CacheWriteTokens int `json:"cache_write_tokens,omitempty"`
}

// Add returns the field-wise sum of two usage records.
func (u Usage) Add(o Usage) Usage {
	return Usage{
		InputTokens:      u.InputTokens + o.InputTokens,
		OutputTokens:     u.OutputTokens + o.OutputTokens,
		CacheReadTokens:  u.CacheReadTokens + o.CacheReadTokens,
		CacheWriteTokens: u.CacheWriteTokens + o.CacheWriteTokens,
	}
}

// Text returns a text content block.
func Text(text string) ContentBlock {
	return ContentBlock{Type: BlockText, Text: text}
}

// ToolUseBlock returns a tool_use content block.
func ToolUseBlock(id, name string, input json.RawMessage) ContentBlock {
	return ContentBlock{Type: BlockToolUse, ID: id, Name: name, Input: input}
}

// ToolResultBlock returns a tool_result content block.
func ToolResultBlock(toolUseID, content string, isError bool) ContentBlock {
	return ContentBlock{Type: BlockToolResult, ToolUseID: toolUseID, Content: content, IsError: isError}
}

// ImageBlock returns an image content block.
func ImageBlock(mediaType, data string) ContentBlock {
	return ContentBlock{Type: BlockImage, MediaType: mediaType, Data: data}
}

// ThinkingBlock returns a thinking content block.
func ThinkingBlock(thinking, signature string) ContentBlock {
	return ContentBlock{Type: BlockThinking, Thinking: thinking, Signature: signature}
}

// RedactedThinkingBlock returns a redacted_thinking content block.
func RedactedThinkingBlock(data string) ContentBlock {
	return ContentBlock{Type: BlockRedactedThinking, Data: data}
}

// UserText returns a user message containing a single text block.
func UserText(text string) Message {
	return Message{Role: RoleUser, Content: []ContentBlock{Text(text)}}
}

// ToolUses returns the tool_use blocks in the message.
func (m Message) ToolUses() []ContentBlock {
	var out []ContentBlock
	for _, b := range m.Content {
		if b.Type == BlockToolUse {
			out = append(out, b)
		}
	}
	return out
}

// TextContent returns the concatenation of all text blocks in the message.
func (m Message) TextContent() string {
	var s string
	for _, b := range m.Content {
		if b.Type == BlockText {
			s += b.Text
		}
	}
	return s
}
