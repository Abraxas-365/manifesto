package builtins

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/fsys"
)

// memFS is a minimal in-memory fsys.Store for offline tool tests.
type memFS struct {
	files  map[string][]byte
	mtimes map[string]time.Time
}

func newMemFS() *memFS {
	return &memFS{files: map[string][]byte{}, mtimes: map[string]time.Time{}}
}

func (m *memFS) ReadFile(_ context.Context, path string) ([]byte, error) {
	data, ok := m.files[path]
	if !ok {
		return nil, fsys.ErrNotExist
	}
	return data, nil
}

func (m *memFS) Stat(_ context.Context, path string) (fsys.FileInfo, error) {
	data, ok := m.files[path]
	if !ok {
		return fsys.FileInfo{}, fsys.ErrNotExist
	}
	return fsys.FileInfo{Name: path, Size: int64(len(data)), ModTime: m.mtimes[path]}, nil
}

func (m *memFS) List(_ context.Context, path string) ([]fsys.FileInfo, error) {
	prefix := strings.TrimSuffix(path, "/") + "/"
	if path == "." || path == "" {
		prefix = ""
	}
	seen := map[string]bool{}
	var out []fsys.FileInfo
	for f := range m.files {
		rel, ok := strings.CutPrefix(f, prefix)
		if !ok {
			continue
		}
		// Immediate child only.
		if i := strings.IndexByte(rel, '/'); i >= 0 {
			dir := rel[:i]
			if !seen[dir] {
				seen[dir] = true
				out = append(out, fsys.FileInfo{Name: dir, IsDir: true})
			}
			continue
		}
		out = append(out, fsys.FileInfo{Name: rel, Size: int64(len(m.files[f])), ModTime: m.mtimes[f]})
	}
	return out, nil
}

func (m *memFS) WriteFile(_ context.Context, path string, data []byte) error {
	m.files[path] = data
	return nil
}

func (m *memFS) MkdirAll(context.Context, string) error { return nil }

// --- matchGlob ---

func TestMatchGlob(t *testing.T) {
	cases := []struct {
		pattern string
		name    string
		want    bool
	}{
		{"*.go", "main.go", true},
		{"*.go", "main.rs", false},
		{"*.go", "sub/main.go", false}, // * does not cross segments
		{"**/*.go", "sub/main.go", true},
		{"**/*.go", "a/b/c/main.go", true},
		{"**/*.go", "main.go", true}, // ** matches zero segments
		{"src/**/*.ts", "src/a/b.ts", true},
		{"src/**/*.ts", "lib/a/b.ts", false},
		{"src/*.ts", "src/a.ts", true},
		{"src/*.ts", "src/a/b.ts", false},
		{"file?.txt", "file1.txt", true},
		{"file?.txt", "file12.txt", false},
		{"**", "any/deep/path.txt", true},
		{"a/**/b", "a/b", true},
		{"a/**/b", "a/x/y/b", true},
		{"a/**/b", "a/x/y/c", false},
	}
	for _, c := range cases {
		if got := matchGlob(c.pattern, c.name); got != c.want {
			t.Errorf("matchGlob(%q, %q) = %v, want %v", c.pattern, c.name, got, c.want)
		}
	}
}

// --- Edit ---

func TestEdit_UniqueReplace(t *testing.T) {
	fs := newMemFS()
	fs.files["f.txt"] = []byte("hello world")
	e := &Edit{FS: fs}

	in, _ := json.Marshal(map[string]any{
		"file_path": "f.txt", "old_string": "world", "new_string": "gophers",
	})
	res, err := e.Execute(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %s", res.Content)
	}
	if got := string(fs.files["f.txt"]); got != "hello gophers" {
		t.Fatalf("got %q", got)
	}
}

func TestEdit_AmbiguousWithoutReplaceAll(t *testing.T) {
	fs := newMemFS()
	fs.files["f.txt"] = []byte("x x x")
	e := &Edit{FS: fs}

	in, _ := json.Marshal(map[string]any{
		"file_path": "f.txt", "old_string": "x", "new_string": "y",
	})
	res, _ := e.Execute(context.Background(), in)
	if !res.IsError {
		t.Fatal("expected ambiguity error when old_string not unique")
	}
	// File unchanged.
	if string(fs.files["f.txt"]) != "x x x" {
		t.Fatal("file should not be modified on ambiguous edit")
	}
}

func TestEdit_ReplaceAll(t *testing.T) {
	fs := newMemFS()
	fs.files["f.txt"] = []byte("x x x")
	e := &Edit{FS: fs}

	in, _ := json.Marshal(map[string]any{
		"file_path": "f.txt", "old_string": "x", "new_string": "y", "replace_all": true,
	})
	res, err := e.Execute(context.Background(), in)
	if err != nil || res.IsError {
		t.Fatalf("unexpected: err=%v res=%+v", err, res)
	}
	if string(fs.files["f.txt"]) != "y y y" {
		t.Fatalf("got %q", string(fs.files["f.txt"]))
	}
}

func TestEdit_NotFound(t *testing.T) {
	fs := newMemFS()
	fs.files["f.txt"] = []byte("hello")
	e := &Edit{FS: fs}

	in, _ := json.Marshal(map[string]any{
		"file_path": "f.txt", "old_string": "absent", "new_string": "z",
	})
	res, _ := e.Execute(context.Background(), in)
	if !res.IsError {
		t.Fatal("expected not-found error")
	}
}

// --- Read ---

func TestRead_Basic(t *testing.T) {
	fs := newMemFS()
	fs.files["f.txt"] = []byte("line1\nline2\nline3")
	r := &Read{FS: fs}

	in, _ := json.Marshal(map[string]any{"file_path": "f.txt"})
	res, err := r.Execute(context.Background(), in)
	if err != nil || res.IsError {
		t.Fatalf("unexpected: err=%v res=%+v", err, res)
	}
	// cat -n style: "1\tline1\n"
	if !strings.Contains(res.Content, "1\tline1") || !strings.Contains(res.Content, "3\tline3") {
		t.Fatalf("unexpected content:\n%s", res.Content)
	}
}

func TestRead_OffsetLimit(t *testing.T) {
	fs := newMemFS()
	fs.files["f.txt"] = []byte("a\nb\nc\nd\ne")
	r := &Read{FS: fs}

	in, _ := json.Marshal(map[string]any{"file_path": "f.txt", "offset": 2, "limit": 2})
	res, err := r.Execute(context.Background(), in)
	if err != nil || res.IsError {
		t.Fatalf("unexpected: err=%v res=%+v", err, res)
	}
	if !strings.Contains(res.Content, "2\tb") || !strings.Contains(res.Content, "3\tc") {
		t.Fatalf("expected lines 2-3, got:\n%s", res.Content)
	}
	if strings.Contains(res.Content, "1\ta") || strings.Contains(res.Content, "4\td") {
		t.Fatalf("range leaked extra lines:\n%s", res.Content)
	}
	if !strings.Contains(res.Content, "truncated at line 4") {
		t.Fatalf("expected truncation notice, got:\n%s", res.Content)
	}
}

func TestRead_Binary(t *testing.T) {
	fs := newMemFS()
	fs.files["b.bin"] = []byte{0x00, 0x01, 0x02}
	r := &Read{FS: fs}

	in, _ := json.Marshal(map[string]any{"file_path": "b.bin"})
	res, _ := r.Execute(context.Background(), in)
	if !strings.Contains(res.Content, "binary") {
		t.Fatalf("expected binary notice, got: %s", res.Content)
	}
}

func TestRead_Missing(t *testing.T) {
	r := &Read{FS: newMemFS()}
	in, _ := json.Marshal(map[string]any{"file_path": "nope.txt"})
	res, _ := r.Execute(context.Background(), in)
	if !res.IsError {
		t.Fatal("expected error for missing file")
	}
}
