// Package fsxstore adapts an fsx.FileSystem (local disk, S3, ...) to the narrow
// fsys.Store the built-in file tools depend on. It is the only harness code that
// imports internal/fsx, keeping the core decoupled from that package.
package fsxstore

import (
	"context"
	"errors"
	"strings"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/fsys"
	"github.com/Abraxas-365/manifesto/internal/fsx"
)

// store wraps an fsx.FileSystem as a fsys.Store.
type store struct {
	fs fsx.FileSystem
}

// New adapts an fsx.FileSystem to a fsys.Store.
func New(fs fsx.FileSystem) fsys.Store { return &store{fs: fs} }

func (s *store) ReadFile(ctx context.Context, path string) ([]byte, error) {
	data, err := s.fs.ReadFile(ctx, path)
	return data, mapErr(err)
}

func (s *store) WriteFile(ctx context.Context, path string, data []byte) error {
	return s.fs.WriteFile(ctx, path, data)
}

func (s *store) Stat(ctx context.Context, path string) (fsys.FileInfo, error) {
	info, err := s.fs.Stat(ctx, path)
	if err != nil {
		return fsys.FileInfo{}, mapErr(err)
	}
	return fsys.FileInfo{Name: info.Name, Size: info.Size, IsDir: info.IsDir, ModTime: info.ModTime}, nil
}

func (s *store) List(ctx context.Context, path string) ([]fsys.FileInfo, error) {
	entries, err := s.fs.List(ctx, path)
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]fsys.FileInfo, len(entries))
	for i, e := range entries {
		out[i] = fsys.FileInfo{Name: e.Name, Size: e.Size, IsDir: e.IsDir, ModTime: e.ModTime}
	}
	return out, nil
}

func (s *store) MkdirAll(ctx context.Context, path string) error {
	return s.fs.CreateDir(ctx, path)
}

// mapErr best-effort translates fsx not-found errors (which are plain formatted
// strings, not sentinels) to fsys.ErrNotExist so tools can detect absence.
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "not found") || strings.Contains(msg, "no such file") ||
		strings.Contains(msg, "does not exist") {
		return errors.Join(fsys.ErrNotExist, err)
	}
	return err
}
