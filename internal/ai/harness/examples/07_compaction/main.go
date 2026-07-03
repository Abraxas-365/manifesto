// Example 07_compaction: keep long conversations within the context window.
//
// When estimated tokens exceed CompactThreshold of the context window, the agent
// compacts history before the next turn. TruncateCompactor drops the oldest
// messages (never orphaning a tool_result); SummarizeCompactor replaces them
// with an LLM-generated summary. Compaction is opt-in — a nil Compactor disables
// it entirely.
//
//	OPENAI_API_KEY=... go run ./internal/ai/harness/examples/07_compaction
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/Abraxas-365/manifesto/internal/ai/harness"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/exec"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm/openai"
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
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		return fmt.Errorf("set OPENAI_API_KEY")
	}

	fs, err := fsxlocal.NewLocalFileSystem(".")
	if err != nil {
		return err
	}
	ex := exec.NewLocalExecutor(".")

	provider := openai.New(key)
	agent := harness.New(provider, builtins.Default(fs, ex))
	agent.System = "You are a helpful assistant."
	agent.Model = "gpt-4o"

	// Option A — cheap and lossy: keep only the most recent 12 messages.
	agent.Compactor = harness.TruncateCompactor{KeepRecent: 12}

	// Option B — lossy but coherent: summarize older messages with a model.
	// agent.Compactor = harness.SummarizeCompactor{
	// 	Provider:   provider, // a RAW provider (no tools), not the agent
	// 	Model:      "gpt-4o-mini",
	// 	MaxTokens:  512,
	// 	KeepRecent: 12,
	// }

	// Trigger compaction earlier than the default (0.8) to see it fire, and get
	// notified when it does.
	agent.CompactThreshold = 0.5
	agent.Hooks.OnCompaction = func(before, after int) {
		fmt.Fprintf(os.Stderr, "[compaction] ~%d -> ~%d tokens\n", before, after)
	}

	ctx := context.Background()
	for i, q := range []string{
		"Give me a one-paragraph history of the Go programming language.",
		"Now list five notable features it introduced.",
		"Summarize everything you've told me so far in two sentences.",
	} {
		out, err := agent.Run(ctx, q)
		if err != nil {
			return err
		}
		fmt.Printf("--- answer %d ---\n%s\n", i+1, out)
	}
	return nil
}
