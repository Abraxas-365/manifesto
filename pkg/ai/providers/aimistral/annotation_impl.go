package aimistral

import (
	"context"
	"encoding/json"

	"github.com/Abraxas-365/manifesto/pkg/ai/ocr"
	"github.com/Abraxas-365/manifesto/pkg/errx"
)

// ============================================================================
// Annotator Implementation
// ============================================================================

// AnnotateDocument implements document-level annotation
func (m *MistralProvider) AnnotateDocument(ctx context.Context, input ocr.Input, schema ocr.AnnotationSchema, opts ...ocr.Option) (*ocr.AnnotatedDocument, *errx.Error) {
	options := ocr.ApplyOptions(opts...)

	if options.Model == "" {
		options.Model = m.defaultModel
	}

	// Validate schema
	if err := m.validateSchema(schema); err != nil {
		return nil, err
	}

	// Validate input
	if err := m.validateInput(input); err != nil {
		return nil, err
	}

	// Build request with document annotation
	req := m.buildOCRRequest(input, options)
	req.DocumentAnnotationFormat = m.convertSchemaToMistralFormat(schema)
	if schema.Prompt != "" {
		req.DocumentAnnotationPrompt = schema.Prompt
	}

	// Call API
	respBody, err := m.client.Post(ctx, "/ocr", req)
	if err != nil {
		return nil, WrapError(err, ErrAnnotationFailed)
	}

	// Parse response
	var resp OCRResponse
	if parseErr := json.Unmarshal(respBody, &resp); parseErr != nil {
		return nil, WrapError(parseErr, ErrAPIResponse).
			WithDetail("error", "failed to parse annotation response")
	}

	// Convert to annotated result
	result := m.convertToResult(&resp, options)

	return &ocr.AnnotatedDocument{
		Result:             result,
		DocumentAnnotation: m.parseAnnotation(resp.DocumentAnnotation),
		BBoxAnnotations:    make(map[string]any),
	}, nil
}

// AnnotateBBoxes implements bbox-level annotation
// Changed receiver from *MistralOCR to *MistralProvider
func (m *MistralProvider) AnnotateBBoxes(ctx context.Context, input ocr.Input, schema ocr.AnnotationSchema, opts ...ocr.Option) (*ocr.AnnotatedDocument, *errx.Error) {
	options := ocr.ApplyOptions(opts...)

	if options.Model == "" {
		options.Model = m.defaultModel
	}

	// Validate schema
	if err := m.validateSchema(schema); err != nil {
		return nil, err
	}

	// Enable images
	if !options.EnableImages {
		options.EnableImages = true
		options.IncludeImageBase64 = true
	}

	// Validate input
	if err := m.validateInput(input); err != nil {
		return nil, err
	}

	// Build request with bbox annotation
	req := m.buildOCRRequest(input, options)
	req.BBoxAnnotationFormat = m.convertSchemaToMistralFormat(schema)

	// Call API
	respBody, err := m.client.Post(ctx, "/ocr", req)
	if err != nil {
		return nil, WrapError(err, ErrAnnotationFailed)
	}

	// Parse response
	var resp OCRResponse
	if parseErr := json.Unmarshal(respBody, &resp); parseErr != nil {
		return nil, WrapError(parseErr, ErrAPIResponse).
			WithDetail("error", "failed to parse bbox annotation response")
	}

	// Convert to annotated result
	result := m.convertToResult(&resp, options)

	// Extract bbox annotations from images
	bboxAnnotations := make(map[string]any)
	for _, page := range resp.Pages {
		for _, img := range page.Images {
			if img.ImageAnnotation != "" {
				bboxAnnotations[img.ID] = m.parseAnnotation(img.ImageAnnotation)
			}
		}
	}

	return &ocr.AnnotatedDocument{
		Result:             result,
		DocumentAnnotation: nil,
		BBoxAnnotations:    bboxAnnotations,
	}, nil
}

// AnnotateBoth implements both document and bbox annotation
// Changed receiver from *MistralOCR to *MistralProvider
func (m *MistralProvider) AnnotateBoth(ctx context.Context, input ocr.Input, docSchema, bboxSchema ocr.AnnotationSchema, opts ...ocr.Option) (*ocr.AnnotatedDocument, *errx.Error) {
	options := ocr.ApplyOptions(opts...)

	if options.Model == "" {
		options.Model = m.defaultModel
	}

	// Validate schemas
	if err := m.validateSchema(docSchema); err != nil {
		return nil, err
	}
	if err := m.validateSchema(bboxSchema); err != nil {
		return nil, err
	}

	// Enable images for bbox annotations
	if !options.EnableImages {
		options.EnableImages = true
		options.IncludeImageBase64 = true
	}

	// Validate input
	if err := m.validateInput(input); err != nil {
		return nil, err
	}

	// Build request with both annotations
	req := m.buildOCRRequest(input, options)
	req.DocumentAnnotationFormat = m.convertSchemaToMistralFormat(docSchema)
	req.BBoxAnnotationFormat = m.convertSchemaToMistralFormat(bboxSchema)
	if docSchema.Prompt != "" {
		req.DocumentAnnotationPrompt = docSchema.Prompt
	}

	// Call API
	respBody, err := m.client.Post(ctx, "/ocr", req)
	if err != nil {
		return nil, WrapError(err, ErrAnnotationFailed)
	}

	// Parse response
	var resp OCRResponse
	if parseErr := json.Unmarshal(respBody, &resp); parseErr != nil {
		return nil, WrapError(parseErr, ErrAPIResponse).
			WithDetail("error", "failed to parse full annotation response")
	}

	// Convert to annotated result
	result := m.convertToResult(&resp, options)

	// Extract bbox annotations
	bboxAnnotations := make(map[string]any)
	for _, page := range resp.Pages {
		for _, img := range page.Images {
			if img.ImageAnnotation != "" {
				bboxAnnotations[img.ID] = m.parseAnnotation(img.ImageAnnotation)
			}
		}
	}

	return &ocr.AnnotatedDocument{
		Result:             result,
		DocumentAnnotation: m.parseAnnotation(resp.DocumentAnnotation),
		BBoxAnnotations:    bboxAnnotations,
	}, nil
}

// ============================================================================
// Helper Methods
// ============================================================================

func (m *MistralProvider) validateSchema(schema ocr.AnnotationSchema) *errx.Error {
	if schema.Name == "" {
		return errorRegistry.New(ErrSchemaInvalid).
			WithDetail("error", "schema name cannot be empty")
	}

	if len(schema.Schema) == 0 {
		return errorRegistry.New(ErrSchemaInvalid).
			WithDetail("error", "schema definition cannot be empty")
	}

	return nil
}

func (m *MistralProvider) convertSchemaToMistralFormat(schema ocr.AnnotationSchema) map[string]any {
	return map[string]any{
		"type": "json_schema",
		"json_schema": map[string]any{
			"name":   schema.Name,
			"schema": schema.Schema,
			"strict": schema.Strict,
		},
	}
}

func (m *MistralProvider) parseAnnotation(jsonStr string) any {
	if jsonStr == "" {
		return nil
	}

	var result any
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		// If parsing fails, return raw string
		return jsonStr
	}
	return result
}
