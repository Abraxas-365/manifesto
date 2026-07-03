package skill

import (
	"os"
	"path/filepath"
	"sync"
)

// Cache owns the local directories that skills are materialized into. It gives
// materialization an explicit, reusable lifecycle instead of leaking anonymous
// temp dirs:
//
//   - A deterministic per-skill subdir (keyed by skill name) makes
//     re-materialization idempotent and lets a persistent BaseDir be reused
//     across process restarts (no repeated S3 downloads).
//   - Close removes only a dir the Cache itself created (ephemeral); a
//     caller-supplied BaseDir is treated as persistent and never deleted.
//   - Being the single owner, the Cache is the natural seam for future
//     TTL/eviction/hash-keying without touching Sources or the Tool.
type Cache struct {
	// BaseDir is the root for materialized skills. Empty means a per-process
	// temp dir is created lazily on first use.
	BaseDir string

	mu        sync.Mutex
	ephemeral bool // true when BaseDir was created by this Cache
	resolved  bool // true once BaseDir has been resolved/created
}

// NewCache returns a Cache rooted at baseDir. An empty baseDir yields an
// ephemeral cache backed by an os.MkdirTemp directory created on first use and
// removed on Close.
func NewCache(baseDir string) *Cache {
	return &Cache{BaseDir: baseDir}
}

// root resolves (and lazily creates) the cache's base directory.
func (c *Cache) root() (string, error) {
	if c.resolved {
		return c.BaseDir, nil
	}
	if c.BaseDir == "" {
		dir, err := os.MkdirTemp("", "harness-skills-*")
		if err != nil {
			return "", err
		}
		c.BaseDir = dir
		c.ephemeral = true
	} else if err := os.MkdirAll(c.BaseDir, 0o755); err != nil {
		return "", err
	}
	c.resolved = true
	return c.BaseDir, nil
}

// Path returns the deterministic directory for a named skill, creating the
// cache root if necessary. The returned path is stable for a given name so
// re-materialization overwrites in place.
func (c *Cache) Path(name string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	root, err := c.root()
	if err != nil {
		// Fall back to a temp path; Materialize will surface the real error.
		return filepath.Join(os.TempDir(), "harness-skills", name)
	}
	return filepath.Join(root, name)
}

// Close removes the cache directory only if it was created by this Cache
// (ephemeral). It is safe to call multiple times.
func (c *Cache) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ephemeral && c.resolved && c.BaseDir != "" {
		err := os.RemoveAll(c.BaseDir)
		c.resolved = false
		c.ephemeral = false
		c.BaseDir = ""
		return err
	}
	return nil
}
