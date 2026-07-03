package builtins

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/fsys"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/tool"
)

// Edit replaces an exact string in a file.
type Edit struct {
	FS fsys.Store
}

type editInput struct {
	FilePath   string `json:"file_path"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all,omitempty"`
}

func (t *Edit) Name() string { return "Edit" }

func (t *Edit) Description() string {
	return "Replace an exact occurrence of old_string with new_string in a file. " +
		"By default old_string must be unique; set replace_all to replace every " +
		"occurrence."
}

func (t *Edit) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"file_path": {"type": "string", "description": "Path to the file to edit"},
			"old_string": {"type": "string", "description": "Exact text to replace"},
			"new_string": {"type": "string", "description": "Replacement text"},
			"replace_all": {"type": "boolean", "description": "Replace all occurrences"}
		},
		"required": ["file_path", "old_string", "new_string"]
	}`)
}

func (t *Edit) IsReadOnly() bool                        { return false }
func (t *Edit) RequiresApproval(_ json.RawMessage) bool { return true }

func (t *Edit) Execute(ctx context.Context, input json.RawMessage) (*tool.Result, error) {
	var in editInput
	if err := json.Unmarshal(input, &in); err != nil {
		return &tool.Result{Content: fmt.Sprintf("Invalid input: %v", err), IsError: true}, nil
	}
	if in.FilePath == "" {
		return &tool.Result{Content: "No file path provided", IsError: true}, nil
	}
	if in.OldString == in.NewString {
		return &tool.Result{Content: "old_string and new_string are identical", IsError: true}, nil
	}

	data, err := t.FS.ReadFile(ctx, in.FilePath)
	if err != nil {
		return &tool.Result{Content: fmt.Sprintf("Error reading file: %v", err), IsError: true}, nil
	}
	content := string(data)

	count := strings.Count(content, in.OldString)
	if count == 0 {
		return &tool.Result{Content: "old_string not found in file", IsError: true}, nil
	}
	if count > 1 && !in.ReplaceAll {
		return &tool.Result{
			Content: fmt.Sprintf("old_string is not unique (%d matches). Provide more context or set replace_all.", count),
			IsError: true,
		}, nil
	}

	var updated string
	if in.ReplaceAll {
		updated = strings.ReplaceAll(content, in.OldString, in.NewString)
	} else {
		updated = strings.Replace(content, in.OldString, in.NewString, 1)
	}

	if err := t.FS.WriteFile(ctx, in.FilePath, []byte(updated)); err != nil {
		return &tool.Result{Content: fmt.Sprintf("Error writing file: %v", err), IsError: true}, nil
	}

	return &tool.Result{Content: fmt.Sprintf("Replaced %d occurrence(s) in %s", count, in.FilePath)}, nil
}
