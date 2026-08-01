// Example 13_fromexecutor: one executor powers BOTH the file tools and Bash, so
// they can never point at different environments ("split-brain" is impossible by
// construction). This is the local, Docker-free way to see the pattern that
// example 12_docker uses for containers.
//
// The trick: builtins.FromExecutor(ex) derives the file store from `ex` itself
// (via the execstore package — Read/Write/Edit run as base64/wc/shell commands
// through the executor). Here `ex` is a LocalExecutor rooted at a scratch
// directory, so:
//
//   - Write creates a file by running a shell command in that dir.
//   - Bash `ls` / `cat` in the SAME dir sees that exact file.
//
// Compare with the classic wiring in 01_minimal, where you pass a separate
// fsxstore + executor and must keep them pointed at the same place yourself.
//
//	OPENAI_API_KEY=sk-... go run ./internal/ai/harness/examples/13_fromexecutor \
//	  "write greeting.txt containing a haiku, then cat it and count its lines with wc -l"
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	agent "github.com/Abraxas-365/manifesto/internal/ai/harness"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/exec"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm/openai"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/tool/builtins"
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

	// A scratch workspace so the agent can't touch the rest of your disk.
	workspace, err := os.MkdirTemp("", "harness-fromexec-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(workspace)
	fmt.Println("workspace:", workspace)

	// ONE backend. LocalExecutor runs shell commands in `workspace`; FromExecutor
	// derives the file tools from this same executor. There is no second
	// filesystem object to accidentally point somewhere else.
	ex := exec.NewLocalExecutor(workspace)
	registry, _ := builtins.FromExecutor(ex)

	agent := agent.New(openai.New(key), registry)
	agent.System = "You are a coding assistant. Your file tools and shell both operate " +
		"in the same workspace directory, so files you write are visible to Bash and vice versa."
	agent.Model = "gpt-4o"

	prompt := strings.Join(os.Args[1:], " ")
	if prompt == "" {
		prompt = "Write greeting.txt containing a short haiku, then use Bash to `cat greeting.txt` " +
			"and count its lines with `wc -l`."
	}

	answer, err := agent.Run(context.Background(), prompt)
	if err != nil {
		return err
	}
	fmt.Println(answer)

	// Prove it from the host side: the file the agent "wrote" through the executor
	// really exists on disk in the workspace.
	if entries, err := os.ReadDir(workspace); err == nil {
		fmt.Print("\nfiles created in workspace:")
		for _, e := range entries {
			fmt.Printf(" %s", e.Name())
		}
		fmt.Println()
	}
	return nil
}
