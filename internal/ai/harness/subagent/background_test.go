package subagent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRunsLifecycle(t *testing.T) {
	rs := NewRuns()
	var doneRun Run
	var wg sync.WaitGroup
	wg.Add(1)
	rs.OnDone = func(r Run) { doneRun = r; wg.Done() }

	release := make(chan struct{})
	r := rs.Start(context.Background(), "scout", "look", func(ctx context.Context) (string, error) {
		<-release
		return "found it", nil
	})
	if got, _ := rs.Get(r.ID); got.Status != RunRunning {
		t.Fatalf("status: %s", got.Status)
	}
	close(release)
	wg.Wait()
	got, _ := rs.Get(r.ID)
	if got.Status != RunDone || got.Result != "found it" {
		t.Fatalf("final: %+v", got)
	}
	if doneRun.ID != r.ID || doneRun.Agent != "scout" {
		t.Fatalf("OnDone: %+v", doneRun)
	}
}

func TestRunsOwnerFromContext(t *testing.T) {
	rs := NewRuns()
	type key struct{}
	rs.OwnerFromContext = func(ctx context.Context) string {
		s, _ := ctx.Value(key{}).(string)
		return s
	}
	var doneRun Run
	var wg sync.WaitGroup
	wg.Add(1)
	rs.OnDone = func(r Run) { doneRun = r; wg.Done() }

	ctx := context.WithValue(context.Background(), key{}, "sess-42")
	r := rs.Start(ctx, "scout", "look", func(ctx context.Context) (string, error) {
		return "ok", nil
	})
	wg.Wait()
	if r.Owner != "sess-42" || doneRun.Owner != "sess-42" {
		t.Fatalf("owner not captured: start=%q done=%q", r.Owner, doneRun.Owner)
	}
}

func TestRunsFailure(t *testing.T) {
	rs := NewRuns()
	r := rs.Start(context.Background(), "", "x", func(ctx context.Context) (string, error) {
		return "", errors.New("kaput")
	})
	got, _ := rs.Wait(r.ID, time.Second)
	if got.Status != RunFailed || got.Err != "kaput" {
		t.Fatalf("failed run: %+v", got)
	}
}

func TestRunsActivity(t *testing.T) {
	rs := NewRuns()
	var lines []string
	rs.OnActivity = func(r Run, line string) { lines = append(lines, line) }

	release := make(chan struct{})
	gotRunID := make(chan string, 1)
	r := rs.Start(context.Background(), "scout", "look", func(ctx context.Context) (string, error) {
		gotRunID <- RunIDFromContext(ctx)
		<-release
		return "done", nil
	})

	// The detached ctx must carry the run id for hook attribution.
	if id := <-gotRunID; id != r.ID {
		t.Fatalf("RunIDFromContext = %q, want %q", id, r.ID)
	}

	rs.AddActivity(r.ID, "→ Read {file}")
	rs.AddActivity(r.ID, "✓ Read")
	rs.AddActivity("nope", "ignored") // unknown run: no-op

	got, _ := rs.Get(r.ID)
	if len(got.Activity) != 2 || got.Activity[0] != "→ Read {file}" || got.Activity[1] != "✓ Read" {
		t.Fatalf("activity: %#v", got.Activity)
	}
	if len(lines) != 2 {
		t.Fatalf("OnActivity calls: %#v", lines)
	}

	// Snapshot isolation: mutating the returned slice must not affect the run.
	got.Activity[0] = "tampered"
	got2, _ := rs.Get(r.ID)
	if got2.Activity[0] != "→ Read {file}" {
		t.Fatalf("snapshot not isolated: %#v", got2.Activity)
	}

	close(release)
	rs.Wait(r.ID, time.Second)
	// Finished runs ignore further activity.
	rs.AddActivity(r.ID, "late")
	got3, _ := rs.Get(r.ID)
	if len(got3.Activity) != 2 {
		t.Fatalf("activity after done: %#v", got3.Activity)
	}
}

func TestRunsActivityCap(t *testing.T) {
	rs := NewRuns()
	release := make(chan struct{})
	r := rs.Start(context.Background(), "", "x", func(ctx context.Context) (string, error) {
		<-release
		return "", nil
	})
	defer close(release)
	for i := 0; i < maxRunActivity+50; i++ {
		rs.AddActivity(r.ID, "line")
	}
	got, _ := rs.Get(r.ID)
	if len(got.Activity) != maxRunActivity {
		t.Fatalf("cap: %d", len(got.Activity))
	}
}

func TestActionStatus(t *testing.T) {
	slow := &stubProvider{answer: "bg answer"}
	tl := newTool(slow)
	tl.Runs = NewRuns()

	release := make(chan struct{})
	r := tl.Runs.Start(context.Background(), "scout", "look", func(ctx context.Context) (string, error) {
		<-release
		return "answer", nil
	})

	// Status while running.
	res, err := tl.Execute(context.Background(), []byte(`{"action":"status","run_id":"`+r.ID+`"}`))
	if err != nil || res.IsError || !strings.Contains(res.Content, "running") {
		t.Fatalf("running status: %+v err=%v", res, err)
	}
	// Status all (no run_id).
	res, _ = tl.Execute(context.Background(), []byte(`{"action":"status"}`))
	if res.IsError || !strings.Contains(res.Content, r.ID) {
		t.Fatalf("status all: %+v", res)
	}
	close(release)
	tl.Runs.Wait(r.ID, time.Second)
	// Status after done.
	res, _ = tl.Execute(context.Background(), []byte(`{"action":"status","run_id":"`+r.ID+`"}`))
	if res.IsError || !strings.Contains(res.Content, "answer") {
		t.Fatalf("done status: %+v", res)
	}
	// Unknown id.
	res, _ = tl.Execute(context.Background(), []byte(`{"action":"status","run_id":"nope"}`))
	if !res.IsError || !strings.Contains(res.Content, r.ID) {
		t.Fatalf("unknown id: %+v", res)
	}
}

func TestRunsKill(t *testing.T) {
	rs := NewRuns()
	var doneRun Run
	var wg sync.WaitGroup
	wg.Add(1)
	rs.OnDone = func(r Run) { doneRun = r; wg.Done() }

	r := rs.Start(context.Background(), "scout", "look", func(ctx context.Context) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})
	if got, _ := rs.Get(r.ID); got.Status != RunRunning {
		t.Fatalf("status before kill: %s", got.Status)
	}
	if err := rs.Kill(r.ID); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	wg.Wait()
	got, _ := rs.Get(r.ID)
	if got.Status != RunFailed {
		t.Fatalf("status after kill: %s", got.Status)
	}
	if !strings.Contains(got.Err, "canceled") {
		t.Fatalf("err after kill: %q", got.Err)
	}
	if doneRun.Status != RunFailed {
		t.Fatalf("OnDone status: %s", doneRun.Status)
	}
	// Kill again should error.
	if err := rs.Kill(r.ID); err == nil {
		t.Fatal("double kill should error")
	}
	// Kill unknown id.
	if err := rs.Kill("nope"); err == nil {
		t.Fatal("kill unknown should error")
	}
}

func TestRunsClose(t *testing.T) {
	rs := NewRuns()
	var wg sync.WaitGroup
	wg.Add(2)
	rs.OnDone = func(r Run) { wg.Done() }

	rs.Start(context.Background(), "a", "x", func(ctx context.Context) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})
	rs.Start(context.Background(), "b", "y", func(ctx context.Context) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})
	rs.Close()
	wg.Wait()
	for _, r := range rs.List() {
		if r.Status != RunFailed {
			t.Fatalf("run %s: %s", r.ID, r.Status)
		}
	}
}

func TestActionStop(t *testing.T) {
	slow := &stubProvider{answer: "bg answer"}
	tl := newTool(slow)
	tl.Runs = NewRuns()
	var wg sync.WaitGroup
	wg.Add(1)
	tl.Runs.OnDone = func(r Run) { wg.Done() }

	r := tl.Runs.Start(context.Background(), "scout", "look", func(ctx context.Context) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})

	res, err := tl.Execute(context.Background(), []byte(`{"action":"stop","run_id":"`+r.ID+`"}`))
	if err != nil || res.IsError {
		t.Fatalf("stop: %+v err=%v", res, err)
	}
	if !strings.Contains(res.Content, r.ID) {
		t.Fatalf("stop response: %q", res.Content)
	}
	wg.Wait()

	// Stop finished run.
	res, _ = tl.Execute(context.Background(), []byte(`{"action":"stop","run_id":"`+r.ID+`"}`))
	if !res.IsError {
		t.Fatal("stop finished should error")
	}

	// Stop unknown.
	res, _ = tl.Execute(context.Background(), []byte(`{"action":"stop","run_id":"nope"}`))
	if !res.IsError {
		t.Fatal("stop unknown should error")
	}

	// Stop without run_id.
	res, _ = tl.Execute(context.Background(), []byte(`{"action":"stop"}`))
	if !res.IsError {
		t.Fatal("stop without run_id should error")
	}

	// Unknown action.
	res, _ = tl.Execute(context.Background(), []byte(`{"action":"bogus"}`))
	if !res.IsError {
		t.Fatal("unknown action should error")
	}
}

func TestWaitToolSingleRun(t *testing.T) {
	rs := NewRuns()
	wt := &WaitTool{Runs: rs}

	r := rs.Start(context.Background(), "scout", "look", func(ctx context.Context) (string, error) {
		return "the answer", nil
	})
	// Wait for the run to actually finish.
	rs.Wait(r.ID, time.Second)

	res, err := wt.Execute(context.Background(), []byte(`{"run_id":"`+r.ID+`"}`))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %s", res.Content)
	}
	if !strings.Contains(res.Content, "the answer") {
		t.Fatalf("expected result in output: %q", res.Content)
	}
	if !strings.Contains(res.Content, "done") {
		t.Fatalf("expected done status: %q", res.Content)
	}
}

func TestWaitToolAllRuns(t *testing.T) {
	rs := NewRuns()
	wt := &WaitTool{Runs: rs}

	// Start 3 runs that complete quickly.
	for i := 0; i < 3; i++ {
		rs.Start(context.Background(), "scout", "task", func(ctx context.Context) (string, error) {
			return "result", nil
		})
	}
	// Let them all finish.
	time.Sleep(50 * time.Millisecond)

	start := time.Now()
	res, err := wt.Execute(context.Background(), []byte(`{}`))
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	// Should return almost immediately since all runs are done.
	if elapsed > 2*time.Second {
		t.Fatalf("WaitTool blocked too long (%s) for already-finished runs", elapsed)
	}
	if !strings.Contains(res.Content, "done") {
		t.Fatalf("expected done in output: %q", res.Content)
	}
}

func TestWaitToolAllRunsDoesNotBlockIndefinitely(t *testing.T) {
	rs := NewRuns()
	wt := &WaitTool{Runs: rs}

	// Start a run that finishes after 100ms.
	rs.Start(context.Background(), "fast", "go", func(ctx context.Context) (string, error) {
		time.Sleep(100 * time.Millisecond)
		return "fast done", nil
	})

	start := time.Now()
	res, err := wt.Execute(context.Background(), []byte(`{"timeout": 10}`))
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	// Should return in ~100ms, not wait the full 10s timeout.
	if elapsed > 2*time.Second {
		t.Fatalf("WaitTool blocked too long: %s (run finishes in 100ms)", elapsed)
	}
	if !strings.Contains(res.Content, "fast done") {
		t.Fatalf("expected result: %q", res.Content)
	}
}

func TestWaitToolUnknownRunID(t *testing.T) {
	rs := NewRuns()
	wt := &WaitTool{Runs: rs}

	res, err := wt.Execute(context.Background(), []byte(`{"run_id":"nope"}`))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected error for unknown run_id")
	}
}

func TestWaitToolNoRuns(t *testing.T) {
	rs := NewRuns()
	wt := &WaitTool{Runs: rs}

	res, err := wt.Execute(context.Background(), []byte(`{}`))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	if !strings.Contains(res.Content, "No background runs") {
		t.Fatalf("expected no runs message: %q", res.Content)
	}
}

func TestWaitToolNilRuns(t *testing.T) {
	wt := &WaitTool{Runs: nil}

	res, err := wt.Execute(context.Background(), []byte(`{}`))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected error when Runs is nil")
	}
	if !strings.Contains(res.Content, "No background runs") {
		t.Fatalf("expected message: %q", res.Content)
	}
}

func TestWaitToolMixedFinishedAndRunning(t *testing.T) {
	rs := NewRuns()
	wt := &WaitTool{Runs: rs}

	// One run that finishes immediately.
	rs.Start(context.Background(), "fast", "quick", func(ctx context.Context) (string, error) {
		return "instant", nil
	})
	time.Sleep(20 * time.Millisecond) // let it finish

	// One run that takes 100ms.
	rs.Start(context.Background(), "slow", "wait", func(ctx context.Context) (string, error) {
		time.Sleep(100 * time.Millisecond)
		return "delayed", nil
	})

	start := time.Now()
	res, err := wt.Execute(context.Background(), []byte(`{"timeout": 10}`))
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("blocked too long: %s", elapsed)
	}
	if !strings.Contains(res.Content, "instant") {
		t.Fatalf("missing fast result: %q", res.Content)
	}
	if !strings.Contains(res.Content, "delayed") {
		t.Fatalf("missing slow result: %q", res.Content)
	}
}

func TestWaitToolAllFinishedReturnsImmediately(t *testing.T) {
	rs := NewRuns()
	wt := &WaitTool{Runs: rs}

	// Start 3 runs that complete instantly.
	for i := 0; i < 3; i++ {
		rs.Start(context.Background(), "done", "task", func(ctx context.Context) (string, error) {
			return "ok", nil
		})
	}
	// Let them finish.
	for _, r := range rs.List() {
		rs.Wait(r.ID, time.Second)
	}

	start := time.Now()
	res, err := wt.Execute(context.Background(), []byte(`{"timeout": 30}`))
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	// All finished — should return near-instantly, not wait 30s.
	if elapsed > 500*time.Millisecond {
		t.Fatalf("blocked %s for already-finished runs", elapsed)
	}
}

func TestWaitToolFailedRun(t *testing.T) {
	rs := NewRuns()
	wt := &WaitTool{Runs: rs}

	r := rs.Start(context.Background(), "bad", "fail", func(ctx context.Context) (string, error) {
		return "", errors.New("something broke")
	})
	rs.Wait(r.ID, time.Second)

	res, err := wt.Execute(context.Background(), []byte(`{"run_id":"`+r.ID+`"}`))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected IsError for failed run")
	}
	if !strings.Contains(res.Content, "something broke") {
		t.Fatalf("expected error message: %q", res.Content)
	}
}

func TestTaskBackgroundParam(t *testing.T) {
	slow := &stubProvider{answer: "bg answer"}
	tl := newTool(slow)
	tl.Runs = NewRuns()

	res, err := tl.Execute(context.Background(), []byte(`{"prompt":"do it","run_in_background":true}`))
	if err != nil || res.IsError {
		t.Fatalf("bg start: %+v err=%v", res, err)
	}
	if !strings.Contains(res.Content, "t1") {
		t.Fatalf("no run id: %q", res.Content)
	}
	got, ok := tl.Runs.Wait("t1", 5*time.Second)
	if !ok || got.Status != RunDone || got.Result != "bg answer" {
		t.Fatalf("bg result: %+v", got)
	}
	// Schema advertises the param only when Runs is set.
	if !strings.Contains(string(tl.InputSchema()), "run_in_background") {
		t.Fatal("schema missing run_in_background")
	}
	plain := newTool(slow)
	if strings.Contains(string(plain.InputSchema()), "run_in_background") {
		t.Fatal("schema must omit run_in_background without Runs")
	}
	res, _ = plain.Execute(context.Background(), []byte(`{"prompt":"x","run_in_background":true}`))
	if !res.IsError {
		t.Fatal("bg without Runs must error")
	}
}
