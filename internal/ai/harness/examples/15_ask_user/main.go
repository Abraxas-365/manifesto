// Example 15_ask_user: interactive question tool as a custom extension.
//
// Demonstrates how to implement an AskUser tool that lets the agent ask the
// user structured questions with multiple-choice options during execution.
// This is implemented purely through a custom tool — no built-in AskUser
// support is required in the agent core.
//
// The tool:
//
//   - Presents questions with numbered options in the terminal
//
//   - Supports single-select and multi-select questions
//
//   - Allows free-text "Other" answers
//
//   - Falls back to inline text when stdin is not a terminal
//
//     ANTHROPIC_API_KEY=... go run ./internal/agent/examples/15_ask_user
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	agent "github.com/Abraxas-365/manifesto/internal/ai/harness"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/exec"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/fsys/fsxstore"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm/anthropic"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/tool"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/tool/builtins"
	"github.com/Abraxas-365/manifesto/internal/fsx/fsxlocal"
)

// ---------------------------------------------------------------------------
// AskUser tool
// ---------------------------------------------------------------------------

// AskQuestion defines a single question with options.
type AskQuestion struct {
	Label       string   `json:"label"`
	Description string   `json:"description,omitempty"`
	Options     []string `json:"options"`
	MultiSelect bool     `json:"multi_select,omitempty"`
}

// AskUserTool allows the AI to ask the user structured questions with options.
type AskUserTool struct{}

func (AskUserTool) Name() string { return "AskUser" }

func (AskUserTool) Description() string {
	return `Ask the user structured questions during execution. Use this when you need to:
1. Gather user preferences or requirements
2. Clarify ambiguous instructions
3. Get decisions on implementation choices

Usage notes:
- Provide 2-6 clear options per question
- If you recommend a specific option, make it the first and add "(Recommended)"
- Use multi_select when multiple answers make sense`
}

func (AskUserTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"questions": {
				"type": "array",
				"description": "1-4 questions to ask the user",
				"items": {
					"type": "object",
					"properties": {
						"label": {"type": "string", "description": "Short question text"},
						"description": {"type": "string", "description": "Additional context"},
						"options": {"type": "array", "items": {"type": "string"}, "description": "2-6 answer options"},
						"multi_select": {"type": "boolean", "description": "Allow multiple selections"}
					},
					"required": ["label", "options"]
				},
				"minItems": 1,
				"maxItems": 4
			}
		},
		"required": ["questions"]
	}`)
}

func (AskUserTool) IsReadOnly() bool { return true }

func (AskUserTool) Execute(_ context.Context, input json.RawMessage) (*tool.Result, error) {
	var params struct {
		Questions []AskQuestion `json:"questions"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return &tool.Result{Content: "invalid input: " + err.Error(), IsError: true}, nil
	}
	if len(params.Questions) == 0 {
		return &tool.Result{Content: "at least one question is required", IsError: true}, nil
	}
	if len(params.Questions) > 4 {
		return &tool.Result{Content: "maximum 4 questions allowed", IsError: true}, nil
	}

	// Non-interactive fallback: if stdin is not a terminal, return
	// questions as text for the agent to include in its response.
	if !isTerminal() {
		var sb strings.Builder
		sb.WriteString("Questions for the user (please respond in your next message):\n\n")
		for i, q := range params.Questions {
			sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, q.Label))
			if q.Description != "" {
				sb.WriteString(fmt.Sprintf("   %s\n", q.Description))
			}
			sb.WriteString("   Options: " + strings.Join(q.Options, ", ") + "\n")
		}
		return &tool.Result{Content: sb.String()}, nil
	}

	// Interactive mode: present each question and collect answers.
	scanner := bufio.NewScanner(os.Stdin)
	answers := make(map[string]string)

	for i, q := range params.Questions {
		fmt.Fprintf(os.Stderr, "\n%s Question %d/%d %s\n",
			strings.Repeat("─", 20), i+1, len(params.Questions), strings.Repeat("─", 20))
		fmt.Fprintf(os.Stderr, "  %s\n", q.Label)
		if q.Description != "" {
			fmt.Fprintf(os.Stderr, "  %s\n", q.Description)
		}
		fmt.Fprintf(os.Stderr, "\n")

		for j, opt := range q.Options {
			fmt.Fprintf(os.Stderr, "  %d) %s\n", j+1, opt)
		}
		fmt.Fprintf(os.Stderr, "  0) Other (type your own)\n")

		if q.MultiSelect {
			fmt.Fprintf(os.Stderr, "\nSelect one or more (comma-separated numbers, e.g. 1,3): ")
		} else {
			fmt.Fprintf(os.Stderr, "\nSelect (number): ")
		}

		if !scanner.Scan() {
			return &tool.Result{Content: "cancelled", IsError: true}, nil
		}
		raw := strings.TrimSpace(scanner.Text())

		if q.MultiSelect {
			var selected []string
			for _, part := range strings.Split(raw, ",") {
				part = strings.TrimSpace(part)
				idx, err := strconv.Atoi(part)
				if err != nil || idx < 0 || idx > len(q.Options) {
					continue
				}
				if idx == 0 {
					fmt.Fprintf(os.Stderr, "Type your answer: ")
					if scanner.Scan() {
						selected = append(selected, strings.TrimSpace(scanner.Text()))
					}
				} else {
					selected = append(selected, q.Options[idx-1])
				}
			}
			if len(selected) == 0 {
				selected = []string{"(no selection)"}
			}
			answers[q.Label] = strings.Join(selected, ", ")
		} else {
			idx, err := strconv.Atoi(raw)
			if err != nil || idx < 0 || idx > len(q.Options) {
				// Treat as free-text
				answers[q.Label] = raw
			} else if idx == 0 {
				fmt.Fprintf(os.Stderr, "Type your answer: ")
				if scanner.Scan() {
					answers[q.Label] = strings.TrimSpace(scanner.Text())
				} else {
					answers[q.Label] = "(no answer)"
				}
			} else {
				answers[q.Label] = q.Options[idx-1]
			}
		}
	}

	data, _ := json.MarshalIndent(answers, "", "  ")
	return &tool.Result{Content: string(data)}, nil
}

// isTerminal checks if stdin is connected to a terminal.
func isTerminal() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
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
	registry.Register(AskUserTool{})

	ag := agent.New(anthropic.New(key), registry)
	ag.System = `You are a helpful coding assistant. Use tools to inspect and modify files.

When you need to clarify requirements or get user preferences, use the AskUser
tool to present structured questions with options.`
	ag.Model = "claude-sonnet-4-20250514"

	prompt := strings.Join(os.Args[1:], " ")
	if prompt == "" {
		prompt = "I want to set up a new project. Ask me what language and framework I'd like to use, and what kind of project it is."
	}

	answer, err := ag.Run(context.Background(), prompt)
	if err != nil {
		return err
	}
	fmt.Println(answer)
	return nil
}
