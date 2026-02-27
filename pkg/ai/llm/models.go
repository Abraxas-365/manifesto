package llm

import "strings"

// Role constants
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleFunction  = "function"
	RoleTool      = "tool"
)

// ContentPartType represents the type of a content part
type ContentPartType string

const (
	ContentPartTypeText       ContentPartType = "text"
	ContentPartTypeImageURL   ContentPartType = "image_url"
	ContentPartTypeInputAudio ContentPartType = "input_audio"
	ContentPartTypeFile       ContentPartType = "file"
)

// ImageDetail controls fidelity for vision models
type ImageDetail string

const (
	ImageDetailAuto ImageDetail = "auto"
	ImageDetailLow  ImageDetail = "low"
	ImageDetailHigh ImageDetail = "high"
)

// ImageURL references an image by URL or base64 data URI
type ImageURL struct {
	URL    string      `json:"url"`
	Detail ImageDetail `json:"detail,omitempty"`
}

// InputAudio carries base64-encoded audio data
type InputAudio struct {
	Data   string `json:"data"`   // base64 encoded
	Format string `json:"format"` // "wav" or "mp3"
}

// FileContent references a file by ID or inline data
type FileContent struct {
	FileID   string `json:"file_id,omitempty"`
	FileData string `json:"file_data,omitempty"` // base64 encoded
	Filename string `json:"filename,omitempty"`
}

// ContentPart represents one part of a multimodal message
type ContentPart struct {
	Type       ContentPartType `json:"type"`
	Text       string          `json:"text,omitempty"`
	ImageURL   *ImageURL       `json:"image_url,omitempty"`
	InputAudio *InputAudio     `json:"input_audio,omitempty"`
	File       *FileContent    `json:"file,omitempty"`
}

// TextPart creates a text content part
func TextPart(text string) ContentPart {
	return ContentPart{Type: ContentPartTypeText, Text: text}
}

// ImagePart creates an image content part from a URL (or base64 data URI)
func ImagePart(url string, detail ...ImageDetail) ContentPart {
	img := &ImageURL{URL: url}
	if len(detail) > 0 {
		img.Detail = detail[0]
	}
	return ContentPart{Type: ContentPartTypeImageURL, ImageURL: img}
}

// AudioPart creates an audio content part from base64 data
func AudioPart(data, format string) ContentPart {
	return ContentPart{Type: ContentPartTypeInputAudio, InputAudio: &InputAudio{Data: data, Format: format}}
}

// FilePart creates a file content part from an uploaded file ID
func FilePart(fileID string) ContentPart {
	return ContentPart{Type: ContentPartTypeFile, File: &FileContent{FileID: fileID}}
}

// FileDataPart creates a file content part from inline base64 data
func FileDataPart(data, filename string) ContentPart {
	return ContentPart{Type: ContentPartTypeFile, File: &FileContent{FileData: data, Filename: filename}}
}

// Message represents a chat message
type Message struct {
	Role         string         `json:"role"`
	Content      string         `json:"content,omitempty"`
	MultiContent []ContentPart  `json:"multi_content,omitempty"` // Multimodal content parts; takes precedence over Content
	Name         string         `json:"name,omitempty"`
	FunctionCall *FunctionCall  `json:"function_call,omitempty"`
	ToolCalls    []ToolCall     `json:"tool_calls,omitempty"`
	ToolCallID   string         `json:"tool_call_id,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

// IsMultimodal returns true if the message contains multimodal content parts
func (m Message) IsMultimodal() bool {
	return len(m.MultiContent) > 0
}

// TextContent returns the text content of the message, extracting from
// MultiContent parts if necessary
func (m Message) TextContent() string {
	if !m.IsMultimodal() {
		return m.Content
	}
	var parts []string
	for _, p := range m.MultiContent {
		if p.Type == ContentPartTypeText {
			parts = append(parts, p.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// Usage represents token usage statistics
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// FunctionCall represents a function call in a message
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Function describes a callable function
type Function struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"` // JSON Schema object
}

// ToolCall represents a tool call
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// Tool represents a callable tool
type Tool struct {
	Type     string   `json:"type"`
	Function Function `json:"function"`
}

// NewUserMessage creates a new user message
func NewUserMessage(content string) Message {
	return Message{
		Role:    RoleUser,
		Content: content,
	}
}

// NewMultimodalUserMessage creates a user message with multimodal content parts
func NewMultimodalUserMessage(parts ...ContentPart) Message {
	return Message{
		Role:         RoleUser,
		MultiContent: parts,
	}
}

// NewImageMessage creates a user message with text and one image URL
func NewImageMessage(text, imageURL string, detail ...ImageDetail) Message {
	parts := []ContentPart{TextPart(text), ImagePart(imageURL, detail...)}
	return Message{
		Role:         RoleUser,
		MultiContent: parts,
	}
}

// NewSystemMessage creates a new system message
func NewSystemMessage(content string) Message {
	return Message{
		Role:    RoleSystem,
		Content: content,
	}
}

// NewAssistantMessage creates a new assistant message
func NewAssistantMessage(content string) Message {
	return Message{
		Role:    RoleAssistant,
		Content: content,
	}
}

// NewFunctionMessage creates a new function message
func NewFunctionMessage(name string, content string) Message {
	return Message{
		Role:    RoleFunction,
		Name:    name,
		Content: content,
	}
}

// NewToolMessage creates a new tool message
func NewToolMessage(toolCallID string, content string) Message {
	return Message{
		Role:       RoleTool,
		ToolCallID: toolCallID,
		Content:    content,
	}
}

// ResponseFormatType represents the format type for model outputs
type ResponseFormatType string

const (
	// JSONObject requests output in JSON object format
	JSONObject ResponseFormatType = "json_object"
	// TextFormat requests output in plain text (default)
	TextFormat ResponseFormatType = "text"
	// JSONSchema requests output conforming to a specific JSON schema
	JSONSchema ResponseFormatType = "json_schema"
)

// ResponseFormat specifies the desired output format
type ResponseFormat struct {
	Type       ResponseFormatType `json:"type"`
	JSONSchema any                `json:"schema,omitempty"` // Optional JSON schema for JSONSchema type
}

// WithResponseFormat specifies the output format
func WithResponseFormat(format *ResponseFormat) Option {
	return func(o *ChatOptions) {
		o.ResponseFormat = format
	}
}

// WithJSONResponseFormat sets the response format to JSON object
func WithJSONResponseFormat() Option {
	return func(o *ChatOptions) {
		o.ResponseFormat = &ResponseFormat{
			Type: JSONObject,
		}
	}
}

// WithJSONSchemaResponseFormat sets the response format to conform to a specific JSON schema
func WithJSONSchemaResponseFormat(schema any) Option {
	return func(o *ChatOptions) {
		o.ResponseFormat = &ResponseFormat{
			Type:       JSONSchema,
			JSONSchema: schema,
		}
	}
}
