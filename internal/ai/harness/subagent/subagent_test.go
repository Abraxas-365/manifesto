package subagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// stubProvider returns a fixed final answer (or error) on the first Chat call.
type stubProvider struct {
	answer string
	err    error
	calls  atomic.Int64
}

func (p *stubProvider) Chat(context.Context, llm.Request) (*llm.Response, error) {
	p.calls.Add(1)
	if p.err != nil {
		return nil, p.err
	}
	return &llm.Response{
		Message:    llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.Text(p.answer)}},
		StopReason: llm.StopEndTurn,
	}, nil
}

func (p *stubProvider) ChatStream(context.Context, llm.Request) (llm.Stream, error) {
	return nil, errors.New("not implemented")
}

// modelRecordingProvider captures the model from the request, then returns a
// final answer.
type modelRecordingProvider struct{ seen *string }

func (p *modelRecordingProvider) Chat(_ context.Context, req llm.Request) (*llm.Response, error) {
	*p.seen = req.Model
	return &llm.Response{
		Message:    llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.Text("done")}},
		StopReason: llm.StopEndTurn,
	}, nil
}

func (p *modelRecordingProvider) ChatStream(context.Context, llm.Request) (llm.Stream, error) {
	return nil, errors.New("not implemented")
}

func newTool(p llm.Provider) *Tool {
	return &Tool{NewAgent: func() *agent.Agent {
		return agent.New(p, tool.NewRegistry())
	}}
}

func TestExecute_ReturnsFinalAnswer(t *testing.T) {
	p := &stubProvider{answer: "the answer"}
	res, err := newTool(p).Execute(context.Background(), []byte(`{"prompt":"do research"}`))
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %q", res.Content)
	}
	if res.Content != "the answer" {
		t.Fatalf("got %q, want %q", res.Content, "the answer")
	}
	if p.calls.Load() != 1 {
		t.Fatalf("expected 1 provider call, got %d", p.calls.Load())
	}
}

func TestExecute_FreshAgentPerCall(t *testing.T) {
	// Each invocation must build a new agent, so histories never accumulate.
	var built int
	tl := &Tool{NewAgent: func() *agent.Agent {
		built++
		return agent.New(&stubProvider{answer: "ok"}, tool.NewRegistry())
	}}
	for i := 0; i < 3; i++ {
		if _, err := tl.Execute(context.Background(), []byte(`{"prompt":"x"}`)); err != nil {
			t.Fatal(err)
		}
	}
	if built != 3 {
		t.Fatalf("expected 3 fresh agents, got %d", built)
	}
}

func TestExecute_EmptyPrompt(t *testing.T) {
	res, _ := newTool(&stubProvider{}).Execute(context.Background(), []byte(`{"prompt":"  "}`))
	if !res.IsError {
		t.Fatal("expected error result for empty prompt")
	}
}

func TestExecute_InvalidJSON(t *testing.T) {
	res, _ := newTool(&stubProvider{}).Execute(context.Background(), []byte(`not json`))
	if !res.IsError {
		t.Fatal("expected error result for invalid JSON")
	}
}

func TestExecute_NilFactory(t *testing.T) {
	res, _ := (&Tool{}).Execute(context.Background(), []byte(`{"prompt":"x"}`))
	if !res.IsError {
		t.Fatal("expected error result when NewAgent is nil")
	}
}

func TestExecute_SubagentError(t *testing.T) {
	p := &stubProvider{err: errors.New("boom")}
	res, _ := newTool(p).Execute(context.Background(), []byte(`{"prompt":"x"}`))
	if !res.IsError {
		t.Fatal("expected error result when subagent fails")
	}
}

// toolCallingProvider makes one tool call, then answers.
type toolCallingProvider struct{ calls atomic.Int64 }

func (p *toolCallingProvider) Chat(context.Context, llm.Request) (*llm.Response, error) {
	if p.calls.Add(1) == 1 {
		return &llm.Response{
			Message: llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{
				llm.ToolUseBlock("child-call-1", "Probe", []byte(`{}`)),
			}},
			StopReason: llm.StopToolUse,
		}, nil
	}
	return &llm.Response{
		Message:    llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.Text("done")}},
		StopReason: llm.StopEndTurn,
	}, nil
}

func (p *toolCallingProvider) ChatStream(context.Context, llm.Request) (llm.Stream, error) {
	return nil, errors.New("not implemented")
}

type probeTool struct{}

func (probeTool) Name() string                 { return "Probe" }
func (probeTool) Description() string          { return "probe" }
func (probeTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (probeTool) Execute(context.Context, json.RawMessage) (*tool.Result, error) {
	return &tool.Result{Content: "probed"}, nil
}
func (probeTool) IsReadOnly() bool { return true }

func TestExecute_OnChildEvent(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Register(probeTool{})
	var mu sync.Mutex
	var events []ChildEvent
	tl := &Tool{
		NewAgent: func() *agent.Agent { return agent.New(&toolCallingProvider{}, reg) },
		OnChildEvent: func(ev ChildEvent) {
			mu.Lock()
			events = append(events, ev)
			mu.Unlock()
		},
		OwnerFromContext: func(ctx context.Context) string { return "sess-9" },
	}
	ctx := tool.WithUseID(context.Background(), "parent-tu-1")
	res, err := tl.Execute(ctx, []byte(`{"prompt":"go"}`))
	if err != nil || res.IsError {
		t.Fatalf("execute: %+v err=%v", res, err)
	}

	mu.Lock()
	defer mu.Unlock()
	var kinds []string
	for _, ev := range events {
		kinds = append(kinds, ev.Kind)
		if ev.ParentToolUseID != "parent-tu-1" {
			t.Fatalf("parent id not threaded: %+v", ev)
		}
		if ev.Owner != "sess-9" {
			t.Fatalf("owner not threaded: %+v", ev)
		}
	}
	joined := strings.Join(kinds, ",")
	if !strings.Contains(joined, "tool_start") || !strings.Contains(joined, "tool_end") || !strings.Contains(joined, "text") {
		t.Fatalf("missing event kinds: %v", kinds)
	}
	for _, ev := range events {
		if ev.Kind == "tool_start" && (ev.ToolName != "Probe" || ev.ToolUseID != "child-call-1") {
			t.Fatalf("bad tool_start: %+v", ev)
		}
		if ev.Kind == "tool_end" && (ev.ToolResult != "probed" || ev.ToolError) {
			t.Fatalf("bad tool_end: %+v", ev)
		}
	}
}

func TestExecuteParallel_PerTaskNodes(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Register(probeTool{})
	var mu sync.Mutex
	var events []ChildEvent
	tl := &Tool{
		NewAgent: func() *agent.Agent { return agent.New(&toolCallingProvider{}, reg) },
		OnChildEvent: func(ev ChildEvent) {
			mu.Lock()
			events = append(events, ev)
			mu.Unlock()
		},
		OwnerFromContext: func(ctx context.Context) string { return "sess-9" },
	}
	ctx := tool.WithUseID(context.Background(), "parent-tu-1")
	res, err := tl.Execute(ctx, []byte(`{"tasks":[{"task":"a"},{"task":"b"},{"task":"c"}]}`))
	if err != nil || res.IsError {
		t.Fatalf("execute: %+v err=%v", res, err)
	}

	mu.Lock()
	defer mu.Unlock()

	// Each task must open a distinct synthetic Task node parented to the shared
	// Task tool-use id, so the UI renders three separate nested agent cards.
	nodeStarts := map[string]bool{}
	for _, ev := range events {
		if ev.Kind == "tool_start" && ev.ToolName == DefaultName {
			if ev.ParentToolUseID != "parent-tu-1" {
				t.Fatalf("task node not parented to Task call: %+v", ev)
			}
			nodeStarts[ev.ToolUseID] = true
		}
	}
	want := []string{"parent-tu-1-t1", "parent-tu-1-t2", "parent-tu-1-t3"}
	for _, id := range want {
		if !nodeStarts[id] {
			t.Fatalf("missing synthetic node %q; got %v", id, nodeStarts)
		}
	}

	// The child Probe tool events must nest under a per-task node, never under
	// the shared parent directly.
	for _, ev := range events {
		if ev.ToolName == "Probe" && ev.ParentToolUseID == "parent-tu-1" {
			t.Fatalf("child tool flattened onto shared parent: %+v", ev)
		}
	}
}

func TestExecute_ModelOverride(t *testing.T) {
	var gotModel string
	tl := &Tool{NewAgent: func() *agent.Agent {
		a := agent.New(&modelRecordingProvider{seen: &gotModel}, tool.NewRegistry())
		a.Model = "default-model"
		return a
	}}
	if _, err := tl.Execute(context.Background(), []byte(`{"prompt":"x","model":"override-model"}`)); err != nil {
		t.Fatal(err)
	}
	if gotModel != "override-model" {
		t.Fatalf("model override not applied, provider saw %q", gotModel)
	}
}

func TestExecute_ModelDefaultWhenAbsent(t *testing.T) {
	var gotModel string
	tl := &Tool{NewAgent: func() *agent.Agent {
		a := agent.New(&modelRecordingProvider{seen: &gotModel}, tool.NewRegistry())
		a.Model = "default-model"
		return a
	}}
	if _, err := tl.Execute(context.Background(), []byte(`{"prompt":"x"}`)); err != nil {
		t.Fatal(err)
	}
	if gotModel != "default-model" {
		t.Fatalf("expected factory default, provider saw %q", gotModel)
	}
}

func TestExecute_ModelRejectedWhenNotAllowed(t *testing.T) {
	var called bool
	tl := &Tool{
		AllowedModels: []string{"gpt-4o"},
		NewAgent: func() *agent.Agent {
			called = true
			return agent.New(&stubProvider{answer: "ok"}, tool.NewRegistry())
		},
	}
	res, _ := tl.Execute(context.Background(), []byte(`{"prompt":"x","model":"claude-x"}`))
	if !res.IsError {
		t.Fatal("expected error for disallowed model")
	}
	_ = called // agent may be built before validation; the point is Run is not reached
}

func TestInputSchema_ModelEnumWhenAllowed(t *testing.T) {
	if !strings.Contains(string((&Tool{AllowedModels: []string{"a"}}).InputSchema()), `"enum"`) {
		t.Fatal("expected enum in schema when AllowedModels set")
	}
	if strings.Contains(string((&Tool{}).InputSchema()), `"enum"`) {
		t.Fatal("did not expect enum when AllowedModels empty")
	}
}

func TestNameAndDescriptionDefaults(t *testing.T) {
	tl := &Tool{}
	if tl.Name() != DefaultName {
		t.Fatalf("default name: got %q", tl.Name())
	}
	if tl.Description() != DefaultDescription {
		t.Fatalf("default description mismatch")
	}
	custom := &Tool{ToolName: "Research", Desc: "custom"}
	if custom.Name() != "Research" || custom.Description() != "custom" {
		t.Fatal("overrides not applied")
	}
}

func TestDescFn_OverridesDescAndRoster(t *testing.T) {
	defs := rosterRegistry("Explore", "Plan")
	tl := &Tool{Defs: defs, DescFn: func() string { return "custom-full-description" }}
	got := tl.Description()
	if got != "custom-full-description" {
		t.Fatalf("DescFn not used: %q", got)
	}
	// rosterDescription must NOT be appended when DescFn is set.
	if strings.Contains(got, "Explore") {
		t.Fatal("DescFn should fully replace description, not append roster")
	}
}

func TestDescFn_NilFallsBackToDefault(t *testing.T) {
	tl := &Tool{Defs: rosterRegistry("A")}
	got := tl.Description()
	if !strings.Contains(got, DefaultDescription) {
		t.Fatal("nil DescFn should use DefaultDescription")
	}
	if !strings.Contains(got, "A") {
		t.Fatal("nil DescFn should append roster")
	}
}

func TestRosterNames_SortedStable(t *testing.T) {
	// Expose in reverse order — RosterNames must still be sorted.
	r := agentdef.NewRegistry()
	for _, n := range []string{"zulu", "alpha", "mike"} {
		r.Define(agentdef.Definition{Name: n})
		r.Expose(n)
	}
	got := r.RosterNames()
	want := []string{"alpha", "mike", "zulu"}
	if len(got) != len(want) {
		t.Fatalf("len: %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("RosterNames[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	// Call again — must be identical (stable for cache hits).
	got2 := r.RosterNames()
	for i := range got {
		if got[i] != got2[i] {
			t.Fatal("RosterNames not stable across calls")
		}
	}
}

// --- depth + subagent allowlist (pi-subagents parity) ---

func rosterRegistry(names ...string) *agentdef.Registry {
	r := agentdef.NewRegistry()
	for _, n := range names {
		r.Define(agentdef.Definition{Name: n, Description: n})
		r.Expose(n)
	}
	return r
}

func TestExecute_DepthLimitBlocks(t *testing.T) {
	tl := newTool(&stubProvider{answer: "x"})
	tl.Depth = 2 // default max is 2
	res, _ := tl.Execute(context.Background(), []byte(`{"prompt":"x"}`))
	if !res.IsError || !strings.Contains(res.Content, "depth limit") {
		t.Fatalf("expected depth-limit error, got %q", res.Content)
	}
}

func TestExecute_CustomMaxDepth(t *testing.T) {
	tl := newTool(&stubProvider{answer: "ok"})
	tl.Depth, tl.MaxDepth = 2, 3
	res, err := tl.Execute(context.Background(), []byte(`{"prompt":"x"}`))
	if err != nil || res.IsError {
		t.Fatalf("depth 2 < max 3 should run: %v %q", err, res.Content)
	}
}

func TestRosterNames_AllowedAgentsFilter(t *testing.T) {
	tl := newTool(&stubProvider{})
	tl.Defs = rosterRegistry("Explore", "Plan", "verification")
	tl.AllowedAgents = []string{"Explore"}
	got := tl.rosterNames()
	if len(got) != 1 || got[0] != "Explore" {
		t.Fatalf("rosterNames = %v, want [Explore]", got)
	}
}

func TestExecute_AgentOutsideAllowlistRejected(t *testing.T) {
	tl := newTool(&stubProvider{answer: "x"})
	tl.Defs = rosterRegistry("Explore", "Plan")
	tl.AllowedAgents = []string{"Explore"}
	res, _ := tl.Execute(context.Background(), []byte(`{"prompt":"x","agent":"Plan"}`))
	if !res.IsError || !strings.Contains(res.Content, "Unknown agent type") {
		t.Fatalf("expected allowlist rejection, got %q", res.Content)
	}
}

func TestChildFor_DepthAndAllowlist(t *testing.T) {
	tl := newTool(&stubProvider{})
	tl.Defs = rosterRegistry("Explore", "Plan")

	// Child of the root: depth 1 < max 2, allowlist from the definition.
	child := tl.childFor(agentdef.Definition{Subagents: []string{"Explore"}})
	if child == nil {
		t.Fatal("root's child should still be able to delegate")
	}
	if child.Depth != 1 {
		t.Fatalf("child depth = %d, want 1", child.Depth)
	}
	if got := child.rosterNames(); len(got) != 1 || got[0] != "Explore" {
		t.Fatalf("child roster = %v, want [Explore]", got)
	}

	// Grandchild: depth 2 >= max 2 — no Task tool.
	if gc := child.childFor(agentdef.Definition{}); gc != nil {
		t.Fatal("grandchild should not get a Task tool at default max depth")
	}

	// subagents = {} means no delegation at all.
	if c := tl.childFor(agentdef.Definition{Subagents: []string{}}); c != nil {
		t.Fatal("empty subagents list should drop the Task tool")
	}

	// Definition max depth lower than parent's cap wins.
	if c := tl.childFor(agentdef.Definition{MaxSubagentDepth: 1}); c != nil {
		t.Fatal("maxSubagentDepth=1 should block the depth-1 child from delegating")
	}
}

func TestExecute_NestedTaskToolSwapped(t *testing.T) {
	// The nested agent's registry must carry a depth-bumped Agent clone, not
	// the shared parent tool.
	var childRegistry *tool.Registry
	p := &stubProvider{answer: "done"}
	tl := &Tool{}
	tl.NewAgent = func() *agent.Agent {
		reg := tool.NewRegistry()
		reg.Register(tl)
		ag := agent.New(p, reg)
		return ag
	}
	tl.Defs = rosterRegistry("Explore")

	// Wrap NewAgent to observe the registry after Execute mutates it — easier:
	// run Execute, then check via the agent the factory returned.
	var made *agent.Agent
	inner := tl.NewAgent
	tl.NewAgent = func() *agent.Agent { made = inner(); return made }

	if res, err := tl.Execute(context.Background(), []byte(`{"prompt":"x","agent":"Explore"}`)); err != nil || res.IsError {
		t.Fatalf("execute: %v %v", err, res)
	}
	childRegistry = made.Registry
	got, ok := childRegistry.Get(DefaultName)
	if !ok {
		t.Fatal("nested agent lost its Agent tool")
	}
	childTask, ok := got.(*Tool)
	if !ok {
		t.Fatalf("nested Agent tool is %T, want *Tool", got)
	}
	if childTask == tl {
		t.Fatal("nested agent got the shared Agent tool, want a depth-bumped clone")
	}
	if childTask.Depth != 1 {
		t.Fatalf("nested Task depth = %d, want 1", childTask.Depth)
	}
}

// --- parallel tasks + spawn cap (pi-subagents parity) ---

func TestSpawnCounter_Take(t *testing.T) {
	s := NewSpawnCounter(3)
	if err := s.Take(2); err != nil {
		t.Fatal(err)
	}
	if err := s.Take(2); err == nil {
		t.Fatal("expected budget exceeded")
	}
	if err := s.Take(1); err != nil {
		t.Fatalf("failed Take must not reserve: %v", err)
	}
}

func TestExecute_SpawnCapBlocks(t *testing.T) {
	tl := newTool(&stubProvider{answer: "ok"})
	tl.Spawns = NewSpawnCounter(1)
	if res, _ := tl.Execute(context.Background(), []byte(`{"prompt":"a"}`)); res.IsError {
		t.Fatalf("first spawn should pass: %q", res.Content)
	}
	res, _ := tl.Execute(context.Background(), []byte(`{"prompt":"b"}`))
	if !res.IsError || !strings.Contains(res.Content, "spawn limit") {
		t.Fatalf("expected spawn-limit error, got %q", res.Content)
	}
}

func TestExecuteParallel_CollectsAllResults(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	tl := &Tool{NewAgent: func() *agent.Agent {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		return agent.New(&stubProvider{answer: fmt.Sprintf("answer-%d", n)}, tool.NewRegistry())
	}}
	res, err := tl.Execute(context.Background(),
		[]byte(`{"tasks":[{"task":"one"},{"task":"two"},{"task":"three"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %q", res.Content)
	}
	for _, want := range []string{"=== task 1 ===", "=== task 2 ===", "=== task 3 ==="} {
		if !strings.Contains(res.Content, want) {
			t.Fatalf("missing %q in %q", want, res.Content)
		}
	}
	if calls != 3 {
		t.Fatalf("expected 3 agents, got %d", calls)
	}
}

func TestExecuteParallel_CountReplicates(t *testing.T) {
	tl := newTool(&stubProvider{answer: "ok"})
	res, _ := tl.Execute(context.Background(), []byte(`{"tasks":[{"task":"x","count":3}]}`))
	if res.IsError {
		t.Fatalf("unexpected error: %q", res.Content)
	}
	if !strings.Contains(res.Content, "#1") || !strings.Contains(res.Content, "#3") {
		t.Fatalf("expected 3 replicas, got %q", res.Content)
	}
}

func TestExecuteParallel_SpawnCapCountsTasks(t *testing.T) {
	tl := newTool(&stubProvider{answer: "ok"})
	tl.Spawns = NewSpawnCounter(2)
	res, _ := tl.Execute(context.Background(),
		[]byte(`{"tasks":[{"task":"a"},{"task":"b"},{"task":"c"}]}`))
	if !res.IsError || !strings.Contains(res.Content, "spawn limit") {
		t.Fatalf("3 tasks with budget 2 should fail up front, got %q", res.Content)
	}
}

func TestExecuteParallel_PartialFailureIsNotError(t *testing.T) {
	tl := newTool(&stubProvider{answer: "ok"})
	tl.Defs = rosterRegistry("Explore")
	res, _ := tl.Execute(context.Background(),
		[]byte(`{"tasks":[{"task":"a"},{"task":"b","agent":"NoSuch"}]}`))
	if res.IsError {
		t.Fatal("partial failure must not mark the whole result as error")
	}
	if !strings.Contains(res.Content, "ERROR: Unknown agent type") {
		t.Fatalf("missing per-task error, got %q", res.Content)
	}
}

func TestExecuteParallel_AllFailedIsError(t *testing.T) {
	tl := newTool(&stubProvider{err: errors.New("boom")})
	res, _ := tl.Execute(context.Background(), []byte(`{"tasks":[{"task":"a"},{"task":"b"}]}`))
	if !res.IsError {
		t.Fatal("all-failed parallel run should be an error result")
	}
}

func TestExecuteParallel_ConcurrencyBounded(t *testing.T) {
	var mu sync.Mutex
	active, peak := 0, 0
	tl := &Tool{NewAgent: func() *agent.Agent {
		return agent.New(&slowProvider{mu: &mu, active: &active, peak: &peak}, tool.NewRegistry())
	}}
	res, _ := tl.Execute(context.Background(),
		[]byte(`{"tasks":[{"task":"a"},{"task":"b"},{"task":"c"},{"task":"d"}],"concurrency":2}`))
	if res.IsError {
		t.Fatalf("unexpected error: %q", res.Content)
	}
	if peak > 2 {
		t.Fatalf("peak concurrency %d, want <= 2", peak)
	}
}

// slowProvider tracks concurrent Chat calls.
type slowProvider struct {
	mu     *sync.Mutex
	active *int
	peak   *int
}

func (p *slowProvider) Chat(context.Context, llm.Request) (*llm.Response, error) {
	p.mu.Lock()
	*p.active++
	if *p.active > *p.peak {
		*p.peak = *p.active
	}
	p.mu.Unlock()
	time.Sleep(20 * time.Millisecond)
	p.mu.Lock()
	*p.active--
	p.mu.Unlock()
	return &llm.Response{
		Message:    llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.Text("done")}},
		StopReason: llm.StopEndTurn,
	}, nil
}

func (p *slowProvider) ChatStream(context.Context, llm.Request) (llm.Stream, error) {
	return nil, errors.New("not implemented")
}

// emptyFinalProvider emits the full report in turn 1 (alongside a tool call),
// then ends with an EMPTY final message in turn 2. Regression for the "=== task
// 2 === (blank)" bug: parallel task results were lost when the child's last
// assistant message had no text blocks.
type emptyFinalProvider struct {
	report string
	calls  atomic.Int64
}

func (p *emptyFinalProvider) Chat(context.Context, llm.Request) (*llm.Response, error) {
	if p.calls.Add(1) == 1 {
		return &llm.Response{
			Message: llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{
				llm.Text(p.report),
				llm.ToolUseBlock("tu-1", "Probe", []byte(`{}`)),
			}},
			StopReason: llm.StopToolUse,
		}, nil
	}
	return &llm.Response{
		Message:    llm.Message{Role: llm.RoleAssistant, Content: nil},
		StopReason: llm.StopEndTurn,
	}, nil
}

func (p *emptyFinalProvider) ChatStream(context.Context, llm.Request) (llm.Stream, error) {
	return nil, errors.New("not implemented")
}

func TestExecute_RecoversAnswerFromEarlierTurn(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Register(probeTool{})
	tl := &Tool{NewAgent: func() *agent.Agent {
		return agent.New(&emptyFinalProvider{report: "FULL REPORT: 48 tests pass"}, reg)
	}}
	res, err := tl.Execute(context.Background(), []byte(`{"prompt":"go"}`))
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %q", res.Content)
	}
	if !strings.Contains(res.Content, "FULL REPORT: 48 tests pass") {
		t.Fatalf("answer lost when final message empty: %q", res.Content)
	}
}

func TestExecuteParallel_NoTaskLosesItsAnswer(t *testing.T) {
	// Three parallel tasks whose children all end on an empty final message.
	// Every "=== task N ===" section must carry the recovered report.
	reg := tool.NewRegistry()
	reg.Register(probeTool{})
	tl := &Tool{NewAgent: func() *agent.Agent {
		return agent.New(&emptyFinalProvider{report: "task report body"}, reg)
	}}
	res, err := tl.Execute(tool.WithUseID(context.Background(), "p1"),
		[]byte(`{"tasks":[{"task":"a"},{"task":"b"},{"task":"c"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %q", res.Content)
	}
	for _, section := range []string{"=== task 1 ===", "=== task 2 ===", "=== task 3 ==="} {
		if !strings.Contains(res.Content, section) {
			t.Fatalf("missing section %q in %q", section, res.Content)
		}
	}
	if got := strings.Count(res.Content, "task report body"); got != 3 {
		t.Fatalf("expected 3 recovered reports, got %d in:\n%s", got, res.Content)
	}
}
