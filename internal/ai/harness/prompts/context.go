package prompts

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// GitStatus collects current git context for dir: branch, main branch, user,
// status, recent commits. Returns "" when not a git repo or on error. Status
// output is capped at 2000 characters to avoid bloating the prompt.
func GitStatus(dir string) string {
	if !isGitRepo(dir) {
		return ""
	}
	git := func(args ...string) string {
		out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}

	branch := git("rev-parse", "--abbrev-ref", "HEAD")
	if branch == "" {
		return ""
	}
	mainBranch := ""
	for _, candidate := range []string{"main", "master", "develop", "trunk"} {
		if git("rev-parse", "--verify", "--quiet", candidate) != "" {
			mainBranch = candidate
			break
		}
	}
	user := git("config", "user.name")
	status := git("status", "--short")
	if len(status) > 2000 {
		status = status[:2000] + "\n... (truncated, use Bash tool for full status)"
	}
	if status == "" {
		status = "(clean)"
	}
	log := git("log", "--oneline", "-5")

	parts := []string{
		"This is the git status at the start of the conversation. Note that this status is a snapshot in time, and will not update during the conversation.",
		fmt.Sprintf("Current branch: %s", branch),
	}
	if mainBranch != "" {
		parts = append(parts, fmt.Sprintf("Main branch (you will usually use this for PRs): %s", mainBranch))
	}
	if user != "" {
		parts = append(parts, fmt.Sprintf("Git user: %s", user))
	}
	parts = append(parts, fmt.Sprintf("Status:\n%s", status))
	if log != "" {
		parts = append(parts, fmt.Sprintf("Recent commits:\n%s", log))
	}
	return "gitStatus: " + strings.Join(parts, "\n\n")
}

// ContextFileNames lists the project context files recognized during
// discovery, in per-directory priority order (first match per directory wins).
// AGENTS.md is included for parity with agent SDKs. Override at startup to
// add product-specific names.
var ContextFileNames = []string{
	"CLAUDE.md",
	"AGENTS.md",
}

// UserContextFile is the path (relative to the user's home directory) of the
// user-level context file read with lowest priority. Empty disables it.
var UserContextFile = filepath.Join(".config", "agent", "CLAUDE.md")

// ReadProjectInstructions discovers and concatenates context files: the
// user-level file first (lowest priority), then one file per directory walking
// from the git root down to dir, so directories closer to dir appear later
// (higher priority). Standalone "@path/to/file.md" lines are resolved inline.
func ReadProjectInstructions(dir string) string {
	var parts []string

	if home, err := os.UserHomeDir(); err == nil && UserContextFile != "" {
		path := filepath.Join(home, UserContextFile)
		if content := readFileIfExists(path); content != "" {
			parts = append(parts, resolveImports(content, filepath.Dir(path), nil, 0))
		}
	}

	for _, d := range collectDirsRootToCwd(findGitRoot(dir), dir) {
		for _, name := range ContextFileNames {
			path := filepath.Join(d, name)
			if content := readFileIfExists(path); content != "" {
				parts = append(parts, resolveImports(content, d, nil, 0))
				break // only first match per directory
			}
		}
	}

	return strings.Join(parts, "\n\n")
}

// findGitRoot walks up from dir looking for a .git entry; returns dir itself
// when none is found.
func findGitRoot(dir string) string {
	current := filepath.Clean(dir)
	for {
		if _, err := os.Stat(filepath.Join(current, ".git")); err == nil {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			return filepath.Clean(dir)
		}
		current = parent
	}
}

// collectDirsRootToCwd returns directories from root down to cwd (inclusive),
// root-first so closer-to-cwd dirs appear later (higher priority).
func collectDirsRootToCwd(root, cwd string) []string {
	root, cwd = filepath.Clean(root), filepath.Clean(cwd)
	if root == cwd {
		return []string{root}
	}
	var stack []string
	for current := cwd; ; {
		stack = append(stack, current)
		if current == root {
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	for i, j := 0, len(stack)-1; i < j; i, j = i+1, j-1 {
		stack[i], stack[j] = stack[j], stack[i]
	}
	return stack
}

// maxImportDepth caps recursion when expanding @file.md imports (legacy value,
// matching claude-code's MAX_INCLUDE_DEPTH). The seen map already prevents
// cycles; the depth cap also bounds deep non-circular chains.
const maxImportDepth = 5

// resolveImports replaces standalone "@path/to/file.md" lines with the
// referenced file's contents. Lines inside fenced code blocks are left
// untouched so documentation examples mentioning @paths are not expanded.
func resolveImports(content, baseDir string, seen map[string]bool, depth int) string {
	if seen == nil {
		seen = make(map[string]bool)
	}
	if depth >= maxImportDepth {
		return content
	}

	var result strings.Builder
	inFence := false
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			result.WriteString(line)
			result.WriteString("\n")
			continue
		}

		if !inFence && strings.HasPrefix(trimmed, "@") && strings.HasSuffix(trimmed, ".md") && !strings.Contains(trimmed, " ") {
			importPath := trimmed[1:]
			if !filepath.IsAbs(importPath) {
				if strings.HasPrefix(importPath, "~/") {
					home, _ := os.UserHomeDir()
					importPath = filepath.Join(home, importPath[2:])
				} else {
					importPath = filepath.Join(baseDir, importPath)
				}
			}
			importPath = filepath.Clean(importPath)

			if seen[importPath] {
				result.WriteString(line)
				result.WriteString("\n")
				continue
			}
			seen[importPath] = true

			if imported := readFileIfExists(importPath); imported != "" {
				result.WriteString(resolveImports(imported, filepath.Dir(importPath), seen, depth+1))
				result.WriteString("\n")
				continue
			}
		}

		result.WriteString(line)
		result.WriteString("\n")
	}
	return strings.TrimRight(result.String(), "\n")
}

func readFileIfExists(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

// UserContextMessage returns a <system-reminder> block containing CLAUDE.md
// content and the current date, to be prepended as the first user message so
// per-project context stays out of the cached system prompt prefix (legacy
// contract).
func UserContextMessage(claudeMD string) string {
	var parts []string
	if claudeMD != "" {
		parts = append(parts, fmt.Sprintf(`# claudeMd
Codebase and user instructions are shown below. Be sure to adhere to these instructions. IMPORTANT: These instructions OVERRIDE any default behavior and you MUST follow them exactly as written.

%s`, claudeMD))
	}
	parts = append(parts, fmt.Sprintf("# currentDate\nToday's date is %s.", time.Now().Format("2006-01-02")))

	return fmt.Sprintf(`<system-reminder>
As you answer the user's questions, you can use the following context:
%s

IMPORTANT: this context may or may not be relevant to your tasks. You should not respond to this context unless it is highly relevant to your task.
</system-reminder>`, strings.Join(parts, "\n\n"))
}
