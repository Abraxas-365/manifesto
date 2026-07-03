package exec

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
)

// bgShell tracks one detached command started via LocalExecutor.Start.
type bgShell struct {
	cmd *exec.Cmd

	mu      sync.Mutex
	stdout  bytes.Buffer // unread output accumulates here
	stderr  bytes.Buffer
	running bool
	exit    int
}

// syncBuffer is an io.Writer that guards a bytes.Buffer with the shell's mutex
// so the reader (Poll) and the os/exec writer goroutine don't race.
type syncBuffer struct {
	sh  *bgShell
	buf *bytes.Buffer
}

func (w syncBuffer) Write(p []byte) (int, error) {
	w.sh.mu.Lock()
	defer w.sh.mu.Unlock()
	return w.buf.Write(p)
}

var bgCounter atomic.Uint64

// Start implements BackgroundExecutor. The command is detached from ctx: it
// keeps running after ctx is cancelled and stops only on its own exit or Kill.
func (e *LocalExecutor) Start(_ context.Context, command string, opts RunOptions) (string, error) {
	// Deliberately use context.Background so the process outlives the tool call.
	cmd := exec.Command("bash", "-c", command)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

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

	sh := &bgShell{cmd: cmd, running: true}
	cmd.Stdout = syncBuffer{sh: sh, buf: &sh.stdout}
	cmd.Stderr = syncBuffer{sh: sh, buf: &sh.stderr}

	if err := cmd.Start(); err != nil {
		return "", Registry.NewWithCause(ErrCommandFailed, err)
	}

	id := "shell_" + strconv.FormatUint(bgCounter.Add(1), 10)
	e.bg.Store(id, sh)

	go func() {
		err := cmd.Wait()
		sh.mu.Lock()
		defer sh.mu.Unlock()
		sh.running = false
		if err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				sh.exit = exitErr.ExitCode()
			} else {
				sh.exit = -1
			}
		}
	}()

	return id, nil
}

// Poll implements BackgroundExecutor. It drains and returns output produced
// since the previous call for id.
func (e *LocalExecutor) Poll(id string) (*BackgroundStatus, error) {
	v, ok := e.bg.Load(id)
	if !ok {
		return nil, Registry.New(ErrShellNotFound).WithDetail("id", id)
	}
	sh := v.(*bgShell)

	sh.mu.Lock()
	defer sh.mu.Unlock()

	status := &BackgroundStatus{
		Stdout:   sh.stdout.String(),
		Stderr:   sh.stderr.String(),
		Running:  sh.running,
		ExitCode: sh.exit,
	}
	sh.stdout.Reset()
	sh.stderr.Reset()
	return status, nil
}

// Kill implements BackgroundExecutor, terminating the shell's process group.
func (e *LocalExecutor) Kill(id string) error {
	v, ok := e.bg.Load(id)
	if !ok {
		return Registry.New(ErrShellNotFound).WithDetail("id", id)
	}
	sh := v.(*bgShell)

	sh.mu.Lock()
	running := sh.running
	pid := 0
	if sh.cmd.Process != nil {
		pid = sh.cmd.Process.Pid
	}
	sh.mu.Unlock()

	if running && pid > 0 {
		// Negative pid targets the whole process group set up in Start.
		_ = syscall.Kill(-pid, syscall.SIGKILL)
	}
	e.bg.Delete(id)
	return nil
}
