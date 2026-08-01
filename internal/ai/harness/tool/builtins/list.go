package builtins

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/fsys"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/tool"
)

// List lists the entries of a directory.
type List struct {
	FS fsys.Store
}

type listInput struct {
	Path string `json:"path"`
}

func (t *List) Name() string { return "List" }

func (t *List) Description() string {
	return "List the contents of a directory, showing each entry's name, size, " +
		"and whether it is a directory."
}

func (t *List) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {"type": "string", "description": "Directory path to list"}
		},
		"required": ["path"]
	}`)
}

func (t *List) IsReadOnly() bool { return true }

func (t *List) Execute(ctx context.Context, input json.RawMessage) (*tool.Result, error) {
	var in listInput
	if err := json.Unmarshal(input, &in); err != nil {
		return &tool.Result{Content: fmt.Sprintf("Invalid input: %v", err), IsError: true}, nil
	}
	if in.Path == "" {
		in.Path = "."
	}

	entries, err := t.FS.List(ctx, in.Path)
	if err != nil {
		return &tool.Result{Content: fmt.Sprintf("Error listing directory: %v", err), IsError: true}, nil
	}
	if len(entries) == 0 {
		return &tool.Result{Content: "(empty directory)"}, nil
	}

	var b strings.Builder
	for _, e := range entries {
		if e.IsDir {
			fmt.Fprintf(&b, "%s/\n", e.Name)
		} else {
			fmt.Fprintf(&b, "%s\t%d bytes\n", e.Name, e.Size)
		}
	}
	return &tool.Result{Content: b.String()}, nil
}
