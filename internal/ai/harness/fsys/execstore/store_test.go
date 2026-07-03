package execstore

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/exec"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/fsys"
)

// newStore returns an execstore rooted at a fresh temp dir, backed by a real
// local shell. These tests require a POSIX shell with coreutils + GNU find.
func newStore(t *testing.T) fsys.Store {
	t.Helper()
	return New(exec.NewLocalExecutor(t.TempDir()))
}

func TestExecStore_WriteReadRoundTrip(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	want := []byte("hello\nworld\n")
	if err := s.WriteFile(ctx, "notes.txt", want); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := s.ReadFile(ctx, "notes.txt")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("round-trip mismatch: got %q want %q", got, want)
	}
}

func TestExecStore_BinarySafe(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	// Bytes including NUL and high bytes must survive base64 transfer intact.
	want := []byte{0x00, 0x01, 0x02, 0xff, 0xfe, '\n', 0x00, 'a'}
	if err := s.WriteFile(ctx, "blob.bin", want); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := s.ReadFile(ctx, "blob.bin")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("binary mismatch: got %v want %v", got, want)
	}
}

func TestExecStore_WriteCreatesParentDirs(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	if err := s.WriteFile(ctx, "a/b/c/deep.txt", []byte("x")); err != nil {
		t.Fatalf("WriteFile nested: %v", err)
	}
	got, err := s.ReadFile(ctx, "a/b/c/deep.txt")
	if err != nil || string(got) != "x" {
		t.Fatalf("nested read: got %q err %v", got, err)
	}
}

func TestExecStore_StatFile(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	_ = s.WriteFile(ctx, "sized.txt", []byte("12345"))
	info, err := s.Stat(ctx, "sized.txt")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.IsDir {
		t.Fatal("expected file, got dir")
	}
	if info.Size != 5 {
		t.Fatalf("size = %d, want 5", info.Size)
	}
	if info.Name != "sized.txt" {
		t.Fatalf("name = %q, want sized.txt", info.Name)
	}
}

func TestExecStore_StatDir(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	_ = s.MkdirAll(ctx, "somedir")
	info, err := s.Stat(ctx, "somedir")
	if err != nil {
		t.Fatalf("Stat dir: %v", err)
	}
	if !info.IsDir {
		t.Fatal("expected dir")
	}
}

func TestExecStore_StatMissing(t *testing.T) {
	s := newStore(t)
	_, err := s.Stat(context.Background(), "nope.txt")
	if !errors.Is(err, fsys.ErrNotExist) {
		t.Fatalf("expected ErrNotExist, got %v", err)
	}
}

func TestExecStore_ReadMissing(t *testing.T) {
	s := newStore(t)
	_, err := s.ReadFile(context.Background(), "nope.txt")
	if !errors.Is(err, fsys.ErrNotExist) {
		t.Fatalf("expected ErrNotExist, got %v", err)
	}
}

func TestExecStore_List(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	_ = s.WriteFile(ctx, "root.txt", []byte("aa"))
	_ = s.MkdirAll(ctx, "sub")
	_ = s.WriteFile(ctx, "sub/child.txt", []byte("bbb"))

	entries, err := s.List(ctx, ".")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	byName := map[string]fsys.FileInfo{}
	for _, e := range entries {
		byName[e.Name] = e
	}
	f, ok := byName["root.txt"]
	if !ok || f.IsDir || f.Size != 2 {
		t.Fatalf("root.txt entry wrong: %+v ok=%v", f, ok)
	}
	d, ok := byName["sub"]
	if !ok || !d.IsDir {
		t.Fatalf("sub dir entry wrong: %+v ok=%v", d, ok)
	}
}

func TestExecStore_WriteTooLarge(t *testing.T) {
	s := newStore(t)
	big := make([]byte, MaxWriteBytes+1)
	err := s.WriteFile(context.Background(), "big.bin", big)
	if err == nil {
		t.Fatal("expected error for oversized write")
	}
}

func TestExecStore_PathWithSpacesAndQuotes(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	name := "weird 'name'.txt"
	if err := s.WriteFile(ctx, name, []byte("ok")); err != nil {
		t.Fatalf("WriteFile quoted: %v", err)
	}
	got, err := s.ReadFile(ctx, name)
	if err != nil || string(got) != "ok" {
		t.Fatalf("quoted read: got %q err %v", got, err)
	}
}
