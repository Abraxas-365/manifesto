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
	return `Performs exact string replacements in files.

Usage:
- You must use the Read tool at least once in the conversation before editing a file, so your old_string matches the current contents exactly.
- When editing text from Read tool output, ensure you preserve the exact indentation (tabs/spaces) as it appears AFTER the line number prefix. The line number prefix format is: line number + tab. Everything after that is the actual file content to match. Never include any part of the line number prefix in the old_string or new_string.
- ALWAYS prefer editing existing files in the codebase. NEVER write new files unless explicitly required.
- Only use emojis if the user explicitly requests it. Avoid adding emojis to files unless asked.
- The edit will FAIL if ` + "`old_string`" + ` is not unique in the file. Either provide a larger string with more surrounding context to make it unique or use ` + "`replace_all`" + ` to change every instance of ` + "`old_string`" + `.
- Use ` + "`replace_all`" + ` for replacing and renaming strings across the file. This parameter is useful if you want to rename a variable for instance.
- Use the smallest old_string that is clearly unique in the file — usually 2-4 adjacent lines is sufficient. Avoid including 10+ lines of context when less would uniquely identify the target.`
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

func (t *Edit) IsReadOnly() bool { return false }

func (t *Edit) Execute(ctx context.Context, input json.RawMessage) (*tool.Result, error) {
	var in editInput
	if err := json.Unmarshal(input, &in); err != nil {
		return &tool.Result{Content: fmt.Sprintf("Invalid input: %v", err), IsError: true}, nil
	}
	if in.FilePath == "" {
		return &tool.Result{Content: "No file path provided", IsError: true}, nil
	}
	in.FilePath = tool.ResolvePath(ctx, in.FilePath)
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
