package skill

import (
	"context"
	"embed"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Abraxas-365/manifesto/internal/fsx"
)

//go:embed testdata/embedskill
var embedFS embed.FS

// memFS is a minimal in-memory fsx.FileSystem for offline skill tests.
type memFS struct {
	files map[string][]byte
}

func newMemFS() *memFS { return &memFS{files: map[string][]byte{}} }

func (m *memFS) ReadFile(_ context.Context, path string) ([]byte, error) {
	data, ok := m.files[path]
	if !ok {
		return nil, fsErr("not found: " + path)
	}
	return data, nil
}
func (m *memFS) ReadFileStream(context.Context, string) (io.ReadCloser, error) {
	return nil, fsErr("unsupported")
}
func (m *memFS) Stat(_ context.Context, path string) (fsx.FileInfo, error) {
	data, ok := m.files[path]
	if !ok {
		return fsx.FileInfo{}, fsErr("not found: " + path)
	}
	return fsx.FileInfo{Name: path, Size: int64(len(data))}, nil
}
func (m *memFS) List(_ context.Context, path string) ([]fsx.FileInfo, error) {
	prefix := strings.TrimSuffix(path, "/") + "/"
	if path == "." || path == "" {
		prefix = ""
	}
	seen := map[string]bool{}
	var out []fsx.FileInfo
	for f := range m.files {
		rel, ok := strings.CutPrefix(f, prefix)
		if !ok {
			continue
		}
		if i := strings.IndexByte(rel, '/'); i >= 0 {
			dir := rel[:i]
			if !seen[dir] {
				seen[dir] = true
				out = append(out, fsx.FileInfo{Name: dir, IsDir: true})
			}
			continue
		}
		out = append(out, fsx.FileInfo{Name: rel, Size: int64(len(m.files[f]))})
	}
	return out, nil
}
func (m *memFS) Exists(_ context.Context, path string) (bool, error) {
	_, ok := m.files[path]
	return ok, nil
}
func (m *memFS) WriteFile(_ context.Context, path string, data []byte) error {
	m.files[path] = data
	return nil
}

func (m *memFS) WriteFileStream(context.Context, string, io.Reader) error {
	return fsErr("unsupported")
}
func (m *memFS) CreateDir(context.Context, string) error         { return nil }
func (m *memFS) DeleteFile(_ context.Context, path string) error { delete(m.files, path); return nil }
func (m *memFS) DeleteDir(context.Context, string, bool) error   { return nil }
func (m *memFS) Join(elem ...string) string                      { return strings.Join(elem, "/") }

type fsErr string

func (e fsErr) Error() string { return string(e) }

const sampleSKILL = `---
name: sample
description: A sample skill.
---

# Sample

Read ${SKILL_DIR}/references/errors.md for the details.
`

func newSampleFS() *memFS {
	m := newMemFS()
	m.files["skills/sample/SKILL.md"] = []byte(sampleSKILL)
	m.files["skills/sample/references/errors.md"] = []byte("errors reference body")
	return m
}

// --- parseFrontmatter ---

func TestParseFrontmatter(t *testing.T) {
	meta, body, err := parseFrontmatter(sampleSKILL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if meta.Name != "sample" || meta.Description != "A sample skill." {
		t.Fatalf("bad meta: %+v", meta)
	}
	if !strings.HasPrefix(strings.TrimSpace(body), "# Sample") {
		t.Fatalf("bad body: %q", body)
	}

	// No frontmatter -> whole thing is body.
	_, b2, err := parseFrontmatter("# just markdown\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(b2, "just markdown") {
		t.Fatalf("expected passthrough body, got %q", b2)
	}

	// Bad YAML.
	if _, _, err := parseFrontmatter("---\n: : bad\n---\nbody\n"); err == nil {
		t.Fatalf("expected error for bad yaml")
	}
}

// --- Static ---

func TestStaticMaterializeAndDir(t *testing.T) {
	st := &Static{
		Name:        "codeskill",
		Description: "An in-code skill.",
		Body:        "Body referencing ${SKILL_DIR}/references/a.md",
		References:  map[string][]byte{"references/a.md": []byte("alpha")},
	}
	sk, err := FromStatic(context.Background(), st)
	if err != nil {
		t.Fatal(err)
	}
	cache := NewCache("")
	defer cache.Close()

	dir, err := sk.Dir(context.Background(), cache)
	if err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "SKILL.md")); err != nil || !strings.Contains(string(b), "codeskill") {
		t.Fatalf("SKILL.md missing/wrong: %v %q", err, b)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "references", "a.md")); err != nil || string(b) != "alpha" {
		t.Fatalf("reference missing/wrong: %v %q", err, b)
	}

	// Dir is memoized: second call returns same path.
	dir2, err := sk.Dir(context.Background(), cache)
	if err != nil {
		t.Fatal(err)
	}
	if dir != dir2 {
		t.Fatalf("Dir not memoized: %q != %q", dir, dir2)
	}
}

// --- FSSource ---

func TestFSSourceLoadAndMaterialize(t *testing.T) {
	fs := newSampleFS()
	sk, err := FromFS(context.Background(), fs, "skills/sample")
	if err != nil {
		t.Fatal(err)
	}
	if sk.Name != "sample" {
		t.Fatalf("bad name %q", sk.Name)
	}
	cache := NewCache("")
	defer cache.Close()
	dir, err := sk.Dir(context.Background(), cache)
	if err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "references", "errors.md")); err != nil || string(b) != "errors reference body" {
		t.Fatalf("reference not materialized: %v %q", err, b)
	}
}

// --- EmbedSource ---

func TestEmbedSource(t *testing.T) {
	sk, err := FromEmbed(context.Background(), embedFS, "testdata/embedskill")
	if err != nil {
		t.Fatal(err)
	}
	if sk.Name != "embedded" {
		t.Fatalf("bad name %q", sk.Name)
	}
	cache := NewCache("")
	defer cache.Close()
	dir, err := sk.Dir(context.Background(), cache)
	if err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "references", "note.md")); err != nil || !strings.Contains(string(b), "embedded reference") {
		t.Fatalf("embed reference not materialized: %v %q", err, b)
	}
}

// --- Registry ---

func TestRegistry(t *testing.T) {
	r := NewRegistry()
	r.Register(New(Meta{Name: "Alpha", Description: "a"}, "body", &Static{Name: "Alpha"}))
	r.Register(New(Meta{Name: "beta", Description: "b"}, "body", &Static{Name: "beta"}))

	if _, ok := r.Get("alpha"); !ok {
		t.Fatal("case-insensitive Get failed")
	}
	if _, ok := r.Get("Beta"); !ok {
		t.Fatal("case-insensitive Get failed for beta")
	}
	if _, ok := r.Get("gamma"); ok {
		t.Fatal("unexpected hit for gamma")
	}
	names := r.Names()
	if len(names) != 2 || names[0] != "Alpha" || names[1] != "beta" {
		t.Fatalf("bad order: %v", names)
	}
	if len(r.All()) != 2 {
		t.Fatalf("All wrong length")
	}
}

// --- Tool ---

func newToolWithSample(t *testing.T) (*Tool, *Cache) {
	t.Helper()
	fs := newSampleFS()
	sk, err := FromFS(context.Background(), fs, "skills/sample")
	if err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry()
	reg.Register(sk)
	cache := NewCache("")
	return &Tool{Registry: reg, Cache: cache}, cache
}

func TestToolExecuteKnown(t *testing.T) {
	tl, cache := newToolWithSample(t)
	defer cache.Close()

	res, err := tl.Execute(context.Background(), json.RawMessage(`{"skill":"sample"}`))
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %q", res.Content)
	}
	if !strings.Contains(res.Content, "Base directory for this skill:") {
		t.Fatalf("missing base directory line: %q", res.Content)
	}
	// ${SKILL_DIR} substituted with the materialized dir, and that file exists.
	dir, err := (tl.Registry.All()[0]).Dir(context.Background(), cache)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(res.Content, "${SKILL_DIR}") {
		t.Fatalf("token not substituted: %q", res.Content)
	}
	if !strings.Contains(res.Content, dir) {
		t.Fatalf("content missing dir %q: %q", dir, res.Content)
	}
	if _, err := os.Stat(filepath.Join(dir, "references", "errors.md")); err != nil {
		t.Fatalf("reference file not on disk: %v", err)
	}
}

func TestToolExecuteUnknown(t *testing.T) {
	tl, cache := newToolWithSample(t)
	defer cache.Close()

	res, err := tl.Execute(context.Background(), json.RawMessage(`{"skill":"nope"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatalf("expected error result")
	}
	if !strings.Contains(res.Content, "sample") {
		t.Fatalf("expected available names listed: %q", res.Content)
	}
}

func TestToolDescriptionListsSkills(t *testing.T) {
	tl, cache := newToolWithSample(t)
	defer cache.Close()
	desc := tl.Description()
	if !strings.Contains(desc, "sample") || !strings.Contains(desc, "A sample skill.") {
		t.Fatalf("description missing skill listing: %q", desc)
	}
}

// --- Cache ---

func TestCacheEphemeralRemovedOnClose(t *testing.T) {
	c := NewCache("")
	p := c.Path("x")
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	root := c.BaseDir
	if root == "" {
		t.Fatal("expected ephemeral root")
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("root should exist: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("ephemeral root should be removed, stat err=%v", err)
	}
}

func TestCachePersistentNotRemoved(t *testing.T) {
	base := t.TempDir()
	c := NewCache(base)
	p := c.Path("y")
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(base); err != nil {
		t.Fatalf("caller-supplied BaseDir must be preserved: %v", err)
	}
	// Deterministic Path.
	c2 := NewCache(base)
	if c2.Path("y") != filepath.Join(base, "y") {
		t.Fatalf("Path not deterministic")
	}
}

var _ fsx.FileSystem = (*memFS)(nil)
