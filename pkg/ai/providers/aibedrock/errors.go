package aibedrock

import (
	"net/http"
	"strings"

	"github.com/Abraxas-365/manifesto/pkg/errx"
)

var (
	errorRegistry = errx.NewRegistry("BEDROCK")

	ErrAPIRequest = errorRegistry.Register(
		"API_REQUEST_FAILED",
		errx.TypeExternal,
		http.StatusBadGateway,
		"Failed to make request to Bedrock API",
	)

	ErrAPIResponse = errorRegistry.Register(
		"API_RESPONSE_INVALID",
		errx.TypeExternal,
		http.StatusBadGateway,
		"Invalid response from Bedrock API",
	)

	ErrAPIUnauthorized = errorRegistry.Register(
		"API_UNAUTHORIZED",
		errx.TypeAuthorization,
		http.StatusUnauthorized,
		"Invalid or missing AWS credentials",
	)

	ErrAPIRateLimit = errorRegistry.Register(
		"API_RATE_LIMIT",
		errx.TypeExternal,
		http.StatusTooManyRequests,
		"Bedrock API rate limit exceeded",
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

	ErrStreamFailed = errorRegistry.Register(
		"STREAM_FAILED",
		errx.TypeExternal,
		http.StatusInternalServerError,
		"Streaming request failed",
	)

	ErrMissingConfig = errorRegistry.Register(
		"MISSING_CONFIG",
		errx.TypeValidation,
		http.StatusBadRequest,
		"AWS config not provided",
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

// ParseBedrockError maps an AWS Bedrock error to an errx.Error
func ParseBedrockError(err error) *errx.Error {
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
		strings.Contains(errLower, "accessdenied") ||
		strings.Contains(errLower, "access denied") ||
		strings.Contains(errLower, "credentials"):
		baseErr = ErrAPIUnauthorized
	case strings.Contains(errLower, "throttl") || strings.Contains(errLower, "rate"):
		baseErr = ErrAPIRateLimit
	case strings.Contains(errLower, "not found") || strings.Contains(errLower, "model"):
		baseErr = ErrModelNotFound
	case strings.Contains(errLower, "context") || strings.Contains(errLower, "too many tokens") ||
		strings.Contains(errLower, "input is too long"):
		baseErr = ErrContextLengthExceeded
	case strings.Contains(errLower, "validation"):
		baseErr = ErrInvalidMessage
	case strings.Contains(errLower, "stream"):
		baseErr = ErrStreamFailed
	default:
		baseErr = ErrAPIRequest
	}

	return errorRegistry.NewWithCause(baseErr, err)
}

// WrapError wraps a standard error with a Bedrock error code
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
