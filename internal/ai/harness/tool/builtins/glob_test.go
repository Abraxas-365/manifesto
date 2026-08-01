package builtins

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestGlobMtimeSortNewestFirst(t *testing.T) {
	fs := newMemFS()
	base := time.Now()
	fs.files["old.go"] = []byte("old")
	fs.mtimes["old.go"] = base.Add(-time.Hour)
	fs.files["new.go"] = []byte("new")
	fs.mtimes["new.go"] = base
	fs.files["mid.go"] = []byte("mid")
	fs.mtimes["mid.go"] = base.Add(-time.Minute)
	fs.files["skip.txt"] = []byte("x")

	g := &Glob{FS: fs}
	res, err := g.Execute(context.Background(), json.RawMessage(`{"pattern":"*.go"}`))
	if err != nil {
		t.Fatal(err)
	}
	want := "new.go\nmid.go\nold.go"
	if res.Content != want {
		t.Errorf("got:\n%s\nwant:\n%s", res.Content, want)
	}
}

func TestGlobZeroMtimeFallsBackToPathOrder(t *testing.T) {
	fs := newMemFS()
	fs.files["b.go"] = []byte("b")
	fs.files["a.go"] = []byte("a")

	g := &Glob{FS: fs}
	res, _ := g.Execute(context.Background(), json.RawMessage(`{"pattern":"*.go"}`))
	if !strings.HasPrefix(res.Content, "a.go") {
		t.Errorf("want path-order fallback, got:\n%s", res.Content)
	}
}
