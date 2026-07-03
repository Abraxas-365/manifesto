// Package builtins provides the default set of harness tools implemented over a
// swappable fsys.Store (file operations) and exec.Executor (shell commands).
package builtins

import (
	"github.com/Abraxas-365/manifesto/internal/ai/harness/exec"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/fsys"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/fsys/execstore"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/tool"
)

// fileTools registers the six file tools over the given store.
func fileTools(r *tool.Registry, store fsys.Store) {
	r.Register(&Read{FS: store})
	r.Register(&Write{FS: store})
	r.Register(&Edit{FS: store})
	r.Register(&List{FS: store})
	r.Register(&Glob{FS: store})
	r.Register(&Grep{FS: store})
}

// Files builds a registry with only the file tools, for storage-only
// environments (e.g. S3) that have no shell to execute against. No Bash means
// there is no compute backend that could diverge from the store.
func Files(store fsys.Store) *tool.Registry {
	r := tool.NewRegistry()
	fileTools(r, store)
	return r
}

// Default builds a registry with the file tools plus Bash, wired to the given
// store and executor. Use this for local development or any case where you
// deliberately pair a matching store and executor. When the executor supports
// detached commands, BashOutput and KillShell are also registered.
func Default(store fsys.Store, ex exec.Executor) *tool.Registry {
	r := Files(store)
	r.Register(&Bash{Exec: ex})
	// When the executor supports detached commands, expose the tools that
	// manage them so the agent can run servers and long-lived processes.
	if bg, ok := ex.(exec.BackgroundExecutor); ok {
		r.Register(&BashOutput{Exec: bg})
		r.Register(&KillShell{Exec: bg})
	}
	return r
}

// FromExecutor builds a full registry (file tools + Bash) whose file operations
// are derived from the SAME executor that backs Bash. Because one environment
// powers both, the file tools and shell cannot point at different worlds — a
// "split-brain" is impossible by construction. Use this for Docker, SSH, or any
// remote executor.
func FromExecutor(ex exec.Executor) *tool.Registry {
	return Default(execstore.New(ex), ex)
}
