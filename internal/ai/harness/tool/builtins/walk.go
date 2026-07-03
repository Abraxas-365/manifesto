package builtins

import (
	"context"
	"path"

	"github.com/Abraxas-365/manifesto/internal/fsx"
)

// MaxWalkFiles bounds a recursive walk to avoid runaway traversals.
const MaxWalkFiles = 20000

// walkFiles recursively lists file paths under root (relative to root, using
// forward slashes). Directories themselves are not emitted. The walk stops once
// maxWalkFiles paths are collected.
func walkFiles(ctx context.Context, fs fsx.FileSystem, root string) ([]string, error) {
	var files []string
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
				files = append(files, child)
				if len(files) >= MaxWalkFiles {
					break
				}
			}
		}
	}

	return files, nil
}
