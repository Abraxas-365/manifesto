// Example 02_router: one Provider that routes by model name.
//
// The router implements llm.Provider, so the agent holds a single provider yet
// can talk to OpenAI or Anthropic depending on agent.Model. Switching model mid
// conversation switches provider automatically.
//
//	OPENAI_API_KEY=... ANTHROPIC_API_KEY=... go run ./internal/ai/harness/examples/02_router
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/Abraxas-365/manifesto/internal/ai/harness"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/exec"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm/anthropic"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm/openai"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm/router"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/tool/builtins"
	"github.com/Abraxas-365/manifesto/internal/fsx/fsxlocal"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	openaiKey := os.Getenv("OPENAI_API_KEY")
	anthropicKey := os.Getenv("ANTHROPIC_API_KEY")

	// Register glob patterns -> providers. There is no hardcoded model->provider
	// table: you declare the routes you want.
	r := router.New()
	if openaiKey != "" {
		r.HandlePattern("gpt-*", openai.New(openaiKey))
		r.HandlePattern("o1-*", openai.New(openaiKey))
	}
	if anthropicKey != "" {
		// Prompt caching is a provider-level option; see example 03/README.
		r.HandlePattern("claude-*", anthropic.NewWithOptions(anthropicKey, []anthropic.Option{
			anthropic.WithPromptCaching(),
		}))
	}

	fs, err := fsxlocal.NewLocalFileSystem(".")
	if err != nil {
		return err
	}
	ex := exec.NewLocalExecutor(".")

	agent := harness.New(r, builtins.Default(fs, ex))
	agent.System = "You are a helpful assistant."

	ctx := context.Background()

	// First turn on OpenAI...
	if openaiKey != "" {
		agent.Model = "gpt-4o"
		out, err := agent.Run(ctx, "In one sentence, what model are you?")
		if err != nil {
			return err
		}
		fmt.Println("gpt-4o:", out)
	}

	// ...then switch to Anthropic. History carries over; only the provider changes.
	if anthropicKey != "" {
		agent.Model = "claude-sonnet-4-20250514"
		out, err := agent.Run(ctx, "And now, in one sentence, what model are you?")
		if err != nil {
			return err
		}
		fmt.Println("claude:", out)
	}
	return nil
}
