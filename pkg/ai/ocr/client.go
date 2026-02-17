package ocr

import (
	"context"
	"fmt"
)

// Client provides unified access to OCR capabilities
type Client struct {
	recognizer TextRecognizer

	// Optional capabilities
	layoutAnalyzer    LayoutAnalyzer
	entityExtractor   EntityExtractor
	tableExtractor    TableExtractor
	formParser        FormParser
	imageExtractor    ImageExtractor
	markdownConverter MarkdownConverter
	batchProcessor    BatchProcessor
	streamProcessor   StreamProcessor

	annotator   Annotator
	documentQnA DocumentQnA
}

// NewClient creates a client from a provider
func NewClient(recognizer TextRecognizer) *Client {
	client := &Client{
		recognizer: recognizer,
	}

	// Detect all capabilities via type assertions
	if la, ok := recognizer.(LayoutAnalyzer); ok {
		client.layoutAnalyzer = la
	}
	if ee, ok := recognizer.(EntityExtractor); ok {
		client.entityExtractor = ee
	}
	if te, ok := recognizer.(TableExtractor); ok {
		client.tableExtractor = te
	}
	if fp, ok := recognizer.(FormParser); ok {
		client.formParser = fp
	}
	if ie, ok := recognizer.(ImageExtractor); ok {
		client.imageExtractor = ie
	}
	if mc, ok := recognizer.(MarkdownConverter); ok {
		client.markdownConverter = mc
	}
	if bp, ok := recognizer.(BatchProcessor); ok {
		client.batchProcessor = bp
	}
	if sp, ok := recognizer.(StreamProcessor); ok {
		client.streamProcessor = sp
	}

	// NEW: Check for annotations and QnA
	if ann, ok := recognizer.(Annotator); ok {
		client.annotator = ann
	}
	if qna, ok := recognizer.(DocumentQnA); ok {
		client.documentQnA = qna
	}

	return client
}

// ============================================================================
// Annotation Methods
// ============================================================================

// Annotate extracts structured information from a document
func (c *Client) Annotate(ctx context.Context, input Input, schema AnnotationSchema, opts ...Option) (*AnnotatedDocument, error) {
	if c.annotator == nil {
		return nil, fmt.Errorf("annotations not supported by this provider")
	}
	return c.annotator.AnnotateDocument(ctx, input, schema, opts...)
}

// AnnotateBBoxes extracts structured information from images in a document
func (c *Client) AnnotateBBoxes(ctx context.Context, input Input, schema AnnotationSchema, opts ...Option) (*AnnotatedDocument, error) {
	if c.annotator == nil {
		return nil, fmt.Errorf("bbox annotations not supported by this provider")
	}
	return c.annotator.AnnotateBBoxes(ctx, input, schema, opts...)
}

// AnnotateFull extracts structured information from both document and images
func (c *Client) AnnotateFull(ctx context.Context, input Input, docSchema, bboxSchema AnnotationSchema, opts ...Option) (*AnnotatedDocument, error) {
	if c.annotator == nil {
		return nil, fmt.Errorf("annotations not supported by this provider")
	}
	return c.annotator.AnnotateBoth(ctx, input, docSchema, bboxSchema, opts...)
}

// ============================================================================
// Document QnA Methods
// ============================================================================

// Ask asks a question about a document
func (c *Client) Ask(ctx context.Context, input Input, question string, opts ...Option) (QnAResponse, error) {
	if c.documentQnA == nil {
		return QnAResponse{}, fmt.Errorf("document QnA not supported by this provider")
	}
	return c.documentQnA.AskQuestion(ctx, input, question, opts...)
}

// AskMultiple asks multiple questions about a document
func (c *Client) AskMultiple(ctx context.Context, input Input, questions []string, opts ...Option) ([]QnAResponse, error) {
	if c.documentQnA == nil {
		return nil, fmt.Errorf("document QnA not supported by this provider")
	}
	return c.documentQnA.AskQuestions(ctx, input, questions, opts...)
}

// ChatWithDocument starts a conversation about a document
func (c *Client) ChatWithDocument(ctx context.Context, input Input, messages []ConversationMessage, opts ...Option) (QnAResponse, error) {
	if c.documentQnA == nil {
		return QnAResponse{}, fmt.Errorf("document QnA not supported by this provider")
	}
	return c.documentQnA.Chat(ctx, input, messages, opts...)
}

// ============================================================================
// Capability Checks
// ============================================================================

func (c *Client) SupportsAnnotations() bool { return c.annotator != nil }
func (c *Client) SupportsDocumentQnA() bool { return c.documentQnA != nil }

// Process is the main entry point - intelligently uses capabilities
func (c *Client) Process(ctx context.Context, input Input, opts ...Option) (*Result, error) {
	options := ApplyOptions(opts...)

	// Start with base text recognition
	result, err := c.recognizer.RecognizeText(ctx, input, opts...)
	if err != nil {
		return nil, err
	}

	builder := NewResultBuilder().
		WithText(result.Text()).
		WithPages(result.Pages()).
		WithConfidence(result.Confidence()).
		WithLanguage(result.Language()).
		WithUsage(result.Usage())

	// Enrich with optional capabilities if requested
	if options.EnableLayout && c.layoutAnalyzer != nil {
		layout, err := c.layoutAnalyzer.AnalyzeLayout(ctx, input, opts...)
		if err == nil {
			builder.WithLayout(layout)
		}
	}

	if options.EnableEntities && c.entityExtractor != nil {
		entities, err := c.entityExtractor.ExtractEntities(ctx, input, opts...)
		if err == nil {
			builder.WithEntities(entities)
		}
	}

	if options.EnableTables && c.tableExtractor != nil {
		tables, err := c.tableExtractor.ExtractTables(ctx, input, opts...)
		if err == nil {
			builder.WithTables(tables)
		}
	}

	if options.EnableFormFields && c.formParser != nil {
		formFields, err := c.formParser.ParseForm(ctx, input, opts...)
		if err == nil {
			builder.WithFormFields(formFields)
		}
	}

	if options.EnableImages && c.imageExtractor != nil {
		images, err := c.imageExtractor.ExtractImages(ctx, input, opts...)
		if err == nil {
			builder.WithImages(images)
		}
	}

	if options.EnableMarkdown && c.markdownConverter != nil {
		markdown, err := c.markdownConverter.ConvertToMarkdown(ctx, input, opts...)
		if err == nil {
			builder.WithMarkdown(markdown)
		}
	}

	return builder.Build(), nil
}

// ProcessBatch processes multiple documents
func (c *Client) ProcessBatch(ctx context.Context, inputs []Input, opts ...Option) ([]*Result, error) {
	if c.batchProcessor != nil {
		return c.batchProcessor.ProcessBatch(ctx, inputs, opts...)
	}

	// Fallback to sequential processing
	results := make([]*Result, len(inputs))
	for i, input := range inputs {
		result, err := c.Process(ctx, input, opts...)
		if err != nil {
			return nil, fmt.Errorf("failed to process document %d: %w", i, err)
		}
		results[i] = result
	}
	return results, nil
}

// ProcessStream processes a large document as a stream
func (c *Client) ProcessStream(ctx context.Context, input Input, opts ...Option) (PageStream, error) {
	if c.streamProcessor != nil {
		return c.streamProcessor.ProcessStream(ctx, input, opts...)
	}
	return nil, fmt.Errorf("streaming not supported by this provider")
}

// Capability checks
func (c *Client) SupportsLayout() bool          { return c.layoutAnalyzer != nil }
func (c *Client) SupportsEntities() bool        { return c.entityExtractor != nil }
func (c *Client) SupportsTables() bool          { return c.tableExtractor != nil }
func (c *Client) SupportsFormParsing() bool     { return c.formParser != nil }
func (c *Client) SupportsImageExtraction() bool { return c.imageExtractor != nil }
func (c *Client) SupportsMarkdown() bool        { return c.markdownConverter != nil }
func (c *Client) SupportsBatch() bool           { return c.batchProcessor != nil }
func (c *Client) SupportsStreaming() bool       { return c.streamProcessor != nil }
