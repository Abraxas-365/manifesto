package llm

import (
	"net/http"

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
