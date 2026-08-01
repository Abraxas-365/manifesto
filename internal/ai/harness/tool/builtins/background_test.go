package builtins

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/exec"
)

// fakeBgExec implements exec.BackgroundExecutor for tool tests.
type fakeBgExec struct {
	fakeExec
	started  string
	polls    int
	killed   string
	status   *exec.BackgroundStatus
	startErr error
}

func (e *fakeBgExec) Start(_ context.Context, command string, _ exec.RunOptions) (string, error) {
	if e.startErr != nil {
		return "", e.startErr
	}
	e.started = command
	return "shell_1", nil
}

func (e *fakeBgExec) Poll(id string) (*exec.BackgroundStatus, error) {
	e.polls++
	if e.status != nil {
		return e.status, nil
	}
	return &exec.BackgroundStatus{Stdout: "hi", Running: true}, nil
}

func (e *fakeBgExec) Kill(id string) error {
	e.killed = id
	return nil
}

func TestBash_BackgroundStart(t *testing.T) {
	fe := &fakeBgExec{}
	b := &Bash{Exec: fe}

	in, _ := json.Marshal(map[string]any{"command": "npm start", "run_in_background": true})
	res, err := b.Execute(context.Background(), in)
	if err != nil || res.IsError {
		t.Fatalf("unexpected: err=%v res=%+v", err, res)
	}
	if fe.started != "npm start" {
		t.Fatalf("Start not called with command, got %q", fe.started)
	}
	if !strings.Contains(res.Content, "shell_1") {
		t.Fatalf("expected shell ID in output, got %q", res.Content)
	}
}

func TestBash_BackgroundUnsupported(t *testing.T) {
	// Plain fakeExec has no background support.
	b := &Bash{Exec: &fakeExec{}}
	in, _ := json.Marshal(map[string]any{"command": "npm start", "run_in_background": true})
	res, _ := b.Execute(context.Background(), in)
	if !res.IsError {
		t.Fatal("expected error when executor lacks background support")
	}
}

func TestBashOutput(t *testing.T) {
	fe := &fakeBgExec{status: &exec.BackgroundStatus{Stdout: "log line", Running: true}}
	tl := &BashOutput{Exec: fe}

	in, _ := json.Marshal(map[string]any{"shell_id": "shell_1"})
	res, err := tl.Execute(context.Background(), in)
	if err != nil || res.IsError {
		t.Fatalf("unexpected: err=%v res=%+v", err, res)
	}
	if !strings.Contains(res.Content, "log line") || !strings.Contains(res.Content, "running") {
		t.Fatalf("unexpected content: %q", res.Content)
	}
}

func TestBashOutput_Exited(t *testing.T) {
	fe := &fakeBgExec{status: &exec.BackgroundStatus{Running: false, ExitCode: 2}}
	tl := &BashOutput{Exec: fe}

	in, _ := json.Marshal(map[string]any{"shell_id": "shell_1"})
	res, _ := tl.Execute(context.Background(), in)
	if !strings.Contains(res.Content, "exited, code 2") {
		t.Fatalf("expected exit notice, got %q", res.Content)
	}
}

func TestKillShell(t *testing.T) {
	fe := &fakeBgExec{}
	tl := &KillShell{Exec: fe}

	in, _ := json.Marshal(map[string]any{"shell_id": "shell_1"})
	res, err := tl.Execute(context.Background(), in)
	if err != nil || res.IsError {
		t.Fatalf("unexpected: err=%v res=%+v", err, res)
	}
	if fe.killed != "shell_1" {
		t.Fatalf("Kill not called, got %q", fe.killed)
	}
}

func TestDefault_RegistersBackgroundTools(t *testing.T) {
	fs := newMemFS()
	// LocalExecutor implements BackgroundExecutor.
	r, _ := Default(fs, exec.NewLocalExecutor(""))
	for _, name := range []string{"BashOutput", "KillShell"} {
		if _, ok := r.Get(name); !ok {
			t.Fatalf("expected %s to be registered for a BackgroundExecutor", name)
		}
	}
}

func TestDefault_NoBackgroundToolsWhenUnsupported(t *testing.T) {
	fs := newMemFS()
	r, _ := Default(fs, &fakeExec{})
	if _, ok := r.Get("BashOutput"); ok {
		t.Fatal("BashOutput should not be registered for a plain Executor")
	}
}
