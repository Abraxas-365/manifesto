// Package subagent provides a tool that runs a nested agent for context-isolated
// work (research, search, focused subtasks). The nested agent has its own fresh
// history, so its intermediate steps never pollute the parent conversation —
// only its final answer is returned to the caller.
package subagent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	agent "github.com/Abraxas-365/manifesto/internal/ai/harness"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/agentdef"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/tool"
)

// DefaultName is the tool name used when Tool.ToolName is empty.
const DefaultName = "SubAgent"

// DefaultDescription is used when Tool.Desc is empty.
const DefaultDescription = "To delegate work, call with { prompt } or { tasks }; omit action. " +
	"The subagent runs in its own isolated context and cannot see this conversation. " +
	"Provide a detailed, standalone prompt. It returns only its final text answer.\n\n" +
	"EXECUTE (omit action):\n" +
	"- SINGLE: { prompt, agent?, model? } — one task\n" +
	"- PARALLEL: { tasks: [{task, agent?, model?}, ...], concurrency? } — concurrent execution\n\n" +
	"CONTROL (use action, omit prompt/tasks):\n" +
	"- { action: \"status\", run_id: \"...\" } — inspect a background run (omit run_id to list all)\n" +
	"- { action: \"stop\", run_id: \"...\" } — cancel a running background subagent"

// Factory builds a fresh agent for a single subagent invocation. It is called
// once per Execute so each subtask starts with a clean history.
type Factory func() *agent.Agent

// Tool runs a nested agent as a tool call. It implements tool.Tool.
type Tool struct {
	// NewAgent builds the nested agent for each invocation (required).
	NewAgent Factory
	// ToolName overrides the tool name. Empty uses DefaultName.
	ToolName string
	// Desc overrides the tool description. Empty uses DefaultDescription.
	Desc string
	// DescFn, when set, builds the full description dynamically (including
	// roster info). When nil, Description() falls back to Desc/DefaultDescription
	// + rosterDescription.
	DescFn func() string
	// AllowedModels, if non-empty, restricts the optional "model" parameter to
	// this set (rendered as a JSON-schema enum) and rejects anything else. Empty
	// allows any model string; the caller-supplied model, when present, overrides
	// the model set by NewAgent.
	AllowedModels []string
	// Defs, when set, enables the "agent" parameter: named blueprints
	// controlling the subagent's prompt, model, thinking, tool subset, and
	// turn budget. Only roster-exposed definitions are selectable.
	Defs *agentdef.Registry
	// SmallModel is the model id used by definitions with model="small".
	SmallModel string
	// SkillBody, when set, resolves a skill name to its body for definitions
	// with AutoloadSkills (legacy: skill bodies preloaded into the system prompt).
	SkillBody func(name string) (string, bool)
	// Runs, when set, enables run_in_background: the task returns a run id
	// immediately and the "status" action reads its status/result later.
	Runs *Runs
	// AllowedAgents, when non-nil, restricts the "agent" parameter to this
	// subset of the roster (legacy subagent_agents). nil = full roster.
	AllowedAgents []string
	// Depth is how many Task nestings deep the agent owning this tool runs
	// (0 = the root agent).
	Depth int
	// MaxDepth caps nesting: a Task call is rejected when Depth >= MaxDepth.
	// <= 0 uses DefaultMaxDepth (pi-subagents maxSubagentDepth contract).
	MaxDepth int
	// Spawns counts child launches across the whole session (shared by all
	// clones of this tool). nil = unlimited.
	Spawns *SpawnCounter
	// MaxConcurrency caps concurrent tasks in a parallel invocation.
	// <= 0 uses DefaultMaxConcurrency.
	MaxConcurrency int
	// OnChildEvent, when set, receives live progress events from nested agents
	// (tool calls, text activity, per-turn usage) so a UI can render what each
	// subagent is doing under its parent Task call. Events may arrive from
	// multiple goroutines (parallel mode).
	OnChildEvent func(ev ChildEvent)
	// OwnerFromContext, when set, extracts the owning session id from the
	// Execute ctx so ChildEvents can be routed to that session's stream.
	OwnerFromContext func(ctx context.Context) string
	// Isolator, when set, enables isolation: "worktree" — the subagent runs in
	// a fresh git worktree instead of the shared tree. nil = isolation rejected.
	Isolator Isolator
}

// ChildEvent is one live progress event from a nested agent run.
type ChildEvent struct {
	// Kind is "tool_start", "tool_end", "text", or "turn_usage".
	Kind string
	// Owner is the session id that owns the root conversation ("" when
	// unknown; from OwnerFromContext).
	Owner string
	// ParentToolUseID is the Task tool-use block ID in the parent conversation
	// that spawned this child (from ctx via tool.UseID; "" when unknown).
	ParentToolUseID string
	// Agent is the child's agent type ("" = default).
	Agent string
	// Tool fields for "tool_start" / "tool_end".
	ToolUseID string
	ToolName  string
	ToolInput json.RawMessage
	// Result fields for "tool_end".
	ToolResult string
	ToolError  bool
	// Usage for "turn_usage".
	InputTokens  int
	OutputTokens int
}

// DefaultMaxSpawns is the session-wide child-launch budget when a SpawnCounter
// is created with limit 0 (pi-subagents DEFAULT_MAX_SUBAGENT_SPAWNS_PER_SESSION).
const DefaultMaxSpawns = 40

// DefaultMaxConcurrency caps parallel task fan-out per invocation
// (pi-subagents MAX_CONCURRENCY).
const DefaultMaxConcurrency = 4

// SpawnCounter is a session-wide budget of subagent launches, counting single
// runs, every task in a parallel array, and nested children (all Tool clones
// share the parent's counter). Safe for concurrent use.
type SpawnCounter struct {
	mu    sync.Mutex
	used  int
	limit int
}

// NewSpawnCounter returns a counter with the given budget; limit <= 0 uses
// DefaultMaxSpawns.
func NewSpawnCounter(limit int) *SpawnCounter {
	if limit <= 0 {
		limit = DefaultMaxSpawns
	}
	return &SpawnCounter{limit: limit}
}

// Take reserves n launches. It returns an error when the budget would be
// exceeded (nothing is reserved then).
func (s *SpawnCounter) Take(n int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.used+n > s.limit {
		return fmt.Errorf("subagent spawn limit reached (%d/%d used, %d requested): do the remaining work directly", s.used, s.limit, n)
	}
	s.used += n
	return nil
}

// Used returns how many launches have been consumed.
func (s *SpawnCounter) Used() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.used
}

// Limit returns the total launch budget.
func (s *SpawnCounter) Limit() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.limit
}

// DefaultMaxDepth is the nesting cap when Tool.MaxDepth is unset (pi-subagents
// DEFAULT_SUBAGENT_MAX_DEPTH): the root may spawn subagents, those subagents
// may spawn one more level, grandchildren may not.
const DefaultMaxDepth = 2

func (t *Tool) effectiveMaxDepth() int {
	if t.MaxDepth > 0 {
		return t.MaxDepth
	}
	return DefaultMaxDepth
}

// rosterNames returns the selectable agent names: the registry roster
// intersected with AllowedAgents when set.
func (t *Tool) rosterNames() []string {
	if t.Defs == nil {
		return nil
	}
	names := t.Defs.RosterNames()
	if t.AllowedAgents == nil {
		return names
	}
	out := make([]string, 0, len(names))
	for _, n := range names {
		if contains(t.AllowedAgents, n) {
			out = append(out, n)
		}
	}
	return out
}

// childFor clones the tool for a nested agent defined by def: depth+1, the
// definition's subagent allowlist, and its (min'd) depth cap. Returns nil when
// the child may not spawn subagents at all — the Task tool is then omitted
// from the child's registry.
func (t *Tool) childFor(def agentdef.Definition) *Tool {
	child := *t
	child.Depth = t.Depth + 1
	// pi-subagents: child max depth = min(parent's, the definition's own).
	child.MaxDepth = t.effectiveMaxDepth()
	if def.MaxSubagentDepth > 0 && def.MaxSubagentDepth < child.MaxDepth {
		child.MaxDepth = def.MaxSubagentDepth
	}
	if child.Depth >= child.MaxDepth {
		return nil
	}
	if def.Subagents != nil {
		if len(def.Subagents) == 0 {
			return nil
		}
		child.AllowedAgents = def.Subagents
	}
	return &child
}

type input struct {
	Prompt      string     `json:"prompt"`
	Model       string     `json:"model,omitempty"`
	Agent       string     `json:"agent,omitempty"`
	Background  bool       `json:"run_in_background,omitempty"`
	Isolation   string     `json:"isolation,omitempty"`
	Tasks       []taskItem `json:"tasks,omitempty"`
	Concurrency int        `json:"concurrency,omitempty"`
	// Control actions (status / stop) — when action is set, prompt/tasks are ignored.
	Action string `json:"action,omitempty"`
	RunID  string `json:"run_id,omitempty"`
}

// taskItem is one entry of a parallel tasks array (pi-subagents PARALLEL mode).
type taskItem struct {
	Task  string `json:"task"`
	Agent string `json:"agent,omitempty"`
	Model string `json:"model,omitempty"`
	Count int    `json:"count,omitempty"`
}

// Name implements tool.Tool.
func (t *Tool) Name() string {
	if t.ToolName != "" {
		return t.ToolName
	}
	return DefaultName
}

// Description implements tool.Tool.
func (t *Tool) Description() string {
	if t.DescFn != nil {
		return t.DescFn()
	}
	desc := t.Desc
	if desc == "" {
		desc = DefaultDescription
	}
	if t.Defs != nil {
		desc += rosterDescription(t.Defs, t.rosterNames())
	}
	return desc
}

// InputSchema implements tool.Tool.
func (t *Tool) InputSchema() json.RawMessage {
	modelField := `{"type": "string", "description": "Optional model override for the subagent"}`
	if len(t.AllowedModels) > 0 {
		enum, _ := json.Marshal(t.AllowedModels)
		modelField = fmt.Sprintf(
			`{"type": "string", "description": "Optional model override for the subagent", "enum": %s}`,
			enum,
		)
	}
	agentField := ""
	if t.Defs != nil {
		if names := t.rosterNames(); len(names) > 0 {
			enum, _ := json.Marshal(names)
			agentField = fmt.Sprintf(
				`,"agent": {"type": "string", "description": "Named agent type to run the task with", "enum": %s}`,
				enum,
			)
		}
	}
	bgField := ""
	if t.Runs != nil {
		bgField = `,"run_in_background": {"type": "boolean", "description": "Launch the subagent in the background and return a run id immediately. Check status with action: \"status\", or block with SubAgentWait."},
			"action": {"type": "string", "description": "Management/control action only. Must be omitted for execution mode (launching with prompt or tasks). Accepted values: \"status\" (inspect background runs) or \"stop\" (cancel one)."},
			"run_id": {"type": "string", "description": "The background run id (e.g. t1). Required for stop, optional for status (omit to list all)."}`
	}
	isolationField := ""
	if t.Isolator != nil {
		isolationField = `,"isolation": {"type": "string", "enum": ["worktree"], "description": "Set to \"worktree\" to run in an isolated git worktree (own copy of the repo). In PARALLEL mode each task gets its own worktree. Worktrees with changes are kept and reported; unchanged ones are removed."}`
	}
	taskAgentField := ""
	if agentField != "" {
		taskAgentField = `, "agent": {"type": "string", "description": "Named agent type for this task"}`
	}
	tasksField := fmt.Sprintf(`,"tasks": {"type": "array", "description": "PARALLEL mode: run several subagents concurrently and collect all results. Each item is a self-contained task. When set, omit prompt.", "items": {"type": "object", "properties": {"task": {"type": "string", "description": "Self-contained task prompt"}%s, "model": {"type": "string", "description": "Model override for this task"}, "count": {"type": "integer", "description": "Replicate this task N times (default 1)"}}, "required": ["task"]}},
		"concurrency": {"type": "integer", "description": "PARALLEL mode: max tasks running at once (default 4)"}`, taskAgentField)
	return json.RawMessage(fmt.Sprintf(`{
		"type": "object",
		"properties": {
			"prompt": {"type": "string", "description": "A detailed, self-contained task for the subagent. It cannot see the parent conversation. Required unless tasks is set."},
			"description": {"type": "string", "description": "A short (3-5 word) summary of the task, shown in the UI"},
			"model": %s%s%s%s%s
		}
	}`, modelField, agentField, bgField, isolationField, tasksField))
}

// IsReadOnly implements tool.Tool. A subagent may use mutating tools, so it is
// conservatively reported as not read-only.
func (t *Tool) IsReadOnly() bool { return false }

// prepare builds and configures a fresh agent for one task: named profile,
// nested Task-tool clone, model override, autoloaded skills. It returns the
// run function (with the profile's timeout applied) or a user-facing error
// message.
func (t *Tool) prepare(agentName, model, prompt string) (func(ctx context.Context) (string, error), string) {
	agent := t.NewAgent()

	// Apply the named agent profile first (prompt, model, tools, turns);
	// an explicit caller model still wins below.
	var timeoutMS int
	var def agentdef.Definition
	if name := strings.TrimSpace(agentName); name != "" {
		if t.Defs == nil {
			return nil, "No agent types are configured"
		}
		var ok bool
		def, ok = t.Defs.Get(name)
		if !ok || !contains(t.rosterNames(), name) {
			return nil, fmt.Sprintf("Unknown agent type %q. Available: %s",
				name, strings.Join(t.rosterNames(), ", "))
		}
		ApplyProfile(agent, def, t.SmallModel)
		timeoutMS = def.TimeoutMS
		// legacy: autoload_skills bodies are appended to the system prompt.
		if t.SkillBody != nil {
			for _, sn := range def.AutoloadSkills {
				if body, ok := t.SkillBody(sn); ok {
					agent.System += fmt.Sprintf("\n\n<skill name=%q>\n%s\n</skill>", sn, body)
				}
			}
		}
	}

	// Nested delegation: swap the shared Task tool for a per-child clone that
	// carries depth+1 and the definition's subagent allowlist/depth cap; drop
	// it entirely when the child may not delegate (depth exhausted or
	// subagents = {}).
	if agent.Registry != nil {
		if _, hasTask := agent.Registry.Get(t.Name()); hasTask {
			sub := tool.NewRegistry()
			for _, tl := range agent.Registry.All() {
				if tl.Name() != t.Name() {
					sub.Register(tl)
					continue
				}
				if child := t.childFor(def); child != nil {
					sub.Register(child)
				}
			}
			agent.Registry = sub
		}
	}

	// Apply the optional caller-supplied model over the factory's default.
	if model = strings.TrimSpace(model); model != "" {
		if len(t.AllowedModels) > 0 && !contains(t.AllowedModels, model) {
			return nil, fmt.Sprintf("Model %q is not allowed. Choose one of: %s",
				model, strings.Join(t.AllowedModels, ", "))
		}
		agent.Model = model
	}

	// legacy: TimeoutMs hard-cancels the run's context.
	childAgentName := strings.TrimSpace(agentName)
	childDepth := t.Depth + 1
	return func(ctx context.Context) (string, error) {
		// Inject subagent metadata so tools can inspect their execution
		// environment. SessionID comes from OwnerFromContext when wired.
		sessionID := ""
		if t.OwnerFromContext != nil {
			sessionID = t.OwnerFromContext(ctx)
		}
		ctx = WithMeta(ctx, Meta{
			SessionID:       sessionID,
			RunID:           RunIDFromContext(ctx),
			AgentName:       childAgentName,
			Depth:           childDepth,
			ParentToolUseID: tool.UseID(ctx),
			Workdir:         tool.Workdir(ctx),
		})
		// Report live child progress under the parent Task call (the tool-use
		// ID travels in ctx). Wired at run time, not prepare time, because the
		// ID is only known once the parent's Execute ctx exists.
		if t.OnChildEvent != nil {
			owner := ""
			if t.OwnerFromContext != nil {
				owner = t.OwnerFromContext(ctx)
			}
			t.wireChildHooks(agent, strings.TrimSpace(agentName), owner, tool.UseID(ctx))
		}
		// Background runs additionally record a per-run activity log so a UI
		// can inspect what the detached child is doing (":bg" in the TUI).
		if runID := RunIDFromContext(ctx); runID != "" && t.Runs != nil {
			t.wireRunActivity(agent, runID)
		}
		if timeoutMS > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, time.Duration(timeoutMS)*time.Millisecond)
			defer cancel()
		}
		answer, err := agent.Run(ctx, prompt)
		return RecoverAnswer(agent, answer), err
	}, ""
}

// RecoverAnswer falls back to the last non-empty assistant text in the child's
// history when the final answer is empty. Models sometimes emit their full
// report in an earlier turn (alongside a final verification tool call) and end
// with an empty message — without this the parent receives "" even though the
// child did the work and reported it.
func RecoverAnswer(ag *agent.Agent, answer string) string {
	if strings.TrimSpace(answer) != "" {
		return answer
	}
	hist := ag.History()
	for i := len(hist) - 1; i >= 0; i-- {
		if hist[i].Role != llm.RoleAssistant {
			continue
		}
		if text := strings.TrimSpace(hist[i].TextContent()); text != "" {
			return text
		}
	}
	return answer
}

// wireRunActivity chains hooks that append one line per child tool call /
// assistant reply to the background run's activity log.
func (t *Tool) wireRunActivity(ag *agent.Agent, runID string) {
	runs := t.Runs
	prev := ag.Hooks
	ag.Hooks.OnToolStart = func(id, name string, input json.RawMessage) *agent.ToolIntercept {
		if prev.OnToolStart != nil {
			if intercept := prev.OnToolStart(id, name, input); intercept != nil {
				return intercept
			}
		}
		runs.AddActivity(runID, "→ "+name+" "+summarizeJSON(input, 120))
		return nil
	}
	ag.Hooks.OnToolEnd = func(name string, result llm.ContentBlock) *llm.ContentBlock {
		var replacement *llm.ContentBlock
		if prev.OnToolEnd != nil {
			replacement = prev.OnToolEnd(name, result)
		}
		if result.IsError {
			runs.AddActivity(runID, "✗ "+name+": "+truncateLine(result.Content, 120))
		} else {
			runs.AddActivity(runID, "✓ "+name)
		}
		return replacement
	}
	ag.Hooks.OnAssistantText = func(text string) {
		if prev.OnAssistantText != nil {
			prev.OnAssistantText(text)
		}
		runs.AddActivity(runID, "· "+truncateLine(text, 120))
	}
}

// summarizeJSON flattens a JSON object into a short single-line summary.
func summarizeJSON(raw json.RawMessage, max int) string {
	return truncateLine(string(raw), max)
}

// truncateLine collapses newlines and truncates to max runes.
func truncateLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) > max {
		return string(r[:max]) + "…"
	}
	return s
}

// emitTaskNodeStart emits a synthetic "tool_start" for a Task-typed node so the
// UI creates a nested Agent card per parallel task; child tool events then nest
// under nodeID (used as their ParentToolUseID). A no-op when OnChildEvent is
// unset.
func (t *Tool) emitTaskNodeStart(owner, parentID, nodeID, agentName, label string) {
	if t.OnChildEvent == nil {
		return
	}
	desc := label
	if agentName != "" {
		desc = agentName + " — " + label
	}
	input, _ := json.Marshal(map[string]string{"description": desc, "subagent_type": agentName})
	t.OnChildEvent(ChildEvent{
		Kind:            "tool_start",
		Owner:           owner,
		ParentToolUseID: parentID,
		Agent:           agentName,
		ToolUseID:       nodeID,
		ToolName:        DefaultName,
		ToolInput:       input,
	})
}

// emitTaskNodeEnd closes the synthetic Task node for a parallel task.
func (t *Tool) emitTaskNodeEnd(owner, parentID, nodeID, agentName, result string, isErr bool) {
	if t.OnChildEvent == nil {
		return
	}
	t.OnChildEvent(ChildEvent{
		Kind:            "tool_end",
		Owner:           owner,
		ParentToolUseID: parentID,
		Agent:           agentName,
		ToolUseID:       nodeID,
		ToolName:        DefaultName,
		ToolResult:      result,
		ToolError:       isErr,
	})
}

// wireChildHooks chains progress reporting onto the child agent's hooks,
func (t *Tool) wireChildHooks(ag *agent.Agent, agentName, owner, parentID string) {
	emit := t.OnChildEvent
	base := ChildEvent{Owner: owner, ParentToolUseID: parentID, Agent: agentName}
	prev := ag.Hooks
	ag.Hooks.OnToolStart = func(id, name string, input json.RawMessage) *agent.ToolIntercept {
		if prev.OnToolStart != nil {
			if intercept := prev.OnToolStart(id, name, input); intercept != nil {
				return intercept
			}
		}
		ev := base
		ev.Kind, ev.ToolUseID, ev.ToolName, ev.ToolInput = "tool_start", id, name, input
		emit(ev)
		return nil
	}
	ag.Hooks.OnToolEnd = func(name string, result llm.ContentBlock) *llm.ContentBlock {
		var replacement *llm.ContentBlock
		if prev.OnToolEnd != nil {
			replacement = prev.OnToolEnd(name, result)
		}
		ev := base
		ev.Kind, ev.ToolUseID, ev.ToolName = "tool_end", result.ToolUseID, name
		ev.ToolResult, ev.ToolError = result.Content, result.IsError
		emit(ev)
		return replacement
	}
	ag.Hooks.OnAssistantText = func(text string) {
		if prev.OnAssistantText != nil {
			prev.OnAssistantText(text)
		}
		ev := base
		ev.Kind = "text"
		emit(ev)
	}
	ag.Hooks.OnUsage = func(turn int, turnUsage, total llm.Usage) {
		if prev.OnUsage != nil {
			prev.OnUsage(turn, turnUsage, total)
		}
		ev := base
		ev.Kind = "turn_usage"
		ev.InputTokens, ev.OutputTokens = turnUsage.InputTokens, turnUsage.OutputTokens
		emit(ev)
	}
}

// Execute builds a fresh agent and runs the prompt, returning its final answer.
// With tasks set it fans out multiple subagents concurrently instead.
// With action set it dispatches a control action (status / stop).
func (t *Tool) Execute(ctx context.Context, raw json.RawMessage) (*tool.Result, error) {
	var in input
	if err := json.Unmarshal(raw, &in); err != nil {
		return &tool.Result{Content: fmt.Sprintf("Invalid input: %v", err), IsError: true}, nil
	}

	// Action dispatch — checked first, like pi-subagents.
	// Normalize: strip launch params so stray fields from the LLM are ignored.
	if in.Action != "" {
		return t.executeAction(input{Action: in.Action, RunID: in.RunID})
	}

	if t.NewAgent == nil {
		return &tool.Result{Content: "Subagent tool is not configured (NewAgent is nil)", IsError: true}, nil
	}
	if t.Depth >= t.effectiveMaxDepth() {
		return &tool.Result{Content: fmt.Sprintf(
			"Subagent depth limit reached (depth %d, max %d): do the work directly instead of delegating.",
			t.Depth, t.effectiveMaxDepth()), IsError: true}, nil
	}
	if msg := t.validateIsolation(in.Isolation); msg != "" {
		return &tool.Result{Content: msg, IsError: true}, nil
	}

	if len(in.Tasks) > 0 {
		return t.executeParallel(ctx, in)
	}
	if strings.TrimSpace(in.Prompt) == "" {
		return &tool.Result{Content: "No prompt provided", IsError: true}, nil
	}
	if t.Spawns != nil {
		if err := t.Spawns.Take(1); err != nil {
			return &tool.Result{Content: err.Error(), IsError: true}, nil
		}
	}

	run, errMsg := t.prepare(in.Agent, in.Model, in.Prompt)
	if errMsg != "" {
		return &tool.Result{Content: errMsg, IsError: true}, nil
	}
	if in.Isolation == isolationWorktree {
		run = t.isolate("solo", run)
	}

	if in.Background {
		if t.Runs == nil {
			return &tool.Result{Content: "Background runs are not configured", IsError: true}, nil
		}
		r := t.Runs.Start(ctx, in.Agent, in.Prompt, run)
		return &tool.Result{Content: fmt.Sprintf(
			"Started background run %s. You will be notified with the result when it completes; use SubAgent with action \"status\" and run_id %q to check on it, or SubAgentWait to block.",
			r.ID, r.ID)}, nil
	}

	answer, err := run(ctx)
	if err != nil {
		return &tool.Result{Content: fmt.Sprintf("Subagent error: %v", err), IsError: true}, nil
	}
	return &tool.Result{Content: answer}, nil
}

// executeAction dispatches control actions (status / stop) on background runs.
func (t *Tool) executeAction(in input) (*tool.Result, error) {
	if t.Runs == nil {
		return &tool.Result{Content: "No background runs are configured", IsError: true}, nil
	}
	switch in.Action {
	case "status":
		if in.RunID == "" {
			// List all runs.
			runs := t.Runs.List()
			if len(runs) == 0 {
				return &tool.Result{Content: "No background runs."}, nil
			}
			var b strings.Builder
			for i, r := range runs {
				if i > 0 {
					b.WriteString("\n\n")
				}
				b.WriteString(formatRunResult(r))
			}
			return &tool.Result{Content: b.String()}, nil
		}
		r, ok := t.Runs.Get(in.RunID)
		if !ok {
			ids := make([]string, 0)
			for _, x := range t.Runs.List() {
				ids = append(ids, x.ID)
			}
			return &tool.Result{Content: fmt.Sprintf("Unknown run id %q. Known: %s",
				in.RunID, strings.Join(ids, ", ")), IsError: true}, nil
		}
		return &tool.Result{Content: formatRunResult(r), IsError: r.Status == RunFailed}, nil

	case "stop":
		if in.RunID == "" {
			return &tool.Result{Content: "run_id is required for action \"stop\"", IsError: true}, nil
		}
		if err := t.Runs.Kill(in.RunID); err != nil {
			return &tool.Result{Content: err.Error(), IsError: true}, nil
		}
		return &tool.Result{Content: fmt.Sprintf("Stopped background subagent run %s.", in.RunID)}, nil

	default:
		return &tool.Result{Content: fmt.Sprintf("Unknown action %q. Available: status, stop", in.Action), IsError: true}, nil
	}
}

// executeParallel fans the tasks array out over a bounded worker pool and
// returns all results in task order (pi-subagents PARALLEL mode).
func (t *Tool) executeParallel(ctx context.Context, in input) (*tool.Result, error) {
	// Expand count replicas up front so spawn accounting and ordering are flat.
	type flatTask struct {
		label string
		item  taskItem
	}
	var flat []flatTask
	for i, task := range in.Tasks {
		if strings.TrimSpace(task.Task) == "" {
			return &tool.Result{Content: fmt.Sprintf("tasks[%d]: empty task", i), IsError: true}, nil
		}
		n := task.Count
		if n <= 0 {
			n = 1
		}
		for c := 0; c < n; c++ {
			label := fmt.Sprintf("task %d", i+1)
			if task.Agent != "" {
				label += " (" + task.Agent + ")"
			}
			if n > 1 {
				label += fmt.Sprintf(" #%d", c+1)
			}
			flat = append(flat, flatTask{label: label, item: task})
		}
	}
	if t.Spawns != nil {
		if err := t.Spawns.Take(len(flat)); err != nil {
			return &tool.Result{Content: err.Error(), IsError: true}, nil
		}
	}

	limit := in.Concurrency
	if limit <= 0 {
		limit = t.MaxConcurrency
	}
	if limit <= 0 {
		limit = DefaultMaxConcurrency
	}

	// Each parallel task gets its own synthetic Agent node under the parent
	// Task card so its tool calls render as a separate nested agent instead of
	// all N tasks flattening into one list. The child's own tool events are
	// re-parented to this node via a per-task UseID.
	parentID := tool.UseID(ctx)
	owner := ""
	if t.OwnerFromContext != nil {
		owner = t.OwnerFromContext(ctx)
	}

	type outcome struct {
		answer string
		errMsg string
	}
	results := make([]outcome, len(flat))
	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup
	for i, ft := range flat {
		wg.Add(1)
		go func(i int, ft flatTask) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			nodeID := fmt.Sprintf("%s-t%d", parentID, i+1)
			t.emitTaskNodeStart(owner, parentID, nodeID, ft.item.Agent, ft.label)
			taskCtx := tool.WithUseID(ctx, nodeID)

			run, errMsg := t.prepare(ft.item.Agent, ft.item.Model, ft.item.Task)
			if errMsg != "" {
				results[i] = outcome{errMsg: errMsg}
				t.emitTaskNodeEnd(owner, parentID, nodeID, ft.item.Agent, errMsg, true)
				return
			}
			if in.Isolation == isolationWorktree {
				run = t.isolate(fmt.Sprintf("task-%d", i+1), run)
			}
			answer, err := run(taskCtx)
			if err != nil {
				msg := fmt.Sprintf("Subagent error: %v", err)
				results[i] = outcome{errMsg: msg}
				t.emitTaskNodeEnd(owner, parentID, nodeID, ft.item.Agent, msg, true)
				return
			}
			results[i] = outcome{answer: answer}
			t.emitTaskNodeEnd(owner, parentID, nodeID, ft.item.Agent, answer, false)
		}(i, ft)
	}
	wg.Wait()

	var b strings.Builder
	failed := 0
	for i, r := range results {
		if i > 0 {
			b.WriteString("\n\n")
		}
		fmt.Fprintf(&b, "=== %s ===\n", flat[i].label)
		if r.errMsg != "" {
			failed++
			b.WriteString("ERROR: " + r.errMsg)
		} else {
			b.WriteString(r.answer)
		}
	}
	return &tool.Result{Content: b.String(), IsError: failed == len(flat)}, nil
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
