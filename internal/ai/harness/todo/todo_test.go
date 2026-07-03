package todo

import (
	"context"
	"encoding/json"
	"testing"
)

func exec(t *testing.T, tl *Tool, body string) (content string, isErr bool) {
	t.Helper()
	res, err := tl.Execute(context.Background(), json.RawMessage(body))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	return res.Content, res.IsError
}

func TestReplacesListAndPersists(t *testing.T) {
	tl := &Tool{}
	_, isErr := exec(t, tl, `{"todos":[
		{"content":"Write code","status":"in_progress","activeForm":"Writing code"},
		{"content":"Run tests","status":"pending","activeForm":"Running tests"}
	]}`)
	if isErr {
		t.Fatal("unexpected error result")
	}
	items, _ := tl.Items(context.Background())
	if len(items) != 2 || items[0].Content != "Write code" {
		t.Fatalf("unexpected items: %+v", items)
	}

	// Declarative replace: a shorter list overwrites the previous one.
	exec(t, tl, `{"todos":[{"content":"Done","status":"completed","activeForm":"Finishing"}]}`)
	items, _ = tl.Items(context.Background())
	if len(items) != 1 || items[0].Status != StatusCompleted {
		t.Fatalf("list not replaced: %+v", items)
	}
}

func TestRejectsMultipleInProgress(t *testing.T) {
	tl := &Tool{}
	_, isErr := exec(t, tl, `{"todos":[
		{"content":"A","status":"in_progress","activeForm":"Aing"},
		{"content":"B","status":"in_progress","activeForm":"Bing"}
	]}`)
	if !isErr {
		t.Fatal("expected error for two in_progress items")
	}
}

func TestRejectsInvalidStatus(t *testing.T) {
	tl := &Tool{}
	_, isErr := exec(t, tl, `{"todos":[{"content":"A","status":"blocked","activeForm":"Aing"}]}`)
	if !isErr {
		t.Fatal("expected error for invalid status")
	}
}

func TestRejectsMissingFields(t *testing.T) {
	tl := &Tool{}
	if _, isErr := exec(t, tl, `{"todos":[{"content":"","status":"pending","activeForm":"x"}]}`); !isErr {
		t.Error("expected error for empty content")
	}
	if _, isErr := exec(t, tl, `{"todos":[{"content":"A","status":"pending","activeForm":""}]}`); !isErr {
		t.Error("expected error for empty activeForm")
	}
}

func TestEmptyListClears(t *testing.T) {
	tl := &Tool{}
	exec(t, tl, `{"todos":[{"content":"A","status":"pending","activeForm":"Aing"}]}`)
	content, isErr := exec(t, tl, `{"todos":[]}`)
	if isErr {
		t.Fatal("empty list should be valid")
	}
	if content != "(todo list cleared)" {
		t.Errorf("unexpected content: %q", content)
	}
	items, _ := tl.Items(context.Background())
	if len(items) != 0 {
		t.Errorf("expected cleared list, got %+v", items)
	}
}

func TestOnChangeFires(t *testing.T) {
	var got []Item
	tl := &Tool{OnChange: func(items []Item) { got = items }}
	exec(t, tl, `{"todos":[{"content":"A","status":"pending","activeForm":"Aing"}]}`)
	if len(got) != 1 || got[0].Content != "A" {
		t.Fatalf("OnChange not fired correctly: %+v", got)
	}
}

func TestCustomStoreUsed(t *testing.T) {
	store := &MemoryStore{}
	tl := &Tool{Store: store}
	exec(t, tl, `{"todos":[{"content":"A","status":"pending","activeForm":"Aing"}]}`)
	items, _ := store.Get(context.Background())
	if len(items) != 1 {
		t.Fatalf("custom store not used: %+v", items)
	}
}

func TestCustomNameAndDescription(t *testing.T) {
	tl := &Tool{ToolName: "Tasks", Desc: "custom"}
	if tl.Name() != "Tasks" || tl.Description() != "custom" {
		t.Errorf("overrides not applied: %q %q", tl.Name(), tl.Description())
	}
	def := &Tool{}
	if def.Name() != "TodoWrite" {
		t.Errorf("default name = %q", def.Name())
	}
}
