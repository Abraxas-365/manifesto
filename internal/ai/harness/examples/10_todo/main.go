// Example 10_todo: give the agent a declarative task list.
//
// todo.Tool lets the model plan multi-step work: it sends the COMPLETE list on
// every call and it replaces the previous one. Storage is swappable — the zero
// value keeps the list in memory (no config), but you can pass your own Store
// to persist it or share it with a UI. OnChange mirrors updates as they happen.
//
//	OPENAI_API_KEY=sk-... go run ./internal/ai/harness/examples/10_todo "refactor the config loader"
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/Abraxas-365/manifesto/internal/ai/harness"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/exec"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm/openai"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/todo"
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
	registry := builtins.Default(fs, ex)

	// In-memory by default. OnChange lets us watch the plan evolve; swap Store
	// for a persistent implementation to survive restarts or drive a UI.
	registry.Register(&todo.Tool{
		OnChange: func(items []todo.Item) {
			fmt.Fprintf(os.Stderr, "\n--- plan (%d) ---\n", len(items))
			for _, it := range items {
				fmt.Fprintf(os.Stderr, "  [%s] %s\n", it.Status, it.Content)
			}
		},
	})

	agent := harness.New(openai.New(key), registry)
	agent.System = "You are a coding assistant. For any multi-step task, first use TodoWrite " +
		"to plan the steps, then work through them, keeping the list updated."
	agent.Model = "gpt-4o"

	prompt := strings.Join(os.Args[1:], " ")
	if prompt == "" {
		prompt = "Plan and outline the steps to add a health-check endpoint to a Go web service."
	}

	answer, err := agent.Run(context.Background(), prompt)
	if err != nil {
		return err
	}
	fmt.Println("\n" + answer)
	return nil
}
