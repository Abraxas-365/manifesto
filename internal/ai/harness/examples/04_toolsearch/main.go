// Example 04_toolsearch: defer rarely-used tools to save tokens.
//
// As the tool set grows, sending every schema each turn is wasteful. A deferred
// tool is advertised to the model as name + hint only (in a system-reminder);
// the model loads its full schema on demand via the ToolSearch tool, after which
// it stays visible for the rest of the run.
//
//	OPENAI_API_KEY=... go run ./internal/ai/harness/examples/04_toolsearch
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

	registry := builtins.Default(fs, ex)

	// Defer two builtins. The hint (2nd arg) is a short teaser used both in the
	// reminder and for ToolSearch keyword matching — keep it to a few words.
	registry.SetDeferred("Grep", "search file contents with a regular expression")
	registry.SetDeferred("Glob", "find files by glob pattern")

	agent := harness.New(openai.New(key), registry)
	agent.System = "You are a helpful coding assistant."
	agent.Model = "gpt-4o"

	// Registers the ToolSearch tool and makes Run hide deferred tools until the
	// model asks for them. Panics if SetDeferred named an unregistered tool.
	agent.EnableToolSearch()

	// This prompt should make the model first call ToolSearch (to load Grep),
	// then call Grep.
	out, err := agent.Run(context.Background(), "Find every Go file that mentions the word TODO.")
	if err != nil {
		return err
	}
	fmt.Println(out)
	return nil
}
