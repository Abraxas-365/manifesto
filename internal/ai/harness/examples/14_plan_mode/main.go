// Example 14_plan_mode: plan mode as a custom extension.
//
// Demonstrates how to implement plan mode (read-only exploration + plan
// approval workflow) purely through custom tools and hooks, without any
// built-in plan-mode support in the agent core.
//
// The extension registers two tools:
//   - EnterPlanMode: switches the agent into read-only exploration mode
//   - ExitPlanMode:  signals the plan is ready for user approval
//
// While plan mode is active:
//
//   - Write and Edit tools are wrapped to return errors instead of executing
//
//   - Bash is wrapped to block destructive commands (allowlist enforced)
//
//   - The agent writes its plan to a file, then calls ExitPlanMode
//
//   - The user reviews the plan and decides how to proceed
//
//     ANTHROPIC_API_KEY=... go run ./internal/agent/examples/14_plan_mode
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	agent "github.com/Abraxas-365/manifesto/internal/ai/harness"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/exec"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/fsys/fsxstore"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm/anthropic"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/tool"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/tool/builtins"
	"github.com/Abraxas-365/manifesto/internal/fsx/fsxlocal"
)

// ---------------------------------------------------------------------------
// Plan state: shared between the two tools and the hook
// ---------------------------------------------------------------------------

type PlanMode struct {
	mu       sync.Mutex
	active   bool
	planFile string
}

func (p *PlanMode) IsActive() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.active
}

func (p *PlanMode) Enter(planFile string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.active = true
	p.planFile = planFile
}

func (p *PlanMode) Exit() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.active = false
}

func (p *PlanMode) PlanFile() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.planFile
}

// ---------------------------------------------------------------------------
// EnterPlanMode tool
// ---------------------------------------------------------------------------

type EnterPlanModeTool struct {
	Plan *PlanMode
}

func (t *EnterPlanModeTool) Name() string { return "EnterPlanMode" }

func (t *EnterPlanModeTool) Description() string {
	return `Enter plan mode for read-only codebase exploration and implementation planning.

Use this before starting non-trivial implementation work to:
1. Explore the codebase with read-only tools
2. Design an implementation approach
3. Write a plan file for user approval

While in plan mode, write tools (Edit, Write) are disabled and Bash is
restricted to read-only commands. Write your plan to the plan file path
returned by this tool, then call ExitPlanMode when ready for review.`
}

func (t *EnterPlanModeTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type": "object", "properties": {}}`)
}

func (t *EnterPlanModeTool) IsReadOnly() bool { return true }

func (t *EnterPlanModeTool) Execute(_ context.Context, _ json.RawMessage) (*tool.Result, error) {
	if t.Plan.IsActive() {
		return &tool.Result{Content: "Already in plan mode.", IsError: true}, nil
	}

	home, _ := os.UserHomeDir()
	planDir := filepath.Join(home, ".agent", "plans")
	os.MkdirAll(planDir, 0700)
	planFile := filepath.Join(planDir, fmt.Sprintf("plan-%d.md", time.Now().Unix()))

	t.Plan.Enter(planFile)

	return &tool.Result{Content: fmt.Sprintf(`Plan mode activated. You are now in read-only exploration mode.

Plan file: %s

Write your implementation plan to this exact path using the Write tool (it is
the only file you may write to). When your plan is complete, call ExitPlanMode.

Workflow:
1. Explore the codebase (Read, Glob, Grep, Bash read-only commands)
2. Write your plan to the plan file above
3. Call ExitPlanMode to submit for user review`, planFile)}, nil
}

// ---------------------------------------------------------------------------
// ExitPlanMode tool
// ---------------------------------------------------------------------------

type ExitPlanModeTool struct {
	Plan *PlanMode
}

func (t *ExitPlanModeTool) Name() string { return "ExitPlanMode" }

func (t *ExitPlanModeTool) Description() string {
	return `Exit plan mode and submit your plan for user approval.

Before calling this tool, ensure you have written your complete plan to the
plan file specified when you entered plan mode. This tool reads the plan back
and presents it to the user for review.`
}

func (t *ExitPlanModeTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type": "object", "properties": {}}`)
}

func (t *ExitPlanModeTool) IsReadOnly() bool { return true }

func (t *ExitPlanModeTool) Execute(_ context.Context, _ json.RawMessage) (*tool.Result, error) {
	if !t.Plan.IsActive() {
		return &tool.Result{Content: "Not in plan mode.", IsError: true}, nil
	}

	planFile := t.Plan.PlanFile()
	raw, err := os.ReadFile(planFile)
	if err != nil || len(strings.TrimSpace(string(raw))) == 0 {
		return &tool.Result{
			Content: fmt.Sprintf("Cannot exit plan mode: no plan found at %s. "+
				"Write your plan to this exact path first.", planFile),
			IsError: true,
		}, nil
	}

	t.Plan.Exit()

	return &tool.Result{
		Content: "Plan submitted for user review. STOP and WAIT for user approval before proceeding.",
	}, nil
}

// ---------------------------------------------------------------------------
// Tool wrappers: enforce read-only restrictions during plan mode
// ---------------------------------------------------------------------------

// blockedTool wraps a tool and returns an error when plan mode is active.
type blockedTool struct {
	inner tool.Tool
	plan  *PlanMode
}

func (b *blockedTool) Name() string                 { return b.inner.Name() }
func (b *blockedTool) Description() string          { return b.inner.Description() }
func (b *blockedTool) InputSchema() json.RawMessage { return b.inner.InputSchema() }
func (b *blockedTool) IsReadOnly() bool             { return b.inner.IsReadOnly() }

func (b *blockedTool) Execute(ctx context.Context, input json.RawMessage) (*tool.Result, error) {
	if b.plan.IsActive() {
		return &tool.Result{
			Content: fmt.Sprintf("Plan mode active: %s is disabled. Use /plan to exit plan mode first.", b.inner.Name()),
			IsError: true,
		}, nil
	}
	return b.inner.Execute(ctx, input)
}

// planAwareWriteTool wraps the Write tool to allow writing ONLY the plan file
// during plan mode.
type planAwareWriteTool struct {
	inner tool.Tool
	plan  *PlanMode
}

func (w *planAwareWriteTool) Name() string                 { return w.inner.Name() }
func (w *planAwareWriteTool) Description() string          { return w.inner.Description() }
func (w *planAwareWriteTool) InputSchema() json.RawMessage { return w.inner.InputSchema() }
func (w *planAwareWriteTool) IsReadOnly() bool             { return w.inner.IsReadOnly() }

func (w *planAwareWriteTool) Execute(ctx context.Context, input json.RawMessage) (*tool.Result, error) {
	if w.plan.IsActive() {
		var in struct {
			FilePath string `json:"file_path"`
		}
		if err := json.Unmarshal(input, &in); err == nil {
			if filepath.Clean(in.FilePath) == filepath.Clean(w.plan.PlanFile()) {
				return w.inner.Execute(ctx, input)
			}
		}
		return &tool.Result{
			Content: fmt.Sprintf("Plan mode active: Write is only allowed for the plan file (%s).", w.plan.PlanFile()),
			IsError: true,
		}, nil
	}
	return w.inner.Execute(ctx, input)
}

// bashGuardTool wraps Bash to only allow safe read-only commands in plan mode.
type bashGuardTool struct {
	inner tool.Tool
	plan  *PlanMode
}

func (b *bashGuardTool) Name() string                 { return b.inner.Name() }
func (b *bashGuardTool) Description() string          { return b.inner.Description() }
func (b *bashGuardTool) InputSchema() json.RawMessage { return b.inner.InputSchema() }
func (b *bashGuardTool) IsReadOnly() bool             { return b.inner.IsReadOnly() }

func (b *bashGuardTool) Execute(ctx context.Context, input json.RawMessage) (*tool.Result, error) {
	if b.plan.IsActive() {
		var in struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(input, &in); err == nil && !isSafeCommand(in.Command) {
			return &tool.Result{
				Content: fmt.Sprintf("Plan mode: command blocked (not in allowlist). Command: %s", in.Command),
				IsError: true,
			}, nil
		}
	}
	return b.inner.Execute(ctx, input)
}

// ---------------------------------------------------------------------------
// Command safety check (allowlist approach matching pi's plan-mode extension)
// ---------------------------------------------------------------------------

var safePatterns = []*regexp.Regexp{
	regexp.MustCompile(`^\s*cat\b`),
	regexp.MustCompile(`^\s*head\b`),
	regexp.MustCompile(`^\s*tail\b`),
	regexp.MustCompile(`^\s*grep\b`),
	regexp.MustCompile(`^\s*find\b`),
	regexp.MustCompile(`^\s*ls\b`),
	regexp.MustCompile(`^\s*pwd\b`),
	regexp.MustCompile(`^\s*echo\b`),
	regexp.MustCompile(`^\s*wc\b`),
	regexp.MustCompile(`^\s*sort\b`),
	regexp.MustCompile(`^\s*diff\b`),
	regexp.MustCompile(`^\s*file\b`),
	regexp.MustCompile(`^\s*stat\b`),
	regexp.MustCompile(`^\s*du\b`),
	regexp.MustCompile(`^\s*tree\b`),
	regexp.MustCompile(`^\s*which\b`),
	regexp.MustCompile(`(?i)^\s*git\s+(status|log|diff|show|branch|remote)`),
	regexp.MustCompile(`^\s*rg\b`),
	regexp.MustCompile(`^\s*fd\b`),
	regexp.MustCompile(`^\s*jq\b`),
}

var destructivePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\brm\b`),
	regexp.MustCompile(`(?i)\bmv\b`),
	regexp.MustCompile(`(?i)\bcp\b`),
	regexp.MustCompile(`(?i)\bmkdir\b`),
	regexp.MustCompile(`(?i)\bchmod\b`),
	regexp.MustCompile(`(?i)\bsudo\b`),
	regexp.MustCompile(`(?i)\bgit\s+(add|commit|push|pull|merge|rebase|reset)`),
	regexp.MustCompile(`(^|[^<])>(?!>)`),
	regexp.MustCompile(`>>`),
}

func isSafeCommand(cmd string) bool {
	for _, p := range destructivePatterns {
		if p.MatchString(cmd) {
			return false
		}
	}
	for _, p := range safePatterns {
		if p.MatchString(cmd) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// wrapRegistryForPlanMode wraps Write, Edit, and Bash tools in the registry
// to enforce plan-mode restrictions.
// ---------------------------------------------------------------------------

func wrapRegistryForPlanMode(reg *tool.Registry, plan *PlanMode) {
	for _, t := range reg.All() {
		switch t.Name() {
		case "Write":
			reg.Register(&planAwareWriteTool{inner: t, plan: plan})
		case "Edit":
			reg.Register(&blockedTool{inner: t, plan: plan})
		case "Bash":
			reg.Register(&bashGuardTool{inner: t, plan: plan})
		}
	}
}

// ---------------------------------------------------------------------------
// main
// ---------------------------------------------------------------------------

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		return fmt.Errorf("set ANTHROPIC_API_KEY")
	}

	fs, err := fsxlocal.NewLocalFileSystem(".")
	if err != nil {
		return err
	}
	ex := exec.NewLocalExecutor(".")

	registry, _ := builtins.Default(fsxstore.New(fs), ex)

	// Set up plan mode state and register the plan-mode tools.
	plan := &PlanMode{}
	registry.Register(&EnterPlanModeTool{Plan: plan})
	registry.Register(&ExitPlanModeTool{Plan: plan})

	// Wrap Write, Edit, and Bash to enforce restrictions during plan mode.
	wrapRegistryForPlanMode(registry, plan)

	ag := agent.New(anthropic.New(key), registry)
	ag.System = `You are a helpful coding assistant. Use tools to inspect and modify files.

When asked to implement something non-trivial, use EnterPlanMode first to
explore the codebase and create a plan. Write your plan to the plan file,
then call ExitPlanMode for user approval before implementing.`
	ag.Model = "claude-sonnet-4-20250514"

	// Hook: intercept ExitPlanMode to show the plan and get user approval.
	ag.Hooks = agent.Hooks{
		OnToolEnd: func(name string, _ llm.ContentBlock) *llm.ContentBlock {
			if name == "ExitPlanMode" && !plan.IsActive() {
				planFile := plan.PlanFile()
				raw, err := os.ReadFile(planFile)
				if err != nil {
					fmt.Fprintf(os.Stderr, "\n[Plan mode] Could not read plan: %v\n", err)
					return nil
				}
				fmt.Fprintf(os.Stderr, "\n%s\n", strings.Repeat("=", 60))
				fmt.Fprintf(os.Stderr, "PLAN (from %s):\n", planFile)
				fmt.Fprintf(os.Stderr, "%s\n", strings.Repeat("-", 60))
				fmt.Fprintf(os.Stderr, "%s\n", string(raw))
				fmt.Fprintf(os.Stderr, "%s\n", strings.Repeat("=", 60))
				fmt.Fprintf(os.Stderr, "Approve? [y/n]: ")

				scanner := bufio.NewScanner(os.Stdin)
				if scanner.Scan() {
					answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
					if answer != "y" && answer != "yes" {
						fmt.Fprintf(os.Stderr, "[Plan rejected by user]\n")
					}
				}
			}
			return nil
		},
	}

	prompt := strings.Join(os.Args[1:], " ")
	if prompt == "" {
		prompt = "Explore this project and create a plan for adding a word-count feature to the main package."
	}

	answer, err := ag.Run(context.Background(), prompt)
	if err != nil {
		return err
	}
	fmt.Println(answer)
	return nil
}
