package skill

import (
	"context"
	"embed"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Abraxas-365/manifesto/internal/fsx"
	"github.com/Abraxas-365/manifesto/internal/fsx/fsxlocal"
)

const skillFile = "SKILL.md"

// FSSource loads a skill from an fsx.FileSystem (local disk or S3).
type FSSource struct {
	FS  fsx.FileSystem
	Dir string
}

// Load reads and parses SKILL.md.
func (s *FSSource) Load(ctx context.Context) (Meta, string, error) {
	data, err := s.FS.ReadFile(ctx, s.FS.Join(s.Dir, skillFile))
	if err != nil {
		return Meta{}, "", errRegistry.NewWithCause(ErrSkillLoad, err).WithDetail("dir", s.Dir)
	}
	return parseFrontmatter(string(data))
}

// Materialize copies every file under Dir into destDir, preserving structure.
func (s *FSSource) Materialize(ctx context.Context, destDir string) error {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	return s.copyDir(ctx, s.Dir, destDir)
}

func (s *FSSource) copyDir(ctx context.Context, srcDir, destDir string) error {
	entries, err := s.FS.List(ctx, srcDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		srcPath := s.FS.Join(srcDir, e.Name)
		destPath := filepath.Join(destDir, e.Name)
		if e.IsDir {
			if err := os.MkdirAll(destPath, 0o755); err != nil {
				return err
			}
			if err := s.copyDir(ctx, srcPath, destPath); err != nil {
				return err
			}
			continue
		}
		data, err := s.FS.ReadFile(ctx, srcPath)
		if err != nil {
			return err
		}
		if err := os.WriteFile(destPath, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// LocalPath exposes the real directory when the backing FS is local, letting
// Dir() skip materialization.
func (s *FSSource) LocalPath() (string, bool) {
	if lfs, ok := s.FS.(*fsxlocal.LocalFileSystem); ok {
		return lfs.AbsPath(s.Dir), true
	}
	return "", false
}

// EmbedSource loads a skill from a Go embed.FS.
type EmbedSource struct {
	FS  embed.FS
	Dir string
}

// Load reads and parses SKILL.md from the embedded filesystem.
func (s *EmbedSource) Load(_ context.Context) (Meta, string, error) {
	data, err := s.FS.ReadFile(path.Join(s.Dir, skillFile))
	if err != nil {
		return Meta{}, "", errRegistry.NewWithCause(ErrSkillLoad, err).WithDetail("dir", s.Dir)
	}
	return parseFrontmatter(string(data))
}

// Materialize copies every embedded entry under Dir into destDir.
func (s *EmbedSource) Materialize(_ context.Context, destDir string) error {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	return fs.WalkDir(s.FS, s.Dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(s.Dir, p)
		if err != nil {
			return err
		}
		dest := filepath.Join(destDir, rel)
		if d.IsDir() {
			return os.MkdirAll(dest, 0o755)
		}
		data, err := s.FS.ReadFile(p)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dest, data, 0o644)
	})
}

// Static is a pure in-code skill. References maps a relative path (e.g.
// "references/errors.md") to its bytes; Materialize writes them onto disk so the
// agent can Bash/Read them exactly like an fsx- or embed-backed skill.
type Static struct {
	Name        string
	Description string
	Body        string
	References  map[string][]byte
}

// Load returns the in-code metadata and body with zero I/O.
func (s *Static) Load(_ context.Context) (Meta, string, error) {
	return Meta{Name: s.Name, Description: s.Description}, s.Body, nil
}

// Materialize writes a reconstructed SKILL.md plus every reference file.
func (s *Static) Materialize(_ context.Context, destDir string) error {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(destDir, skillFile), []byte(s.renderSkillMD()), 0o644); err != nil {
		return err
	}
	// Deterministic order for reproducible materialization.
	keys := make([]string, 0, len(s.References))
	for k := range s.References {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		dest := filepath.Join(destDir, filepath.FromSlash(k))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dest, s.References[k], 0o644); err != nil {
			return err
		}
	}
	return nil
}

func (s *Static) renderSkillMD() string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("name: ")
	b.WriteString(s.Name)
	b.WriteByte('\n')
	b.WriteString("description: ")
	b.WriteString(s.Description)
	b.WriteByte('\n')
	b.WriteString("---\n\n")
	b.WriteString(s.Body)
	return b.String()
}
