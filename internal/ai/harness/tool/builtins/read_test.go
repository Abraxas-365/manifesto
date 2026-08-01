package builtins

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestReadRepeatDedup(t *testing.T) {
	fs := newMemFS()
	fs.files["a.txt"] = []byte("hello\nworld\n")
	fs.mtimes["a.txt"] = time.Now()
	r := &Read{FS: fs, Cache: NewReadCache()}

	in := json.RawMessage(`{"file_path":"a.txt"}`)
	res1, _ := r.Execute(context.Background(), in)
	if !strings.Contains(res1.Content, "hello") {
		t.Fatalf("first read must return content:\n%s", res1.Content)
	}
	res2, _ := r.Execute(context.Background(), in)
	if !strings.Contains(res2.Content, "unchanged since last read") {
		t.Errorf("second read must return stub:\n%s", res2.Content)
	}

	// Changed mtime invalidates.
	fs.mtimes["a.txt"] = time.Now().Add(time.Second)
	res3, _ := r.Execute(context.Background(), in)
	if !strings.Contains(res3.Content, "hello") {
		t.Errorf("modified file must return content again:\n%s", res3.Content)
	}

	// Different range is a separate cache entry.
	res4, _ := r.Execute(context.Background(), json.RawMessage(`{"file_path":"a.txt","offset":2}`))
	if !strings.Contains(res4.Content, "world") {
		t.Errorf("different range must not hit cache:\n%s", res4.Content)
	}
}

func TestReadDedupDisabledWithoutMtime(t *testing.T) {
	fs := newMemFS()
	fs.files["a.txt"] = []byte("data\n") // zero mtime
	r := &Read{FS: fs, Cache: NewReadCache()}
	in := json.RawMessage(`{"file_path":"a.txt"}`)
	r.Execute(context.Background(), in)
	res, _ := r.Execute(context.Background(), in)
	if strings.Contains(res.Content, "unchanged") {
		t.Error("no-mtime backend must never dedup")
	}
}

func TestReadImage(t *testing.T) {
	fs := newMemFS()
	png := []byte("\x89PNG\r\n\x1a\nfakebody")
	fs.files["pic.PNG"] = png
	r := &Read{FS: fs}

	res, err := r.Execute(context.Background(), json.RawMessage(`{"file_path":"pic.PNG"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Images) != 1 {
		t.Fatalf("want 1 image block, got %d (%s)", len(res.Images), res.Content)
	}
	img := res.Images[0]
	if img.MediaType != "image/png" {
		t.Errorf("media type: %s", img.MediaType)
	}
	if decoded, _ := base64.StdEncoding.DecodeString(img.Data); string(decoded) != string(png) {
		t.Error("image data round-trip failed")
	}
	if !strings.Contains(res.Content, "[Image: pic.PNG]") {
		t.Errorf("content placeholder: %s", res.Content)
	}
}

func TestReadImageTooLarge(t *testing.T) {
	fs := newMemFS()
	fs.files["big.png"] = make([]byte, maxImageBytes+1)
	r := &Read{FS: fs}
	res, _ := r.Execute(context.Background(), json.RawMessage(`{"file_path":"big.png"}`))
	if !res.IsError || !strings.Contains(res.Content, "too large") {
		t.Errorf("want size error, got: %s", res.Content)
	}
}
