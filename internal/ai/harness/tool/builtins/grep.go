package builtins

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/fsys"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/tool"
)

// DefaultGrepHeadLimit is the default cap on output lines/entries (legacy value).
const DefaultGrepHeadLimit = 250

// DefaultMaxGrepOutput caps total grep output size when Grep.MaxOutput is not
// set (legacy value).
const DefaultMaxGrepOutput = 20_000

// Grep searches file contents for a regular expression. It is a pure-Go port
// of legacy's ripgrep-backed tool with the same schema (output_mode, context
// lines, head_limit/offset, type filter, multiline), running over the
// abstract fsys.Store so it works on any backing store.
type Grep struct {
	FS fsys.Store

	// MaxOutput caps total output size in bytes. Zero means
	// DefaultMaxGrepOutput. Negative means no cap.
	MaxOutput int
}

func (t *Grep) maxOutput() int {
	if t.MaxOutput == 0 {
		return DefaultMaxGrepOutput
	}
	return t.MaxOutput
}

type grepInput struct {
	Pattern     string `json:"pattern"`
	Path        string `json:"path,omitempty"`
	Glob        string `json:"glob,omitempty"`
	Type        string `json:"type,omitempty"`
	OutputMode  string `json:"output_mode,omitempty"` // content | files_with_matches | count
	Context     int    `json:"context,omitempty"`
	ContextC    int    `json:"-C,omitempty"`
	BeforeCtx   int    `json:"-B,omitempty"`
	AfterCtx    int    `json:"-A,omitempty"`
	IgnoreCase  bool   `json:"-i,omitempty"`
	LineNumbers *bool  `json:"-n,omitempty"`
	HeadLimit   int    `json:"head_limit,omitempty"`
	Offset      int    `json:"offset,omitempty"`
	Multiline   bool   `json:"multiline,omitempty"`
}

// flexGrepInt tolerates quoted numeric args (e.g. {"-C":"3"}) that LLMs
// frequently emit instead of bare numbers (legacy lesson).
type flexGrepInt int

func (f *flexGrepInt) UnmarshalJSON(data []byte) error {
	s := strings.Trim(string(data), `"`)
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return err
	}
	*f = flexGrepInt(n)
	return nil
}

func (in *grepInput) UnmarshalJSON(data []byte) error {
	type alias grepInput
	aux := struct {
		*alias
		Context   flexGrepInt `json:"context,omitempty"`
		ContextC  flexGrepInt `json:"-C,omitempty"`
		BeforeCtx flexGrepInt `json:"-B,omitempty"`
		AfterCtx  flexGrepInt `json:"-A,omitempty"`
		HeadLimit flexGrepInt `json:"head_limit,omitempty"`
		Offset    flexGrepInt `json:"offset,omitempty"`
	}{alias: (*alias)(in)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	in.Context = int(aux.Context)
	in.ContextC = int(aux.ContextC)
	in.BeforeCtx = int(aux.BeforeCtx)
	in.AfterCtx = int(aux.AfterCtx)
	in.HeadLimit = int(aux.HeadLimit)
	in.Offset = int(aux.Offset)
	return nil
}

// typeExtensions maps rg-style --type names to file extensions.
var typeExtensions = map[string][]string{
	"go":     {".go"},
	"js":     {".js", ".jsx", ".mjs", ".cjs"},
	"ts":     {".ts", ".tsx", ".mts", ".cts"},
	"py":     {".py", ".pyi"},
	"rust":   {".rs"},
	"java":   {".java"},
	"c":      {".c", ".h"},
	"cpp":    {".cpp", ".cc", ".cxx", ".hpp", ".hh", ".h"},
	"cs":     {".cs"},
	"rb":     {".rb"},
	"php":    {".php"},
	"swift":  {".swift"},
	"kotlin": {".kt", ".kts"},
	"lua":    {".lua"},
	"sh":     {".sh", ".bash", ".zsh"},
	"html":   {".html", ".htm"},
	"css":    {".css", ".scss", ".sass", ".less"},
	"json":   {".json"},
	"yaml":   {".yaml", ".yml"},
	"toml":   {".toml"},
	"md":     {".md", ".markdown"},
	"sql":    {".sql"},
}

func (t *Grep) Name() string { return "Grep" }

func (t *Grep) Description() string {
	return `A powerful content search tool

  Usage:
  - ALWAYS use Grep for content search tasks. NEVER invoke ` + "`grep`" + ` or ` + "`rg`" + ` as a Bash command. The Grep tool has been optimized for correct permissions and access.
  - Supports full regex syntax (e.g., "log.*Error", "function\\s+\\w+")
  - Filter files with glob parameter (e.g., "*.js", "**/*.tsx") or type parameter (e.g., "js", "py", "rust")
  - Output modes: "content" shows matching lines, "files_with_matches" shows only file paths (default), "count" shows match counts
  - Use the Task tool for open-ended searches requiring multiple rounds
  - Pattern syntax: literal braces need escaping (use ` + "`interface\\{\\}`" + ` to find ` + "`interface{}`" + ` in Go code)
  - Multiline matching: By default patterns match within single lines only. For cross-line patterns like ` + "`struct \\{[\\s\\S]*?field`" + `, use ` + "`multiline: true`" + ``
}

func (t *Grep) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"pattern": {"type": "string", "description": "The regular expression pattern to search for in file contents"},
			"path": {"type": "string", "description": "File or directory to search in. Defaults to current working directory."},
			"glob": {"type": "string", "description": "Glob pattern to filter files (e.g. \"*.js\", \"**/*.tsx\")"},
			"type": {"type": "string", "description": "File type to search. Common types: js, py, rust, go, java, etc."},
			"output_mode": {"type": "string", "enum": ["content", "files_with_matches", "count"], "description": "Output mode: \"content\" shows matching lines, \"files_with_matches\" shows file paths (default), \"count\" shows match counts."},
			"context": {"type": "number", "description": "Number of lines to show before and after each match. Requires output_mode: \"content\"."},
			"-A": {"type": "number", "description": "Number of lines to show after each match. Requires output_mode: \"content\"."},
			"-B": {"type": "number", "description": "Number of lines to show before each match. Requires output_mode: \"content\"."},
			"-i": {"type": "boolean", "description": "Case insensitive search"},
			"-n": {"type": "boolean", "description": "Show line numbers in output. Defaults to true."},
			"head_limit": {"type": "number", "description": "Limit output to first N lines/entries. Defaults to 250 when unspecified. Pass 0 for unlimited."},
			"offset": {"type": "number", "description": "Skip first N lines/entries before applying head_limit."},
			"multiline": {"type": "boolean", "description": "Enable multiline mode where . matches newlines and patterns can span lines. Default: false."}
		},
		"required": ["pattern"]
	}`)
}

func (t *Grep) IsReadOnly() bool { return true }

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
	root = tool.ResolvePath(ctx, root)

	expr := in.Pattern
	if in.Multiline {
		expr = "(?s)" + expr
	}
	if in.IgnoreCase {
		expr = "(?i)" + expr
	}
	re, err := regexp.Compile(expr)
	if err != nil {
		return &tool.Result{Content: grepErrorMessage(err, in.Pattern), IsError: true}, nil
	}

	mode := in.OutputMode
	if mode == "" {
		mode = "files_with_matches"
	}
	switch mode {
	case "content", "files_with_matches", "count":
	default:
		return &tool.Result{Content: fmt.Sprintf("Invalid output_mode %q (want content, files_with_matches, or count)", mode), IsError: true}, nil
	}

	files, err := t.candidateFiles(ctx, root, in)
	if err != nil {
		return &tool.Result{Content: fmt.Sprintf("Error walking directory: %v", err), IsError: true}, nil
	}

	before, after := in.BeforeCtx, in.AfterCtx
	if c := max(in.Context, in.ContextC); c > 0 {
		before, after = max(before, c), max(after, c)
	}
	lineNumbers := in.LineNumbers == nil || *in.LineNumbers

	var lines []string
	for _, f := range files {
		data, err := t.FS.ReadFile(ctx, f)
		if err != nil || bytes.IndexByte(data, 0) >= 0 {
			continue
		}
		switch mode {
		case "files_with_matches":
			if re.Match(data) {
				lines = append(lines, f)
			}
		case "count":
			if n := countMatches(re, data, in.Multiline); n > 0 {
				lines = append(lines, fmt.Sprintf("%s:%d", f, n))
			}
		case "content":
			lines = append(lines, contentMatches(re, f, data, in.Multiline, before, after, lineNumbers)...)
		}
		if len(lines) > in.Offset+headLimit(in)*2 && headLimit(in) > 0 {
			break // gathered comfortably past the window; stop early
		}
	}

	if len(lines) == 0 {
		return &tool.Result{Content: "No matches found"}, nil
	}

	// Apply offset then head_limit (legacy order).
	if in.Offset > 0 {
		if in.Offset >= len(lines) {
			return &tool.Result{Content: "No matches found (offset beyond results)"}, nil
		}
		lines = lines[in.Offset:]
	}
	output := ""
	if limit := headLimit(in); limit > 0 && len(lines) > limit {
		remaining := len(lines) - limit
		output = strings.Join(lines[:limit], "\n") + fmt.Sprintf("\n... (%d more results)", remaining)
	} else {
		output = strings.Join(lines, "\n")
	}

	if max := t.maxOutput(); max >= 0 && len(output) > max {
		output = output[:max] + fmt.Sprintf(
			"\n[Grep output truncated at %d bytes. Narrow your pattern or use head_limit to reduce results.]",
			max)
	}
	return &tool.Result{Content: strings.TrimSpace(output)}, nil
}

func headLimit(in grepInput) int {
	if in.HeadLimit > 0 {
		return in.HeadLimit
	}
	if in.HeadLimit == 0 {
		return DefaultGrepHeadLimit
	}
	return 0
}

// candidateFiles resolves the file set to search: a single file path, or a
// recursive walk filtered by glob/type.
func (t *Grep) candidateFiles(ctx context.Context, root string, in grepInput) ([]string, error) {
	if st, err := t.FS.Stat(ctx, root); err == nil && !st.IsDir {
		return []string{root}, nil
	}
	files, err := walkFiles(ctx, t.FS, root)
	if err != nil {
		return nil, err
	}
	var exts []string
	if in.Type != "" {
		exts = typeExtensions[strings.ToLower(in.Type)]
		if exts == nil {
			exts = []string{"." + strings.ToLower(in.Type)}
		}
	}
	var out []string
	for _, f := range files {
		rel := strings.TrimPrefix(f, root+"/")
		if strings.HasPrefix(rel, ".git/") || strings.Contains(rel, "/.git/") {
			continue
		}
		if in.Glob != "" && !matchGlob(in.Glob, rel) {
			continue
		}
		if exts != nil && !hasAnySuffix(f, exts) {
			continue
		}
		out = append(out, f)
	}
	sort.Strings(out)
	return out, nil
}

func hasAnySuffix(s string, suffixes []string) bool {
	for _, suf := range suffixes {
		if strings.HasSuffix(s, suf) {
			return true
		}
	}
	return false
}

// countMatches counts matching lines (single-line mode) or total matches
// (multiline mode), mirroring rg -c.
func countMatches(re *regexp.Regexp, data []byte, multiline bool) int {
	if multiline {
		return len(re.FindAllIndex(data, -1))
	}
	n := 0
	for _, line := range strings.Split(string(data), "\n") {
		if re.MatchString(line) {
			n++
		}
	}
	return n
}

// contentMatches renders matching lines with optional context, mirroring
// rg -n -A/-B output (":" for match lines, "-" for context lines).
func contentMatches(re *regexp.Regexp, path string, data []byte, multiline bool, before, after int, lineNumbers bool) []string {
	lines := strings.Split(string(data), "\n")

	matched := map[int]bool{}
	if multiline {
		// Map byte-offset matches back to the line numbers they span.
		text := string(data)
		offsets := lineOffsets(text)
		for _, loc := range re.FindAllStringIndex(text, -1) {
			start, end := lineForOffset(offsets, loc[0]), lineForOffset(offsets, max(loc[0], loc[1]-1))
			for i := start; i <= end; i++ {
				matched[i] = true
			}
		}
	} else {
		for i, line := range lines {
			if re.MatchString(line) {
				matched[i] = true
			}
		}
	}
	if len(matched) == 0 {
		return nil
	}

	include := map[int]bool{}
	for i := range matched {
		for j := max(0, i-before); j <= min(len(lines)-1, i+after); j++ {
			include[j] = true
		}
	}

	idxs := make([]int, 0, len(include))
	for i := range include {
		idxs = append(idxs, i)
	}
	sort.Ints(idxs)

	var out []string
	prev := -2
	for _, i := range idxs {
		if (before > 0 || after > 0) && prev >= 0 && i > prev+1 {
			out = append(out, "--")
		}
		sep := ":"
		if !matched[i] {
			sep = "-"
		}
		if lineNumbers {
			out = append(out, fmt.Sprintf("%s%s%d%s%s", path, sep, i+1, sep, lines[i]))
		} else {
			out = append(out, fmt.Sprintf("%s%s%s", path, sep, lines[i]))
		}
		prev = i
	}
	return out
}

func lineOffsets(text string) []int {
	offsets := []int{0}
	for i, c := range text {
		if c == '\n' {
			offsets = append(offsets, i+1)
		}
	}
	return offsets
}

func lineForOffset(offsets []int, off int) int {
	return sort.SearchInts(offsets, off+1) - 1
}

// grepErrorMessage turns a regex compile error into an actionable hint: the
// dominant failure mode is the model treating Grep as literal-substring
// search (legacy lesson).
func grepErrorMessage(err error, pattern string) string {
	return fmt.Sprintf(
		"Grep error: %v\n\nThe pattern %q is not valid regex — Grep searches by regular "+
			"expression, not literal text. Escape regex metacharacters ( ) { } [ ] . * + ? | ^ $ \\ "+
			"with a backslash (e.g. \"ToolUse\\{\" or \"streamOnce\\(ctx\").",
		err, pattern)
}
