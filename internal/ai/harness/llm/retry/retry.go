// Package retry provides a decorator llm.Provider that transparently retries
// retryable provider calls (rate limits, transient server errors) with
// exponential backoff and jitter. It is opt-in: wrap any provider with Wrap.
package retry

import (
	"context"
	"errors"
	"math/rand"
	"time"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm"
	"github.com/Abraxas-365/manifesto/internal/errx"
)

// Default retry configuration.
const (
	DefaultMaxAttempts = 4
	DefaultBaseDelay   = 500 * time.Millisecond
	DefaultMaxDelay    = 30 * time.Second
)

// Provider wraps another llm.Provider and retries retryable errors.
type Provider struct {
	// Next is the wrapped provider.
	Next llm.Provider
	// MaxAttempts is the total number of attempts (including the first).
	MaxAttempts int
	// BaseDelay is the initial backoff delay, doubled each attempt.
	BaseDelay time.Duration
	// MaxDelay caps the backoff delay.
	MaxDelay time.Duration
	// OnRetry, if set, is called before each retry sleep.
	OnRetry func(attempt int, err error, delay time.Duration)
}

// Option configures a Provider.
type Option func(*Provider)

// WithMaxAttempts sets the total number of attempts.
func WithMaxAttempts(n int) Option { return func(p *Provider) { p.MaxAttempts = n } }

// WithBaseDelay sets the initial backoff delay.
func WithBaseDelay(d time.Duration) Option { return func(p *Provider) { p.BaseDelay = d } }

// WithMaxDelay caps the backoff delay.
func WithMaxDelay(d time.Duration) Option { return func(p *Provider) { p.MaxDelay = d } }

// WithOnRetry sets the retry callback.
func WithOnRetry(fn func(attempt int, err error, delay time.Duration)) Option {
	return func(p *Provider) { p.OnRetry = fn }
}

// Wrap returns a retrying provider around next, applying the given options.
func Wrap(next llm.Provider, opts ...Option) *Provider {
	p := &Provider{
		Next:        next,
		MaxAttempts: DefaultMaxAttempts,
		BaseDelay:   DefaultBaseDelay,
		MaxDelay:    DefaultMaxDelay,
	}
	for _, opt := range opts {
		opt(p)
	}
	if p.MaxAttempts < 1 {
		p.MaxAttempts = 1
	}
	return p
}

// Chat implements llm.Provider with retries.
func (p *Provider) Chat(ctx context.Context, req llm.Request) (*llm.Response, error) {
	var lastErr error
	for attempt := 0; attempt < p.MaxAttempts; attempt++ {
		resp, err := p.Next.Chat(ctx, req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if !p.sleepBeforeRetry(ctx, attempt, err) {
			return nil, err
		}
	}
	return nil, lastErr
}

// ChatStream implements llm.Provider with retries on the initial stream open.
func (p *Provider) ChatStream(ctx context.Context, req llm.Request) (llm.Stream, error) {
	var lastErr error
	for attempt := 0; attempt < p.MaxAttempts; attempt++ {
		stream, err := p.Next.ChatStream(ctx, req)
		if err == nil {
			return stream, nil
		}
		lastErr = err
		if !p.sleepBeforeRetry(ctx, attempt, err) {
			return nil, err
		}
	}
	return nil, lastErr
}

// sleepBeforeRetry decides whether to retry after a failed attempt and, if so,
// waits for the backoff delay. It returns false when the caller should give up
// (non-retryable error, attempts exhausted, or context cancelled).
func (p *Provider) sleepBeforeRetry(ctx context.Context, attempt int, err error) bool {
	if attempt >= p.MaxAttempts-1 || !retryable(err) {
		return false
	}

	delay := backoff(p.BaseDelay, p.MaxDelay, attempt)
	if p.OnRetry != nil {
		p.OnRetry(attempt+1, err, delay)
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// backoff returns an exponentially increasing delay with jitter, capped at max.
func backoff(base, max time.Duration, attempt int) time.Duration {
	d := base << attempt
	if d <= 0 || d > max {
		d = max
	}
	jitter := time.Duration(rand.Int63n(int64(d)/2 + 1))
	return d/2 + jitter
}

// retryable reports whether err is worth retrying. It inspects the http_status
// detail attached by the provider error parsers: 429 (rate limit), 529
// (overloaded), and any 5xx are retryable. Context cancellation is not.
func retryable(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	var e *errx.Error
	if !errors.As(err, &e) {
		return false
	}
	status, ok := e.Details["http_status"].(int)
	if !ok {
		return false
	}
	return status == 429 || status >= 500
}
