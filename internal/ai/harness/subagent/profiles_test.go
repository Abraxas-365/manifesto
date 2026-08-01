package subagent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	agent "github.com/Abraxas-365/manifesto/internal/ai/harness"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/agentdef"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/llm"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/tool"
)

// recordingProvider captures the full request for assertions on model,
// system prompt, and tool defs.
type recordingProvider struct{ req *llm.Request }

func (p *recordingProvider) Chat(_ context.Context, req llm.Request) (*llm.Response, error) {
	*p.req = req
	return &llm.Response{
		Message:    llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.Text("done")}},
		StopReason: llm.StopEndTurn,
	}, nil
}

func (p *recordingProvider) ChatStream(context.Context, llm.Request) (llm.Stream, error) {
	return nil, nil
}

// roTool / rwTool are minimal tools for subset assertions.
type namedTool struct {
	name     string
	readOnly bool
}

func (t *namedTool) Name() string                 { return t.name }
func (t *namedTool) Description() string          { return t.name }
func (t *namedTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (t *namedTool) IsReadOnly() bool             { return t.readOnly }
func (t *namedTool) Execute(context.Context, json.RawMessage) (*tool.Result, error) {
	return &tool.Result{Content: "ok"}, nil
}

func defsWith(t *testing.T, defs ...agentdef.Definition) *agentdef.Registry {
	t.Helper()
	r := agentdef.NewRegistry()
	for _, d := range defs {
		if err := r.Define(d); err != nil {
			t.Fatal(err)
		}
		r.Expose(d.Name)
	}
	return r
}

func profTool(req *llm.Request, defs *agentdef.Registry) *Tool {
	return &Tool{
		NewAgent: func() *agent.Agent {
			reg := tool.NewRegistry()
			reg.Register(&namedTool{name: "Read", readOnly: true})
			reg.Register(&namedTool{name: "Write", readOnly: false})
			reg.Register(&namedTool{name: "Grep", readOnly: true})
			ag := agent.New(&recordingProvider{req: req}, reg)
			ag.Model = "parent-model"
			ag.System = "parent system"
			return ag
		},
		Defs:       defs,
		SmallModel: "tiny-model",
	}
}

func TestAgentParam_AppliesProfile(t *testing.T) {
	var req llm.Request
	defs := defsWith(t, agentdef.Definition{
		Name: "explorer", Description: "read-only recon",
		Model: "small", SystemPrompt: "You are an explorer.",
		Tools: []string{"Read", "Grep"},
	})
	res, err := profTool(&req, defs).Execute(context.Background(),
		[]byte(`{"prompt":"look around","agent":"explorer"}`))
	if err != nil || res.IsError {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if req.Model != "tiny-model" {
		t.Errorf("model=small should map to SmallModel, got %q", req.Model)
	}
	if req.System != "You are an explorer." {
		t.Errorf("system: %q", req.System)
	}
	names := make([]string, 0, len(req.Tools))
	for _, td := range req.Tools {
		names = append(names, td.Name)
	}
	if strings.Join(names, ",") != "Read,Grep" {
		t.Errorf("tool subset: %v", names)
	}
}

func TestAgentParam_ReadOnlyDropsMutatingTools(t *testing.T) {
	var req llm.Request
	defs := defsWith(t, agentdef.Definition{
		Name: "auditor", Tools: []string{"*"}, ReadOnly: true,
	})
	if res, _ := profTool(&req, defs).Execute(context.Background(),
		[]byte(`{"prompt":"audit","agent":"auditor"}`)); res.IsError {
		t.Fatalf("res: %+v", res)
	}
	for _, td := range req.Tools {
		if td.Name == "Write" {
			t.Fatal("readOnly profile leaked a mutating tool")
		}
	}
}

func TestAgentParam_InheritKeepsParentModelAndPrompt(t *testing.T) {
	var req llm.Request
	defs := defsWith(t, agentdef.Definition{Name: "clone"})
	profTool(&req, defs).Execute(context.Background(),
		[]byte(`{"prompt":"x","agent":"clone"}`))
	if req.Model != "parent-model" || req.System != "parent system" {
		t.Fatalf("inherit broken: model=%q system=%q", req.Model, req.System)
	}
}

func TestAgentParam_AppendPromptMode(t *testing.T) {
	var req llm.Request
	defs := defsWith(t, agentdef.Definition{
		Name: "extra", SystemPrompt: "Extra rules.", SystemPromptMode: "append",
	})
	profTool(&req, defs).Execute(context.Background(),
		[]byte(`{"prompt":"x","agent":"extra"}`))
	if req.System != "parent system\n\nExtra rules." {
		t.Fatalf("append: %q", req.System)
	}
}

func TestAgentParam_UnknownAndUnexposed(t *testing.T) {
	var req llm.Request
	defs := defsWith(t, agentdef.Definition{Name: "known"})
	// hidden: defined but not exposed
	defs.Define(agentdef.Definition{Name: "hidden"})

	res, _ := profTool(&req, defs).Execute(context.Background(),
		[]byte(`{"prompt":"x","agent":"ghost"}`))
	if !res.IsError || !strings.Contains(res.Content, "known") {
		t.Fatalf("unknown agent: %+v", res)
	}
	res, _ = profTool(&req, defs).Execute(context.Background(),
		[]byte(`{"prompt":"x","agent":"hidden"}`))
	if !res.IsError {
		t.Fatal("unexposed agent must be rejected")
	}
}

func TestAgentParam_ExplicitModelBeatsProfile(t *testing.T) {
	var req llm.Request
	defs := defsWith(t, agentdef.Definition{Name: "m", Model: "profile-model"})
	profTool(&req, defs).Execute(context.Background(),
		[]byte(`{"prompt":"x","agent":"m","model":"caller-model"}`))
	if req.Model != "caller-model" {
		t.Fatalf("caller model must win: %q", req.Model)
	}
}

func TestAgentParam_ToolExcludeWithWildcard(t *testing.T) {
	var req llm.Request
	defs := defsWith(t, agentdef.Definition{
		Name: "safe", Tools: []string{"*"}, ToolExclude: []string{"Write"},
	})
	tl := profTool(&req, defs)
	res, err := tl.Execute(context.Background(), []byte(`{"prompt":"x","agent":"safe"}`))
	if err != nil || res.IsError {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	names := make(map[string]bool)
	for _, td := range req.Tools {
		names[td.Name] = true
	}
	if names["Write"] {
		t.Fatal("Write should be excluded")
	}
	if !names["Read"] || !names["Grep"] {
		t.Fatal("Read and Grep should be included with wildcard")
	}
}

func TestAgentParam_ThinkingApplied(t *testing.T) {
	var req llm.Request
	defs := defsWith(t, agentdef.Definition{
		Name: "thinker", Thinking: "high",
	})
	profTool(&req, defs).Execute(context.Background(),
		[]byte(`{"prompt":"x","agent":"thinker"}`))
	if req.Reasoning != "high" {
		t.Fatalf("thinking not applied: %v", req.Reasoning)
	}
}

func TestAgentParam_ThinkingOffDisablesReasoning(t *testing.T) {
	var req llm.Request
	defs := defsWith(t, agentdef.Definition{
		Name: "fast", Thinking: "off",
	})
	profTool(&req, defs).Execute(context.Background(),
		[]byte(`{"prompt":"x","agent":"fast"}`))
	if req.Reasoning != "" {
		t.Fatalf("thinking=off should leave reasoning empty, got %v", req.Reasoning)
	}
}

func TestChildFor_SubagentAgentsRestrictsChild(t *testing.T) {
	defs := defsWith(t,
		agentdef.Definition{Name: "parent", Subagents: []string{"Explore"}},
		agentdef.Definition{Name: "Explore", Description: "explore"},
		agentdef.Definition{Name: "Plan", Description: "plan"},
	)
	tl := &Tool{Defs: defs, MaxDepth: 3, Depth: 0}
	child := tl.childFor(agentdef.Definition{Name: "parent", Subagents: []string{"Explore"}})
	if child == nil {
		t.Fatal("child should not be nil")
	}
	if len(child.AllowedAgents) != 1 || child.AllowedAgents[0] != "Explore" {
		t.Fatalf("child allowed agents: %v", child.AllowedAgents)
	}
	if child.Depth != 1 {
		t.Fatalf("child depth: %d", child.Depth)
	}
}

func TestChildFor_EmptySubagentsRemovesSubAgentTool(t *testing.T) {
	defs := defsWith(t, agentdef.Definition{Name: "leaf"})
	tl := &Tool{Defs: defs, MaxDepth: 3, Depth: 0}
	child := tl.childFor(agentdef.Definition{Subagents: []string{}})
	if child != nil {
		t.Fatal("empty subagents list should remove SubAgent tool (childFor returns nil)")
	}
}

func TestChildFor_DepthExhaustedReturnsNil(t *testing.T) {
	defs := defsWith(t, agentdef.Definition{Name: "deep"})
	tl := &Tool{Defs: defs, MaxDepth: 2, Depth: 1}
	child := tl.childFor(agentdef.Definition{Name: "deep"})
	if child != nil {
		t.Fatal("should return nil when depth exhausted")
	}
}

func TestChildFor_MaxSubagentDepthCapsChild(t *testing.T) {
	defs := defsWith(t, agentdef.Definition{Name: "mid"})
	tl := &Tool{Defs: defs, MaxDepth: 10, Depth: 0}
	child := tl.childFor(agentdef.Definition{Name: "mid", MaxSubagentDepth: 2})
	if child == nil {
		t.Fatal("child should exist")
	}
	if child.MaxDepth != 2 {
		t.Fatalf("max depth should be capped to 2, got %d", child.MaxDepth)
	}
}

func TestSchemaAndDescriptionListRoster(t *testing.T) {
	defs := defsWith(t,
		agentdef.Definition{Name: "b-agent", Description: "beta"},
		agentdef.Definition{Name: "a-agent", Description: "alpha"},
	)
	tl := &Tool{NewAgent: nil, Defs: defs}
	schema := string(tl.InputSchema())
	if !strings.Contains(schema, `"agent"`) || !strings.Contains(schema, `"a-agent","b-agent"`) {
		t.Fatalf("schema: %s", schema)
	}
	desc := tl.Description()
	if !strings.Contains(desc, "a-agent: alpha") || !strings.Contains(desc, "b-agent: beta") {
		t.Fatalf("description: %s", desc)
	}
	// No roster -> no agent param.
	empty := &Tool{Defs: agentdef.NewRegistry()}
	if strings.Contains(string(empty.InputSchema()), `"agent"`) {
		t.Fatal("agent param must be absent with empty roster")
	}
}
