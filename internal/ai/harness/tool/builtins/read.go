package builtins

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/tool"
	"github.com/Abraxas-365/manifesto/internal/fsx"
)

// MaxReadBytes rejects whole-file reads larger than this before loading.
const MaxReadBytes = 256 * 1024

// Read reads file contents with an optional line range.
type Read struct {
	FS fsx.FileSystem
}

type readInput struct {
	FilePath string       `json:"file_path"`
	Offset   tool.FlexInt `json:"offset,omitempty"`
	Limit    tool.FlexInt `json:"limit,omitempty"`
}

func (t *Read) Name() string { return "Read" }

func (t *Read) Description() string {
	return "Read a file from the filesystem. Returns contents in `cat -n` format " +
		"(line numbers followed by a tab). Use offset (1-based line) and limit to " +
		"read a specific range of a large file."
}

func (t *Read) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"file_path": {"type": "string", "description": "Path to the file to read"},
			"offset": {"type": "number", "description": "1-based line number to start from"},
			"limit": {"type": "number", "description": "Maximum number of lines to read (default 2000)"}
		},
		"required": ["file_path"]
	}`)
}

func (t *Read) IsReadOnly() bool                        { return true }
func (t *Read) RequiresApproval(_ json.RawMessage) bool { return false }

func (t *Read) Execute(ctx context.Context, input json.RawMessage) (*tool.Result, error) {
	var in readInput
	if err := json.Unmarshal(input, &in); err != nil {
		return &tool.Result{Content: fmt.Sprintf("Invalid input: %v", err), IsError: true}, nil
	}
	if in.FilePath == "" {
		return &tool.Result{Content: "No file path provided", IsError: true}, nil
	}

	offset := in.Offset.Value(0)
	limit := in.Limit.Value(0)
	explicitRange := offset != 0 || limit != 0

	info, err := t.FS.Stat(ctx, in.FilePath)
	if err != nil {
		return &tool.Result{Content: fmt.Sprintf("Error reading file: %v", err), IsError: true}, nil
	}
	if info.Size > MaxReadBytes && !explicitRange {
		return &tool.Result{
			Content: fmt.Sprintf("File too large to read in full (%d KB). Use Grep to find content, or read a range with offset and limit.", info.Size/1024),
			IsError: true,
		}, nil
	}

	data, err := t.FS.ReadFile(ctx, in.FilePath)
	if err != nil {
		return &tool.Result{Content: fmt.Sprintf("Error reading file: %v", err), IsError: true}, nil
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return &tool.Result{Content: fmt.Sprintf("File %s appears to be binary and cannot be read as text.", in.FilePath)}, nil
	}

	if limit == 0 {
		limit = 2000
	}
	if offset == 0 {
		offset = 1
	}

	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return &tool.Result{Content: "(empty file)"}, nil
	}

	endLine := offset + limit // first line NOT included
	var out strings.Builder
	linesRead := 0
	for i, line := range lines {
		lineNum := i + 1
		if lineNum < offset {
			continue
		}
		if lineNum >= endLine {
			fmt.Fprintf(&out, "\n[truncated at line %d — file has more content. Use offset=%d with limit to read more.]", endLine, endLine+1)
			break
		}
		fmt.Fprintf(&out, "%d\t%s\n", lineNum, line)
		linesRead++
	}

	if linesRead == 0 {
		return &tool.Result{Content: fmt.Sprintf("No lines in range (file has %d lines)", len(lines)), IsError: true}, nil
	}
	return &tool.Result{Content: out.String()}, nil
}
