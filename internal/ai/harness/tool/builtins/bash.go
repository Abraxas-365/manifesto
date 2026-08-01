package builtins

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/exec"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/tool"
)

// DefaultMaxBashOutput caps the combined output returned to the model (~50KB)
// when Bash.MaxOutput is not set.
const DefaultMaxBashOutput = 50 * 1024

// DefaultMaxBashLines caps the output to this many lines. Whichever limit
// (bytes or lines) is hit first wins.
const DefaultMaxBashLines = 2000

// DefaultBashDeniedCommands is the built-in denylist of obviously destructive
// commands used when Bash.DeniedCommands is nil. Copy and extend it to
// customize the denylist without losing the defaults.
var DefaultBashDeniedCommands = []string{"rm -rf /", ":(){:|:&};:", "mkfs", "dd if=/dev/zero"}

// Bash runs shell commands via a swappable Executor.
type Bash struct {
	Exec exec.Executor

	// MaxOutput caps the combined stdout/stderr returned to the model, in
	// bytes. Zero means DefaultMaxBashOutput. Negative means no cap.
	MaxOutput int

	// DeniedCommands is a substring denylist: a command containing any entry is
	// rejected. Nil means DefaultBashDeniedCommands. Set to an empty non-nil slice
	// (e.g. []string{}) to disable the denylist entirely.
	DeniedCommands []string
}

func (t *Bash) maxOutput() int {
	if t.MaxOutput == 0 {
		return DefaultMaxBashOutput
	}
	return t.MaxOutput
}

func (t *Bash) deniedCommands() []string {
	if t.DeniedCommands == nil {
		return DefaultBashDeniedCommands
	}
	return t.DeniedCommands
}

type bashInput struct {
	Command         string       `json:"command"`
	Description     string       `json:"description,omitempty"`
	Timeout         tool.FlexInt `json:"timeout,omitempty"` // milliseconds
	Workdir         string       `json:"workdir,omitempty"` // relative paths resolve against the default work dir
	RunInBackground bool         `json:"run_in_background,omitempty"`
}

func (t *Bash) Name() string { return "Bash" }

func (t *Bash) Description() string {
	desc := `Executes a given bash command and returns its combined stdout/stderr.

All commands run in the session working directory by default. Use the ` + "`workdir`" + ` parameter (absolute, or relative to the session directory) if you need to run a command in a different directory. AVOID using ` + "`cd <directory> && <command>`" + ` patterns — use ` + "`workdir`" + ` instead. Each invocation runs in a fresh non-interactive shell — shell state (exports, aliases, cd) does NOT persist between commands, and user profile aliases/functions are not available.

IMPORTANT: Avoid using this tool to run ` + "`find`" + `, ` + "`grep`" + `, ` + "`cat`" + `, ` + "`head`" + `, ` + "`tail`" + `, ` + "`sed`" + `, ` + "`awk`" + `, or ` + "`echo`" + ` commands, unless explicitly instructed or after you have verified that a dedicated tool cannot accomplish your task. Instead, use the appropriate dedicated tool:

 - File search: Use Glob (NOT find or ls)
 - Content search: Use Grep (NOT grep or rg)
 - Read files: Use Read (NOT cat/head/tail). Read supports ` + "`offset`" + ` and ` + "`limit`" + ` parameters for line ranges — use those instead of ` + "`sed -n`" + `
 - Edit files: Use Edit (NOT sed/awk)
 - Write files: Use Write (NOT echo >/cat <<EOF)
 - Communication: Output text directly (NOT echo/printf)
While the Bash tool can do similar things, it's better to use the built-in tools.

# Instructions
 - Always quote file paths that contain spaces with double quotes (e.g., "path with spaces/file.txt")
 - You may specify an optional timeout in milliseconds. By default, your command will timeout after 120000ms (2 minutes).
 - When issuing multiple commands:
  - If the commands are independent and can run in parallel, make multiple Bash tool calls in a single message.
  - If the commands depend on each other and must run sequentially, use a single Bash call with '&&' to chain them together.
  - Use ';' only when you need to run commands sequentially but don't care if earlier commands fail.
  - DO NOT use newlines to separate commands (newlines are ok in quoted strings).
 - For git commands:
  - Prefer to create a new commit rather than amending an existing commit.
  - Before running destructive operations (e.g., git reset --hard, git push --force, git checkout --), consider whether there is a safer alternative that achieves the same goal. Only use destructive operations when they are truly the best approach.
  - Never skip hooks (--no-verify) or bypass signing (--no-gpg-sign) unless the user has explicitly asked for it. If a hook fails, investigate and fix the underlying issue.
 - Avoid ` + "`sleep`" + ` — don't sleep between runnable commands or retry/poll in sleep loops; diagnose failures instead.`
	if _, ok := t.Exec.(exec.BackgroundExecutor); ok {
		desc += "\n - Set run_in_background=true to launch a long-lived command " +
			"(e.g. a server) detached; it returns a shell ID to use with " +
			"BashOutput and KillShell instead of blocking."
	}
	return desc
}

func (t *Bash) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"command": {"type": "string", "description": "The shell command to run"},
			"description": {"type": "string", "description": "Clear, concise description of what this command does in active voice"},
			"timeout": {"type": "number", "description": "Timeout in milliseconds (default 120000)"},
			"workdir": {"type": "string", "description": "The working directory to run the command in. Defaults to the session directory. Use this instead of 'cd' commands."},
			"run_in_background": {"type": "boolean", "description": "Launch detached and return a shell ID instead of blocking (for servers and long-lived processes)"}
		},
		"required": ["command"]
	}`)
}

func (t *Bash) IsReadOnly() bool { return false }

func (t *Bash) Execute(ctx context.Context, input json.RawMessage) (*tool.Result, error) {
	var in bashInput
	if err := json.Unmarshal(input, &in); err != nil {
		return &tool.Result{Content: fmt.Sprintf("Invalid input: %v", err), IsError: true}, nil
	}
	if strings.TrimSpace(in.Command) == "" {
		return &tool.Result{Content: "No command provided", IsError: true}, nil
	}
	for _, bad := range t.deniedCommands() {
		if strings.Contains(in.Command, bad) {
			return &tool.Result{Content: fmt.Sprintf("Command blocked by denylist: %q", bad), IsError: true}, nil
		}
	}

	var opts exec.RunOptions
	if ms := in.Timeout.Value(0); ms > 0 {
		opts.Timeout = time.Duration(ms) * time.Millisecond
	}
	// Isolated working directory (e.g. a git worktree): commands default to
	// the isolation dir, and a relative workdir resolves against it.
	isoDir := tool.Workdir(ctx)
	if in.Workdir != "" {
		wd := in.Workdir
		if !filepath.IsAbs(wd) {
			if isoDir != "" {
				wd = filepath.Join(isoDir, wd)
			} else if le, ok := t.Exec.(*exec.LocalExecutor); ok && le.DefaultWorkDir != "" {
				wd = filepath.Join(le.DefaultWorkDir, wd)
			}
		} else {
			wd = tool.ResolvePath(ctx, wd)
		}
		if info, err := os.Stat(wd); err != nil || !info.IsDir() {
			return &tool.Result{Content: fmt.Sprintf("workdir does not exist or is not a directory: %s", wd), IsError: true}, nil
		}
		opts.WorkDir = wd
	} else if isoDir != "" {
		opts.WorkDir = isoDir
	}

	if in.RunInBackground {
		bg, ok := t.Exec.(exec.BackgroundExecutor)
		if !ok {
			return &tool.Result{Content: "This executor does not support background commands.", IsError: true}, nil
		}
		id, err := bg.Start(ctx, in.Command, opts)
		if err != nil {
			return &tool.Result{Content: err.Error(), IsError: true}, nil
		}
		return &tool.Result{Content: fmt.Sprintf(
			"Started background shell %s. Use BashOutput with this ID to read output, or KillShell to stop it.", id)}, nil
	}

	res, err := t.Exec.Run(ctx, in.Command, opts)
	if err != nil {
		msg := err.Error()
		if res != nil {
			if out := combineOutput(res); out != "" {
				msg = out + "\n" + msg
			}
		}
		return &tool.Result{Content: msg, IsError: true}, nil
	}

	out := combineOutput(res)
	if out == "" {
		out = "(no output)"
	}
	if max := t.maxOutput(); max >= 0 {
		out = truncateOutput(out, max, DefaultMaxBashLines)
	}
	return &tool.Result{Content: out, IsError: res.ExitCode != 0}, nil
}

// truncateOutput truncates out when it exceeds maxBytes or maxLines,
// keeping the tail (most recent output). The full output is written to a temp
// file so nothing is lost — the model can Read the file with offset/limit.
func truncateOutput(out string, maxBytes, maxLines int) string {
	origLines := strings.Split(out, "\n")
	byteExceeded := len(out) > maxBytes
	lineExceeded := len(origLines) > maxLines

	if !byteExceeded && !lineExceeded {
		return out
	}

	// Save full output to temp file before truncating.
	fullPath := ""
	if f, err := os.CreateTemp("", "claudio-bash-*.txt"); err == nil {
		if _, werr := f.WriteString(out); werr == nil {
			fullPath = f.Name()
		}
		f.Close()
	}

	totalLines := len(origLines)
	truncated := out

	// Apply line cap (keep tail).
	if lineExceeded {
		kept := origLines[totalLines-maxLines:]
		truncated = strings.Join(kept, "\n")
	}

	// Apply byte cap (keep tail).
	if len(truncated) > maxBytes {
		truncated = truncated[len(truncated)-maxBytes:]
	}

	shownLines := len(strings.Split(truncated, "\n"))
	startLine := totalLines - shownLines + 1

	if fullPath != "" {
		return "... (truncated)\n" +
			truncated +
			fmt.Sprintf("\n[Showing lines %d-%d of %d. Full output: %s — use Read with offset/limit if you need more.]",
				startLine, totalLines, totalLines, fullPath)
	}
	return "... (truncated)\n" +
		truncated +
		fmt.Sprintf("\n[Showing lines %d-%d of %d]", startLine, totalLines, totalLines)
}

func combineOutput(res *exec.RunResult) string {
	var b strings.Builder
	if res.Stdout != "" {
		b.WriteString(res.Stdout)
	}
	if res.Stderr != "" {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("STDERR:\n")
		b.WriteString(res.Stderr)
	}
	if res.ExitCode != 0 {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "[exit code: %d]", res.ExitCode)
	}
	return b.String()
}
