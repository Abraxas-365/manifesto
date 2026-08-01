package builtins

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/exec"
)

// fakeExec returns a canned result and records the command it received.
type fakeExec struct {
	stdout   string
	exitCode int
	lastCmd  string
	calls    int
}

func (e *fakeExec) Run(_ context.Context, command string, _ exec.RunOptions) (*exec.RunResult, error) {
	e.calls++
	e.lastCmd = command
	return &exec.RunResult{Stdout: e.stdout, ExitCode: e.exitCode}, nil
}

func runBash(t *testing.T, b *Bash, command string) (string, bool) {
	t.Helper()
	in, _ := json.Marshal(map[string]any{"command": command})
	res, err := b.Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return res.Content, res.IsError
}

func TestBash_DefaultDenylist(t *testing.T) {
	fe := &fakeExec{stdout: "should not run"}
	b := &Bash{Exec: fe}

	_, isErr := runBash(t, b, "sudo rm -rf / --no-preserve-root")
	if !isErr {
		t.Fatal("expected default denylist to block 'rm -rf /'")
	}
	if fe.calls != 0 {
		t.Fatal("denied command must not reach the executor")
	}
}

func TestBash_CustomDenylist(t *testing.T) {
	fe := &fakeExec{stdout: "output"}
	b := &Bash{Exec: fe, DeniedCommands: []string{"forbidden"}}

	// Custom denylist blocks its own entry.
	if _, isErr := runBash(t, b, "run forbidden thing"); !isErr {
		t.Fatal("expected custom denylist to block 'forbidden'")
	}
	// And no longer blocks the default entries it replaced.
	if _, isErr := runBash(t, b, "rm -rf /"); isErr {
		t.Fatal("custom denylist should replace defaults, allowing 'rm -rf /'")
	}
	if fe.calls != 1 {
		t.Fatalf("expected the allowed command to run once, got %d calls", fe.calls)
	}
}

func TestBash_DisabledDenylist(t *testing.T) {
	fe := &fakeExec{stdout: "ran"}
	b := &Bash{Exec: fe, DeniedCommands: []string{}} // non-nil empty = disabled

	if _, isErr := runBash(t, b, "rm -rf /"); isErr {
		t.Fatal("empty non-nil denylist should disable blocking")
	}
	if fe.calls != 1 {
		t.Fatal("command should have run")
	}
}

func TestBash_CustomMaxOutput(t *testing.T) {
	fe := &fakeExec{stdout: strings.Repeat("x", 100)}
	b := &Bash{Exec: fe, MaxOutput: 10}

	out, _ := runBash(t, b, "echo big")
	if !strings.Contains(out, "[Showing lines") {
		t.Fatalf("expected truncation with small MaxOutput, got len=%d", len(out))
	}
	// Tail truncation: the last 10 bytes should be preserved.
	if !strings.Contains(out, strings.Repeat("x", 10)) {
		t.Fatalf("expected last 10 bytes preserved in truncated output, got %q", out)
	}
}

func TestBash_TruncationSpillsToTempFile(t *testing.T) {
	fe := &fakeExec{stdout: strings.Repeat("x", 100)}
	b := &Bash{Exec: fe, MaxOutput: 10}

	out, _ := runBash(t, b, "echo big")
	// The notice must point at a readable file containing the FULL output.
	i := strings.Index(out, "Full output: ")
	if i < 0 {
		t.Fatalf("truncation notice missing spill path: %q", out)
	}
	rest := out[i+len("Full output: "):]
	path := rest[:strings.IndexByte(rest, ' ')]
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("spill file unreadable: %v", err)
	}
	defer os.Remove(path)
	if string(data) != strings.Repeat("x", 100) {
		t.Fatalf("spill file must hold full output, got %d bytes", len(data))
	}
}

func TestBash_UncappedOutput(t *testing.T) {
	fe := &fakeExec{stdout: strings.Repeat("y", DefaultMaxBashOutput+50)}
	b := &Bash{Exec: fe, MaxOutput: -1} // negative = no cap

	out, _ := runBash(t, b, "echo huge")
	if strings.Contains(out, "[Showing lines") {
		t.Fatal("expected no truncation with MaxOutput=-1")
	}
}

func TestBash_DefaultMaxOutputTruncates(t *testing.T) {
	fe := &fakeExec{stdout: strings.Repeat("z", DefaultMaxBashOutput+50)}
	b := &Bash{Exec: fe} // defaults

	out, _ := runBash(t, b, "echo huge")
	if !strings.Contains(out, "[Showing lines") {
		t.Fatal("expected default cap to truncate oversized output")
	}
}

// workdirExec records the RunOptions it received.
type workdirExec struct {
	fakeExec
	lastOpts exec.RunOptions
}

func (e *workdirExec) Run(ctx context.Context, command string, opts exec.RunOptions) (*exec.RunResult, error) {
	e.lastOpts = opts
	return e.fakeExec.Run(ctx, command, opts)
}

func TestBash_WorkdirParam(t *testing.T) {
	dir := t.TempDir()
	fe := &workdirExec{fakeExec: fakeExec{stdout: "ok"}}
	b := &Bash{Exec: fe}

	in, _ := json.Marshal(map[string]any{"command": "pwd", "workdir": dir})
	res, err := b.Execute(context.Background(), in)
	if err != nil || res.IsError {
		t.Fatalf("unexpected error: %v %s", err, res.Content)
	}
	if fe.lastOpts.WorkDir != dir {
		t.Errorf("workdir not passed: got %q", fe.lastOpts.WorkDir)
	}

	// Nonexistent workdir is rejected before reaching the executor.
	in, _ = json.Marshal(map[string]any{"command": "pwd", "workdir": dir + "/nope"})
	res, _ = b.Execute(context.Background(), in)
	if !res.IsError || !strings.Contains(res.Content, "workdir does not exist") {
		t.Errorf("want workdir validation error, got: %s", res.Content)
	}
}

func TestBash_DescriptionParamAccepted(t *testing.T) {
	fe := &fakeExec{stdout: "ok"}
	b := &Bash{Exec: fe}
	in, _ := json.Marshal(map[string]any{"command": "true", "description": "Runs true"})
	res, err := b.Execute(context.Background(), in)
	if err != nil || res.IsError {
		t.Fatalf("description param must be accepted: %v %s", err, res.Content)
	}
}
