// Example 06_subagent: delegate an isolated subtask and pick its model.
//
// The subagent (Task) tool runs a nested agent with its own fresh history, so
// its intermediate steps never pollute the parent conversation — only the final
// answer comes back. Via the optional "model" parameter the parent model can
// choose which model runs the subtask; because the nested agent uses the same
// router, that model is dispatched to the right provider.
//
//	OPENAI_API_KEY=... [ANTHROPIC_API_KEY=...] go run ./internal/ai/harness/examples/06_subagent
package main

import (
	"context"
	"fmt"
	"os"

	agent "github.com/Abraxas-365/manifesto/internal/ai/harness"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/exec"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/fsys/fsxstore"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm/anthropic"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm/openai"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm/router"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/subagent"
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
	if openaiKey == "" {
		return fmt.Errorf("set OPENAI_API_KEY")
	}

	r := router.New()
	r.HandlePattern("gpt-*", openai.New(openaiKey))

	models := []string{"gpt-4o", "gpt-4o-mini"}
	if k := os.Getenv("ANTHROPIC_API_KEY"); k != "" {
		r.HandlePattern("claude-*", anthropic.New(k))
		models = append(models, "claude-sonnet-4-20250514")
	}

	fs, err := fsxlocal.NewLocalFileSystem(".")
	if err != nil {
		return err
	}
	ex := exec.NewLocalExecutor(".")
	store := fsxstore.New(fs)

	registry, _ := builtins.Default(store, ex)

	// Register the Task tool. AllowedModels renders as an enum on the "model"
	// parameter and is validated at call time. NewAgent runs once per call, so
	// each subtask starts clean; it shares the router, tools, and retry.
	registry.Register(&subagent.Tool{
		AllowedModels: models,
		NewAgent: func() *agent.Agent {
			subReg, _ := builtins.Default(store, ex)
			sub := agent.New(r, subReg)
			sub.System = "You are a focused subagent. Return only the final answer."
			sub.Model = "gpt-4o-mini"
			sub.EnableRetry()
			return sub
		},
	})

	agent := agent.New(r, registry)
	agent.System = "You are an orchestrator. Delegate research to the Task tool when useful."
	agent.Model = "gpt-4o"

	out, err := agent.Run(context.Background(),
		"Use the Task tool to count the Go files in this directory, then report the number.")
	if err != nil {
		return err
	}
	fmt.Println(out)
	return nil
}
