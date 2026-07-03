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
