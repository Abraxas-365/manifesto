// Package fsys defines the narrow filesystem interface the built-in file tools
// depend on. It is intentionally a strict subset of a POSIX filesystem —
// exactly what Read/Write/Edit/List/Glob/Grep use, nothing more — so the harness
// does not couple to any particular storage package and an implementation can be
// derived from a shell executor (see fsys/execstore) to keep file tools and Bash
// in the same environment.
package fsys

import (
	"context"
	"errors"
)

// FileInfo is the minimal metadata the file tools need.
type FileInfo struct {
	Name  string
	Size  int64
	IsDir bool
}

// Store is the narrow filesystem backing the built-in file tools.
type Store interface {
	ReadFile(ctx context.Context, path string) ([]byte, error)
	WriteFile(ctx context.Context, path string, data []byte) error
	Stat(ctx context.Context, path string) (FileInfo, error)
	List(ctx context.Context, path string) ([]FileInfo, error)
	MkdirAll(ctx context.Context, path string) error
}

// ErrNotExist is returned (possibly wrapped) by a Store when a path is absent,
// so tools can distinguish "missing" from transport errors.
var ErrNotExist = errors.New("file does not exist")
