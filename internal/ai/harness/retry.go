package harness

import (
	"time"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm/retry"
)

// EnableRetry wraps the agent's Provider in a retrying provider, bridging the
// retry callback to Hooks.OnRetry so retries surface through the same hook
// surface as the rest of the loop. The Hooks.OnRetry field is read at call time,
// so it may be set before or after EnableRetry. Returns the agent for chaining.
func (a *Agent) EnableRetry(opts ...retry.Option) *Agent {
	bridge := retry.WithOnRetry(func(attempt int, err error, delay time.Duration) {
		if a.Hooks.OnRetry != nil {
			a.Hooks.OnRetry(attempt, err, delay)
		}
	})
	a.Provider = retry.Wrap(a.Provider, append([]retry.Option{bridge}, opts...)...)
	return a
}
