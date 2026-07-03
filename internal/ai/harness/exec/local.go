package exec

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// DefaultTimeout caps a command's run time when RunOptions.Timeout is zero.
const DefaultTimeout = 120 * time.Second

// LocalExecutor runs commands on the local machine via `bash -c`. It also
// implements BackgroundExecutor for detached, long-lived commands.
type LocalExecutor struct {
	// DefaultWorkDir is used when RunOptions.WorkDir is empty.
	DefaultWorkDir string

	// bg holds detached shells started via Start, keyed by shell ID.
	bg sync.Map
}

// NewLocalExecutor creates a LocalExecutor rooted at workDir.
func NewLocalExecutor(workDir string) *LocalExecutor {
	return &LocalExecutor{DefaultWorkDir: workDir}
}

// Run implements Executor.
func (e *LocalExecutor) Run(ctx context.Context, command string, opts RunOptions) (*RunResult, error) {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "bash", "-c", command)

	// Kill the entire process group on timeout, not just bash. Without this,
	// child processes keep the stdout/stderr pipes open and cmd.Run() blocks
	// indefinitely even after bash is killed.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	// After Cancel fires, wait up to 5s for pipes to drain, then forcibly close
	// them so cmd.Run() returns.
	cmd.WaitDelay = 5 * time.Second

	workDir := opts.WorkDir
	if workDir == "" {
		workDir = e.DefaultWorkDir
	}
	if workDir != "" {
		cmd.Dir = workDir
	}
	if len(opts.Env) > 0 {
		cmd.Env = append(cmd.Environ(), opts.Env...)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	result := &RunResult{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}

	if runCtx.Err() == context.DeadlineExceeded {
		result.TimedOut = true
		result.ExitCode = -1
		return result, Registry.NewWithCause(ErrTimeout, err).
			WithDetail("timeout", timeout.String())
	}

	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
			// A non-zero exit is a normal command outcome, not a harness error.
			return result, nil
		}
		result.ExitCode = -1
		return result, Registry.NewWithCause(ErrCommandFailed, err)
	}

	result.ExitCode = 0
	return result, nil
}
