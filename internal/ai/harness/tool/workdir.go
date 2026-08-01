package tool

import (
	"context"
	"path/filepath"
	"strings"
)

// workdirKey carries an isolated working directory (e.g. a git worktree)
// through tool execution context.
type workdirKey struct{}

type workdirVal struct {
	dir      string // isolated checkout root the tools should operate in
	mainRoot string // original repo root the isolation was forked from
}

// WithWorkdir returns a ctx carrying an isolated working directory (e.g. a git
// worktree) and the main root it was forked from. Builtin tools resolve paths
// against it so an isolated agent's file operations land inside dir instead of
// the main tree.
func WithWorkdir(ctx context.Context, dir, mainRoot string) context.Context {
	return context.WithValue(ctx, workdirKey{}, workdirVal{dir: dir, mainRoot: mainRoot})
}

// Workdir returns the isolated working directory stored by WithWorkdir
// ("" when none is set).
func Workdir(ctx context.Context) string {
	v, _ := ctx.Value(workdirKey{}).(workdirVal)
	return v.dir
}

// ResolvePath rewrites path so file tools operate inside the isolated working
// directory instead of the main tree:
//
//  1. Relative paths are resolved against the isolated dir rather than the
//     process CWD.
//  2. Absolute paths under the main root are rewritten to the equivalent path
//     under the isolated dir.
//
// Paths outside the main root (e.g. /etc/hosts, $HOME) and paths already
// inside the isolated dir are returned unchanged. When no workdir is set the
// original path is returned unchanged.
func ResolvePath(ctx context.Context, path string) string {
	v, _ := ctx.Value(workdirKey{}).(workdirVal)
	if v.dir == "" {
		return path
	}
	if !filepath.IsAbs(path) {
		return filepath.Join(v.dir, path)
	}
	sep := string(filepath.Separator)
	// Already inside the isolated dir — nothing to remap.
	if path == v.dir || strings.HasPrefix(path, v.dir+sep) {
		return path
	}
	if v.mainRoot == "" {
		return path
	}
	rel, err := filepath.Rel(v.mainRoot, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		// Symlink mismatch (macOS /var vs /private/var, symlinked project
		// dirs): the git-derived mainRoot is canonical while the model's path
		// uses the symbolic spelling, or vice versa. A missed match here
		// silently breaks isolation — the agent would edit the MAIN repo — so
		// retry with both sides canonicalized. The target file may not exist
		// yet (new Write), so canonicalize its directory.
		canonRoot, rootErr := filepath.EvalSymlinks(v.mainRoot)
		dir, base := filepath.Split(path)
		canonDir, dirErr := filepath.EvalSymlinks(filepath.Clean(dir))
		if rootErr == nil && dirErr == nil {
			canonPath := filepath.Join(canonDir, base)
			if r2, err2 := filepath.Rel(canonRoot, canonPath); err2 == nil && !strings.HasPrefix(r2, "..") {
				rel, err = r2, nil
			}
		}
	}
	if err != nil || strings.HasPrefix(rel, "..") {
		return path // outside the main root — do not remap
	}
	return filepath.Join(v.dir, rel)
}
