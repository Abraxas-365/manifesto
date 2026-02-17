package aimistral

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/Abraxas-365/manifesto/pkg/errx"
)

const (
	DefaultBaseURL   = "https://api.mistral.ai/v1"
	DefaultTimeout   = 5 * time.Minute // OCR can take a while
	MaxRetries       = 3
	DefaultModel     = "mistral-ocr-latest"
	DefaultChatModel = "mistral-small-latest"
)

// HTTPClient handles all HTTP communication with Mistral API
type HTTPClient struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
	maxRetries int
}

// NewHTTPClient creates a new HTTP client for Mistral API
func NewHTTPClient(apiKey, baseURL string, httpClient *http.Client) *HTTPClient {
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: DefaultTimeout,
		}
	}

	if baseURL == "" {
		baseURL = DefaultBaseURL
	}

	return &HTTPClient{
		apiKey:     apiKey,
		baseURL:    baseURL,
		httpClient: httpClient,
		maxRetries: MaxRetries,
	}
}

// Post makes a POST request to the Mistral API
func (c *HTTPClient) Post(ctx context.Context, endpoint string, payload any) ([]byte, *errx.Error) {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, WrapError(err, ErrInvalidInput).
			WithDetail("error", "failed to marshal request payload")
	}

	var lastErr *errx.Error

	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff
			backoff := time.Duration(attempt) * time.Second
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return nil, WrapError(ctx.Err(), ErrAPIRequest).
					WithDetail("error", "context cancelled during retry")
			}
		}

		body, err := c.doRequest(ctx, endpoint, jsonData)
		if err == nil {
			return body, nil
		}

		lastErr = err

		// Don't retry on certain errors
		if !c.shouldRetry(err) {
			break
		}
	}

	return nil, lastErr
}

// doRequest performs the actual HTTP request
func (c *HTTPClient) doRequest(ctx context.Context, endpoint string, body []byte) ([]byte, *errx.Error) {
	url := c.baseURL + endpoint

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(body))
	if err != nil {
		return nil, WrapError(err, ErrAPIRequest).
			WithDetail("error", "failed to create HTTP request")
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("User-Agent", "manifesto-ocr/1.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, WrapError(err, ErrAPIRequest).
			WithDetail("error", "HTTP request failed").
			WithDetail("url", url)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, WrapError(err, ErrAPIResponse).
			WithDetail("error", "failed to read response body")
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, ParseAPIError(resp.StatusCode, respBody)
	}

	return respBody, nil
}

// shouldRetry determines if an error is retryable
func (c *HTTPClient) shouldRetry(err *errx.Error) bool {
	// Retry on rate limits and temporary failures
	if err.Code == ErrAPIRateLimit.Code {
		return true
	}

	// Don't retry on validation errors or auth errors
	if err.Type == errx.TypeValidation || err.Type == errx.TypeAuthorization {
		return false
	}

	// Retry on 5xx errors
	if statusCode, ok := err.Details["status_code"].(int); ok {
		return statusCode >= 500 && statusCode < 600
	}

	return false
}
