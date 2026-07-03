// Package todo provides a declarative task-list tool for harness agents. The
// model sends the complete list on every call and it replaces the previous one
// (mirroring the built-in TodoWrite semantics).
//
// Storage is swappable via Store. The zero value uses an in-memory store, so
// the tool works with no configuration:
//
//	registry.Register(&todo.Tool{})
//
// For persistence (across processes, or shared with a UI), supply your own
// Store implementation:
//
//	registry.Register(&todo.Tool{Store: myRedisStore})
package todo

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/tool"
)

// Status is the state of a single todo item.
type Status string

const (
	StatusPending    Status = "pending"
	StatusInProgress Status = "in_progress"
	StatusCompleted  Status = "completed"
)

// Item is a single task in the list.
type Item struct {
	// Content is the task in imperative form (e.g. "Run tests").
	Content string `json:"content"`
	// Status is one of pending, in_progress, completed.
	Status Status `json:"status"`
	// ActiveForm is the present-continuous form shown while in progress
	// (e.g. "Running tests").
	ActiveForm string `json:"activeForm"`
}

// Store persists the current todo list. Implementations must be safe for
// concurrent use. Get returns the current list (nil/empty if none set).
type Store interface {
	Get(ctx context.Context) ([]Item, error)
	Set(ctx context.Context, items []Item) error
}

// MemoryStore is an in-memory Store, safe for concurrent use. The zero value is
// ready to use.
type MemoryStore struct {
	mu    sync.Mutex
	items []Item
}

func (m *MemoryStore) Get(context.Context) ([]Item, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Item, len(m.items))
	copy(out, m.items)
	return out, nil
}

func (m *MemoryStore) Set(_ context.Context, items []Item) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items = make([]Item, len(items))
	copy(m.items, items)
	return nil
}

const defaultName = "TodoWrite"

const defaultDescription = "Create and manage a structured task list for the current session. This is " +
	"DECLARATIVE: send the COMPLETE list every call and it replaces the previous one. To start a " +
	"task set its status to \"in_progress\" (keep only one in_progress at a time); to finish it, " +
	"resend the list with that task \"completed\". Use for multi-step work; skip it for trivial " +
	"single-step tasks."

// Tool is a declarative todo-list tool. The zero value works with an internal
// in-memory store; set Store to persist elsewhere.
type Tool struct {
	// Store persists the list. Nil uses an internal in-memory store.
	Store Store
	// ToolName overrides the tool name (default "TodoWrite").
	ToolName string
	// Desc overrides the tool description.
	Desc string
	// OnChange, if set, is called after the list is successfully replaced.
	OnChange func(items []Item)

	once    sync.Once
	fallbck Store
}

func (t *Tool) store() Store {
	if t.Store != nil {
		return t.Store
	}
	t.once.Do(func() { t.fallbck = &MemoryStore{} })
	return t.fallbck
}

func (t *Tool) Name() string {
	if t.ToolName != "" {
		return t.ToolName
	}
	return defaultName
}

func (t *Tool) Description() string {
	if t.Desc != "" {
		return t.Desc
	}
	return defaultDescription
}

func (t *Tool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"todos": {
				"type": "array",
				"description": "The complete todo list. Replaces the previous one.",
				"items": {
					"type": "object",
					"properties": {
						"content": {"type": "string", "description": "Task in imperative form (e.g. \"Run tests\")"},
						"status": {"type": "string", "enum": ["pending", "in_progress", "completed"]},
						"activeForm": {"type": "string", "description": "Present-continuous form shown while in progress (e.g. \"Running tests\")"}
					},
					"required": ["content", "status", "activeForm"]
				}
			}
		},
		"required": ["todos"]
	}`)
}

func (t *Tool) IsReadOnly() bool { return false }

func (t *Tool) RequiresApproval(_ json.RawMessage) bool { return false }

type input struct {
	Todos []Item `json:"todos"`
}

func (t *Tool) Execute(ctx context.Context, raw json.RawMessage) (*tool.Result, error) {
	var in input
	if err := json.Unmarshal(raw, &in); err != nil {
		return errResult("Invalid input: %v", err), nil
	}

	inProgress := 0
	for i, it := range in.Todos {
		if strings.TrimSpace(it.Content) == "" {
			return errResult("todos[%d]: content is required", i), nil
		}
		if strings.TrimSpace(it.ActiveForm) == "" {
			return errResult("todos[%d] (%q): activeForm is required", i, it.Content), nil
		}
		switch it.Status {
		case StatusPending, StatusInProgress, StatusCompleted:
		default:
			return errResult("todos[%d] (%q): invalid status %q (want pending|in_progress|completed)",
				i, it.Content, it.Status), nil
		}
		if it.Status == StatusInProgress {
			inProgress++
		}
	}
	if inProgress > 1 {
		return errResult("only one task may be in_progress at a time, found %d", inProgress), nil
	}

	if err := t.store().Set(ctx, in.Todos); err != nil {
		return errResult("Failed to save todos: %v", err), nil
	}
	if t.OnChange != nil {
		t.OnChange(in.Todos)
	}

	return &tool.Result{Content: render(in.Todos)}, nil
}

// Items returns the current list from the store. Useful for a UI or for
// inspecting progress outside the tool call.
func (t *Tool) Items(ctx context.Context) ([]Item, error) {
	return t.store().Get(ctx)
}

func render(items []Item) string {
	if len(items) == 0 {
		return "(todo list cleared)"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Todo list updated (%d items):\n", len(items))
	for _, it := range items {
		fmt.Fprintf(&b, "%s %s\n", marker(it.Status), it.Content)
	}
	return strings.TrimRight(b.String(), "\n")
}

func marker(s Status) string {
	switch s {
	case StatusCompleted:
		return "[x]"
	case StatusInProgress:
		return "[~]"
	default:
		return "[ ]"
	}
}

func errResult(format string, args ...any) *tool.Result {
	return &tool.Result{Content: fmt.Sprintf(format, args...), IsError: true}
}
