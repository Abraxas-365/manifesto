// Example 12_docker: a remote Executor that runs the agent's shell commands
// *inside a Docker container* via `docker exec`, paired with a FileSystem that
// stays consistent with it.
//
// This is the answer to "how do I let the agent compile, run, and start servers
// safely, without giving it my host shell?" The container is a disposable
// sandbox: destructive commands, servers, and builds all happen inside it.
//
// Split-brain is impossible here by construction: builtins.FromExecutor derives
// the file tools' storage from the SAME DockerExecutor that backs Bash, so
// Read/Write/Edit and shell commands all run inside the one container. No bind
// mount, no host filesystem, nothing to keep in sync.
//
// Because DockerExecutor implements exec.BackgroundExecutor, builtins.FromExecutor
// (via Default) also registers BashOutput and KillShell, so the agent can start a
// server in the container with run_in_background=true and poll its logs.
//
// Prereqs: Docker running, and a container with a working directory, e.g.:
//
//	docker run -d --name agent-sandbox -w /workspace \
//	  golang:1.22 sh -c 'mkdir -p /workspace && sleep infinity'
//
// Then:
//
//	OPENAI_API_KEY=sk-... \
//	AGENT_CONTAINER=agent-sandbox \
//	CONTAINER_WORKDIR=/workspace \
//	go run ./internal/ai/harness/examples/12_docker \
//	  "write hello.go that prints hi, then run it with go run"
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/Abraxas-365/manifesto/internal/ai/harness"
	hexec "github.com/Abraxas-365/manifesto/internal/ai/harness/exec"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm/openai"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/tool/builtins"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		return fmt.Errorf("set OPENAI_API_KEY")
	}
	container := os.Getenv("AGENT_CONTAINER")
	if container == "" {
		return fmt.Errorf("set AGENT_CONTAINER (see file header for setup)")
	}
	containerWorkdir := os.Getenv("CONTAINER_WORKDIR")
	if containerWorkdir == "" {
		containerWorkdir = "/workspace"
	}

	// One executor backs everything. FromExecutor derives the file tools' storage
	// (via execstore) from this same DockerExecutor, so file ops and Bash both run
	// inside the container — they cannot point at different worlds.
	ex := &DockerExecutor{Container: container, WorkDir: containerWorkdir}

	// FromExecutor registers the file tools + Bash; since DockerExecutor is a
	// BackgroundExecutor, BashOutput/KillShell come along too.
	registry := builtins.FromExecutor(ex)

	agent := harness.New(openai.New(key), registry)
	agent.System = "You are a coding assistant working inside a Docker sandbox. " +
		"File tools edit the workspace; Bash runs commands in the container. " +
		"For long-lived processes like servers, use run_in_background then BashOutput."
	agent.Model = "gpt-4o"

	prompt := strings.Join(os.Args[1:], " ")
	if prompt == "" {
		prompt = "Create hello.go that prints hello from the container, then run it with `go run hello.go`."
	}

	answer, err := agent.Run(context.Background(), prompt)
	if err != nil {
		return err
	}
	fmt.Println(answer)
	return nil
}

// DockerExecutor runs commands inside a running container via `docker exec`. It
// implements exec.Executor and exec.BackgroundExecutor, so it is a drop-in
// replacement for LocalExecutor that targets the container instead of the host.
type DockerExecutor struct {
	Container string
	WorkDir   string

	bg      sync.Map // id -> *dockerShell
	counter atomic.Uint64
}

// Compile-time proof that DockerExecutor is a full BackgroundExecutor.
var _ hexec.BackgroundExecutor = (*DockerExecutor)(nil)

// dockerArgs builds the `docker exec` argument list for a command.
func (e *DockerExecutor) dockerArgs(command string, env []string) []string {
	args := []string{"exec"}
	if e.WorkDir != "" {
		args = append(args, "-w", e.WorkDir)
	}
	for _, kv := range env {
		args = append(args, "-e", kv)
	}
	args = append(args, e.Container, "bash", "-c", command)
	return args
}

// Run executes command to completion inside the container.
func (e *DockerExecutor) Run(ctx context.Context, command string, opts hexec.RunOptions) (*hexec.RunResult, error) {
	runCtx := ctx
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(runCtx, "docker", e.dockerArgs(command, opts.Env)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	res := &hexec.RunResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if runCtx.Err() == context.DeadlineExceeded {
		res.TimedOut = true
		res.ExitCode = -1
		return res, fmt.Errorf("command timed out")
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			res.ExitCode = exitErr.ExitCode()
			return res, nil
		}
		res.ExitCode = -1
		return res, err
	}
	res.ExitCode = 0
	return res, nil
}

type dockerShell struct {
	cmd *exec.Cmd

	mu      sync.Mutex
	stdout  bytes.Buffer
	stderr  bytes.Buffer
	running bool
	exit    int
}

type dockerSyncBuf struct {
	sh  *dockerShell
	buf *bytes.Buffer
}

func (w dockerSyncBuf) Write(p []byte) (int, error) {
	w.sh.mu.Lock()
	defer w.sh.mu.Unlock()
	return w.buf.Write(p)
}

// Start launches a detached `docker exec` that outlives the tool call.
func (e *DockerExecutor) Start(_ context.Context, command string, opts hexec.RunOptions) (string, error) {
	cmd := exec.Command("docker", e.dockerArgs(command, opts.Env)...)
	sh := &dockerShell{cmd: cmd, running: true}
	cmd.Stdout = dockerSyncBuf{sh: sh, buf: &sh.stdout}
	cmd.Stderr = dockerSyncBuf{sh: sh, buf: &sh.stderr}

	if err := cmd.Start(); err != nil {
		return "", err
	}
	id := "shell_" + strconv.FormatUint(e.counter.Add(1), 10)
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

// Poll drains output produced since the previous call for id.
func (e *DockerExecutor) Poll(id string) (*hexec.BackgroundStatus, error) {
	v, ok := e.bg.Load(id)
	if !ok {
		return nil, fmt.Errorf("shell not found: %s", id)
	}
	sh := v.(*dockerShell)
	sh.mu.Lock()
	defer sh.mu.Unlock()
	st := &hexec.BackgroundStatus{
		Stdout:   sh.stdout.String(),
		Stderr:   sh.stderr.String(),
		Running:  sh.running,
		ExitCode: sh.exit,
	}
	sh.stdout.Reset()
	sh.stderr.Reset()
	return st, nil
}

// Kill terminates the detached `docker exec`. Note: this stops the streaming
// exec; to also stop the in-container process you would `docker exec` a kill by
// name/pid. For the common case (a server tied to the exec) this is enough.
func (e *DockerExecutor) Kill(id string) error {
	v, ok := e.bg.Load(id)
	if !ok {
		return fmt.Errorf("shell not found: %s", id)
	}
	sh := v.(*dockerShell)
	sh.mu.Lock()
	proc := sh.cmd.Process
	sh.mu.Unlock()
	if proc != nil {
		_ = proc.Kill()
	}
	e.bg.Delete(id)
	return nil
}
