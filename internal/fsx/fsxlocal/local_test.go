package fsxlocal

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Absolute paths must bypass basePath rooting (legacy tool contract: Read/Glob/
// Grep can reach any file on the machine). Relative paths stay rooted.
func TestFullPath_AbsolutePassthrough(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	fs, err := NewLocalFileSystem(root)
	if err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(outside, "x.txt")
	if err := os.WriteFile(target, []byte("outside content"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := fs.ReadFile(context.Background(), target)
	if err != nil {
		t.Fatalf("absolute path outside root must be readable: %v", err)
	}
	if string(got) != "outside content" {
		t.Fatalf("got %q", got)
	}

	if p := fs.AbsPath("rel/file.go"); p != filepath.Join(fs.basePath, "rel/file.go") {
		t.Fatalf("relative path must stay rooted, got %q", p)
	}
	if p := fs.AbsPath(target); p != target {
		t.Fatalf("absolute path must pass through, got %q", p)
	}
}

func TestFullPath_RelativeStaysRooted(t *testing.T) {
	root := t.TempDir()
	fs, err := NewLocalFileSystem(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "in.txt"), []byte("inside"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := fs.ReadFile(context.Background(), "in.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "inside" {
		t.Fatalf("got %q", got)
	}
}
