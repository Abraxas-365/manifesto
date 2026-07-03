package builtins

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/exec"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/tool"
)

// DefaultMaxBashOutput caps the combined output returned to the model (~30KB)
// when Bash.MaxOutput is not set.
const DefaultMaxBashOutput = 30 * 1024

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
	Command string       `json:"command"`
	Timeout tool.FlexInt `json:"timeout,omitempty"` // milliseconds
}

func (t *Bash) Name() string { return "Bash" }

func (t *Bash) Description() string {
	return "Execute a shell command and return its combined stdout/stderr. " +
		"Optionally set timeout in milliseconds (default 120000)."
}

func (t *Bash) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"command": {"type": "string", "description": "The shell command to run"},
			"timeout": {"type": "number", "description": "Timeout in milliseconds (default 120000)"}
		},
		"required": ["command"]
	}`)
}

func (t *Bash) IsReadOnly() bool                        { return false }
func (t *Bash) RequiresApproval(_ json.RawMessage) bool { return true }

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
	if max := t.maxOutput(); max >= 0 && len(out) > max {
		out = out[:max] + "\n[output truncated]"
	}
	return &tool.Result{Content: out, IsError: res.ExitCode != 0}, nil
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
