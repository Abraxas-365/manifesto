// Package builtins provides the default set of harness tools implemented over a
// swappable fsx.FileSystem and exec.Executor.
package builtins

import (
	"github.com/Abraxas-365/manifesto/internal/ai/harness/exec"
	"github.com/Abraxas-365/manifesto/internal/ai/harness/tool"
	"github.com/Abraxas-365/manifesto/internal/fsx"
)

// Default builds a registry containing all built-in tools wired to the given
// filesystem and executor.
func Default(fs fsx.FileSystem, ex exec.Executor) *tool.Registry {
	r := tool.NewRegistry()
	r.Register(&Read{FS: fs})
	r.Register(&Write{FS: fs})
	r.Register(&Edit{FS: fs})
	r.Register(&List{FS: fs})
	r.Register(&Glob{FS: fs})
	r.Register(&Grep{FS: fs})
	r.Register(&Bash{Exec: ex})
	return r
}
