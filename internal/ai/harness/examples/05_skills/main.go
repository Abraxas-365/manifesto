// Example 05_skills: on-demand instruction sets the agent can load.
//
// A skill is a SKILL.md (short body + frontmatter) plus reference files. Loading
// it materializes those files to a real local dir and returns the body, so the
// agent can Read/Bash the references on demand. Skills can come from fsx
// (local/S3), a Go embed.FS, or — as here — pure in-code structs.
//
//	OPENAI_API_KEY=... go run ./internal/ai/harness/examples/05_skills
package main

import (
	"context"
	"fmt"
	"os"

	agent "github.com/Abraxas-365/manifesto/internal/ai/harness"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/exec"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/fsys/fsxstore"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm/openai"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/skill"
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

	ctx := context.Background()

	// An in-code skill. References are written to disk on load, so the agent can
	// Bash/Read ${SKILL_DIR}/references/style.md just like a file-backed skill.
	static := &skill.Static{
		Name:        "go-style",
		Description: "House Go style rules. Use when writing or reviewing Go code.",
		Body: "# Go Style\n\n" +
			"Follow the rules in ${SKILL_DIR}/references/style.md before writing code.",
		References: map[string][]byte{
			"references/style.md": []byte(
				"- Wrap errors with %w.\n- Table-driven tests.\n- No naked returns.\n"),
		},
	}
	sk, err := skill.FromStatic(ctx, static)
	if err != nil {
		return err
	}

	skReg := skill.NewRegistry()
	skReg.Register(sk)

	// You could also load from disk or S3:
	//   sk, _ := skill.FromFS(ctx, fs, ".agent/skills/manifesto")
	// or from an embed.FS:
	//   sk, _ := skill.FromEmbed(ctx, embedded, "skills/go-style")

	fs, err := fsxlocal.NewLocalFileSystem(".")
	if err != nil {
		return err
	}
	ex := exec.NewLocalExecutor(".")

	registry, _ := builtins.Default(fsxstore.New(fs), ex)
	skillTool := &skill.Tool{Registry: skReg}
	defer skillTool.Close() // removes the ephemeral materialization dir
	registry.Register(skillTool)

	agent := agent.New(openai.New(key), registry)
	agent.System = "You are a Go expert. When a task needs house conventions, load the go-style skill."
	agent.Model = "gpt-4o"

	out, err := agent.Run(ctx, "Load the go-style skill and summarize its error-handling rule.")
	if err != nil {
		return err
	}
	fmt.Println(out)
	return nil
}
