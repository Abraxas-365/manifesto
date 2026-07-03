package exec

import (
	"context"
	"strings"
	"testing"
	"time"
)

func pollUntil(t *testing.T, e *LocalExecutor, id string, want func(*BackgroundStatus) bool) *BackgroundStatus {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var last *BackgroundStatus
	var acc strings.Builder
	for time.Now().Before(deadline) {
		st, err := e.Poll(id)
		if err != nil {
			t.Fatalf("Poll: %v", err)
		}
		acc.WriteString(st.Stdout)
		st.Stdout = acc.String()
		last = st
		if want(st) {
			return st
		}
		time.Sleep(20 * time.Millisecond)
	}
	return last
}

func TestBackground_StartPollExit(t *testing.T) {
	e := NewLocalExecutor("")
	id, err := e.Start(context.Background(), "echo hello; sleep 0.1; echo world", RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(id, "shell_") {
		t.Fatalf("unexpected id %q", id)
	}

	st := pollUntil(t, e, id, func(s *BackgroundStatus) bool { return !s.Running })
	if st.Running {
		t.Fatal("command should have exited")
	}
	if !strings.Contains(st.Stdout, "hello") || !strings.Contains(st.Stdout, "world") {
		t.Fatalf("missing output: %q", st.Stdout)
	}
	if st.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0", st.ExitCode)
	}
}

func TestBackground_Kill(t *testing.T) {
	e := NewLocalExecutor("")
	id, err := e.Start(context.Background(), "while true; do sleep 0.05; done", RunOptions{})
	if err != nil {
		t.Fatal(err)
	}

	// Confirm it is running.
	st, err := e.Poll(id)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Running {
		t.Fatal("expected long-lived command to be running")
	}

	if err := e.Kill(id); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	// After Kill the ID is gone.
	if _, err := e.Poll(id); err == nil {
		t.Fatal("expected Poll to fail after Kill")
	}
}

func TestBackground_PollIncremental(t *testing.T) {
	e := NewLocalExecutor("")
	id, err := e.Start(context.Background(), "echo one; sleep 0.3; echo two", RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Kill(id)

	// First chunk should contain "one" but not "two".
	first := pollUntil(t, e, id, func(s *BackgroundStatus) bool {
		return strings.Contains(s.Stdout, "one")
	})
	if strings.Contains(first.Stdout, "two") {
		t.Skip("timing: second line arrived too fast to test incremental drain")
	}

	// A fresh Poll drains only new output (accumulator reset here).
	st := pollUntil(t, e, id, func(s *BackgroundStatus) bool {
		return strings.Contains(s.Stdout, "two")
	})
	if !strings.Contains(st.Stdout, "two") {
		t.Fatalf("expected second line, got %q", st.Stdout)
	}
}

func TestBackground_PollUnknownID(t *testing.T) {
	e := NewLocalExecutor("")
	if _, err := e.Poll("shell_999"); err == nil {
		t.Fatal("expected error for unknown shell ID")
	}
	if err := e.Kill("shell_999"); err == nil {
		t.Fatal("expected error for killing unknown shell ID")
	}
}
