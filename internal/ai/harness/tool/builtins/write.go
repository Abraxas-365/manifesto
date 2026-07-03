package builtins

import (
	"context"
	"encoding/json"
	"fmt"
	"path"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/tool"
	"github.com/Abraxas-365/manifesto/internal/fsx"
)

// Write creates or overwrites a file.
type Write struct {
	FS fsx.FileSystem
}

type writeInput struct {
	FilePath string `json:"file_path"`
	Content  string `json:"content"`
}

func (t *Write) Name() string { return "Write" }

func (t *Write) Description() string {
	return "Write content to a file, creating it (and parent directories) or " +
		"overwriting it if it exists."
}

func (t *Write) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"file_path": {"type": "string", "description": "Path to the file to write"},
			"content": {"type": "string", "description": "Content to write"}
		},
		"required": ["file_path", "content"]
	}`)
}

func (t *Write) IsReadOnly() bool                        { return false }
func (t *Write) RequiresApproval(_ json.RawMessage) bool { return true }

func (t *Write) Execute(ctx context.Context, input json.RawMessage) (*tool.Result, error) {
	var in writeInput
	if err := json.Unmarshal(input, &in); err != nil {
		return &tool.Result{Content: fmt.Sprintf("Invalid input: %v", err), IsError: true}, nil
	}
	if in.FilePath == "" {
		return &tool.Result{Content: "No file path provided", IsError: true}, nil
	}

	if dir := path.Dir(in.FilePath); dir != "" && dir != "." {
		if err := t.FS.CreateDir(ctx, dir); err != nil {
			return &tool.Result{Content: fmt.Sprintf("Error creating directory: %v", err), IsError: true}, nil
		}
	}

	if err := t.FS.WriteFile(ctx, in.FilePath, []byte(in.Content)); err != nil {
		return &tool.Result{Content: fmt.Sprintf("Error writing file: %v", err), IsError: true}, nil
	}

	return &tool.Result{Content: fmt.Sprintf("Wrote %d bytes to %s", len(in.Content), in.FilePath)}, nil
}
