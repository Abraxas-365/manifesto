// === ./pkg/ai/providers/mistral/options.go ===
package aimistral

import (
	"net/http"
	"time"
)

// ProviderOption configures the Mistral provider
type ProviderOption func(*MistralProvider)

// WithBaseURL sets a custom base URL
func WithBaseURL(url string) ProviderOption {
	return func(p *MistralProvider) {
		p.baseURL = url
	}
}

// WithHTTPClient sets a custom HTTP client
func WithHTTPClient(client *http.Client) ProviderOption {
	return func(p *MistralProvider) {
		p.httpClient = client
	}
}

// WithTimeout sets the request timeout
func WithTimeout(timeout time.Duration) ProviderOption {
	return func(p *MistralProvider) {
		if p.httpClient == nil {
			p.httpClient = &http.Client{}
		}
		p.httpClient.Timeout = timeout
	}
}

// WithMaxRetries sets the maximum number of retries
func WithMaxRetries(maxRetries int) ProviderOption {
	return func(p *MistralProvider) {
		p.maxRetries = maxRetries
	}
}

// WithDefaultModel sets the default OCR model
func WithDefaultModel(model string) ProviderOption {
	return func(p *MistralProvider) {
		p.defaultModel = model
	}
}

// WithDefaultChatModel sets the default chat model for QnA
func WithDefaultChatModel(model string) ProviderOption {
	return func(p *MistralProvider) {
		p.defaultChatModel = model
	}
}
