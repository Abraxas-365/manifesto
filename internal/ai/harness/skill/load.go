package skill

import (
	"context"
	"embed"

	"github.com/Abraxas-365/manifesto/internal/fsx"
)

// FromFS builds and loads a skill from an fsx.FileSystem directory.
func FromFS(ctx context.Context, filesystem fsx.FileSystem, dir string) (*Skill, error) {
	src := &FSSource{FS: filesystem, Dir: dir}
	meta, body, err := src.Load(ctx)
	if err != nil {
		return nil, err
	}
	return New(meta, body, src), nil
}

// FromEmbed builds and loads a skill from a Go embed.FS directory.
func FromEmbed(ctx context.Context, filesystem embed.FS, dir string) (*Skill, error) {
	src := &EmbedSource{FS: filesystem, Dir: dir}
	meta, body, err := src.Load(ctx)
	if err != nil {
		return nil, err
	}
	return New(meta, body, src), nil
}

// FromStatic builds a skill from an in-code Static source.
func FromStatic(ctx context.Context, s *Static) (*Skill, error) {
	meta, body, err := s.Load(ctx)
	if err != nil {
		return nil, err
	}
	return New(meta, body, s), nil
}

// LoadDir loads every immediate subdirectory of root that contains a SKILL.md
// as a skill.
func LoadDir(ctx context.Context, filesystem fsx.FileSystem, root string) ([]*Skill, error) {
	entries, err := filesystem.List(ctx, root)
	if err != nil {
		return nil, errRegistry.NewWithCause(ErrSkillLoad, err).WithDetail("dir", root)
	}
	var skills []*Skill
	for _, e := range entries {
		if !e.IsDir {
			continue
		}
		dir := filesystem.Join(root, e.Name)
		exists, err := filesystem.Exists(ctx, filesystem.Join(dir, skillFile))
		if err != nil || !exists {
			continue
		}
		sk, err := FromFS(ctx, filesystem, dir)
		if err != nil {
			return nil, err
		}
		skills = append(skills, sk)
	}
	return skills, nil
}
