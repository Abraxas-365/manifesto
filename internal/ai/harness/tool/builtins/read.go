package builtins

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/fsys"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/tool"
	"github.com/Abraxas-365/manifesto/internal/imageutil"
)

// MaxReadBytes rejects whole-file reads larger than this before loading.
const MaxReadBytes = 256 * 1024

// maxImageBytes is the raw-image cap (~5MB base64, Claude API hard limit).
const maxImageBytes = 3_750_000

// imageMediaTypes maps supported image extensions to MIME types.
var imageMediaTypes = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
}

// ReadCache dedups repeated identical reads of unchanged files: on a hit the
// tool returns a short stub instead of the full content, saving tokens (legacy
// lesson). Entries are validated against the file's mtime on every hit.
type ReadCache struct {
	mu      sync.Mutex
	entries map[readKey]time.Time // key -> mtime at read time
}

type readKey struct {
	Path   string
	Offset int
	Limit  int
}

// NewReadCache returns an empty cache.
func NewReadCache() *ReadCache { return &ReadCache{entries: map[readKey]time.Time{}} }

func (c *ReadCache) hit(key readKey, mtime time.Time) bool {
	if mtime.IsZero() {
		return false // backend has no mtime; never dedup
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	prev, ok := c.entries[key]
	return ok && prev.Equal(mtime)
}

func (c *ReadCache) put(key readKey, mtime time.Time) {
	if mtime.IsZero() {
		return
	}
	c.mu.Lock()
	c.entries[key] = mtime
	c.mu.Unlock()
}

// InvalidatePath removes all cache entries whose path matches, so a re-Read
// goes to disk instead of returning a stub pointing at cleared content.
func (c *ReadCache) InvalidatePath(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k := range c.entries {
		if k.Path == path {
			delete(c.entries, k)
		}
	}
}

// InvalidateAll clears the entire cache. Called after compaction clears tool
// results — stubs reference content that no longer exists in the conversation.
func (c *ReadCache) InvalidateAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	clear(c.entries)
}

// Read reads file contents with an optional line range.
type Read struct {
	FS fsys.Store

	// Cache, when set, enables repeat-read dedup: identical reads of an
	// unchanged file return a stub instead of re-sending the content.
	Cache *ReadCache
}

type readInput struct {
	FilePath string       `json:"file_path"`
	Offset   tool.FlexInt `json:"offset,omitempty"`
	Limit    tool.FlexInt `json:"limit,omitempty"`
}

func (t *Read) Name() string { return "Read" }

func (t *Read) Description() string {
	return `Reads a file from the local filesystem. You can access any file directly by using this tool.
Assume this tool is able to read all files on the machine. If the User provides a path to a file assume that path is valid. It is okay to read a file that does not exist; an error will be returned.

Usage:
- By default, it reads up to 2000 lines starting from the beginning of the file
- When you already know which part of the file you need, only read that part using the offset (1-based line) and limit parameters. This can be important for larger files.
- Results are returned using cat -n format, with line numbers starting at 1 (line number followed by a tab)
- This tool allows reading images (eg PNG, JPG, etc). When reading an image file the contents are presented visually.
- This tool can only read files, not directories. To read a directory, use the List tool.
- Files larger than 256KB must be read with an explicit offset/limit range; use Grep to locate the relevant section first.
- If a file has not changed since your last Read this session, you will receive a stub instead of the full content. The content from the earlier Read result in this conversation is still current — refer to that instead of re-reading.`
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

func (t *Read) IsReadOnly() bool { return true }

func (t *Read) Execute(ctx context.Context, input json.RawMessage) (*tool.Result, error) {
	var in readInput
	if err := json.Unmarshal(input, &in); err != nil {
		return &tool.Result{Content: fmt.Sprintf("Invalid input: %v", err), IsError: true}, nil
	}
	if in.FilePath == "" {
		return &tool.Result{Content: "No file path provided", IsError: true}, nil
	}
	in.FilePath = tool.ResolvePath(ctx, in.FilePath)

	offset := in.Offset.Value(0)
	limit := in.Limit.Value(0)
	explicitRange := offset != 0 || limit != 0

	info, err := t.FS.Stat(ctx, in.FilePath)
	if err != nil {
		return &tool.Result{Content: fmt.Sprintf("Error reading file: %v", err), IsError: true}, nil
	}

	// Image files — return as vision content blocks instead of text.
	if mt, ok := imageMediaTypes[strings.ToLower(path.Ext(in.FilePath))]; ok {
		raw, err := t.FS.ReadFile(ctx, in.FilePath)
		if err != nil {
			return &tool.Result{Content: fmt.Sprintf("Error reading image: %v", err), IsError: true}, nil
		}
		compressed, mt := imageutil.CompressImage(raw, mt)
		if len(compressed) > maxImageBytes {
			return &tool.Result{Content: fmt.Sprintf("Image too large (%s, max ~3.75MB)", imageutil.HumanFileSize(len(compressed))), IsError: true}, nil
		}
		return &tool.Result{
			Content: fmt.Sprintf("[Image: %s]", path.Base(in.FilePath)),
			Images:  []tool.ImageData{{MediaType: mt, Data: base64.StdEncoding.EncodeToString(compressed)}},
		}, nil
	}

	// Repeat-read dedup: unchanged file + identical range = stub.
	key := readKey{Path: in.FilePath, Offset: offset, Limit: limit}
	if t.Cache != nil && t.Cache.hit(key, info.ModTime) {
		return &tool.Result{Content: "File unchanged since last read. The content from the earlier Read tool_result in this conversation is still current — refer to that instead of re-reading."}, nil
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
	if t.Cache != nil {
		t.Cache.put(key, info.ModTime)
	}
	return &tool.Result{Content: out.String()}, nil
}
