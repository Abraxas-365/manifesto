package subagent

// Background runs: Agent with run_in_background=true returns a run id
// immediately; the AgentOutput tool polls status/result. Mirrors the Bash
// tool's run_in_background + WorkerOutput pattern (and pi-subagents' async
// delegation).

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/tool"
)

// RunStatus is the lifecycle state of a background run.
type RunStatus string

const (
	RunRunning RunStatus = "running"
	RunDone    RunStatus = "done"
	RunFailed  RunStatus = "failed"
)

// Run is one background subagent execution.
type Run struct {
	ID      string
	Agent   string // agent type, "" for the default
	Owner   string // owning session id ("" when unknown)
	Prompt  string
	Status  RunStatus
	Result  string
	Err     string
	Started time.Time
	Ended   time.Time
	// Activity is a rolling log of what the child agent is doing (tool calls,
	// text), newest last. Capped at maxRunActivity entries.
	Activity []string
	done     chan struct{}
	cancel   context.CancelFunc // cancels the run's detached context
}

// snapshot returns a copy safe to hand out (Activity deep-copied so later
// appends never race with readers).
func (r *Run) snapshot() Run {
	c := *r
	c.Activity = append([]string(nil), r.Activity...)
	return c
}

// maxRunActivity caps the per-run activity log (oldest entries dropped).
const maxRunActivity = 500

// runMetaKey carries the run id into the detached background context so the
// child agent's progress hooks can attribute events to the run.
type runMetaKey struct{}

// RunIDFromContext returns the background run id injected by Runs.Start
// ("" for foreground executions).
func RunIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(runMetaKey{}).(string)
	return id
}

// Runs tracks background subagent executions. Safe for concurrent use.
type Runs struct {
	mu   sync.Mutex
	runs map[string]*Run
	seq  int
	// OnDone, when set, is called after a run finishes (bus notification /
	// completion injection into the owning session).
	OnDone func(r Run)
	// OnStart, when set, is called when a run is registered (bus notification
	// so a UI can show the run as live immediately).
	OnStart func(r Run)
	// OwnerFromContext, when set, extracts the owning session id from the
	// Start ctx so OnDone can route the completion back to that session.
	OwnerFromContext func(ctx context.Context) string
	// OnActivity, when set, is called after an activity line is appended to a
	// running run (bus notification so a UI can show live progress).
	OnActivity func(r Run, line string)
}

// NewRuns returns an empty run tracker.
func NewRuns() *Runs { return &Runs{runs: map[string]*Run{}} }

// Start registers a new run and executes fn on a goroutine. fn returns the
// final answer or an error. ctx is only used to capture the owning session
// (OwnerFromContext); execution deliberately detaches from it.
func (rs *Runs) Start(ctx context.Context, agentName, prompt string, fn func(ctx context.Context) (string, error)) *Run {
	owner := ""
	if rs.OwnerFromContext != nil {
		owner = rs.OwnerFromContext(ctx)
	}
	rs.mu.Lock()
	rs.seq++
	r := &Run{
		ID:      fmt.Sprintf("t%d", rs.seq),
		Agent:   agentName,
		Owner:   owner,
		Prompt:  prompt,
		Status:  RunRunning,
		Started: time.Now(),
		done:    make(chan struct{}),
	}
	rs.runs[r.ID] = r
	startCB := rs.OnStart
	startSnapshot := r.snapshot()
	rs.mu.Unlock()

	if startCB != nil {
		startCB(startSnapshot)
	}

	runCtx, runCancel := context.WithCancel(
		context.WithValue(context.WithoutCancel(ctx), runMetaKey{}, r.ID))
	r.cancel = runCancel

	go func() {
		// Background runs deliberately detach from the tool call's ctx: the
		// parent turn ends before the run does. The detached context is
		// cancellable via Kill().
		out, err := fn(runCtx)
		rs.mu.Lock()
		r.Ended = time.Now()
		if err != nil {
			r.Status = RunFailed
			r.Err = err.Error()
		} else {
			r.Status = RunDone
			r.Result = out
		}
		snapshot := r.snapshot()
		cb := rs.OnDone
		rs.mu.Unlock()
		close(r.done)
		if cb != nil {
			cb(snapshot)
		}
	}()
	return r
}

// Kill cancels a running background run by id. Returns an error if the id is
// unknown or the run already finished. The run's goroutine will see context
// cancellation, stop, and fire OnDone with RunFailed status.
func (rs *Runs) Kill(id string) error {
	rs.mu.Lock()
	r, ok := rs.runs[id]
	if !ok {
		rs.mu.Unlock()
		return fmt.Errorf("unknown run id %q", id)
	}
	if r.Status != RunRunning {
		rs.mu.Unlock()
		return fmt.Errorf("run %s already finished (%s)", id, r.Status)
	}
	cancel := r.cancel
	rs.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

// Close cancels all running background runs. Called on shutdown.
func (rs *Runs) Close() {
	rs.mu.Lock()
	var cancels []context.CancelFunc
	for _, r := range rs.runs {
		if r.Status == RunRunning && r.cancel != nil {
			cancels = append(cancels, r.cancel)
		}
	}
	rs.mu.Unlock()
	for _, c := range cancels {
		c()
	}
}

// Get returns a snapshot of the run.
func (rs *Runs) Get(id string) (Run, bool) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	r, ok := rs.runs[id]
	if !ok {
		return Run{}, false
	}
	return r.snapshot(), true
}

// AddActivity appends a progress line to a running run's activity log and
// fires OnActivity. No-op for unknown or finished runs.
func (rs *Runs) AddActivity(id, line string) {
	rs.mu.Lock()
	r, ok := rs.runs[id]
	if !ok || r.Status != RunRunning {
		rs.mu.Unlock()
		return
	}
	r.Activity = append(r.Activity, line)
	if len(r.Activity) > maxRunActivity {
		r.Activity = r.Activity[len(r.Activity)-maxRunActivity:]
	}
	cb := rs.OnActivity
	snapshot := r.snapshot()
	rs.mu.Unlock()
	if cb != nil {
		cb(snapshot, line)
	}
}

// Wait blocks until the run finishes or timeout elapses, returning the final
// snapshot. timeout <= 0 returns immediately.
func (rs *Runs) Wait(id string, timeout time.Duration) (Run, bool) {
	rs.mu.Lock()
	r, ok := rs.runs[id]
	rs.mu.Unlock()
	if !ok {
		return Run{}, false
	}
	if timeout > 0 {
		select {
		case <-r.done:
		case <-time.After(timeout):
		}
	}
	return rs.Get(id)
}

// List returns snapshots of all runs, oldest first.
func (rs *Runs) List() []Run {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	out := make([]Run, 0, len(rs.runs))
	for _, r := range rs.runs {
		out = append(out, r.snapshot())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Started.Before(out[j].Started) })
	return out
}

// WaitTool blocks until one or all background SubAgent runs finish.
type WaitTool struct {
	Runs *Runs
}

func (t *WaitTool) Name() string { return "SubAgentWait" }

func (t *WaitTool) Description() string {
	return "Block until background SubAgent runs finish. " +
		"Pass a specific run_id, or omit to wait on all active runs. " +
		"Returns the final status and result of the completed run(s)."
}

func (t *WaitTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"run_id": {"type": "string", "description": "Wait for this specific run. Omit to wait on all active runs."},
			"timeout": {"type": "number", "description": "Max seconds to wait (default: 1800 = 30 min)"}
		}
	}`)
}

func (t *WaitTool) IsReadOnly() bool { return true }

func (t *WaitTool) Execute(_ context.Context, input json.RawMessage) (*tool.Result, error) {
	var in struct {
		RunID   string  `json:"run_id"`
		Timeout float64 `json:"timeout"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return &tool.Result{Content: fmt.Sprintf("Invalid input: %v", err), IsError: true}, nil
	}
	if t.Runs == nil {
		return &tool.Result{Content: "No background runs are configured", IsError: true}, nil
	}
	timeout := time.Duration(in.Timeout * float64(time.Second))
	if timeout <= 0 {
		timeout = 30 * time.Minute
	}

	if in.RunID != "" {
		// Wait for a specific run.
		r, ok := t.Runs.Wait(in.RunID, timeout)
		if !ok {
			return &tool.Result{Content: fmt.Sprintf("Unknown run id %q", in.RunID), IsError: true}, nil
		}
		return &tool.Result{Content: formatRunResult(r), IsError: r.Status == RunFailed}, nil
	}

	// Wait for all active runs.
	deadline := time.Now().Add(timeout)
	runs := t.Runs.List()
	var waited []Run
	for _, r := range runs {
		if r.Status != RunRunning {
			waited = append(waited, r)
			continue
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			got, _ := t.Runs.Get(r.ID)
			waited = append(waited, got)
			continue
		}
		got, _ := t.Runs.Wait(r.ID, remaining)
		waited = append(waited, got)
	}

	var b strings.Builder
	for i, r := range waited {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(formatRunResult(r))
	}
	if b.Len() == 0 {
		return &tool.Result{Content: "No background runs found."}, nil
	}
	return &tool.Result{Content: b.String()}, nil
}

func formatRunResult(r Run) string {
	var b strings.Builder
	fmt.Fprintf(&b, "run %s: %s", r.ID, r.Status)
	if r.Agent != "" {
		fmt.Fprintf(&b, " (agent %s)", r.Agent)
	}
	switch r.Status {
	case RunRunning:
		fmt.Fprintf(&b, ", elapsed %s", time.Since(r.Started).Round(time.Second))
	case RunDone:
		fmt.Fprintf(&b, " in %s\n\n%s", r.Ended.Sub(r.Started).Round(time.Second), r.Result)
	case RunFailed:
		fmt.Fprintf(&b, " in %s\n\nerror: %s", r.Ended.Sub(r.Started).Round(time.Second), r.Err)
	}
	return b.String()
}
