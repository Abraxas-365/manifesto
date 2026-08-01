package skill

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func writeSkill(t *testing.T, root, dir, content string) {
	t.Helper()
	d := filepath.Join(root, dir)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverProjectOverridesUser(t *testing.T) {
	user := t.TempDir()
	proj := t.TempDir()
	writeSkill(t, user, "commit", "---\nname: commit\ndescription: user version\n---\nuser body")
	writeSkill(t, user, "review", "---\nname: review\ndescription: reviews\n---\nreview body")
	writeSkill(t, proj, "commit", "---\nname: commit\ndescription: project version\n---\nproject body")

	reg := Discover(context.Background(), user, proj)

	if got := len(reg.All()); got != 2 {
		t.Fatalf("want 2 skills, got %d: %v", got, reg.Names())
	}
	commit, ok := reg.Get("commit")
	if !ok {
		t.Fatal("commit skill missing")
	}
	if commit.Description != "project version" || commit.Body != "project body" {
		t.Errorf("project must override user: %+v", commit.Meta)
	}
	if _, ok := reg.Get("review"); !ok {
		t.Error("user-only skill missing")
	}
}

func TestDiscoverSkipsMalformedAndNonSkillDirs(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "good", "---\nname: good\ndescription: fine\n---\nbody")
	writeSkill(t, root, "bad", "---\nname: [unclosed\n---\nbody") // invalid yaml
	// Dir without SKILL.md is ignored.
	if err := os.MkdirAll(filepath.Join(root, "notaskill"), 0o755); err != nil {
		t.Fatal(err)
	}

	reg := Discover(context.Background(), root)
	if got := reg.Names(); len(got) != 1 || got[0] != "good" {
		t.Fatalf("want [good], got %v", got)
	}
}

func TestDiscoverNameDefaultsToDirName(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "unnamed", "---\ndescription: no name field\n---\nbody")

	reg := Discover(context.Background(), root)
	if _, ok := reg.Get("unnamed"); !ok {
		t.Fatalf("want skill named after dir, got %v", reg.Names())
	}
}

func TestDiscoverDirsOnlyExisting(t *testing.T) {
	home := t.TempDir()
	proj := t.TempDir()
	if err := os.MkdirAll(filepath.Join(proj, ".agent", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	// home has no skills dir.
	dirs := DiscoverDirs(home, proj)
	if len(dirs) != 1 || dirs[0] != filepath.Join(proj, ".agent", "skills") {
		t.Fatalf("dirs: %v", dirs)
	}
}
