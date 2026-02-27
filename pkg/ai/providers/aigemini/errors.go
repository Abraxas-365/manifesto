package aigemini

import (
	"net/http"
	"strings"

	"github.com/Abraxas-365/manifesto/pkg/errx"
)

var (
	errorRegistry = errx.NewRegistry("GEMINI")

	ErrAPIRequest = errorRegistry.Register(
		"API_REQUEST_FAILED",
		errx.TypeExternal,
		http.StatusBadGateway,
		"Failed to make request to Gemini API",
	)

	ErrAPIResponse = errorRegistry.Register(
		"API_RESPONSE_INVALID",
		errx.TypeExternal,
		http.StatusBadGateway,
		"Invalid response from Gemini API",
	)

	ErrAPIUnauthorized = errorRegistry.Register(
		"API_UNAUTHORIZED",
		errx.TypeAuthorization,
		http.StatusUnauthorized,
		"Invalid or missing Gemini API key",
	)

	ErrAPIRateLimit = errorRegistry.Register(
		"API_RATE_LIMIT",
		errx.TypeExternal,
		http.StatusTooManyRequests,
		"Gemini API rate limit exceeded",
	)

	ErrAPIQuotaExceeded = errorRegistry.Register(
		"API_QUOTA_EXCEEDED",
		errx.TypeExternal,
		http.StatusForbidden,
		"Gemini API quota exceeded",
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

	ErrNoEmbeddingReturned = errorRegistry.Register(
		"NO_EMBEDDING_RETURNED",
		errx.TypeExternal,
		http.StatusInternalServerError,
		"No embedding returned in API response",
	)

	ErrStreamFailed = errorRegistry.Register(
		"STREAM_FAILED",
		errx.TypeExternal,
		http.StatusInternalServerError,
		"Streaming request failed",
	)

	ErrMissingAPIKey = errorRegistry.Register(
		"MISSING_API_KEY",
		errx.TypeValidation,
		http.StatusBadRequest,
		"Gemini API key not provided",
	)

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

// ParseGeminiError maps a Gemini SDK error to an errx.Error
func ParseGeminiError(err error) *errx.Error {
	if err == nil {
		return nil
	}

	var customErr *errx.Error
	if errx.As(err, &customErr) {
		return customErr
	}

	errLower := strings.ToLower(err.Error())

	var baseErr *errx.ErrorCode
	switch {
	case strings.Contains(errLower, "unauthorized") ||
		strings.Contains(errLower, "invalid api key") ||
		strings.Contains(errLower, "permission denied"):
		baseErr = ErrAPIUnauthorized
	case strings.Contains(errLower, "rate limit") || strings.Contains(errLower, "resource exhausted"):
		baseErr = ErrAPIRateLimit
	case strings.Contains(errLower, "quota"):
		baseErr = ErrAPIQuotaExceeded
	case strings.Contains(errLower, "not found") || strings.Contains(errLower, "model"):
		baseErr = ErrModelNotFound
	case strings.Contains(errLower, "context") || strings.Contains(errLower, "too many tokens"):
		baseErr = ErrContextLengthExceeded
	case strings.Contains(errLower, "stream"):
		baseErr = ErrStreamFailed
	default:
		baseErr = ErrAPIRequest
	}

	return errorRegistry.NewWithCause(baseErr, err)
}

// WrapError wraps a standard error with a Gemini error code
func WrapError(err error, code *errx.ErrorCode) *errx.Error {
	if err == nil {
		return nil
	}

	var customErr *errx.Error
	if errx.As(err, &customErr) {
		return customErr
	}

	return errorRegistry.NewWithCause(code, err)
}
