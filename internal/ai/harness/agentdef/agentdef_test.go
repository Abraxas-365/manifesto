package agentdef

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefineRequiresName(t *testing.T) {
	if err := NewRegistry().Define(Definition{}); err == nil {
		t.Fatal("want error for empty name")
	}
}

func TestDefinePartialUpdatePreservesFields(t *testing.T) {
	r := NewRegistry()
	if err := r.Define(Definition{
		Name: "rev", Description: "reviewer", Model: "small",
		Tools: []string{"Read", "Grep"}, MaxTurns: 30,
	}); err != nil {
		t.Fatal(err)
	}
	// legacy: redefining with partial fields merges, unset fields survive.
	if err := r.Define(Definition{Name: "rev", Thinking: "high"}); err != nil {
		t.Fatal(err)
	}
	d, ok := r.Get("rev")
	if !ok {
		t.Fatal("missing")
	}
	if d.Description != "reviewer" || d.Model != "small" || d.MaxTurns != 30 ||
		len(d.Tools) != 2 || d.Thinking != "high" {
		t.Fatalf("merge lost fields: %+v", d)
	}
	// Explicit new slice replaces the old one.
	r.Define(Definition{Name: "rev", Tools: []string{"*"}})
	d, _ = r.Get("rev")
	if !d.AllowsAll() {
		t.Fatalf("tools not replaced: %+v", d.Tools)
	}
}

func TestAllowsTool(t *testing.T) {
	all := Definition{Tools: []string{"*"}, ToolExclude: []string{"Bash"}}
	if all.AllowsTool("Bash") {
		t.Error("exclude ignored")
	}
	if !all.AllowsTool("Read") {
		t.Error("wildcard should allow Read")
	}
	allow := Definition{Tools: []string{"Read"}}
	if allow.AllowsTool("Write") || !allow.AllowsTool("Read") {
		t.Error("allowlist broken")
	}
	empty := Definition{}
	if !empty.AllowsTool("Anything") {
		t.Error("legacy: omitted tools = all tools")
	}
}

func TestRosterRegisterIsNotExpose(t *testing.T) {
	r := NewRegistry()
	r.Define(Definition{Name: "a"})
	r.Define(Definition{Name: "b"})

	if len(r.Roster()) != 0 {
		t.Fatal("define must not expose (legacy two-step contract)")
	}
	r.Expose("a")
	r.Expose("a") // duplicate expose is a no-op
	r.Expose("ghost")
	// legacy: undefined roster names are silently skipped in listings.
	if got := r.Roster(); len(got) != 1 || got[0].Name != "a" {
		t.Fatalf("roster: %+v", got)
	}
	r.Expose("b")
	r.Unexpose("a")
	if got := r.RosterNames(); len(got) != 1 || got[0] != "b" {
		t.Fatalf("after unexpose: %v", got)
	}
	r.ClearRoster()
	if len(r.Roster()) != 0 {
		t.Fatal("clear failed")
	}
}

func TestExposeBeforeDefineIsLenient(t *testing.T) {
	// legacy: RosterAdd before RegisterDynamic is fine; the agent shows up once
	// defined.
	r := NewRegistry()
	r.Expose("late")
	if len(r.RosterNames()) != 0 {
		t.Fatal("undefined name must be hidden")
	}
	r.Define(Definition{Name: "late"})
	if got := r.RosterNames(); len(got) != 1 || got[0] != "late" {
		t.Fatalf("late definition not surfaced: %v", got)
	}
}

func TestPutReplacesAndRemoveDrops(t *testing.T) {
	r := NewRegistry()
	r.Define(Definition{Name: "x", Description: "old", MaxTurns: 9})
	// Put = wholesale replace (legacy agents.register), no merge.
	r.Put(Definition{Name: "x", Description: "new"})
	if d, _ := r.Get("x"); d.MaxTurns != 0 || d.Description != "new" {
		t.Fatalf("put must replace: %+v", d)
	}
	r.Expose("x")
	r.Remove("x")
	if _, ok := r.Get("x"); ok {
		t.Fatal("remove failed")
	}
	if len(r.RosterNames()) != 0 {
		t.Fatal("remove must drop roster entry")
	}
}

func TestParseMarkdownV1Frontmatter(t *testing.T) {
	md := []byte(`---
name: backend-expert
description: Backend specialist
model: small
thinking: high
tools: Read,Bash,Grep
autoLoadSkills: sql-expert, testing
maxTurns: 25
---
You are a backend expert.

Do backend things.`)
	d, err := ParseMarkdown(md, "fallback")
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "backend-expert" || d.Model != "small" || d.Thinking != "high" {
		t.Fatalf("meta: %+v", d)
	}
	if len(d.Tools) != 3 || d.Tools[1] != "Bash" {
		t.Fatalf("comma tools: %v", d.Tools)
	}
	if len(d.AutoloadSkills) != 2 || d.AutoloadSkills[1] != "testing" {
		t.Fatalf("skills: %v", d.AutoloadSkills)
	}
	if d.MaxTurns != 25 {
		t.Fatalf("maxTurns: %d", d.MaxTurns)
	}
	if d.SystemPrompt != "You are a backend expert.\n\nDo backend things." {
		t.Fatalf("body: %q", d.SystemPrompt)
	}
}

func TestParseMarkdownListToolsAndDefaults(t *testing.T) {
	md := []byte(`---
description: pi-style
tools:
  - Read
  - Grep
systemPromptMode: replace
readOnly: true
---
Body.`)
	d, err := ParseMarkdown(md, "from-filename")
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "from-filename" {
		t.Fatalf("name default: %q", d.Name)
	}
	if len(d.Tools) != 2 || !d.ReadOnly || d.SystemPromptMode != "replace" {
		t.Fatalf("parsed: %+v", d)
	}
}

func TestParseMarkdownNoFrontmatter(t *testing.T) {
	d, err := ParseMarkdown([]byte("Just a prompt."), "bare")
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "bare" || d.SystemPrompt != "Just a prompt." {
		t.Fatalf("parsed: %+v", d)
	}
}

func TestLoadDirBothLayouts(t *testing.T) {
	dir := t.TempDir()
	// Flat file (pi-subagents style).
	os.WriteFile(filepath.Join(dir, "scout.md"),
		[]byte("---\ndescription: recon\n---\nScout prompt."), 0o644)
	// Subdir with AGENT.md (legacy style).
	os.MkdirAll(filepath.Join(dir, "oracle"), 0o755)
	os.WriteFile(filepath.Join(dir, "oracle", "AGENT.md"),
		[]byte("---\ndescription: second opinion\n---\nOracle prompt."), 0o644)
	// Noise that must be ignored.
	os.WriteFile(filepath.Join(dir, "README.txt"), []byte("x"), 0o644)
	os.MkdirAll(filepath.Join(dir, "empty-dir"), 0o755)

	r := NewRegistry()
	loaded, errs := LoadDir(r, dir)
	if len(errs) != 0 {
		t.Fatalf("errs: %v", errs)
	}
	if len(loaded) != 2 {
		t.Fatalf("loaded: %v", loaded)
	}
	if _, ok := r.Get("scout"); !ok {
		t.Error("flat file agent missing")
	}
	if d, ok := r.Get("oracle"); !ok || d.SystemPrompt != "Oracle prompt." {
		t.Errorf("subdir agent: %+v", d)
	}
}

func TestLoadDirMissingIsFine(t *testing.T) {
	loaded, errs := LoadDir(NewRegistry(), "/nonexistent/agents")
	if loaded != nil || errs != nil {
		t.Fatal("missing dir must be silent")
	}
}

func TestLoadDirLaterWinsOnClash(t *testing.T) {
	user, proj := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(user, "rev.md"),
		[]byte("---\ndescription: user version\n---\nU."), 0o644)
	os.WriteFile(filepath.Join(proj, "rev.md"),
		[]byte("---\ndescription: project version\n---\nP."), 0o644)

	r := NewRegistry()
	LoadDir(r, user)
	LoadDir(r, proj) // later dir wins via merge
	d, _ := r.Get("rev")
	if d.Description != "project version" || d.SystemPrompt != "P." {
		t.Fatalf("precedence: %+v", d)
	}
}
