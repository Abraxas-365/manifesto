package builtins

import (
	"context"
	"encoding/json"
	"fmt"
	"path"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/fsys"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/tool"
)

// Write creates or overwrites a file.
type Write struct {
	FS fsys.Store
}

type writeInput struct {
	FilePath string `json:"file_path"`
	Content  string `json:"content"`
}

func (t *Write) Name() string { return "Write" }

func (t *Write) Description() string {
	return `Writes a file to the local filesystem, creating parent directories as needed.

Usage:
- This tool will overwrite the existing file if there is one at the provided path.
- If this is an existing file, you MUST use the Read tool first to read the file's contents.
- Prefer the Edit tool for modifying existing files — it only sends the diff. Only use this tool to create new files or for complete rewrites.
- NEVER create documentation files (*.md) or README files unless explicitly requested by the User.
- Only use emojis if the user explicitly requests it. Avoid writing emojis to files unless asked.`
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

func (t *Write) IsReadOnly() bool { return false }

func (t *Write) Execute(ctx context.Context, input json.RawMessage) (*tool.Result, error) {
	var in writeInput
	if err := json.Unmarshal(input, &in); err != nil {
		return &tool.Result{Content: fmt.Sprintf("Invalid input: %v", err), IsError: true}, nil
	}
	if in.FilePath == "" {
		return &tool.Result{Content: "No file path provided", IsError: true}, nil
	}
	in.FilePath = tool.ResolvePath(ctx, in.FilePath)

	if dir := path.Dir(in.FilePath); dir != "" && dir != "." {
		if err := t.FS.MkdirAll(ctx, dir); err != nil {
			return &tool.Result{Content: fmt.Sprintf("Error creating directory: %v", err), IsError: true}, nil
		}
	}

	if err := t.FS.WriteFile(ctx, in.FilePath, []byte(in.Content)); err != nil {
		return &tool.Result{Content: fmt.Sprintf("Error writing file: %v", err), IsError: true}, nil
	}

	return &tool.Result{Content: fmt.Sprintf("Wrote %d bytes to %s", len(in.Content), in.FilePath)}, nil
}
