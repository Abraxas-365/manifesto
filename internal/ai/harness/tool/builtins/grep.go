package builtins

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/tool"
	"github.com/Abraxas-365/manifesto/internal/fsx"
)

// MaxGrepMatches bounds the number of reported matches.
const MaxGrepMatches = 500

// Grep searches file contents for a regular expression.
type Grep struct {
	FS fsx.FileSystem
}

type grepInput struct {
	Pattern       string `json:"pattern"`
	Path          string `json:"path,omitempty"`
	Glob          string `json:"glob,omitempty"`
	CaseSensitive bool   `json:"case_sensitive,omitempty"`
}

func (t *Grep) Name() string { return "Grep" }

func (t *Grep) Description() string {
	return "Search file contents for a regular expression. Returns matching lines " +
		"as path:line:text. Optionally restrict to files matching a glob pattern."
}

func (t *Grep) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"pattern": {"type": "string", "description": "Regular expression to search for"},
			"path": {"type": "string", "description": "Base directory to search (default \".\")"},
			"glob": {"type": "string", "description": "Only search files matching this glob"},
			"case_sensitive": {"type": "boolean", "description": "Case-sensitive match (default false)"}
		},
		"required": ["pattern"]
	}`)
}

func (t *Grep) IsReadOnly() bool                        { return true }
func (t *Grep) RequiresApproval(_ json.RawMessage) bool { return false }

func (t *Grep) Execute(ctx context.Context, input json.RawMessage) (*tool.Result, error) {
	var in grepInput
	if err := json.Unmarshal(input, &in); err != nil {
		return &tool.Result{Content: fmt.Sprintf("Invalid input: %v", err), IsError: true}, nil
	}
	if in.Pattern == "" {
		return &tool.Result{Content: "No pattern provided", IsError: true}, nil
	}
	root := in.Path
	if root == "" {
		root = "."
	}

	expr := in.Pattern
	if !in.CaseSensitive {
		expr = "(?i)" + expr
	}
	re, err := regexp.Compile(expr)
	if err != nil {
		return &tool.Result{Content: fmt.Sprintf("Invalid regexp: %v", err), IsError: true}, nil
	}

	files, err := walkFiles(ctx, t.FS, root)
	if err != nil {
		return &tool.Result{Content: fmt.Sprintf("Error walking directory: %v", err), IsError: true}, nil
	}

	var out strings.Builder
	matches := 0
	for _, f := range files {
		if in.Glob != "" {
			rel := strings.TrimPrefix(f, root+"/")
			if !matchGlob(in.Glob, rel) {
				continue
			}
		}
		data, err := t.FS.ReadFile(ctx, f)
		if err != nil || bytes.IndexByte(data, 0) >= 0 {
			continue
		}
		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			if re.MatchString(line) {
				fmt.Fprintf(&out, "%s:%d:%s\n", f, i+1, line)
				matches++
				if matches >= MaxGrepMatches {
					out.WriteString("[match limit reached]\n")
					return &tool.Result{Content: out.String()}, nil
				}
			}
		}
	}

	if matches == 0 {
		return &tool.Result{Content: "No matches found"}, nil
	}
	return &tool.Result{Content: out.String()}, nil
}
