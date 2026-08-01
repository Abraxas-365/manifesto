package prompts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuildStructure(t *testing.T) {
	got := Build("/tmp/proj", "test-model", "")

	static, dynamic, found := strings.Cut(got, DynamicBoundary)
	if !found {
		t.Fatal("missing DynamicBoundary")
	}
	for _, want := range []string{
		"You are " + AssistantName,
		"# System",
		"# Doing tasks",
		"# Executing actions with care",
		"# Using your tools",
		"# Tone and style",
		"# Output efficiency",
	} {
		if !strings.Contains(static, want) {
			t.Errorf("static part missing %q", want)
		}
	}
	if strings.Contains(static, "# Environment") {
		t.Error("environment leaked into static part")
	}
	for _, want := range []string{
		"# Environment",
		"Primary working directory: /tmp/proj",
		"Model: test-model",
		time.Now().Format("2006-01-02"),
	} {
		if !strings.Contains(dynamic, want) {
			t.Errorf("dynamic part missing %q", want)
		}
	}
}

func TestBuildStaticPrefixStableAcrossDirs(t *testing.T) {
	a, _, _ := strings.Cut(Build("/tmp/a", "m1", ""), DynamicBoundary)
	b, _, _ := strings.Cut(Build("/tmp/b", "m2", "extra"), DynamicBoundary)
	if a != b {
		t.Error("static prefix differs across sessions; breaks prompt caching")
	}
}

func TestBuildAdditionalContext(t *testing.T) {
	got := Build("/tmp", "m", "gitStatus: on branch main")
	_, dynamic, _ := strings.Cut(got, DynamicBoundary)
	if !strings.Contains(dynamic, "gitStatus: on branch main") {
		t.Error("additional context missing from dynamic part")
	}
}

func TestBuildWithOverrideReplace(t *testing.T) {
	got := BuildWithOverride("/tmp", "m", "", "You are a pirate.", "replace")
	if !strings.HasPrefix(got, "You are a pirate.") {
		t.Error("override not at start")
	}
	if strings.Contains(got, "# Doing tasks") {
		t.Error("replace mode kept builtin sections")
	}
	if !strings.Contains(got, "# Environment") {
		t.Error("replace mode dropped environment")
	}
}

func TestBuildWithOverrideAppend(t *testing.T) {
	got := BuildWithOverride("/tmp", "m", "", "Extra rule.", "append")
	static, _, _ := strings.Cut(got, DynamicBoundary)
	if !strings.Contains(static, "# Doing tasks") {
		t.Error("append mode lost builtin sections")
	}
	if !strings.Contains(static, "Extra rule.") {
		t.Error("append mode missing override in static block")
	}
}

func TestBuildWithOverrideEmpty(t *testing.T) {
	if BuildWithOverride("/tmp", "m", "", "", "replace") != Build("/tmp", "m", "") {
		t.Error("empty override should equal Build")
	}
}

func TestFinalize(t *testing.T) {
	got := Finalize(Build("/tmp", "m", ""))
	if strings.Contains(got, DynamicBoundary) {
		t.Error("Finalize left boundary marker in prompt")
	}
}

func TestGitStatus(t *testing.T) {
	dir := t.TempDir()
	if GitStatus(dir) != "" {
		t.Error("non-repo dir should return empty git status")
	}

	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	// -b main isn't supported by older git; rename after init instead.
	run("symbolic-ref", "HEAD", "refs/heads/main")
	run("config", "user.email", "t@t.io")
	run("config", "user.name", "Test User")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hi"), 0o644)
	run("add", ".")
	run("commit", "-m", "first commit")
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("new"), 0o644)

	got := GitStatus(dir)
	for _, want := range []string{
		"gitStatus:",
		"Current branch: main",
		"Main branch (you will usually use this for PRs): main",
		"Git user: Test User",
		"?? b.txt",
		"first commit",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("GitStatus missing %q in:\n%s", want, got)
		}
	}
}

func TestReadProjectInstructions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := t.TempDir()
	if ReadProjectInstructions(dir) != "" {
		t.Error("no files should return empty")
	}
	os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("agents"), 0o644)
	if got := ReadProjectInstructions(dir); got != "agents" {
		t.Errorf("got %q, want agents fallback", got)
	}
	os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("claude"), 0o644)
	if got := ReadProjectInstructions(dir); got != "claude" {
		t.Errorf("got %q, want CLAUDE.md to win", got)
	}
}

func TestReadProjectInstructionsWalkAndUserLevel(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	userDir := filepath.Join(home, filepath.Dir(UserContextFile))
	os.MkdirAll(userDir, 0o755)
	os.WriteFile(filepath.Join(home, UserContextFile), []byte("user-level"), 0o644)

	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, ".git"), 0o755)
	os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("root-level"), 0o644)
	sub := filepath.Join(root, "a", "b")
	os.MkdirAll(sub, 0o755)
	os.WriteFile(filepath.Join(sub, "CLAUDE.md"), []byte("sub-level"), 0o644)

	got := ReadProjectInstructions(sub)
	ui := strings.Index(got, "user-level")
	ri := strings.Index(got, "root-level")
	si := strings.Index(got, "sub-level")
	if ui == -1 || ri == -1 || si == -1 {
		t.Fatalf("missing levels in:\n%s", got)
	}
	if !(ui < ri && ri < si) {
		t.Errorf("wrong precedence order (user < root < sub):\n%s", got)
	}
}

func TestReadProjectInstructionsImports(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "extra.md"), []byte("imported-content"), 0o644)
	os.WriteFile(filepath.Join(dir, "cycle.md"), []byte("@cycle.md\ncycle-body"), 0o644)
	os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(
		"before\n@extra.md\n@cycle.md\n```\n@fenced.md\n```\nafter"), 0o644)

	got := ReadProjectInstructions(dir)
	for _, want := range []string{"before", "imported-content", "cycle-body", "after", "@fenced.md"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "@extra.md") {
		t.Error("@extra.md should have been replaced by its content")
	}
}

func TestUserContextMessage(t *testing.T) {
	got := UserContextMessage("Always use tabs.")
	for _, want := range []string{
		"<system-reminder>",
		"</system-reminder>",
		"# claudeMd",
		"Always use tabs.",
		"# currentDate",
		time.Now().Format("2006-01-02"),
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q", want)
		}
	}

	empty := UserContextMessage("")
	if strings.Contains(empty, "# claudeMd") {
		t.Error("empty CLAUDE.md should omit claudeMd section")
	}
	if !strings.Contains(empty, "# currentDate") {
		t.Error("date section always present")
	}
}

func TestOutputStyleSection(t *testing.T) {
	if OutputStyleSection(StyleDefault) != "" {
		t.Error("default style should be empty")
	}
	if OutputStyleSection("bogus") != "" {
		t.Error("unknown style should be empty")
	}
	for _, s := range []OutputStyle{StyleVerbose, StyleConcise, StyleBrief, StyleMarkdown} {
		if !strings.Contains(OutputStyleSection(s), "# Output Style:") {
			t.Errorf("style %q missing header", s)
		}
	}
}

func TestWithStaticSection(t *testing.T) {
	full := Build("/tmp/x", "m", "")
	got := WithStaticSection(full, "# Extra Section")
	bi := strings.Index(got, DynamicBoundary)
	si := strings.Index(got, "# Extra Section")
	if si == -1 || bi == -1 || si > bi {
		t.Error("section must be inserted before the dynamic boundary")
	}
	if WithStaticSection(full, "") != full {
		t.Error("empty section must be a no-op")
	}
}
