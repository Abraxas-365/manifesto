package builtins

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func grepFS() *memFS {
	fs := newMemFS()
	fs.files["a.go"] = []byte("package main\n\nfunc Hello() {}\nfunc World() {}\n")
	fs.files["b.py"] = []byte("def hello():\n    pass\n")
	fs.files["sub/c.go"] = []byte("package sub\n\nfunc Hello() {}\n")
	return fs
}

func runGrep(t *testing.T, fs *memFS, input string) string {
	t.Helper()
	g := &Grep{FS: fs}
	res, err := g.Execute(context.Background(), json.RawMessage(input))
	if err != nil {
		t.Fatal(err)
	}
	return res.Content
}

func TestGrepFilesWithMatchesDefault(t *testing.T) {
	out := runGrep(t, grepFS(), `{"pattern":"func Hello"}`)
	if !strings.Contains(out, "a.go") || !strings.Contains(out, "sub/c.go") {
		t.Errorf("want file paths, got:\n%s", out)
	}
	if strings.Contains(out, "func Hello") {
		t.Errorf("default mode should not include matching lines:\n%s", out)
	}
}

func TestGrepContentModeLineNumbers(t *testing.T) {
	out := runGrep(t, grepFS(), `{"pattern":"func Hello","output_mode":"content"}`)
	if !strings.Contains(out, "a.go:3:func Hello() {}") {
		t.Errorf("want path:line:text, got:\n%s", out)
	}
}

func TestGrepCountMode(t *testing.T) {
	out := runGrep(t, grepFS(), `{"pattern":"func","output_mode":"count"}`)
	if !strings.Contains(out, "a.go:2") || !strings.Contains(out, "sub/c.go:1") {
		t.Errorf("want per-file counts, got:\n%s", out)
	}
}

func TestGrepContextLines(t *testing.T) {
	out := runGrep(t, grepFS(), `{"pattern":"func Hello","output_mode":"content","path":"a.go","-A":1}`)
	if !strings.Contains(out, "a.go:3:func Hello() {}") || !strings.Contains(out, "a.go-4-func World() {}") {
		t.Errorf("want match + after-context, got:\n%s", out)
	}
}

func TestGrepTypeFilter(t *testing.T) {
	out := runGrep(t, grepFS(), `{"pattern":"hello|Hello","type":"py","-i":true}`)
	if !strings.Contains(out, "b.py") || strings.Contains(out, "a.go") {
		t.Errorf("type filter failed:\n%s", out)
	}
}

func TestGrepGlobFilter(t *testing.T) {
	out := runGrep(t, grepFS(), `{"pattern":"Hello","glob":"**/*.go"}`)
	if strings.Contains(out, "b.py") {
		t.Errorf("glob filter failed:\n%s", out)
	}
}

func TestGrepHeadLimitAndOffset(t *testing.T) {
	fs := newMemFS()
	var b strings.Builder
	for range 10 {
		b.WriteString("match line\n")
	}
	fs.files["big.txt"] = []byte(b.String())

	out := runGrep(t, fs, `{"pattern":"match","output_mode":"content","head_limit":3}`)
	if got := strings.Count(out, "big.txt:"); got != 3 {
		t.Errorf("head_limit: want 3 lines, got %d:\n%s", got, out)
	}
	if !strings.Contains(out, "more results") {
		t.Errorf("want truncation notice:\n%s", out)
	}

	out = runGrep(t, fs, `{"pattern":"match","output_mode":"content","offset":8}`)
	if got := strings.Count(out, "big.txt:"); got != 2 {
		t.Errorf("offset: want 2 lines, got %d:\n%s", got, out)
	}
}

func TestGrepMultiline(t *testing.T) {
	fs := newMemFS()
	fs.files["m.go"] = []byte("type S struct {\n\tName string\n}\n")
	out := runGrep(t, fs, `{"pattern":"struct \\{.*?Name","output_mode":"content","multiline":true}`)
	if !strings.Contains(out, "m.go:1:") || !strings.Contains(out, "m.go:2:") {
		t.Errorf("multiline should span lines 1-2:\n%s", out)
	}
	// Without multiline the same pattern must not match.
	if out := runGrep(t, fs, `{"pattern":"struct \\{.*?Name"}`); out != "No matches found" {
		t.Errorf("single-line mode should not match:\n%s", out)
	}
}

func TestGrepQuotedNumericArgs(t *testing.T) {
	out := runGrep(t, grepFS(), `{"pattern":"func Hello","output_mode":"content","path":"a.go","-A":"1"}`)
	if !strings.Contains(out, "a.go-4-func World() {}") {
		t.Errorf("quoted -A should be tolerated:\n%s", out)
	}
}

func TestGrepInvalidRegexHint(t *testing.T) {
	g := &Grep{FS: grepFS()}
	res, _ := g.Execute(context.Background(), json.RawMessage(`{"pattern":"foo("}`))
	if !res.IsError || !strings.Contains(res.Content, "not valid regex") {
		t.Errorf("want actionable regex error, got:\n%s", res.Content)
	}
}
