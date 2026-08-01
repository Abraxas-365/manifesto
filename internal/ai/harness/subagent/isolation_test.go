package subagent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	agent "github.com/Abraxas-365/manifesto/internal/ai/harness"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/agentdef"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/tool"
)

// fakeIsolator records isolate calls and returns scripted isolations.
// Thread-safe for parallel task tests.
type fakeIsolator struct {
	err      error
	kept     bool
	mu       sync.Mutex
	labels   []string
	finished int
}

func (f *fakeIsolator) Isolate(label string) (*Isolation, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.mu.Lock()
	f.labels = append(f.labels, label)
	f.mu.Unlock()
	return &Isolation{
		Dir:      "/tmp/wt-" + label,
		MainRoot: "/main",
		Branch:   "claudio-parallel-" + label,
		Finish: func() bool {
			f.mu.Lock()
			f.finished++
			f.mu.Unlock()
			return f.kept
		},
	}, nil
}

func TestIsolation_RejectedWithoutIsolator(t *testing.T) {
	res, _ := newTool(&stubProvider{answer: "ok"}).Execute(context.Background(),
		[]byte(`{"prompt":"x","isolation":"worktree"}`))
	if !res.IsError || !strings.Contains(res.Content, "not available") {
		t.Fatalf("expected isolation-unavailable error, got: %q", res.Content)
	}
}

func TestIsolation_UnknownMode(t *testing.T) {
	tl := newTool(&stubProvider{answer: "ok"})
	tl.Isolator = &fakeIsolator{}
	res, _ := tl.Execute(context.Background(), []byte(`{"prompt":"x","isolation":"container"}`))
	if !res.IsError || !strings.Contains(res.Content, "Unknown isolation mode") {
		t.Fatalf("expected unknown-mode error, got: %q", res.Content)
	}
}

func TestIsolation_SoloRun_WorkdirOnContext(t *testing.T) {
	// The provider can't see ctx, but a tool executed by the subagent can:
	// verify through the answer flow instead — here we assert the isolate
	// wrapper ran (labels recorded) and Finish was called exactly once.
	iso := &fakeIsolator{}
	tl := newTool(&stubProvider{answer: "done"})
	tl.Isolator = iso

	res, err := tl.Execute(context.Background(), []byte(`{"prompt":"x","isolation":"worktree"}`))
	if err != nil || res.IsError {
		t.Fatalf("unexpected error: %v %s", err, res.Content)
	}
	if len(iso.labels) != 1 {
		t.Fatalf("expected 1 isolation, got %d", len(iso.labels))
	}
	if iso.finished != 1 {
		t.Fatalf("Finish must run exactly once, got %d", iso.finished)
	}
	if !strings.Contains(res.Content, "[worktree removed") {
		t.Fatalf("unchanged worktree should report removal, got: %q", res.Content)
	}
}

func TestIsolation_KeptWorktreeReported(t *testing.T) {
	iso := &fakeIsolator{kept: true}
	tl := newTool(&stubProvider{answer: "done"})
	tl.Isolator = iso

	res, _ := tl.Execute(context.Background(), []byte(`{"prompt":"x","isolation":"worktree"}`))
	if !strings.Contains(res.Content, "[worktree kept") ||
		!strings.Contains(res.Content, "/tmp/wt-solo") ||
		!strings.Contains(res.Content, "claudio-parallel-solo") {
		t.Fatalf("kept worktree must report path+branch, got: %q", res.Content)
	}
}

func TestIsolation_IsolateErrorSurfaced(t *testing.T) {
	iso := &fakeIsolator{err: errors.New("dirty tree")}
	tl := newTool(&stubProvider{answer: "done"})
	tl.Isolator = iso

	res, _ := tl.Execute(context.Background(), []byte(`{"prompt":"x","isolation":"worktree"}`))
	if !res.IsError || !strings.Contains(res.Content, "worktree isolation") {
		t.Fatalf("isolate error must surface, got: %q", res.Content)
	}
}

func TestIsolation_FinishRunsEvenOnRunError(t *testing.T) {
	iso := &fakeIsolator{}
	tl := newTool(&stubProvider{err: errors.New("boom")})
	tl.Isolator = iso

	res, _ := tl.Execute(context.Background(), []byte(`{"prompt":"x","isolation":"worktree"}`))
	if !res.IsError {
		t.Fatal("expected subagent error result")
	}
	if iso.finished != 1 {
		t.Fatalf("Finish must run on failure too (cleanup), got %d", iso.finished)
	}
}

func TestIsolation_ParallelTasks_OneWorktreePerTask(t *testing.T) {
	iso := &fakeIsolator{}
	tl := newTool(&stubProvider{answer: "ok"})
	tl.Isolator = iso

	res, err := tl.Execute(context.Background(),
		[]byte(`{"tasks":[{"task":"a"},{"task":"b"},{"task":"c"}],"isolation":"worktree"}`))
	if err != nil || res.IsError {
		t.Fatalf("unexpected error: %v %s", err, res.Content)
	}
	if len(iso.labels) != 3 {
		t.Fatalf("expected 3 worktrees, got %d: %v", len(iso.labels), iso.labels)
	}
	if iso.finished != 3 {
		t.Fatalf("expected 3 Finish calls, got %d", iso.finished)
	}
}

func TestIsolation_NoIsolationByDefault(t *testing.T) {
	iso := &fakeIsolator{}
	tl := newTool(&stubProvider{answer: "ok"})
	tl.Isolator = iso

	res, err := tl.Execute(context.Background(), []byte(`{"prompt":"x"}`))
	if err != nil || res.IsError {
		t.Fatalf("unexpected error: %v %s", err, res.Content)
	}
	if len(iso.labels) != 0 {
		t.Fatal("isolator must not run without isolation input")
	}
	if strings.Contains(res.Content, "worktree") {
		t.Fatalf("no worktree notes without isolation, got: %q", res.Content)
	}
}

func TestIsolation_SchemaMentionsWorktreeOnlyWhenConfigured(t *testing.T) {
	plain := newTool(&stubProvider{})
	if strings.Contains(string(plain.InputSchema()), "isolation") {
		t.Error("schema must not offer isolation without an isolator")
	}
	isoTool := newTool(&stubProvider{})
	isoTool.Isolator = &fakeIsolator{}
	if !strings.Contains(string(isoTool.InputSchema()), `"isolation"`) {
		t.Error("schema must offer isolation when isolator is configured")
	}
}

// ctxProbeTool records the workdir seen on its Execute ctx.
type ctxProbeTool struct{ seen *string }

func (p ctxProbeTool) Name() string                 { return "Probe" }
func (p ctxProbeTool) Description() string          { return "probe" }
func (p ctxProbeTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (p ctxProbeTool) IsReadOnly() bool             { return true }
func (p ctxProbeTool) Execute(ctx context.Context, _ json.RawMessage) (*tool.Result, error) {
	*p.seen = tool.Workdir(ctx)
	return &tool.Result{Content: "probed"}, nil
}

// Verifies the isolated workdir actually reaches the subagent's tool context.
func TestIsolation_WorkdirReachesSubagentTools(t *testing.T) {
	var seen string
	reg := tool.NewRegistry()
	reg.Register(ctxProbeTool{seen: &seen})
	tl := &Tool{NewAgent: func() *agent.Agent {
		return agent.New(&toolCallingProvider{}, reg)
	}}
	tl.Isolator = &fakeIsolator{}

	res, err := tl.Execute(context.Background(), []byte(`{"prompt":"x","isolation":"worktree"}`))
	if err != nil || res.IsError {
		t.Fatalf("unexpected error: %v %s", err, res.Content)
	}
	if seen != "/tmp/wt-solo" {
		t.Fatalf("workdir did not reach subagent tool ctx: %q", seen)
	}
}

// ---------------------------------------------------------------------------
// Edge case: context cancellation still calls Finish (cleanup)
// ---------------------------------------------------------------------------

func TestIsolation_ContextCancelled_FinishStillRuns(t *testing.T) {
	iso := &fakeIsolator{kept: true}
	// Provider that blocks until context is canceled.
	tl := &Tool{NewAgent: func() *agent.Agent {
		return agent.New(&blockingProvider{}, tool.NewRegistry())
	}}
	tl.Isolator = iso

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	res, _ := tl.Execute(ctx, []byte(`{"prompt":"x","isolation":"worktree"}`))
	if !res.IsError {
		t.Fatal("expected error from canceled context")
	}
	if iso.finished != 1 {
		t.Fatalf("Finish must be called on cancellation, got %d calls", iso.finished)
	}
}

// blockingProvider returns ctx.Err() so it fails on canceled contexts.
type blockingProvider struct{}

func (p *blockingProvider) Chat(ctx context.Context, _ llm.Request) (*llm.Response, error) {
	return nil, ctx.Err()
}
func (p *blockingProvider) ChatStream(context.Context, llm.Request) (llm.Stream, error) {
	return nil, errors.New("not implemented")
}

// ---------------------------------------------------------------------------
// Edge case: run error with kept worktree reports path in error message
// ---------------------------------------------------------------------------

func TestIsolation_RunError_KeptWorktreeReportedInError(t *testing.T) {
	iso := &fakeIsolator{kept: true}
	tl := newTool(&stubProvider{err: errors.New("model exploded")})
	tl.Isolator = iso

	res, _ := tl.Execute(context.Background(), []byte(`{"prompt":"x","isolation":"worktree"}`))
	if !res.IsError {
		t.Fatal("expected error result")
	}
	if !strings.Contains(res.Content, "worktree kept") {
		t.Fatalf("error should mention kept worktree, got: %q", res.Content)
	}
	if !strings.Contains(res.Content, "/tmp/wt-solo") {
		t.Fatalf("error should include worktree path, got: %q", res.Content)
	}
}

func TestIsolation_RunError_CleanWorktreeNoExtraMessage(t *testing.T) {
	iso := &fakeIsolator{kept: false}
	tl := newTool(&stubProvider{err: errors.New("model exploded")})
	tl.Isolator = iso

	res, _ := tl.Execute(context.Background(), []byte(`{"prompt":"x","isolation":"worktree"}`))
	if !res.IsError {
		t.Fatal("expected error result")
	}
	if strings.Contains(res.Content, "worktree kept") {
		t.Fatalf("clean worktree should not be mentioned in error, got: %q", res.Content)
	}
}

// ---------------------------------------------------------------------------
// Edge case: parallel tasks with count replication get unique worktrees
// ---------------------------------------------------------------------------

func TestIsolation_ParallelWithCountReplication(t *testing.T) {
	iso := &fakeIsolator{}
	tl := newTool(&stubProvider{answer: "ok"})
	tl.Isolator = iso

	res, err := tl.Execute(context.Background(),
		[]byte(`{"tasks":[{"task":"a","count":3}],"isolation":"worktree"}`))
	if err != nil || res.IsError {
		t.Fatalf("unexpected error: %v %s", err, res.Content)
	}
	if len(iso.labels) != 3 {
		t.Fatalf("expected 3 worktrees (count=3), got %d: %v", len(iso.labels), iso.labels)
	}
	// All labels must be unique.
	seen := map[string]bool{}
	for _, l := range iso.labels {
		if seen[l] {
			t.Fatalf("duplicate worktree label: %q", l)
		}
		seen[l] = true
	}
	if iso.finished != 3 {
		t.Fatalf("expected 3 Finish calls, got %d", iso.finished)
	}
}

// ---------------------------------------------------------------------------
// Edge case: parallel tasks — one errors, others succeed, all Finish called
// ---------------------------------------------------------------------------

func TestIsolation_ParallelPartialFailure_AllFinishCalled(t *testing.T) {
	var finished atomic.Int64
	// Isolator that tracks finish calls atomically (parallel goroutines).
	mkIso := func() *Isolation {
		return &Isolation{
			Dir:      "/tmp/wt",
			MainRoot: "/main",
			Branch:   "b",
			Finish: func() bool {
				finished.Add(1)
				return false
			},
		}
	}
	iso := &scriptedIsolator{factory: mkIso}

	callCount := atomic.Int64{}
	failOnSecond := &stubProvider{answer: "ok"}
	// Build a tool where the second call fails.
	tl := &Tool{NewAgent: func() *agent.Agent {
		n := callCount.Add(1)
		var p llm.Provider
		if n == 2 {
			p = &stubProvider{err: errors.New("boom on task 2")}
		} else {
			p = failOnSecond
		}
		return agent.New(p, tool.NewRegistry())
	}}
	tl.Isolator = iso

	res, err := tl.Execute(context.Background(),
		[]byte(`{"tasks":[{"task":"a"},{"task":"b"},{"task":"c"}],"isolation":"worktree"}`))
	if err != nil {
		t.Fatalf("Execute returned err: %v", err)
	}
	// One task should have errored.
	if !strings.Contains(res.Content, "ERROR") {
		t.Fatalf("expected partial failure, got: %q", res.Content)
	}
	// All 3 Finish calls must have happened.
	if got := finished.Load(); got != 3 {
		t.Fatalf("expected 3 Finish calls, got %d", got)
	}
}

// scriptedIsolator calls factory() for each Isolate, useful for per-call tracking.
type scriptedIsolator struct {
	factory func() *Isolation
}

func (s *scriptedIsolator) Isolate(label string) (*Isolation, error) {
	iso := s.factory()
	iso.Dir += "-" + label
	iso.Branch += "-" + label
	return iso, nil
}

// ---------------------------------------------------------------------------
// Edge case: nested subagent inherits Isolator
// ---------------------------------------------------------------------------

func TestIsolation_ChildInheritsIsolator(t *testing.T) {
	iso := &fakeIsolator{}
	parent := newTool(&stubProvider{answer: "ok"})
	parent.Isolator = iso
	parent.Depth = 0
	parent.MaxDepth = 3

	def := agentdef.Definition{Name: "child"}
	child := parent.childFor(def)
	if child == nil {
		t.Fatal("childFor returned nil")
	}
	if child.Isolator == nil {
		t.Fatal("child must inherit Isolator from parent")
	}
	if child.Isolator != iso {
		t.Fatal("child must share the same Isolator instance")
	}
}

// ---------------------------------------------------------------------------
// Edge case: background + isolation — worktree created in goroutine
// ---------------------------------------------------------------------------

func TestIsolation_BackgroundDeferredCreation(t *testing.T) {
	iso := &fakeIsolator{}
	tl := newTool(&stubProvider{answer: "bg done"})
	tl.Isolator = iso
	tl.Runs = NewRuns()

	res, err := tl.Execute(context.Background(),
		[]byte(`{"prompt":"x","isolation":"worktree","run_in_background":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	if !strings.Contains(res.Content, "Started background run") {
		t.Fatalf("expected background run start message, got: %q", res.Content)
	}

	// At this point the worktree might not be created yet (deferred to
	// the goroutine). Wait for the run to finish.
	runs := tl.Runs.List()
	if len(runs) == 0 {
		t.Fatal("no background runs registered")
	}

	// Poll until done (with short timeout).
	for i := 0; i < 100; i++ {
		r, ok := tl.Runs.Get(runs[0].ID)
		if ok && r.Status != RunRunning {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	r, _ := tl.Runs.Get(runs[0].ID)
	if r.Status == RunRunning {
		t.Fatal("background run did not finish")
	}
	// Worktree must have been created and finished.
	if len(iso.labels) != 1 {
		t.Fatalf("expected 1 worktree creation, got %d", len(iso.labels))
	}
	if iso.finished != 1 {
		t.Fatalf("expected 1 Finish call, got %d", iso.finished)
	}
}
