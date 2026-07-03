// Example 09_provider_options: the three layers of provider configuration.
//
// llm.Request carries a small portable core plus three extensibility layers,
// each zero-cost when unused:
//
//  1. Portable pointer knobs — Temperature/TopP are *float64: nil = omit. The
//     adapter also drops temperature for models that reject it (reasoning models).
//
//  2. Unified Reasoning knob — one provider-agnostic level; each adapter maps it
//     to its own mechanism (OpenAI reasoning_effort, Anthropic thinking budget)
//     and silently omits it when the model can't reason.
//
//  3. Per-provider raw bag — ProviderOptions[provider] merges arbitrary keys
//     straight into the outgoing request body (escape hatch for anything without
//     a first-class field).
//
//     OPENAI_API_KEY=sk-... go run ./internal/ai/harness/examples/09_provider_options "your question"
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/Abraxas-365/manifesto/internal/ai/harness"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/exec"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm/openai"
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
	registry := builtins.Default(fsxstore.New(fs), ex)

	agent := harness.New(openai.New(key), registry)
	agent.System = "You are a careful coding assistant."

	// A reasoning model: temperature would be rejected, so leaving it set is
	// harmless — the adapter drops it and maps Reasoning to reasoning_effort.
	agent.Model = "o3-mini"

	// Layer 1: portable pointer knobs. nil = omit; set = apply when supported.
	temp := 0.2
	agent.Temperature = &temp // ignored for o3-mini (reasoning model)
	topP := 0.9
	agent.TopP = &topP

	// Layer 2: unified reasoning. Maps to reasoning_effort="high" for o3-mini;
	// would map to a thinking budget on Anthropic, or be omitted on gpt-4o.
	agent.Reasoning = llm.ReasoningHigh

	// Layer 3: raw per-provider escape hatch, merged into the request body.
	agent.ProviderOptions = map[string]map[string]any{
		"openai": {"service_tier": "flex"},
	}

	prompt := strings.Join(os.Args[1:], " ")
	if prompt == "" {
		prompt = "Explain what this program configures, step by step."
	}

	answer, err := agent.Run(context.Background(), prompt)
	if err != nil {
		return err
	}
	fmt.Println(answer)
	return nil
}
