package builtins

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/fsys"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/tool"
)

// Glob finds files matching a glob pattern via a recursive filesystem walk.
// It supports `*` and `?` within a path segment and `**` to match across any
// number of segments.
type Glob struct {
	FS fsys.Store
}

type globInput struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path,omitempty"`
}

func (t *Glob) Name() string { return "Glob" }

func (t *Glob) Description() string {
	return "Find files matching a glob pattern (e.g. \"**/*.go\", \"src/*.ts\"). " +
		"Supports * and ? within a segment and ** across directories."
}

func (t *Glob) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"pattern": {"type": "string", "description": "Glob pattern to match"},
			"path": {"type": "string", "description": "Base directory to search (default \".\")"}
		},
		"required": ["pattern"]
	}`)
}

func (t *Glob) IsReadOnly() bool                        { return true }
func (t *Glob) RequiresApproval(_ json.RawMessage) bool { return false }

func (t *Glob) Execute(ctx context.Context, input json.RawMessage) (*tool.Result, error) {
	var in globInput
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

	files, err := walkFiles(ctx, t.FS, root)
	if err != nil {
		return &tool.Result{Content: fmt.Sprintf("Error walking directory: %v", err), IsError: true}, nil
	}

	var matches []string
	for _, f := range files {
		rel := strings.TrimPrefix(f, root+"/")
		if matchGlob(in.Pattern, rel) {
			matches = append(matches, f)
		}
	}

	if len(matches) == 0 {
		return &tool.Result{Content: "No files matched the pattern"}, nil
	}
	return &tool.Result{Content: strings.Join(matches, "\n")}, nil
}

// matchGlob reports whether name matches pattern, where ** spans any number of
// path segments (including zero) and * / ? match within a single segment.
func matchGlob(pattern, name string) bool {
	pParts := strings.Split(pattern, "/")
	nParts := strings.Split(name, "/")
	return matchSegments(pParts, nParts)
}

func matchSegments(pat, name []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			// Collapse consecutive ** segments.
			rest := pat[1:]
			if len(rest) == 0 {
				return true // trailing ** matches everything remaining
			}
			// Try matching the remaining pattern at each suffix of name.
			for i := 0; i <= len(name); i++ {
				if matchSegments(rest, name[i:]) {
					return true
				}
			}
			return false
		}
		if len(name) == 0 {
			return false
		}
		if ok, _ := path.Match(pat[0], name[0]); !ok {
			return false
		}
		pat = pat[1:]
		name = name[1:]
	}
	return len(name) == 0
}
