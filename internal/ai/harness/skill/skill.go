package skill

import (
	"context"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// Meta is a skill's frontmatter metadata.
type Meta struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// Source loads a skill's content and can materialize it onto local disk.
type Source interface {
	// Load returns the skill's metadata and Markdown body. It should be cheap
	// (no reference files copied).
	Load(ctx context.Context) (Meta, string, error)
	// Materialize writes all of the skill's files (SKILL.md + references) under
	// destDir so the agent can Bash/Read them.
	Materialize(ctx context.Context, destDir string) error
}

// localPathSource is an optional fast-path: a Source already backed by a real
// local directory can expose it so Dir() skips materialization entirely.
type localPathSource interface {
	// LocalPath returns the real on-disk directory and true when available.
	LocalPath() (string, bool)
}

// Skill is a loaded skill: metadata, body, and its backing source.
type Skill struct {
	Meta
	Body   string
	source Source

	mu      sync.Mutex
	dir     string // memoized materialized (or real) directory
	dirDone bool
}

// New builds a Skill from already-loaded metadata, body and source.
func New(meta Meta, body string, source Source) *Skill {
	return &Skill{Meta: meta, Body: body, source: source}
}

// Source returns the skill's backing source.
func (s *Skill) Source() Source { return s.source }

// Dir returns a local directory containing the skill's files, materializing it
// once via the cache if needed. Local sources short-circuit to their real path.
// The result is memoized for the life of the Skill.
func (s *Skill) Dir(ctx context.Context, cache *Cache) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.dirDone {
		return s.dir, nil
	}

	// Fast-path: source already on local disk.
	if lp, ok := s.source.(localPathSource); ok {
		if p, ok := lp.LocalPath(); ok {
			s.dir = p
			s.dirDone = true
			return p, nil
		}
	}

	if cache == nil {
		cache = NewCache("")
	}
	dest := cache.Path(s.Name)
	if err := s.source.Materialize(ctx, dest); err != nil {
		return "", errRegistry.NewWithCause(ErrSkillLoad, err).WithDetail("skill", s.Name)
	}
	s.dir = dest
	s.dirDone = true
	return dest, nil
}

// parseFrontmatter splits a leading `---`…`---` YAML block from the Markdown
// body. When no frontmatter is present, the whole input is treated as the body.
func parseFrontmatter(md string) (Meta, string, error) {
	s := strings.TrimLeft(md, "\ufeff \t\r\n")
	if !strings.HasPrefix(s, "---") {
		return Meta{}, md, nil
	}
	// Drop the opening delimiter line.
	rest := s[len("---"):]
	rest = strings.TrimPrefix(rest, "\r")
	rest = strings.TrimPrefix(rest, "\n")

	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return Meta{}, md, nil
	}
	yamlBlock := rest[:idx]
	body := rest[idx+len("\n---"):]
	// Trim the remainder of the closing delimiter line.
	if nl := strings.IndexByte(body, '\n'); nl >= 0 {
		body = body[nl+1:]
	} else {
		body = ""
	}

	var meta Meta
	if err := yaml.Unmarshal([]byte(yamlBlock), &meta); err != nil {
		return Meta{}, "", errRegistry.NewWithCause(ErrParseFrontmatter, err)
	}
	return meta, body, nil
}
