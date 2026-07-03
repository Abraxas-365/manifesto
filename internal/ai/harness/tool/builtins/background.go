package builtins

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/exec"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/tool"
)

// BashOutput retrieves new output from a background shell started by Bash with
// run_in_background=true. It only works with a BackgroundExecutor.
type BashOutput struct {
	Exec exec.BackgroundExecutor
}

type bashOutputInput struct {
	ShellID string `json:"shell_id"`
}

func (t *BashOutput) Name() string { return "BashOutput" }

func (t *BashOutput) Description() string {
	return "Retrieve output produced since the last check from a background " +
		"shell (started via Bash with run_in_background=true). Returns whether " +
		"the shell is still running."
}

func (t *BashOutput) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"shell_id": {"type": "string", "description": "The shell ID returned by Bash"}
		},
		"required": ["shell_id"]
	}`)
}

func (t *BashOutput) IsReadOnly() bool                        { return true }
func (t *BashOutput) RequiresApproval(_ json.RawMessage) bool { return false }

func (t *BashOutput) Execute(_ context.Context, input json.RawMessage) (*tool.Result, error) {
	var in bashOutputInput
	if err := json.Unmarshal(input, &in); err != nil {
		return &tool.Result{Content: fmt.Sprintf("Invalid input: %v", err), IsError: true}, nil
	}
	if strings.TrimSpace(in.ShellID) == "" {
		return &tool.Result{Content: "No shell_id provided", IsError: true}, nil
	}

	status, err := t.Exec.Poll(in.ShellID)
	if err != nil {
		return &tool.Result{Content: err.Error(), IsError: true}, nil
	}

	var b strings.Builder
	if status.Stdout != "" {
		b.WriteString(status.Stdout)
	}
	if status.Stderr != "" {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("STDERR:\n")
		b.WriteString(status.Stderr)
	}
	if b.Len() == 0 {
		b.WriteString("(no new output)")
	}
	if status.Running {
		b.WriteString("\n[status: running]")
	} else {
		fmt.Fprintf(&b, "\n[status: exited, code %d]", status.ExitCode)
	}
	return &tool.Result{Content: b.String()}, nil
}

// KillShell terminates a background shell by ID. It only works with a
// BackgroundExecutor.
type KillShell struct {
	Exec exec.BackgroundExecutor
}

type killShellInput struct {
	ShellID string `json:"shell_id"`
}

func (t *KillShell) Name() string { return "KillShell" }

func (t *KillShell) Description() string {
	return "Terminate a background shell (started via Bash with " +
		"run_in_background=true) by its shell ID."
}

func (t *KillShell) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"shell_id": {"type": "string", "description": "The shell ID returned by Bash"}
		},
		"required": ["shell_id"]
	}`)
}

func (t *KillShell) IsReadOnly() bool                        { return false }
func (t *KillShell) RequiresApproval(_ json.RawMessage) bool { return true }

func (t *KillShell) Execute(_ context.Context, input json.RawMessage) (*tool.Result, error) {
	var in killShellInput
	if err := json.Unmarshal(input, &in); err != nil {
		return &tool.Result{Content: fmt.Sprintf("Invalid input: %v", err), IsError: true}, nil
	}
	if strings.TrimSpace(in.ShellID) == "" {
		return &tool.Result{Content: "No shell_id provided", IsError: true}, nil
	}
	if err := t.Exec.Kill(in.ShellID); err != nil {
		return &tool.Result{Content: err.Error(), IsError: true}, nil
	}
	return &tool.Result{Content: fmt.Sprintf("Killed shell %s.", in.ShellID)}, nil
}
