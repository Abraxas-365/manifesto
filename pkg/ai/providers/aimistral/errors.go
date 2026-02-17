package aimistral

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Abraxas-365/manifesto/pkg/errx"
)

var (
	// Error registry for Mistral provider
	errorRegistry = errx.NewRegistry("MISTRAL")

	// API Errors
	ErrAPIRequest = errorRegistry.Register(
		"API_REQUEST_FAILED",
		errx.TypeExternal,
		http.StatusBadGateway,
		"Failed to make request to Mistral API",
	)

	ErrAPIResponse = errorRegistry.Register(
		"API_RESPONSE_INVALID",
		errx.TypeExternal,
		http.StatusBadGateway,
		"Invalid response from Mistral API",
	)

	ErrAPIUnauthorized = errorRegistry.Register(
		"API_UNAUTHORIZED",
		errx.TypeAuthorization,
		http.StatusUnauthorized,
		"Invalid or missing API key",
	)

	ErrAPIRateLimit = errorRegistry.Register(
		"API_RATE_LIMIT",
		errx.TypeExternal,
		http.StatusTooManyRequests,
		"Mistral API rate limit exceeded",
	)

	ErrAPIQuotaExceeded = errorRegistry.Register(
		"API_QUOTA_EXCEEDED",
		errx.TypeExternal,
		http.StatusForbidden,
		"Mistral API quota exceeded",
	)

	// Input Errors
	ErrInvalidInput = errorRegistry.Register(
		"INVALID_INPUT",
		errx.TypeValidation,
		http.StatusBadRequest,
		"Invalid input parameters",
	)

	ErrDocumentTooLarge = errorRegistry.Register(
		"DOCUMENT_TOO_LARGE",
		errx.TypeValidation,
		http.StatusBadRequest,
		"Document exceeds maximum size (50MB)",
	)

	ErrTooManyPages = errorRegistry.Register(
		"TOO_MANY_PAGES",
		errx.TypeValidation,
		http.StatusBadRequest,
		"Document exceeds maximum page count (1000)",
	)

	ErrUnsupportedFormat = errorRegistry.Register(
		"UNSUPPORTED_FORMAT",
		errx.TypeValidation,
		http.StatusBadRequest,
		"Unsupported document format",
	)

	// Processing Errors
	ErrProcessingFailed = errorRegistry.Register(
		"PROCESSING_FAILED",
		errx.TypeExternal,
		http.StatusBadGateway,
		"Document processing failed",
	)

	ErrAnnotationFailed = errorRegistry.Register(
		"ANNOTATION_FAILED",
		errx.TypeExternal,
		http.StatusBadGateway,
		"Document annotation failed",
	)

	ErrSchemaInvalid = errorRegistry.Register(
		"SCHEMA_INVALID",
		errx.TypeValidation,
		http.StatusBadRequest,
		"Invalid annotation schema",
	)

	// Configuration Errors
	ErrMissingAPIKey = errorRegistry.Register(
		"MISSING_API_KEY",
		errx.TypeValidation,
		http.StatusBadRequest,
		"Missing Mistral API key",
	)

	ErrInvalidConfig = errorRegistry.Register(
		"INVALID_CONFIG",
		errx.TypeValidation,
		http.StatusBadRequest,
		"Invalid configuration",
	)
)

// MistralAPIError represents an error from the Mistral API
type MistralAPIError struct {
	StatusCode int
	Message    string
	Type       string
	Details    map[string]any
}

// Error implements the error interface
func (e *MistralAPIError) Error() string {
	return fmt.Sprintf("Mistral API error (status %d): %s", e.StatusCode, e.Message)
}

// ParseAPIError parses an error response from the Mistral API
func ParseAPIError(statusCode int, body []byte) *errx.Error {
	apiErr := &MistralAPIError{
		StatusCode: statusCode,
		Details:    make(map[string]any),
	}

	// Try to parse JSON error response
	var errResp struct {
		Error struct {
			Message string         `json:"message"`
			Type    string         `json:"type"`
			Code    string         `json:"code"`
			Details map[string]any `json:"details"`
		} `json:"error"`
		Message string `json:"message"`
	}

	if err := json.Unmarshal(body, &errResp); err == nil {
		if errResp.Error.Message != "" {
			apiErr.Message = errResp.Error.Message
			apiErr.Type = errResp.Error.Type
			apiErr.Details = errResp.Error.Details
		} else if errResp.Message != "" {
			apiErr.Message = errResp.Message
		}
	}

	// If parsing failed, use raw body
	if apiErr.Message == "" {
		apiErr.Message = string(body)
	}

	// Map status code to appropriate error
	var baseErr *errx.ErrorCode
	switch statusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		if statusCode == http.StatusForbidden &&
			(apiErr.Type == "quota_exceeded" || apiErr.Type == "insufficient_quota") {
			baseErr = ErrAPIQuotaExceeded
		} else {
			baseErr = ErrAPIUnauthorized
		}
	case http.StatusTooManyRequests:
		baseErr = ErrAPIRateLimit
	case http.StatusBadRequest:
		baseErr = ErrInvalidInput
	case http.StatusRequestEntityTooLarge:
		baseErr = ErrDocumentTooLarge
	default:
		baseErr = ErrAPIRequest
	}

	err := errorRegistry.NewWithMessage(baseErr, apiErr.Message)
	err.WithDetail("status_code", statusCode)

	if apiErr.Type != "" {
		err.WithDetail("error_type", apiErr.Type)
	}

	for k, v := range apiErr.Details {
		err.WithDetail(k, v)
	}

	return err
}

// WrapError wraps a standard error with appropriate Mistral error code
func WrapError(err error, code *errx.ErrorCode) *errx.Error {
	if err == nil {
		return nil
	}

	// Check if it's already a custom error
	var customErr *errx.Error
	if errx.As(err, &customErr) {
		return customErr
	}

	return errorRegistry.NewWithCause(code, err)
}
