package builtins

import (
	"context"
	"path"
	"time"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/fsys"
)

// MaxWalkFiles bounds a recursive walk to avoid runaway traversals.
const MaxWalkFiles = 20000

// fileEntry is one walked file: its path plus the mtime the listing reported
// (zero when the backend has none).
type fileEntry struct {
	Path    string
	ModTime time.Time
}

// walkFiles recursively lists file paths under root. Directories themselves
// are not emitted. The walk stops once MaxWalkFiles paths are collected.
func walkFiles(ctx context.Context, fs fsys.Store, root string) ([]string, error) {
	entries, err := walkEntries(ctx, fs, root)
	if err != nil {
		return nil, err
	}
	files := make([]string, len(entries))
	for i, e := range entries {
		files[i] = e.Path
	}
	return files, nil
}

// walkEntries is walkFiles with per-file mtimes preserved.
func walkEntries(ctx context.Context, fs fsys.Store, root string) ([]fileEntry, error) {
	var files []fileEntry
	queue := []string{root}

	for len(queue) > 0 && len(files) < MaxWalkFiles {
		dir := queue[0]
		queue = queue[1:]

		entries, err := fs.List(ctx, dir)
		if err != nil {
			// Skip directories we cannot read rather than aborting the walk.
			continue
		}
		for _, e := range entries {
			child := path.Join(dir, e.Name)
			if e.IsDir {
				queue = append(queue, child)
			} else {
				files = append(files, fileEntry{Path: child, ModTime: e.ModTime})
				if len(files) >= MaxWalkFiles {
					break
				}
			}
		}
	}

	return files, nil
}
