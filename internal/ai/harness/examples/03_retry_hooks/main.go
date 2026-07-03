// Example 03_retry_hooks: transparent retry + observability hooks.
//
// EnableRetry wraps the provider so 429s and 5xx are retried with backoff. Hooks
// give you a window into every turn, tool call, retry, and token usage without
// changing the loop.
//
//	OPENAI_API_KEY=... go run ./internal/ai/harness/examples/03_retry_hooks
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/Abraxas-365/manifesto/internal/ai/harness"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/exec"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm/openai"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm/retry"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/tool/builtins"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/fsys/fsxstore"
	"github.com/Abraxas-365/manifesto/internal/fsx/fsxlocal"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		return fmt.Errorf("set OPENAI_API_KEY")
	}

	fs, err := fsxlocal.NewLocalFileSystem(".")
	if err != nil {
		return err
	}
	ex := exec.NewLocalExecutor(".")

	agent := harness.New(openai.New(key), builtins.Default(fsxstore.New(fs), ex))
	agent.System = "You are a helpful coding assistant."
	agent.Model = "gpt-4o"

	// Observability: every callback is optional (nil = ignored).
	agent.Hooks = harness.Hooks{
		OnTurnStart: func(turn int) {
			fmt.Fprintf(os.Stderr, "[turn %d]\n", turn)
		},
		OnToolStart: func(name string, input json.RawMessage) {
			fmt.Fprintf(os.Stderr, "  -> %s %s\n", name, input)
		},
		OnToolEnd: func(name string, _ llm.ContentBlock) {
			fmt.Fprintf(os.Stderr, "  <- %s done\n", name)
		},
		OnRetry: func(attempt int, err error, delay time.Duration) {
			fmt.Fprintf(os.Stderr, "  [retry %d in %v: %v]\n", attempt, delay, err)
		},
		OnUsage: func(turn int, u, total llm.Usage) {
			fmt.Fprintf(os.Stderr, "  [usage turn=%d in=%d out=%d total_out=%d]\n",
				turn, u.InputTokens, u.OutputTokens, total.OutputTokens)
		},
	}

	// Retry with custom bounds. The hook above receives each retry via
	// Hooks.OnRetry (EnableRetry bridges the two).
	agent.EnableRetry(
		retry.WithMaxAttempts(4),
		retry.WithBaseDelay(500*time.Millisecond),
	)

	out, err := agent.Run(context.Background(), "How many Go files are in this directory?")
	if err != nil {
		return err
	}
	fmt.Println("\n" + out)
	fmt.Fprintf(os.Stderr, "\ntotal usage: %+v\n", agent.TotalUsage())
	return nil
}
