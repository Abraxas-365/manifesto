// Example 01_minimal: the smallest possible harness agent.
//
// Builtins (file + shell tools) over the local filesystem and shell, one
// provider, and a single Run call.
//
//	OPENAI_API_KEY=sk-... go run ./internal/ai/harness/examples/01_minimal "your question"
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	agent "github.com/Abraxas-365/manifesto/internal/ai/harness"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/exec"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/fsys/fsxstore"
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

	// Swappable environment: local disk + local shell. Swap these two lines for
	// S3 / a remote executor and nothing else changes.
	fs, err := fsxlocal.NewLocalFileSystem(".")
	if err != nil {
		return err
	}
	ex := exec.NewLocalExecutor(".")

	// builtins.Default wires Read, Write, Edit, List, Glob, Grep, Bash.
	registry, _ := builtins.Default(fsxstore.New(fs), ex)

	agent := agent.New(openai.New(key), registry)
	agent.System = "You are a helpful coding assistant. Use the tools to inspect files."
	agent.Model = "gpt-4o"

	prompt := strings.Join(os.Args[1:], " ")
	if prompt == "" {
		prompt = "List the Go files in the current directory and describe what this program does."
	}

	answer, err := agent.Run(context.Background(), prompt)
	if err != nil {
		return err
	}
	fmt.Println(answer)
	return nil
}
