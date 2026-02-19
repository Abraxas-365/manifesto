package aiopenai

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Abraxas-365/manifesto/pkg/errx"
)

var (
	// Error registry for OpenAI provider
	errorRegistry = errx.NewRegistry("OPENAI")

	// API Errors
	ErrAPIRequest = errorRegistry.Register(
		"API_REQUEST_FAILED",
		errx.TypeExternal,
		http.StatusBadGateway,
		"Failed to make request to OpenAI API",
	)

	ErrAPIResponse = errorRegistry.Register(
		"API_RESPONSE_INVALID",
		errx.TypeExternal,
		http.StatusBadGateway,
		"Invalid response from OpenAI API",
	)

	ErrAPIUnauthorized = errorRegistry.Register(
		"API_UNAUTHORIZED",
		errx.TypeAuthorization,
		http.StatusUnauthorized,
		"Invalid or missing OpenAI API key",
	)

	ErrAPIRateLimit = errorRegistry.Register(
		"API_RATE_LIMIT",
		errx.TypeExternal,
		http.StatusTooManyRequests,
		"OpenAI API rate limit exceeded",
	)

	ErrAPIQuotaExceeded = errorRegistry.Register(
		"API_QUOTA_EXCEEDED",
		errx.TypeExternal,
		http.StatusForbidden,
		"OpenAI API quota exceeded",
	)

	ErrModelNotFound = errorRegistry.Register(
		"MODEL_NOT_FOUND",
		errx.TypeValidation,
		http.StatusNotFound,
		"Requested model not found or not accessible",
	)

	ErrContextLengthExceeded = errorRegistry.Register(
		"CONTEXT_LENGTH_EXCEEDED",
		errx.TypeValidation,
		http.StatusBadRequest,
		"Context length exceeds model maximum",
	)

	ErrInvalidRequest = errorRegistry.Register(
		"INVALID_REQUEST",
		errx.TypeValidation,
		http.StatusBadRequest,
		"Invalid request parameters",
	)

	// Input Validation Errors
	ErrEmptyMessages = errorRegistry.Register(
		"EMPTY_MESSAGES",
		errx.TypeValidation,
		http.StatusBadRequest,
		"Messages array cannot be empty",
	)

	ErrInvalidMessage = errorRegistry.Register(
		"INVALID_MESSAGE",
		errx.TypeValidation,
		http.StatusBadRequest,
		"Invalid message format",
	)

	ErrUnsupportedRole = errorRegistry.Register(
		"UNSUPPORTED_ROLE",
		errx.TypeValidation,
		http.StatusBadRequest,
		"Unsupported message role",
	)

	ErrEmptyEmbeddingInput = errorRegistry.Register(
		"EMPTY_EMBEDDING_INPUT",
		errx.TypeValidation,
		http.StatusBadRequest,
		"Embedding input cannot be empty",
	)

	ErrEmptySpeechInput = errorRegistry.Register(
		"EMPTY_SPEECH_INPUT",
		errx.TypeValidation,
		http.StatusBadRequest,
		"Speech input cannot be empty",
	)

	// Response Errors
	ErrNoChoicesInResponse = errorRegistry.Register(
		"NO_CHOICES_IN_RESPONSE",
		errx.TypeExternal,
		http.StatusInternalServerError,
		"No choices returned in API response",
	)

	ErrNoEmbeddingReturned = errorRegistry.Register(
		"NO_EMBEDDING_RETURNED",
		errx.TypeExternal,
		http.StatusInternalServerError,
		"No embedding returned in API response",
	)

	// Stream Errors
	ErrStreamFailed = errorRegistry.Register(
		"STREAM_FAILED",
		errx.TypeExternal,
		http.StatusInternalServerError,
		"Streaming request failed",
	)

	// Configuration Errors
	ErrMissingAPIKey = errorRegistry.Register(
		"MISSING_API_KEY",
		errx.TypeValidation,
		http.StatusBadRequest,
		"OpenAI API key not provided",
	)

	ErrInvalidConfiguration = errorRegistry.Register(
		"INVALID_CONFIGURATION",
		errx.TypeValidation,
		http.StatusBadRequest,
		"Invalid provider configuration",
	)

	// Processing Errors
	ErrJSONParsing = errorRegistry.Register(
		"JSON_PARSING_FAILED",
		errx.TypeInternal,
		http.StatusInternalServerError,
		"Failed to parse JSON",
	)

	ErrConversionFailed = errorRegistry.Register(
		"CONVERSION_FAILED",
		errx.TypeInternal,
		http.StatusInternalServerError,
		"Failed to convert data format",
	)
)

// OpenAIAPIError represents an error from the OpenAI API
type OpenAIAPIError struct {
	Message    string
	Type       string
	Param      string
	Code       string
	StatusCode int
}

// Error implements the error interface
func (e *OpenAIAPIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("OpenAI API error [%s]: %s", e.Code, e.Message)
	}
	return fmt.Sprintf("OpenAI API error: %s", e.Message)
}

// ParseOpenAIError parses an OpenAI API error
func ParseOpenAIError(err error) *errx.Error {
	if err == nil {
		return nil
	}

	// Check if it's already a custom error
	var customErr *errx.Error
	if errx.As(err, &customErr) {
		return customErr
	}

	errMsg := err.Error()
	errLower := strings.ToLower(errMsg)

	// Try to determine error type from message
	var baseErr *errx.ErrorCode

	// Authentication errors
	if strings.Contains(errLower, "unauthorized") ||
		strings.Contains(errLower, "invalid api key") ||
		strings.Contains(errLower, "incorrect api key") {
		baseErr = ErrAPIUnauthorized
	} else if strings.Contains(errLower, "rate limit") || strings.Contains(errLower, "rate_limit") {
		baseErr = ErrAPIRateLimit
	} else if strings.Contains(errLower, "quota") || strings.Contains(errLower, "insufficient_quota") {
		baseErr = ErrAPIQuotaExceeded
	} else if strings.Contains(errLower, "model") && strings.Contains(errLower, "not found") {
		baseErr = ErrModelNotFound
	} else if strings.Contains(errLower, "context length") || strings.Contains(errLower, "maximum context") {
		baseErr = ErrContextLengthExceeded
	} else if strings.Contains(errLower, "invalid") {
		baseErr = ErrInvalidRequest
	} else if strings.Contains(errLower, "stream") {
		baseErr = ErrStreamFailed
	} else {
		baseErr = ErrAPIRequest
	}

	return errorRegistry.NewWithCause(baseErr, err)
}

// WrapError wraps a standard error with appropriate OpenAI error code
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

// ParseAPIErrorResponse parses error from API response body
func ParseAPIErrorResponse(statusCode int, body []byte) *errx.Error {
	apiErr := &OpenAIAPIError{
		StatusCode: statusCode,
	}

	// Try to parse JSON error response
	var errResp struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Param   string `json:"param"`
			Code    string `json:"code"`
		} `json:"error"`
	}

	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error.Message != "" {
		apiErr.Message = errResp.Error.Message
		apiErr.Type = errResp.Error.Type
		apiErr.Param = errResp.Error.Param
		apiErr.Code = errResp.Error.Code
	} else {
		// Fallback to raw body
		apiErr.Message = string(body)
	}

	// Map to appropriate error code
	var baseErr *errx.ErrorCode
	switch statusCode {
	case http.StatusUnauthorized:
		baseErr = ErrAPIUnauthorized
	case http.StatusForbidden:
		if strings.Contains(strings.ToLower(apiErr.Type), "quota") {
			baseErr = ErrAPIQuotaExceeded
		} else {
			baseErr = ErrAPIUnauthorized
		}
	case http.StatusTooManyRequests:
		baseErr = ErrAPIRateLimit
	case http.StatusNotFound:
		if strings.Contains(strings.ToLower(apiErr.Message), "model") {
			baseErr = ErrModelNotFound
		} else {
			baseErr = ErrAPIRequest
		}
	case http.StatusBadRequest:
		if strings.Contains(strings.ToLower(apiErr.Message), "context") {
			baseErr = ErrContextLengthExceeded
		} else {
			baseErr = ErrInvalidRequest
		}
	default:
		if statusCode >= 500 {
			baseErr = ErrAPIResponse
		} else {
			baseErr = ErrAPIRequest
		}
	}

	customErr := errorRegistry.NewWithMessage(baseErr, apiErr.Message)
	customErr.WithDetail("status_code", statusCode)

	if apiErr.Type != "" {
		customErr.WithDetail("error_type", apiErr.Type)
	}
	if apiErr.Code != "" {
		customErr.WithDetail("error_code", apiErr.Code)
	}
	if apiErr.Param != "" {
		customErr.WithDetail("param", apiErr.Param)
	}

	return customErr
}
