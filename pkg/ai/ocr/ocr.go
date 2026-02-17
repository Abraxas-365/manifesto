// === ./pkg/ai/ocr/ocr.go ===
package ocr

import (
	"context"
	"io"
)

// ============================================================================
// LAYER 1: Core Capabilities (Single Responsibility Interfaces)
// ============================================================================

// TextRecognizer is the minimal OCR interface - all providers must implement this
type TextRecognizer interface {
	RecognizeText(ctx context.Context, input Input, opts ...Option) (*Result, error)
}

// LayoutAnalyzer extracts detailed document layout (blocks, lines, tokens)
type LayoutAnalyzer interface {
	AnalyzeLayout(ctx context.Context, input Input, opts ...Option) (*Layout, error)
}

// EntityExtractor extracts structured entities (dates, amounts, names)
type EntityExtractor interface {
	ExtractEntities(ctx context.Context, input Input, opts ...Option) ([]Entity, error)
}

// TableExtractor extracts tables from documents
type TableExtractor interface {
	ExtractTables(ctx context.Context, input Input, opts ...Option) ([]Table, error)
}

// FormParser extracts key-value pairs from forms
type FormParser interface {
	ParseForm(ctx context.Context, input Input, opts ...Option) ([]FormField, error)
}

// ImageExtractor extracts embedded images from documents
type ImageExtractor interface {
	ExtractImages(ctx context.Context, input Input, opts ...Option) ([]Image, error)
}

// MarkdownConverter converts documents to markdown (Mistral specialty)
type MarkdownConverter interface {
	ConvertToMarkdown(ctx context.Context, input Input, opts ...Option) (string, error)
}

// BatchProcessor supports efficient batch processing
type BatchProcessor interface {
	ProcessBatch(ctx context.Context, inputs []Input, opts ...Option) ([]*Result, error)
}

// StreamProcessor supports streaming for large documents
type StreamProcessor interface {
	ProcessStream(ctx context.Context, input Input, opts ...Option) (PageStream, error)
}

// ============================================================================
// ANNOTATIONS INTERFACE
// ============================================================================

// Annotator extracts structured information from documents using custom schemas
type Annotator interface {
	// AnnotateDocument extracts structured data from the entire document
	AnnotateDocument(ctx context.Context, input Input, schema AnnotationSchema, opts ...Option) (*AnnotatedDocument, error)

	// AnnotateBBoxes extracts structured data from images/figures in the document
	AnnotateBBoxes(ctx context.Context, input Input, schema AnnotationSchema, opts ...Option) (*AnnotatedDocument, error)

	// AnnotateBoth extracts structured data from both document and bboxes
	AnnotateBoth(ctx context.Context, input Input, docSchema, bboxSchema AnnotationSchema, opts ...Option) (*AnnotatedDocument, error)
}

// AnnotationSchema represents a JSON schema for structured extraction
type AnnotationSchema struct {
	// Name of the schema
	Name string

	// Schema definition (JSON Schema format)
	Schema map[string]any

	// Description/prompt to guide the annotation
	Prompt string

	// Whether to enforce strict schema adherence
	Strict bool
}

// AnnotatedDocument contains the OCR result plus structured annotations
type AnnotatedDocument struct {
	// Base OCR result
	*Result

	// Document-level structured annotation
	DocumentAnnotation any

	// Per-bbox annotations (keyed by image ID)
	BBoxAnnotations map[string]any
}

// ============================================================================
// DOCUMENT QnA INTERFACE
// ============================================================================

// DocumentQnA enables question-answering on documents using LLMs
type DocumentQnA interface {
	// AskQuestion asks a single question about the document
	AskQuestion(ctx context.Context, input Input, question string, opts ...Option) (QnAResponse, error)

	// AskQuestions asks multiple questions about the document
	AskQuestions(ctx context.Context, input Input, questions []string, opts ...Option) ([]QnAResponse, error)

	// Chat enables multi-turn conversation about the document
	Chat(ctx context.Context, input Input, messages []ConversationMessage, opts ...Option) (QnAResponse, error)
}

// QnAResponse represents the answer to a question about a document
type QnAResponse struct {
	// The answer text
	Answer string

	// Confidence score (0-1) if available
	Confidence float32

	// Source pages/locations where answer was found
	Sources []AnswerSource

	// Token usage for the query
	TokenUsage TokenUsage
}

// AnswerSource indicates where in the document the answer came from
type AnswerSource struct {
	// Page number
	PageNumber int

	// Text excerpt from the source
	Excerpt string

	// Bounding box if available
	BoundingBox *BoundingBox
}

// ConversationMessage represents a message in a document conversation
type ConversationMessage struct {
	Role    string // "user" or "assistant"
	Content string
}

// TokenUsage represents token consumption for QnA
type TokenUsage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// ============================================================================
// LAYER 2: Input Abstraction (Unified Input Handling)
// ============================================================================

// Input represents various input sources
type Input struct {
	// Source type
	Type InputType

	// Data based on type
	Reader io.Reader // For file uploads
	URL    string    // For URLs
	Data   []byte    // For base64 or raw bytes
	Path   string    // For local file paths

	// Metadata
	MimeType string
	Metadata map[string]any
}

type InputType string

const (
	InputTypeReader      InputType = "reader"
	InputTypeURL         InputType = "url"
	InputTypeImageURL    InputType = "image_url"
	InputTypeDocumentURL InputType = "document_url"
	InputTypeBase64      InputType = "base64"
	InputTypeGCS         InputType = "gcs"
	InputTypeS3          InputType = "s3"
	InputTypePath        InputType = "path"
)

// Input builders for convenience
func FromReader(r io.Reader, mimeType string) Input {
	return Input{Type: InputTypeReader, Reader: r, MimeType: mimeType}
}

func FromURL(url string) Input {
	return Input{Type: InputTypeURL, URL: url}
}

func FromBase64(data []byte, mimeType string) Input {
	return Input{Type: InputTypeBase64, Data: data, MimeType: mimeType}
}

// ============================================================================
// LAYER 3: Result Model (Immutable, Builder-Based)
// ============================================================================

// Result is the unified OCR result with optional capabilities
type Result struct {
	// Core fields (always present)
	id         string
	text       string
	pages      []Page
	confidence float32
	language   string

	// Capability-based extensions (optional, use getters)
	layout     *Layout
	entities   []Entity
	tables     []Table
	formFields []FormField
	images     []Image
	markdown   string

	// Metadata
	usage    Usage
	metadata map[string]any
}

// Getters (immutable access)
func (r *Result) ID() string               { return r.id }
func (r *Result) Text() string             { return r.text }
func (r *Result) Pages() []Page            { return r.pages }
func (r *Result) Confidence() float32      { return r.confidence }
func (r *Result) Language() string         { return r.language }
func (r *Result) Usage() Usage             { return r.usage }
func (r *Result) Metadata() map[string]any { return r.metadata }

// Capability checks
func (r *Result) HasLayout() bool     { return r.layout != nil }
func (r *Result) HasEntities() bool   { return len(r.entities) > 0 }
func (r *Result) HasTables() bool     { return len(r.tables) > 0 }
func (r *Result) HasFormFields() bool { return len(r.formFields) > 0 }
func (r *Result) HasImages() bool     { return len(r.images) > 0 }
func (r *Result) HasMarkdown() bool   { return r.markdown != "" }

// Capability accessors
func (r *Result) Layout() *Layout         { return r.layout }
func (r *Result) Entities() []Entity      { return r.entities }
func (r *Result) Tables() []Table         { return r.tables }
func (r *Result) FormFields() []FormField { return r.formFields }
func (r *Result) Images() []Image         { return r.images }
func (r *Result) Markdown() string        { return r.markdown }

// ============================================================================
// LAYER 4: Result Builder (Fluent Construction)
// ============================================================================

// ResultBuilder constructs Results with fluent API
type ResultBuilder struct {
	result Result
}

func NewResultBuilder() *ResultBuilder {
	return &ResultBuilder{
		result: Result{
			metadata: make(map[string]any),
		},
	}
}

func (b *ResultBuilder) WithText(text string) *ResultBuilder {
	b.result.text = text
	return b
}

func (b *ResultBuilder) WithPages(pages []Page) *ResultBuilder {
	b.result.pages = pages
	return b
}

func (b *ResultBuilder) WithConfidence(confidence float32) *ResultBuilder {
	b.result.confidence = confidence
	return b
}

func (b *ResultBuilder) WithLanguage(language string) *ResultBuilder {
	b.result.language = language
	return b
}

func (b *ResultBuilder) WithLayout(layout *Layout) *ResultBuilder {
	b.result.layout = layout
	return b
}

func (b *ResultBuilder) WithEntities(entities []Entity) *ResultBuilder {
	b.result.entities = entities
	return b
}

func (b *ResultBuilder) WithTables(tables []Table) *ResultBuilder {
	b.result.tables = tables
	return b
}

func (b *ResultBuilder) WithFormFields(formFields []FormField) *ResultBuilder {
	b.result.formFields = formFields
	return b
}

func (b *ResultBuilder) WithImages(images []Image) *ResultBuilder {
	b.result.images = images
	return b
}

func (b *ResultBuilder) WithMarkdown(markdown string) *ResultBuilder {
	b.result.markdown = markdown
	return b
}

func (b *ResultBuilder) WithUsage(usage Usage) *ResultBuilder {
	b.result.usage = usage
	return b
}

func (b *ResultBuilder) WithMetadata(key string, value any) *ResultBuilder {
	b.result.metadata[key] = value
	return b
}

func (b *ResultBuilder) Build() *Result {
	return &b.result
}

// ============================================================================
// LAYER 5: Page Stream (For Large Documents)
// ============================================================================

type PageStream interface {
	// Next returns the next page, or io.EOF when done
	Next() (*Page, error)

	// Close releases resources
	Close() error
}

// ============================================================================
// Core Data Models
// ============================================================================

// Page represents a single page with all possible data
type Page struct {
	// Core
	Number     int
	Text       string
	Confidence float32
	Dimensions Dimensions

	// Layout (optional)
	Blocks     []TextBlock
	Paragraphs []TextBlock
	Lines      []TextBlock
	Words      []TextBlock

	// Structured data (optional)
	Entities   []Entity
	Tables     []Table
	FormFields []FormField
	Images     []Image

	// Provider-specific (optional)
	Markdown   string
	Header     *string
	Footer     *string
	Hyperlinks []Hyperlink

	// Metadata
	Metadata map[string]any
}

// Layout represents detailed document layout structure
type Layout struct {
	ReadingOrder []LayoutElement
	Columns      int
	Orientation  string // "portrait", "landscape"
	Skew         float32
}

type LayoutElement struct {
	Type        ElementType // "text", "image", "table", "figure"
	BoundingBox BoundingBox
	Content     any // Type-specific content
}

type ElementType string

const (
	ElementTypeText   ElementType = "text"
	ElementTypeImage  ElementType = "image"
	ElementTypeTable  ElementType = "table"
	ElementTypeFigure ElementType = "figure"
)

// TextBlock represents a block of text at any granularity
type TextBlock struct {
	Text        string
	Type        BlockType
	BoundingBox BoundingBox
	Confidence  float32
	Language    string
	Style       *TextStyle
}

type BlockType string

const (
	BlockTypeBlock     BlockType = "block"
	BlockTypeParagraph BlockType = "paragraph"
	BlockTypeLine      BlockType = "line"
	BlockTypeWord      BlockType = "word"
	BlockTypeTitle     BlockType = "title"
	BlockTypeHeading   BlockType = "heading"
	BlockTypeCaption   BlockType = "caption"
)

// Entity represents structured data
type Entity struct {
	Type            string // "date", "amount", "name", "address", etc.
	Value           string // Raw value
	NormalizedValue any    // Typed value (time.Time, float64, etc.)
	Confidence      float32
	MentionText     string
	BoundingBox     BoundingBox
	PageNumber      int
	Properties      map[string]any // Entity-specific properties
}

// Table represents an extracted table
type Table struct {
	ID          string
	Rows        int
	Columns     int
	HeaderRows  []TableRow
	BodyRows    []TableRow
	BoundingBox BoundingBox
	Confidence  float32

	// Added fields
	Placeholder string
	Format      TableFormat

	// Export formats
	HTML     string
	Markdown string
	CSV      string

	// Structured data
	Data [][]TableCell
}

type TableRow struct {
	Cells []TableCell
}

type TableCell struct {
	Text        string
	RowSpan     int
	ColSpan     int
	IsHeader    bool
	BoundingBox BoundingBox
	Confidence  float32
}

// FormField represents a key-value pair
type FormField struct {
	FieldName        string
	FieldValue       string
	FieldType        FieldType
	NameConfidence   float32
	ValueConfidence  float32
	NameBoundingBox  BoundingBox
	ValueBoundingBox BoundingBox
	PageNumber       int
}

type FieldType string

const (
	FieldTypeText      FieldType = "text"
	FieldTypeNumber    FieldType = "number"
	FieldTypeDate      FieldType = "date"
	FieldTypeCheckbox  FieldType = "checkbox"
	FieldTypeSignature FieldType = "signature"
)

// Image represents an embedded image
type Image struct {
	ID          string
	Format      string // "jpeg", "png", etc.
	Base64      string
	URL         string
	BoundingBox BoundingBox
	Width       int
	Height      int
	Caption     string
	Placeholder string // Markdown placeholder

	// Added field
	Metadata map[string]any
}

// Hyperlink represents a detected link
type Hyperlink struct {
	Text        string
	URL         string
	Type        LinkType
	BoundingBox BoundingBox
}

type LinkType string

const (
	LinkTypeExternal LinkType = "external"
	LinkTypeInternal LinkType = "internal"
	LinkTypeEmail    LinkType = "email"
)

// BoundingBox represents spatial position
type BoundingBox struct {
	X          float32
	Y          float32
	Width      float32
	Height     float32
	Vertices   []Vertex
	Normalized bool // Whether coords are normalized (0-1)
}

type Vertex struct {
	X float32
	Y float32
}

// Dimensions represents page dimensions
type Dimensions struct {
	Width  float32
	Height float32
	Unit   string // "px", "pt", "in"
}

// TextStyle represents text styling
type TextStyle struct {
	FontFamily string
	FontSize   float32
	Bold       bool
	Italic     bool
	Underline  bool
	Color      string
}

// Usage represents processing usage
type Usage struct {
	PagesProcessed  int
	ImagesExtracted int
	TablesExtracted int
	EntitiesFound   int
	ProcessingTime  int // milliseconds
	Credits         float32
	Cost            float32
	Currency        string
	ProviderData    map[string]any
}
