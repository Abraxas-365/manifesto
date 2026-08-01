package tool

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePath_NoWorkdir(t *testing.T) {
	ctx := context.Background()
	for _, p := range []string{"main.go", "/abs/path.go", "."} {
		if got := ResolvePath(ctx, p); got != p {
			t.Errorf("ResolvePath(%q) = %q, want unchanged", p, got)
		}
	}
}

func TestResolvePath_Relative(t *testing.T) {
	ctx := WithWorkdir(context.Background(), "/wt", "/main")
	if got := ResolvePath(ctx, "internal/a.go"); got != filepath.Join("/wt", "internal/a.go") {
		t.Errorf("relative path not resolved against workdir: %q", got)
	}
	if got := ResolvePath(ctx, "."); got != "/wt" {
		t.Errorf("'.' should resolve to workdir, got %q", got)
	}
}

func TestResolvePath_AbsoluteUnderMainRoot(t *testing.T) {
	// Use real dirs so the symlink-canonicalization fallback has something to
	// stat without erroring on nonexistent paths.
	main := t.TempDir()
	wt := t.TempDir()
	if err := os.MkdirAll(filepath.Join(main, "internal"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx := WithWorkdir(context.Background(), wt, main)

	got := ResolvePath(ctx, filepath.Join(main, "internal", "a.go"))
	want := filepath.Join(wt, "internal", "a.go")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolvePath_AbsoluteOutsideMainRoot(t *testing.T) {
	main := t.TempDir()
	wt := t.TempDir()
	ctx := WithWorkdir(context.Background(), wt, main)

	outside := filepath.Join(t.TempDir(), "hosts")
	if got := ResolvePath(ctx, outside); got != outside {
		t.Errorf("path outside main root must not be remapped: %q", got)
	}
}

func TestResolvePath_AlreadyInWorkdir(t *testing.T) {
	main := t.TempDir()
	wt := t.TempDir()
	ctx := WithWorkdir(context.Background(), wt, main)

	inside := filepath.Join(wt, "a.go")
	if got := ResolvePath(ctx, inside); got != inside {
		t.Errorf("path already in workdir must not be remapped: %q", got)
	}
	if got := ResolvePath(ctx, wt); got != wt {
		t.Errorf("workdir itself must not be remapped: %q", got)
	}
}

func TestResolvePath_SymlinkedMainRoot(t *testing.T) {
	// The model may spell the main root via a symlink while mainRoot is
	// canonical (or vice versa). Both must still remap into the workdir.
	real := t.TempDir()
	if err := os.MkdirAll(filepath.Join(real, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	wt := t.TempDir()

	ctx := WithWorkdir(context.Background(), wt, real)
	got := ResolvePath(ctx, filepath.Join(link, "src", "a.go"))
	want := filepath.Join(wt, "src", "a.go")
	if got != want {
		t.Errorf("symlinked spelling not remapped: got %q, want %q", got, want)
	}
}

func TestWorkdir(t *testing.T) {
	if Workdir(context.Background()) != "" {
		t.Error("Workdir on bare ctx should be empty")
	}
	ctx := WithWorkdir(context.Background(), "/wt", "/main")
	if Workdir(ctx) != "/wt" {
		t.Errorf("Workdir = %q, want /wt", Workdir(ctx))
	}
}

// Worktrees live in $TMPDIR, completely outside mainRoot. Sibling worktree
// paths must not be remapped into the current worktree — they're outside
// mainRoot and should be returned unchanged.
func TestResolvePath_SiblingWorktreeInTmpdir(t *testing.T) {
	mainRoot := t.TempDir() // e.g. /home/user/project
	myWt := filepath.Join(os.TempDir(), "claudio-worktree-task-1")
	siblingWt := filepath.Join(os.TempDir(), "claudio-worktree-task-2")
	if err := os.MkdirAll(myWt, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(myWt) })

	ctx := WithWorkdir(context.Background(), myWt, mainRoot)

	// Sibling worktree path is outside mainRoot → should NOT be remapped.
	sibPath := filepath.Join(siblingWt, "internal", "foo.go")
	if got := ResolvePath(ctx, sibPath); got != sibPath {
		t.Errorf("sibling worktree path in $TMPDIR must not be remapped: got %q", got)
	}
}

// Path to mainRoot itself should remap to worktree root.
func TestResolvePath_MainRootItself(t *testing.T) {
	main := t.TempDir()
	wt := t.TempDir()
	ctx := WithWorkdir(context.Background(), wt, main)

	if got := ResolvePath(ctx, main); got != wt {
		t.Errorf("mainRoot itself should remap to worktree root: got %q, want %q", got, wt)
	}
}

// Empty mainRoot means no absolute path remapping — only relative paths join.
func TestResolvePath_EmptyMainRoot(t *testing.T) {
	wt := t.TempDir()
	ctx := WithWorkdir(context.Background(), wt, "")

	// Relative still joins.
	if got := ResolvePath(ctx, "a.go"); got != filepath.Join(wt, "a.go") {
		t.Errorf("relative with empty mainRoot: got %q", got)
	}
	// Absolute stays unchanged (no mainRoot to match against).
	abs := "/some/random/path"
	if got := ResolvePath(ctx, abs); got != abs {
		t.Errorf("absolute with empty mainRoot should pass through: got %q", got)
	}
}
