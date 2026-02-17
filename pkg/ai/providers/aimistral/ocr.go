// === ./pkg/ai/providers/mistral/ocr.go ===
package aimistral

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/Abraxas-365/manifesto/pkg/ai/ocr"
	"github.com/Abraxas-365/manifesto/pkg/errx"
)

// MistralProvider implements OCR capabilities for Mistral AI
type MistralProvider struct {
	apiKey           string
	baseURL          string
	httpClient       *http.Client
	client           *HTTPClient
	maxRetries       int
	defaultModel     string
	defaultChatModel string
}

// NewMistralProvider creates a new Mistral OCR provider
func NewMistralProvider(apiKey string, opts ...ProviderOption) (*MistralProvider, *errx.Error) {
	if apiKey == "" {
		apiKey = os.Getenv("MISTRAL_API_KEY")
	}

	if apiKey == "" {
		return nil, errorRegistry.New(ErrMissingAPIKey)
	}

	provider := &MistralProvider{
		apiKey:           apiKey,
		baseURL:          DefaultBaseURL,
		maxRetries:       MaxRetries,
		defaultModel:     DefaultModel,
		defaultChatModel: DefaultChatModel,
	}

	// Apply options
	for _, opt := range opts {
		opt(provider)
	}

	// Create HTTP client
	provider.client = NewHTTPClient(provider.apiKey, provider.baseURL, provider.httpClient)
	provider.client.maxRetries = provider.maxRetries

	return provider, nil
}

// ============================================================================
// TextRecognizer Implementation
// ============================================================================

// RecognizeText implements the core OCR functionality
func (m *MistralProvider) RecognizeText(ctx context.Context, input ocr.Input, opts ...ocr.Option) (*ocr.Result, *errx.Error) {
	options := ocr.ApplyOptions(opts...)

	if options.Model == "" {
		options.Model = m.defaultModel
	}

	// Validate input
	if err := m.validateInput(input); err != nil {
		return nil, err
	}

	// Build request
	req := m.buildOCRRequest(input, options)

	// Call API
	respBody, err := m.client.Post(ctx, "/ocr", req)
	if err != nil {
		return nil, err
	}

	// Parse response
	var resp OCRResponse
	if parseErr := json.Unmarshal(respBody, &resp); parseErr != nil {
		return nil, WrapError(parseErr, ErrAPIResponse).
			WithDetail("error", "failed to parse OCR response")
	}

	// Convert to unified format
	result := m.convertToResult(&resp, options)
	return result, nil
}

// RecognizeURL is a convenience method for URL inputs
func (m *MistralProvider) RecognizeURL(ctx context.Context, url string, opts ...ocr.Option) (*ocr.Result, *errx.Error) {
	return m.RecognizeText(ctx, ocr.FromURL(url), opts...)
}

// ============================================================================
// Optional Capability Implementations
// ============================================================================

// ConvertToMarkdown implements MarkdownConverter
func (m *MistralProvider) ConvertToMarkdown(ctx context.Context, input ocr.Input, opts ...ocr.Option) (string, *errx.Error) {
	result, err := m.RecognizeText(ctx, input, append(opts, ocr.WithMarkdown())...)
	if err != nil {
		return "", err
	}
	return result.Markdown(), nil
}

// ExtractImages implements ImageExtractor
func (m *MistralProvider) ExtractImages(ctx context.Context, input ocr.Input, opts ...ocr.Option) ([]ocr.Image, *errx.Error) {
	allOpts := append([]ocr.Option{ocr.WithImages(true)}, opts...)
	result, err := m.RecognizeText(ctx, input, allOpts...)
	if err != nil {
		return nil, err
	}

	var images []ocr.Image
	for _, page := range result.Pages() {
		images = append(images, page.Images...)
	}
	return images, nil
}

// ExtractTables implements TableExtractor
func (m *MistralProvider) ExtractTables(ctx context.Context, input ocr.Input, opts ...ocr.Option) ([]ocr.Table, *errx.Error) {
	allOpts := append([]ocr.Option{ocr.WithTables(ocr.TableFormatHTML)}, opts...)
	result, err := m.RecognizeText(ctx, input, allOpts...)
	if err != nil {
		return nil, err
	}

	var tables []ocr.Table
	for _, page := range result.Pages() {
		tables = append(tables, page.Tables...)
	}
	return tables, nil
}

// ============================================================================
// Request Building
// ============================================================================

func (m *MistralProvider) buildOCRRequest(input ocr.Input, options *ocr.Options) *OCRRequest {
	req := &OCRRequest{
		Model:    options.Model,
		Document: m.convertInputToDocument(input),
	}

	// Table format
	if options.EnableTables && options.TableFormat != "" {
		req.TableFormat = string(options.TableFormat)
	}

	// Header/Footer extraction
	req.ExtractHeader = options.ExtractHeader
	req.ExtractFooter = options.ExtractFooter

	// Image base64
	req.IncludeImageBase64 = options.IncludeImageBase64

	// Provider-specific options
	if annotationFormat, ok := options.ProviderOptions["bbox_annotation_format"]; ok {
		req.BBoxAnnotationFormat = annotationFormat
	}
	if annotationFormat, ok := options.ProviderOptions["document_annotation_format"]; ok {
		req.DocumentAnnotationFormat = annotationFormat
	}
	if prompt, ok := options.ProviderOptions["document_annotation_prompt"].(string); ok {
		req.DocumentAnnotationPrompt = prompt
	}
	if pages, ok := options.ProviderOptions["pages"].([]int); ok {
		req.Pages = pages
	}

	return req
}

func (m *MistralProvider) convertInputToDocument(input ocr.Input) DocumentInput {
	switch input.Type {
	case ocr.InputTypeURL, ocr.InputTypeDocumentURL:
		return DocumentInput{
			Type:        "document_url",
			DocumentURL: input.URL,
		}
	case ocr.InputTypeImageURL:
		return DocumentInput{
			Type:     "image_url",
			ImageURL: input.URL,
		}
	case ocr.InputTypeBase64:
		mimeType := input.MimeType
		if mimeType == "" {
			mimeType = "application/pdf"
		}
		dataURL := fmt.Sprintf("data:%s;base64,%s", mimeType, string(input.Data))
		return DocumentInput{
			Type:        "document_url",
			DocumentURL: dataURL,
		}
	default:
		return DocumentInput{
			Type:        "document_url",
			DocumentURL: input.URL,
		}
	}
}

// ============================================================================
// Response Conversion
// ============================================================================

func (m *MistralProvider) convertToResult(resp *OCRResponse, options *ocr.Options) *ocr.Result {
	builder := ocr.NewResultBuilder()

	var fullText strings.Builder
	var fullMarkdown strings.Builder
	pages := make([]ocr.Page, len(resp.Pages))

	for i, mistralPage := range resp.Pages {
		page := ocr.Page{
			Number:     mistralPage.Index,
			Text:       mistralPage.Markdown,
			Markdown:   mistralPage.Markdown,
			Header:     mistralPage.Header,
			Footer:     mistralPage.Footer,
			Hyperlinks: m.convertHyperlinks(mistralPage.Hyperlinks),
			Dimensions: m.convertDimensions(mistralPage.Dimensions),
		}

		// Convert images
		if options.EnableImages {
			page.Images = m.convertImages(mistralPage.Images)
		}

		// Convert tables
		if options.EnableTables {
			page.Tables = m.convertTables(mistralPage.Tables)
		}

		pages[i] = page
		fullText.WriteString(mistralPage.Markdown)
		fullText.WriteString("\n\n")
		fullMarkdown.WriteString(mistralPage.Markdown)
		fullMarkdown.WriteString("\n\n")
	}

	builder.WithText(fullText.String()).
		WithPages(pages).
		WithMarkdown(fullMarkdown.String()).
		WithUsage(ocr.Usage{
			PagesProcessed:  resp.UsageInfo.PagesProcessed,
			ImagesExtracted: m.countImages(resp.Pages),
			TablesExtracted: m.countTables(resp.Pages),
		}).
		WithMetadata("model", resp.Model)

	// Add document annotation if present
	if resp.DocumentAnnotation != "" {
		builder.WithMetadata("document_annotation", resp.DocumentAnnotation)
	}

	return builder.Build()
}

func (m *MistralProvider) convertImages(mistralImages []ImageData) []ocr.Image {
	images := make([]ocr.Image, len(mistralImages))
	for i, img := range mistralImages {
		images[i] = ocr.Image{
			ID:          img.ID,
			Base64:      img.ImageBase64,
			Format:      m.detectImageFormat(img.ID),
			Placeholder: fmt.Sprintf("![%s](%s)", img.ID, img.ID),
			BoundingBox: ocr.BoundingBox{
				X:      float32(img.TopLeftX),
				Y:      float32(img.TopLeftY),
				Width:  float32(img.BottomRightX - img.TopLeftX),
				Height: float32(img.BottomRightY - img.TopLeftY),
			},
		}

		// Add annotation if present
		if img.ImageAnnotation != "" {
			images[i].Metadata = map[string]interface{}{
				"annotation": img.ImageAnnotation,
			}
		}
	}
	return images
}

func (m *MistralProvider) convertTables(mistralTables []TableData) []ocr.Table {
	tables := make([]ocr.Table, len(mistralTables))
	for i, tbl := range mistralTables {
		format := ocr.TableFormatHTML
		if tbl.Format != "" {
			format = ocr.TableFormat(tbl.Format)
		}

		tables[i] = ocr.Table{
			ID:          tbl.ID,
			Placeholder: fmt.Sprintf("[%s](%s)", tbl.ID, tbl.ID),
			Format:      format,
		}

		// Set content based on format
		switch format {
		case ocr.TableFormatHTML:
			tables[i].HTML = tbl.Content
		case ocr.TableFormatMarkdown:
			tables[i].Markdown = tbl.Content
		case ocr.TableFormatCSV:
			tables[i].CSV = tbl.Content
		}
	}
	return tables
}

func (m *MistralProvider) convertHyperlinks(urls []string) []ocr.Hyperlink {
	links := make([]ocr.Hyperlink, len(urls))
	for i, url := range urls {
		linkType := ocr.LinkTypeExternal
		if strings.HasPrefix(url, "mailto:") {
			linkType = ocr.LinkTypeEmail
		}

		links[i] = ocr.Hyperlink{
			Text: url,
			URL:  url,
			Type: linkType,
		}
	}
	return links
}

func (m *MistralProvider) convertDimensions(dims map[string]interface{}) ocr.Dimensions {
	return ocr.Dimensions{
		Width:  getFloat32(dims, "width"),
		Height: getFloat32(dims, "height"),
		Unit:   "px",
	}
}

func (m *MistralProvider) detectImageFormat(filename string) string {
	ext := strings.ToLower(filename[strings.LastIndex(filename, ".")+1:])
	switch ext {
	case "jpg", "jpeg":
		return "jpeg"
	case "png":
		return "png"
	default:
		return ext
	}
}

func (m *MistralProvider) countImages(pages []PageData) int {
	count := 0
	for _, page := range pages {
		count += len(page.Images)
	}
	return count
}

func (m *MistralProvider) countTables(pages []PageData) int {
	count := 0
	for _, page := range pages {
		count += len(page.Tables)
	}
	return count
}

// ============================================================================
// Validation
// ============================================================================

func (m *MistralProvider) validateInput(input ocr.Input) *errx.Error {
	// Validate input type
	switch input.Type {
	case ocr.InputTypeReader:
		return errorRegistry.New(ErrInvalidInput).
			WithDetail("error", "io.Reader input not directly supported, use base64 encoding")
	case ocr.InputTypeURL, ocr.InputTypeDocumentURL, ocr.InputTypeImageURL:
		if input.URL == "" {
			return errorRegistry.New(ErrInvalidInput).
				WithDetail("error", "URL cannot be empty")
		}
	case ocr.InputTypeBase64:
		if len(input.Data) == 0 {
			return errorRegistry.New(ErrInvalidInput).
				WithDetail("error", "base64 data cannot be empty")
		}
		// Check size (50MB limit)
		if len(input.Data) > 50*1024*1024 {
			return errorRegistry.New(ErrDocumentTooLarge)
		}
	default:
		return errorRegistry.New(ErrInvalidInput).
			WithDetail("error", "unsupported input type").
			WithDetail("type", string(input.Type))
	}

	return nil
}

// Helper functions

func getFloat32(m map[string]interface{}, key string) float32 {
	if v, ok := m[key]; ok {
		if f, ok := v.(float64); ok {
			return float32(f)
		}
	}
	return 0
}
