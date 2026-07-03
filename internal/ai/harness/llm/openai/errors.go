package openai

import (
	"errors"
	"strings"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm"
	"github.com/Abraxas-365/manifesto/internal/errx"
	"github.com/openai/openai-go/v3"
)

// ParseOpenAIError maps an OpenAI SDK error into the HARNESS_LLM registry.
func ParseOpenAIError(err error) *errx.Error {
	if err == nil {
		return nil
	}

	var customErr *errx.Error
	if errx.As(err, &customErr) {
		return customErr
	}

	wrapped := llm.Registry.NewWithCause(llm.ErrProviderCall, err).
		WithDetail("provider", "openai").
		WithDetail("detail", strings.TrimSpace(err.Error()))

	var apiErr *openai.Error
	if errors.As(err, &apiErr) && apiErr.StatusCode != 0 {
		wrapped.WithDetail("http_status", apiErr.StatusCode)
	}

	return wrapped
}
