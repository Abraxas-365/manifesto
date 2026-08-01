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
// Read/Write/Edit and shell commands all run inside the container. No bind
// mount, no host filesystem, nothing to keep in sync.
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
	"strings"

	agent "github.com/Abraxas-365/manifesto/internal/ai/harness"
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

	registry, _ := builtins.FromExecutor(ex)

	agent := agent.New(openai.New(key), registry)
	agent.System = "You are a coding assistant working inside a Docker sandbox. " +
		"File tools edit the workspace; Bash runs commands in the container."
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
// implements exec.Executor, so it is a drop-in replacement for LocalExecutor
// that targets the container instead of the host.
type DockerExecutor struct {
	Container string
	WorkDir   string
}

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
