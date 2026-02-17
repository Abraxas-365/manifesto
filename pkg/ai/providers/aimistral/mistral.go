package aimistral

// ============================================================================
// OCR API Types
// ============================================================================

// OCRRequest represents a request to the Mistral OCR API
type OCRRequest struct {
	Model                    string        `json:"model"`
	Document                 DocumentInput `json:"document"`
	TableFormat              string        `json:"table_format,omitempty"`
	ExtractHeader            bool          `json:"extract_header,omitempty"`
	ExtractFooter            bool          `json:"extract_footer,omitempty"`
	IncludeImageBase64       bool          `json:"include_image_base64,omitempty"`
	Pages                    []int         `json:"pages,omitempty"`
	BBoxAnnotationFormat     any           `json:"bbox_annotation_format,omitempty"`
	DocumentAnnotationFormat any           `json:"document_annotation_format,omitempty"`
	DocumentAnnotationPrompt string        `json:"document_annotation_prompt,omitempty"`
}

// DocumentInput represents different ways to provide a document
type DocumentInput struct {
	Type        string `json:"type"` // "document_url" or "image_url"
	DocumentURL string `json:"document_url,omitempty"`
	ImageURL    string `json:"image_url,omitempty"`
}

// OCRResponse represents the response from Mistral OCR API
type OCRResponse struct {
	Pages              []PageData `json:"pages"`
	Model              string     `json:"model"`
	DocumentAnnotation string     `json:"document_annotation,omitempty"`
	UsageInfo          UsageInfo  `json:"usage_info"`
}

// PageData represents a single page in the OCR response
type PageData struct {
	Index      int            `json:"index"`
	Markdown   string         `json:"markdown"`
	Images     []ImageData    `json:"images"`
	Tables     []TableData    `json:"tables"`
	Hyperlinks []string       `json:"hyperlinks"`
	Header     *string        `json:"header"`
	Footer     *string        `json:"footer"`
	Dimensions map[string]any `json:"dimensions"`
}

// ImageData represents an extracted image
type ImageData struct {
	ID              string `json:"id"`
	TopLeftX        int    `json:"top_left_x"`
	TopLeftY        int    `json:"top_left_y"`
	BottomRightX    int    `json:"bottom_right_x"`
	BottomRightY    int    `json:"bottom_right_y"`
	ImageBase64     string `json:"image_base64,omitempty"`
	ImageAnnotation string `json:"image_annotation,omitempty"`
}

// TableData represents an extracted table
type TableData struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	Format  string `json:"format"`
}

// UsageInfo represents API usage information
type UsageInfo struct {
	PagesProcessed int `json:"pages_processed"`
	DocSizeBytes   int `json:"doc_size_bytes"`
}

// ============================================================================
// Chat Completion API Types (for QnA)
// ============================================================================

// ChatRequest represents a chat completion request
type ChatRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
}

// ChatMessage represents a message in the conversation
type ChatMessage struct {
	Role    string        `json:"role"`
	Content []ContentPart `json:"content"`
}

// ContentPart represents a part of message content
type ContentPart struct {
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`
	DocumentURL string `json:"document_url,omitempty"`
	ImageURL    string `json:"image_url,omitempty"`
}

// ChatResponse represents a chat completion response
type ChatResponse struct {
	ID      string `json:"id"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int    `json:"index"`
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// AnnotationFormat represents the format for annotations
type AnnotationFormat struct {
	Type       string         `json:"type"`
	JSONSchema map[string]any `json:"json_schema"`
}

// Helper to create annotation format
func NewAnnotationFormat(name string, schema map[string]any, strict bool) AnnotationFormat {
	return AnnotationFormat{
		Type: "json_schema",
		JSONSchema: map[string]any{
			"name":   name,
			"schema": schema,
			"strict": strict,
		},
	}
}
