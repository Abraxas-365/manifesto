package retry

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm"
	"github.com/Abraxas-365/manifesto/internal/errx"
)

var testRegistry = errx.NewRegistry("TEST")
var errCode = testRegistry.Register("BOOM", errx.TypeExternal, 502, "boom")

func statusErr(status int) error {
	return testRegistry.New(errCode).WithDetail("http_status", status)
}

// scriptProvider fails a set number of times then succeeds.
type scriptProvider struct {
	failures int
	err      error
	calls    int
}

func (p *scriptProvider) Chat(context.Context, llm.Request) (*llm.Response, error) {
	p.calls++
	if p.calls <= p.failures {
		return nil, p.err
	}
	return &llm.Response{Message: llm.Message{Role: llm.RoleAssistant}}, nil
}

func (p *scriptProvider) ChatStream(context.Context, llm.Request) (llm.Stream, error) {
	p.calls++
	if p.calls <= p.failures {
		return nil, p.err
	}
	return nil, nil
}

func fastOpts(onRetry func(int, error, time.Duration)) []Option {
	return []Option{
		WithBaseDelay(time.Millisecond),
		WithMaxDelay(2 * time.Millisecond),
		WithOnRetry(onRetry),
	}
}

func TestChat_RetriesThenSucceeds(t *testing.T) {
	p := &scriptProvider{failures: 2, err: statusErr(429)}
	var retries int
	rp := Wrap(p, fastOpts(func(int, error, time.Duration) { retries++ })...)

	if _, err := rp.Chat(context.Background(), llm.Request{}); err != nil {
		t.Fatalf("expected success after retries, got %v", err)
	}
	if p.calls != 3 {
		t.Fatalf("expected 3 calls, got %d", p.calls)
	}
	if retries != 2 {
		t.Fatalf("expected 2 retries, got %d", retries)
	}
}

func TestChat_NonRetryableGivesUp(t *testing.T) {
	p := &scriptProvider{failures: 5, err: statusErr(400)}
	rp := Wrap(p, fastOpts(nil)...)

	if _, err := rp.Chat(context.Background(), llm.Request{}); err == nil {
		t.Fatal("expected error for non-retryable status")
	}
	if p.calls != 1 {
		t.Fatalf("expected 1 call for non-retryable error, got %d", p.calls)
	}
}

func TestChat_ExhaustsMaxAttempts(t *testing.T) {
	p := &scriptProvider{failures: 10, err: statusErr(503)}
	rp := Wrap(p, append(fastOpts(nil), WithMaxAttempts(3))...)

	if _, err := rp.Chat(context.Background(), llm.Request{}); err == nil {
		t.Fatal("expected error after exhausting attempts")
	}
	if p.calls != 3 {
		t.Fatalf("expected 3 attempts, got %d", p.calls)
	}
}

func TestChat_StopsOnContextCancel(t *testing.T) {
	p := &scriptProvider{failures: 10, err: statusErr(503)}
	rp := Wrap(p, WithBaseDelay(time.Hour), WithMaxDelay(time.Hour))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := rp.Chat(ctx, llm.Request{}); err == nil {
		t.Fatal("expected error when context cancelled")
	}
	if p.calls != 1 {
		t.Fatalf("expected 1 call before cancel abort, got %d", p.calls)
	}
}

func TestRetryable_ContextErrorsNotRetried(t *testing.T) {
	if retryable(context.Canceled) {
		t.Fatal("context.Canceled must not be retryable")
	}
	if retryable(errors.New("plain error")) {
		t.Fatal("non-errx error must not be retryable")
	}
}
