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

	"github.com/Abraxas-365/manifesto/internal/ai/harness"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/tool"
)

// DefaultName is the tool name used when Tool.ToolName is empty.
const DefaultName = "Task"

// DefaultDescription is used when Tool.Desc is empty.
const DefaultDescription = "Delegate a self-contained subtask to a nested agent that runs in its own " +
	"isolated context. Provide a detailed, standalone prompt: the subagent cannot see this " +
	"conversation. It returns only its final text answer. Use this to research or search " +
	"without cluttering the main context."

// Factory builds a fresh agent for a single subagent invocation. It is called
// once per Execute so each subtask starts with a clean history.
type Factory func() *harness.Agent

// Tool runs a nested agent as a tool call. It implements tool.Tool.
type Tool struct {
	// NewAgent builds the nested agent for each invocation (required).
	NewAgent Factory
	// ToolName overrides the tool name. Empty uses DefaultName.
	ToolName string
	// Desc overrides the tool description. Empty uses DefaultDescription.
	Desc string
	// AllowedModels, if non-empty, restricts the optional "model" parameter to
	// this set (rendered as a JSON-schema enum) and rejects anything else. Empty
	// allows any model string; the caller-supplied model, when present, overrides
	// the model set by NewAgent.
	AllowedModels []string
}

type input struct {
	Prompt string `json:"prompt"`
	Model  string `json:"model,omitempty"`
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
	if t.Desc != "" {
		return t.Desc
	}
	return DefaultDescription
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
	return json.RawMessage(fmt.Sprintf(`{
		"type": "object",
		"properties": {
			"prompt": {"type": "string", "description": "A detailed, self-contained task for the subagent. It cannot see the parent conversation."},
			"model": %s
		},
		"required": ["prompt"]
	}`, modelField))
}

// IsReadOnly implements tool.Tool. A subagent may use mutating tools, so it is
// conservatively reported as not read-only.
func (t *Tool) IsReadOnly() bool { return false }

// RequiresApproval implements tool.Tool.
func (t *Tool) RequiresApproval(_ json.RawMessage) bool { return false }

// Execute builds a fresh agent and runs the prompt, returning its final answer.
func (t *Tool) Execute(ctx context.Context, raw json.RawMessage) (*tool.Result, error) {
	var in input
	if err := json.Unmarshal(raw, &in); err != nil {
		return &tool.Result{Content: fmt.Sprintf("Invalid input: %v", err), IsError: true}, nil
	}
	if strings.TrimSpace(in.Prompt) == "" {
		return &tool.Result{Content: "No prompt provided", IsError: true}, nil
	}
	if t.NewAgent == nil {
		return &tool.Result{Content: "Subagent tool is not configured (NewAgent is nil)", IsError: true}, nil
	}

	agent := t.NewAgent()

	// Apply the optional caller-supplied model over the factory's default.
	if model := strings.TrimSpace(in.Model); model != "" {
		if len(t.AllowedModels) > 0 && !contains(t.AllowedModels, model) {
			return &tool.Result{Content: fmt.Sprintf("Model %q is not allowed. Choose one of: %s",
				model, strings.Join(t.AllowedModels, ", ")), IsError: true}, nil
		}
		agent.Model = model
	}

	answer, err := agent.Run(ctx, in.Prompt)
	if err != nil {
		return &tool.Result{Content: fmt.Sprintf("Subagent error: %v", err), IsError: true}, nil
	}
	return &tool.Result{Content: answer}, nil
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
