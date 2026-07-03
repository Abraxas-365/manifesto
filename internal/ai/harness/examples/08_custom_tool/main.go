// Example 08_custom_tool: implement tool.Tool and register your own capability.
//
// A tool is any type implementing the six-method tool.Tool interface. Here we
// add a "WordCount" tool the model can call. Register it alongside the builtins
// and the model will use it when appropriate.
//
//	OPENAI_API_KEY=... go run ./internal/ai/harness/examples/08_custom_tool
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/Abraxas-365/manifesto/internal/ai/harness"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/exec"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm/openai"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/tool"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/tool/builtins"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/fsys/fsxstore"
	"github.com/Abraxas-365/manifesto/internal/fsx/fsxlocal"
)

// WordCount counts the words in a string. It implements tool.Tool.
type WordCount struct{}

func (WordCount) Name() string { return "WordCount" }

func (WordCount) Description() string {
	return "Count the number of whitespace-separated words in the given text."
}

func (WordCount) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"text": {"type": "string", "description": "The text to count words in"}
		},
		"required": ["text"]
	}`)
}

// IsReadOnly: the tool has no side effects.
func (WordCount) IsReadOnly() bool { return true }

// RequiresApproval: no gating needed. Return true to route through Agent.Approver.
func (WordCount) RequiresApproval(json.RawMessage) bool { return false }

func (WordCount) Execute(_ context.Context, raw json.RawMessage) (*tool.Result, error) {
	var in struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		// Return tool-level errors as an error Result (IsError) so the model can
		// see and recover from them, rather than aborting the run.
		return &tool.Result{Content: fmt.Sprintf("invalid input: %v", err), IsError: true}, nil
	}
	n := len(strings.Fields(in.Text))
	return &tool.Result{Content: fmt.Sprintf("%d", n)}, nil
}

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
	registry.Register(WordCount{}) // <- your tool, alongside the builtins

	agent := harness.New(openai.New(key), registry)
	agent.System = "You are a helpful assistant. Use tools when they help."
	agent.Model = "gpt-4o"

	out, err := agent.Run(context.Background(),
		`Use the WordCount tool to count the words in: "the quick brown fox jumps".`)
	if err != nil {
		return err
	}
	fmt.Println(out)
	return nil
}
