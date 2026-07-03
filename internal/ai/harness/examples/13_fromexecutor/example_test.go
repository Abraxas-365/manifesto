package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/exec"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/fsys/execstore"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/tool/builtins"
)

// TestFromExecutor_SharedEnvironment proves the whole point of FromExecutor with
// no API key: a file WRITTEN through the file tools is immediately visible to
// Bash, because both are backed by the same executor/workspace.
func TestFromExecutor_SharedEnvironment(t *testing.T) {
	workspace := t.TempDir()
	ex := exec.NewLocalExecutor(workspace)
	registry := builtins.FromExecutor(ex)

	ctx := context.Background()

	// 1. Write a file using the Write tool (runs as a shell command via execstore).
	write, _ := registry.Get("Write")
	in, _ := json.Marshal(map[string]any{
		"file_path": "hello.txt",
		"content":   "hi from the file tool\n",
	})
	if res, err := write.Execute(ctx, in); err != nil || res.IsError {
		t.Fatalf("Write failed: err=%v res=%+v", err, res)
	}

	// 2. Read it back with Bash — same environment, so it's there.
	bash, _ := registry.Get("Bash")
	in, _ = json.Marshal(map[string]any{"command": "cat hello.txt"})
	res, err := bash.Execute(ctx, in)
	if err != nil || res.IsError {
		t.Fatalf("Bash failed: err=%v res=%+v", err, res)
	}
	if !strings.Contains(res.Content, "hi from the file tool") {
		t.Fatalf("Bash did not see the file written by the file tool: %q", res.Content)
	}

	// 3. And it's a real file on the host workspace too.
	if _, err := os.Stat(workspace + "/hello.txt"); err != nil {
		t.Fatalf("expected hello.txt on disk: %v", err)
	}
}

// TestConstructors_ToolSets contrasts the three constructors: Files is
// storage-only (no shell can diverge from the store), while Default and
// FromExecutor both wire Bash alongside the file tools.
func TestConstructors_ToolSets(t *testing.T) {
	store := execstore.New(exec.NewLocalExecutor(t.TempDir()))

	files := builtins.Files(store)
	if _, ok := files.Get("Read"); !ok {
		t.Fatal("Files should register the file tools")
	}
	if _, ok := files.Get("Bash"); ok {
		t.Fatal("Files must NOT register Bash (storage-only)")
	}

	full := builtins.FromExecutor(exec.NewLocalExecutor(t.TempDir()))
	if _, ok := full.Get("Bash"); !ok {
		t.Fatal("FromExecutor should register Bash")
	}
}
