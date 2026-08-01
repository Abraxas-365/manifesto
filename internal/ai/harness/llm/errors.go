package llm

import (
	"net/http"
	"strings"

	"github.com/Abraxas-365/manifesto/internal/errx"
)

// Registry holds the error codes for the harness LLM layer.
var Registry = errx.NewRegistry("HARNESS_LLM")

var (
	ErrMissingAPIKey = Registry.Register(
		"MISSING_API_KEY",
		errx.TypeValidation,
		http.StatusBadRequest,
		"LLM API key not provided",
	)

	ErrEmptyMessages = Registry.Register(
		"EMPTY_MESSAGES",
		errx.TypeValidation,
		http.StatusBadRequest,
		"Messages array cannot be empty",
	)

	ErrProviderCall = Registry.Register(
		"PROVIDER_CALL_FAILED",
		errx.TypeExternal,
		http.StatusBadGateway,
		"LLM provider request failed",
	)

	ErrUnsupportedRole = Registry.Register(
		"UNSUPPORTED_ROLE",
		errx.TypeValidation,
		http.StatusBadRequest,
		"Unsupported message role",
	)
)

// IsContextOverflow reports whether err is a provider "request exceeds the
// model's context window" rejection. Anthropic reports overflow as a stop
// reason (StopContextWindowExceeded) on a successful response, but OpenAI
// (and OpenAI-compatible gateways) reject the request with an
// invalid_request_error — code "context_length_exceeded" — which surfaces
// here as an opaque provider error. The agent loop uses this to trigger the
// same compact-and-retry recovery for both providers.
func IsContextOverflow(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "context_length_exceeded") ||
		strings.Contains(msg, "exceeds the context window") ||
		strings.Contains(msg, "maximum context length") ||
		strings.Contains(msg, "prompt is too long") ||
		strings.Contains(msg, "input length and `max_tokens` exceed context limit")
}
