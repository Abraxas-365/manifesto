// Package execstore implements fsys.Store over an exec.Executor: file operations
// are derived from shell commands run in the executor's environment. Pairing the
// built-in file tools with a store from the SAME executor that backs Bash keeps
// both in one environment, so they cannot diverge ("split-brain").
//
// The commands use only portable POSIX shell constructs plus base64/wc/mkdir, so
// they run on both GNU/Linux containers and BSD/macOS dev shells (List avoids
// GNU-only `find -printf`; Read/Write pipe base64 over stdin rather than passing
// filenames positionally).
package execstore

import (
	"context"
	"encoding/base64"
	"fmt"
	"path"
	"strconv"
	"strings"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/exec"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/fsys"
)

// MaxWriteBytes caps a single WriteFile. The base64 payload is inlined in the
// command string, so very large writes risk exceeding ARG_MAX. Chunked writes
// are a future enhancement (would need executor stdin support).
const MaxWriteBytes = 256 * 1024

type store struct {
	ex exec.Executor
}

// New derives a fsys.Store from an exec.Executor.
func New(ex exec.Executor) fsys.Store { return &store{ex: ex} }

// sh single-quote-escapes a path for safe interpolation into a shell command.
func sh(p string) string {
	return "'" + strings.ReplaceAll(p, "'", `'\''`) + "'"
}

// run executes script and returns trimmed stdout, mapping absence to
// fsys.ErrNotExist and other failures to a descriptive error.
func (s *store) run(ctx context.Context, script string) (string, error) {
	res, err := s.ex.Run(ctx, script, exec.RunOptions{})
	if err != nil {
		if res != nil && isNotFound(res.ExitCode, res.Stderr) {
			return "", fsys.ErrNotExist
		}
		return "", err
	}
	if res.ExitCode != 0 {
		if isNotFound(res.ExitCode, res.Stderr) {
			return "", fsys.ErrNotExist
		}
		return "", fmt.Errorf("command failed (exit %d): %s", res.ExitCode, strings.TrimSpace(res.Stderr))
	}
	return res.Stdout, nil
}

func isNotFound(exitCode int, stderr string) bool {
	if exitCode == 2 {
		return true
	}
	msg := strings.ToLower(stderr)
	return strings.Contains(msg, "no such file") || strings.Contains(msg, "not found")
}

func (s *store) ReadFile(ctx context.Context, p string) ([]byte, error) {
	// Pipe the file over stdin (portable across GNU/BSD base64, unlike a
	// positional filename argument).
	out, err := s.run(ctx, "base64 < "+sh(p))
	if err != nil {
		return nil, err
	}
	data, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(out, "\n", ""))
	if err != nil {
		return nil, fmt.Errorf("decode base64 output: %w", err)
	}
	return data, nil
}

func (s *store) WriteFile(ctx context.Context, p string, data []byte) error {
	if len(data) > MaxWriteBytes {
		return fmt.Errorf("write too large (%d bytes, max %d)", len(data), MaxWriteBytes)
	}
	b64 := base64.StdEncoding.EncodeToString(data)
	dir := path.Dir(p)
	script := fmt.Sprintf("mkdir -p -- %s && printf %%s %s | base64 -d > %s",
		sh(dir), sh(b64), sh(p))
	_, err := s.run(ctx, script)
	return err
}

func (s *store) Stat(ctx context.Context, p string) (fsys.FileInfo, error) {
	script := fmt.Sprintf(
		"if [ -d %[1]s ]; then echo 'd 0'; elif [ -e %[1]s ]; then echo \"f $(wc -c < %[1]s)\"; else exit 2; fi",
		sh(p))
	out, err := s.run(ctx, script)
	if err != nil {
		return fsys.FileInfo{}, err
	}
	fields := strings.Fields(out)
	if len(fields) != 2 {
		return fsys.FileInfo{}, fmt.Errorf("unexpected stat output: %q", out)
	}
	info := fsys.FileInfo{Name: path.Base(p), IsDir: fields[0] == "d"}
	if !info.IsDir {
		size, convErr := strconv.ParseInt(fields[1], 10, 64)
		if convErr != nil {
			return fsys.FileInfo{}, fmt.Errorf("parse size %q: %w", fields[1], convErr)
		}
		info.Size = size
	}
	return info, nil
}

func (s *store) List(ctx context.Context, p string) ([]fsys.FileInfo, error) {
	// Portable listing: iterate immediate children, emit "<type>\t<size>\t<name>"
	// using only POSIX test/wc/basename (no GNU `find -printf`). A glob that
	// matches nothing (empty dir) is guarded so the loop body is skipped.
	dir := sh(p)
	script := `for e in ` + dir + `/* ` + dir + `/.*; do ` +
		`b=$(basename -- "$e"); ` +
		`[ "$b" = "." ] || [ "$b" = ".." ] && continue; ` +
		`[ -e "$e" ] || continue; ` +
		`if [ -d "$e" ]; then printf 'd\t0\t%s\n' "$b"; ` +
		`else printf 'f\t%s\t%s\n' "$(wc -c < "$e" | tr -d '[:space:]')" "$b"; fi; ` +
		`done`
	out, err := s.run(ctx, script)
	if err != nil {
		return nil, err
	}
	var entries []fsys.FileInfo
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		isDir := parts[0] == "d"
		var size int64
		if !isDir {
			size, _ = strconv.ParseInt(parts[1], 10, 64)
		}
		entries = append(entries, fsys.FileInfo{Name: parts[2], Size: size, IsDir: isDir})
	}
	return entries, nil
}

func (s *store) MkdirAll(ctx context.Context, p string) error {
	_, err := s.run(ctx, "mkdir -p -- "+sh(p))
	return err
}
