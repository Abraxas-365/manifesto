package exec

import (
	"context"
	"time"
)

// RunOptions configures a single command execution.
type RunOptions struct {
	// WorkDir is the working directory for the command. Empty means the
	// executor's default.
	WorkDir string
	// Timeout caps the total run time. Zero means the executor default.
	Timeout time.Duration
	// Env holds additional environment variables ("KEY=VALUE").
	Env []string
}

// RunResult is the outcome of a command execution.
type RunResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	TimedOut bool
}

// Executor runs shell commands. Implementations may target the local machine, a
// remote worker, or a command service backed by an S3 filesystem — the tool
// layer depends only on this interface, so the execution environment is
// swappable.
type Executor interface {
	Run(ctx context.Context, command string, opts RunOptions) (*RunResult, error)
}

// BackgroundStatus is a snapshot of a detached command's progress. Stdout and
// Stderr contain only the output produced since the previous poll.
type BackgroundStatus struct {
	Stdout   string
	Stderr   string
	Running  bool
	ExitCode int
}

// BackgroundExecutor is an optional capability: an Executor that can also launch
// long-lived (detached) commands — servers, watchers, builds you want to keep
// running — then poll their incremental output or kill them by ID.
//
// Not every Executor supports it. Tools should type-assert for this interface
// and fall back gracefully (or report "background not supported") when an
// Executor implements only the base Executor.
type BackgroundExecutor interface {
	Executor
	// Start launches command detached from ctx's lifetime and returns an opaque
	// shell ID. The command keeps running after the starting tool call returns;
	// it stops only when it exits on its own or Kill is called.
	Start(ctx context.Context, command string, opts RunOptions) (string, error)
	// Poll returns output produced since the previous Poll for the given ID,
	// along with whether the command is still running.
	Poll(id string) (*BackgroundStatus, error)
	// Kill terminates the command (and its process group) by ID.
	Kill(id string) error
}
