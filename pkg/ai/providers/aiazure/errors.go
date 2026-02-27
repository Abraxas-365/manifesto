package aiazure

import (
	"net/http"
	"strings"

	"github.com/Abraxas-365/manifesto/pkg/errx"
)

var (
	errorRegistry = errx.NewRegistry("AZURE_OPENAI")

	ErrAPIRequest = errorRegistry.Register(
		"API_REQUEST_FAILED",
		errx.TypeExternal,
		http.StatusBadGateway,
		"Failed to make request to Azure OpenAI API",
	)

	ErrAPIResponse = errorRegistry.Register(
		"API_RESPONSE_INVALID",
		errx.TypeExternal,
		http.StatusBadGateway,
		"Invalid response from Azure OpenAI API",
	)

	ErrAPIUnauthorized = errorRegistry.Register(
		"API_UNAUTHORIZED",
		errx.TypeAuthorization,
		http.StatusUnauthorized,
		"Invalid or missing Azure OpenAI credentials",
	)

	ErrAPIRateLimit = errorRegistry.Register(
		"API_RATE_LIMIT",
		errx.TypeExternal,
		http.StatusTooManyRequests,
		"Azure OpenAI API rate limit exceeded",
	)

	ErrAPIQuotaExceeded = errorRegistry.Register(
		"API_QUOTA_EXCEEDED",
		errx.TypeExternal,
		http.StatusForbidden,
		"Azure OpenAI API quota exceeded",
	)

	ErrModelNotFound = errorRegistry.Register(
		"MODEL_NOT_FOUND",
		errx.TypeValidation,
		http.StatusNotFound,
		"Requested deployment not found or not accessible",
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

	ErrStreamFailed = errorRegistry.Register(
		"STREAM_FAILED",
		errx.TypeExternal,
		http.StatusInternalServerError,
		"Streaming request failed",
	)

	ErrMissingEndpoint = errorRegistry.Register(
		"MISSING_ENDPOINT",
		errx.TypeValidation,
		http.StatusBadRequest,
		"Azure OpenAI endpoint not provided",
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

// ParseAzureError maps an Azure OpenAI SDK error to an errx.Error
func ParseAzureError(err error) *errx.Error {
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
		strings.Contains(errLower, "access denied"):
		baseErr = ErrAPIUnauthorized
	case strings.Contains(errLower, "rate limit") || strings.Contains(errLower, "rate_limit"):
		baseErr = ErrAPIRateLimit
	case strings.Contains(errLower, "quota") || strings.Contains(errLower, "insufficient_quota"):
		baseErr = ErrAPIQuotaExceeded
	case strings.Contains(errLower, "not found") || strings.Contains(errLower, "deployment"):
		baseErr = ErrModelNotFound
	case strings.Contains(errLower, "context length") || strings.Contains(errLower, "maximum context"):
		baseErr = ErrContextLengthExceeded
	case strings.Contains(errLower, "stream"):
		baseErr = ErrStreamFailed
	default:
		baseErr = ErrAPIRequest
	}

	return errorRegistry.NewWithCause(baseErr, err)
}

// WrapError wraps a standard error with an Azure OpenAI error code
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
