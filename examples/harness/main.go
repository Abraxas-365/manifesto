// Command harness is an interactive end-to-end demo for the AI harness. It wires
// a local filesystem, a local shell executor, and BOTH the Anthropic and OpenAI
// providers behind a model router into a single agent, then runs a chat loop.
//
// Because the agent talks to a router (not one provider), the model — and thus
// the provider — can change mid-conversation. Start on GPT-4o, switch to Claude,
// switch back: the same conversation history is replayed to whichever provider
// owns the selected model.
//
// Usage:
//
//	OPENAI_API_KEY=... ANTHROPIC_API_KEY=... go run ./examples/harness
//
// Only the keys you set are wired; routes for a missing key are skipped. Set
// HARNESS_MODEL to choose the starting model (default: the first available).
//
// In the chat:
//   - /model <name>   switch the model (and provider) for the next turn
//   - /models         list selectable models
//   - /exit           quit
//
// The agent also has a subagent (Task) tool: it can delegate a subtask to a
// nested agent and choose which model runs it via the tool's "model" parameter,
// which the shared router dispatches to the right provider.
//
// Swapping the environment to S3 is a one-line change: replace the fsxlocal
// filesystem (and the executor) with an S3-backed implementation — the tools
// and agent loop are unchanged. It also demonstrates the optional "lego" pieces:
// prompt caching (Anthropic), transparent retry/backoff, a model router,
// deferred tools + ToolSearch (schemas loaded on demand), and observability
// hooks.
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Abraxas-365/manifesto/internal/ai/harness"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/exec"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/fsys/fsxstore"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm/anthropic"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm/openai"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm/router"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/skill"
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
	// Build a router over whichever providers have API keys. Each provider is
	// registered under a glob pattern for its model family, so the router can
	// dispatch by model name — cross-provider switching for free.
	r := router.New()
	var models []string

	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		r.HandlePattern("gpt-*", openai.New(key))
		r.HandlePattern("o1-*", openai.New(key))
		models = append(models, "gpt-4o", "gpt-4o-mini")
	}
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		// Prompt caching is enabled for Anthropic; the router sends Claude models here.
		ap := anthropic.NewWithOptions(key, []anthropic.Option{anthropic.WithPromptCaching()})
		r.HandlePattern("claude-*", ap)
		models = append(models, "claude-sonnet-4-20250514")
	}
	if len(models) == 0 {
		return fmt.Errorf("set OPENAI_API_KEY and/or ANTHROPIC_API_KEY")
	}

	// Starting model: HARNESS_MODEL if valid, else the first available.
	startModel := os.Getenv("HARNESS_MODEL")
	if startModel == "" {
		startModel = models[0]
	}

	workDir, err := os.Getwd()
	if err != nil {
		return err
	}

	// Swappable environment: local disk + local shell. Replace these two lines
	// with S3/remote implementations of fsx.FileSystem and exec.Executor to run
	// the same agent against a different backend.
	fs, err := fsxlocal.NewLocalFileSystem(workDir)
	if err != nil {
		return err
	}
	ex := exec.NewLocalExecutor(workDir)

	registry, _ := builtins.Default(fsxstore.New(fs), ex)

	agent := harness.New(r, registry)
	agent.System = "You are a helpful coding assistant. Use the available tools to inspect files and answer the user's question."
	agent.Model = startModel

	// Subagent (Task) tool: the model can delegate a self-contained subtask to a
	// nested agent and, via the optional "model" parameter, pick which model runs
	// it. Because the nested agent talks to the same router, any AllowedModels
	// entry is dispatched to the correct provider automatically — e.g. run the
	// subtask on Claude even while the parent is on GPT-4o. Each call gets a fresh
	// agent (clean history), sharing the router, tools, and retry.
	registry.Register(&subagent.Tool{
		AllowedModels: models,
		NewAgent: func() *harness.Agent {
			subReg, _ := builtins.Default(fsxstore.New(fs), ex)
			sub := harness.New(r, subReg)
			sub.System = "You are a focused subagent. Complete the task and return only the final answer."
			sub.Model = startModel
			sub.EnableRetry()
			return sub
		},
	})

	// Observability hooks: surface retries and per-turn token usage (including
	// prompt-cache hits) on stderr.
	agent.Hooks = harness.Hooks{
		OnRetry: func(attempt int, err error, delay time.Duration) {
			fmt.Fprintf(os.Stderr, "[retry] attempt %d after %v: %v\n", attempt, delay, err)
		},
		OnUsage: func(turn int, u, total llm.Usage) {
			fmt.Fprintf(os.Stderr, "[usage] turn %d model=%s: in=%d out=%d cacheRead=%d cacheWrite=%d\n",
				turn, agent.Model, u.InputTokens, u.OutputTokens, u.CacheReadTokens, u.CacheWriteTokens)
		},
	}

	// Skills: expose reusable, on-demand instruction sets. Loading a skill
	// materializes its files to a local dir and returns the body, so the agent can
	// Bash/Read its references. Guarded so a missing skills dir doesn't break the
	// demo. Works identically for fsx (local/S3), embedded, or in-code skills.
	skReg := skill.NewRegistry()
	if sk, err := skill.FromFS(context.Background(), fs, ".claudio/skills/manifesto"); err == nil {
		skReg.Register(sk)
	}
	skillTool := &skill.Tool{Registry: skReg}
	defer skillTool.Close()
	registry.Register(skillTool)

	// Deferred tools + ToolSearch: as the tool set grows, sending every schema
	// each turn wastes tokens. Deferred tools are advertised as name+hint only;
	// the model loads a schema on demand via the ToolSearch tool. Here we defer
	// Grep/Glob to demo the flow — they stay hidden until the model searches.
	registry.SetDeferred("Grep", "search file contents with a regular expression")
	registry.SetDeferred("Glob", "find files by glob pattern")
	agent.EnableToolSearch()

	// Transparently retry rate limits and transient server errors. This wraps the
	// router, so retries apply to every provider.
	agent.EnableRetry()

	return chat(agent, models)
}

// chat runs the interactive loop, handling slash commands and model switching.
func chat(agent *harness.Agent, models []string) error {
	fmt.Printf("model: %s  (/model <name>, /models, /exit)\n", agent.Model)
	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Print("\n> ")
		line, err := reader.ReadString('\n')
		if err != nil { // EOF (Ctrl-D)
			fmt.Println()
			return nil
		}
		input := strings.TrimSpace(line)
		if input == "" {
			continue
		}

		switch {
		case input == "/exit" || input == "/quit":
			return nil
		case input == "/models":
			fmt.Println("available models:")
			for _, m := range models {
				marker := "  "
				if m == agent.Model {
					marker = "* "
				}
				fmt.Printf("%s%s\n", marker, m)
			}
			continue
		case strings.HasPrefix(input, "/model"):
			name := strings.TrimSpace(strings.TrimPrefix(input, "/model"))
			if name == "" {
				fmt.Printf("current model: %s\n", agent.Model)
				continue
			}
			if !containsStr(models, name) {
				fmt.Printf("unknown model %q (see /models)\n", name)
				continue
			}
			agent.Model = name
			fmt.Printf("switched to %s\n", name)
			continue
		}

		answer, err := agent.Run(context.Background(), input)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			continue
		}
		fmt.Println(answer)
	}
}

func containsStr(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
