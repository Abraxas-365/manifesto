package skill

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/tool"
)

// ToolName is the default name exposed to the model.
const ToolName = "Skill"

// Tool exposes registered skills to the agent. Invoking a skill materializes its
// files to a local directory and returns the body with ${SKILL_DIR} substituted,
// so the agent can Bash/Read the reference files.
type Tool struct {
	Registry *Registry
	// Cache owns materialized skill dirs. Nil means a lazily-created ephemeral
	// Cache is used.
	Cache *Cache

	mu        sync.Mutex
	closeOnce sync.Once
}

type input struct {
	Skill     string `json:"skill"`
	Arguments string `json:"arguments,omitempty"`
}

func (t *Tool) Name() string { return ToolName }

func (t *Tool) Description() string {
	var b strings.Builder
	b.WriteString("Load a skill by name to get specialized instructions for a task. ")
	b.WriteString("The response includes the skill's base directory so you can Read or " +
		"Bash its reference files on demand.")
	if t.Registry != nil {
		skills := t.Registry.All()
		if len(skills) > 0 {
			b.WriteString("\n\nAvailable skills:\n")
			for _, s := range skills {
				fmt.Fprintf(&b, "- %s: %s\n", s.Name, s.Description)
			}
		}
	}
	return b.String()
}

func (t *Tool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"skill": {"type": "string", "description": "The name of the skill to load"},
			"arguments": {"type": "string", "description": "Optional free-form arguments for the skill"}
		},
		"required": ["skill"]
	}`)
}

func (t *Tool) IsReadOnly() bool { return true }

func (t *Tool) RequiresApproval(_ json.RawMessage) bool { return false }

func (t *Tool) cache() *Cache {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.Cache == nil {
		t.Cache = NewCache("")
	}
	return t.Cache
}

func (t *Tool) Execute(ctx context.Context, raw json.RawMessage) (*tool.Result, error) {
	var in input
	if err := json.Unmarshal(raw, &in); err != nil {
		return &tool.Result{Content: fmt.Sprintf("Invalid input: %v", err), IsError: true}, nil
	}
	name := strings.TrimSpace(in.Skill)
	if name == "" {
		return &tool.Result{Content: "No skill name provided. " + t.availableList(), IsError: true}, nil
	}
	if t.Registry == nil {
		return &tool.Result{Content: "No skills are registered.", IsError: true}, nil
	}
	sk, ok := t.Registry.Get(name)
	if !ok {
		return &tool.Result{
			Content: fmt.Sprintf("Unknown skill %q. %s", name, t.availableList()),
			IsError: true,
		}, nil
	}

	dir, err := sk.Dir(ctx, t.cache())
	if err != nil {
		return &tool.Result{Content: fmt.Sprintf("Failed to load skill %q: %v", name, err), IsError: true}, nil
	}

	body := sk.Body
	body = strings.ReplaceAll(body, "${SKILL_DIR}", dir)
	body = strings.ReplaceAll(body, "${CLAUDE_SKILL_DIR}", dir)

	content := "Base directory for this skill: " + dir + "\n\n" + body
	return &tool.Result{Content: content}, nil
}

func (t *Tool) availableList() string {
	if t.Registry == nil {
		return "No skills are registered."
	}
	names := t.Registry.Names()
	if len(names) == 0 {
		return "No skills are registered."
	}
	return "Available skills: " + strings.Join(names, ", ") + "."
}

// Close releases materialized dirs owned by the tool's Cache. It removes only an
// ephemeral (self-created) cache dir; a caller-supplied BaseDir is preserved. It
// is safe to call multiple times.
func (t *Tool) Close() error {
	var err error
	t.closeOnce.Do(func() {
		if t.Cache != nil {
			err = t.Cache.Close()
		}
	})
	return err
}
