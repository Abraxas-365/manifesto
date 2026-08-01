package subagent

// Isolation support: with isolation: "worktree" a subagent runs inside a
// fresh isolated checkout (a git worktree) instead of the shared tree, so
// parallel writers cannot trample each other or the user's work. The harness
// stays git-agnostic: an Isolator implementation is injected by the host
// (see your app wiring), and the builtin file/shell tools honor the isolated dir
// via tool.WithWorkdir on the run's context.

import (
	"context"
	"fmt"

	"github.com/Abraxas-365/manifesto/internal/ai/harness/tool"
)

// Isolation is one provisioned isolated working copy (e.g. a git worktree).
type Isolation struct {
	// Dir is the isolated checkout root the subagent works in.
	Dir string
	// MainRoot is the original root the isolation was forked from; absolute
	// paths under it are remapped into Dir by the builtin file tools.
	MainRoot string
	// Branch is the branch checked out in Dir (informational, for the result).
	Branch string
	// Note is optional extra information populated by Finish when the
	// isolation is kept (e.g. a diff stat and captured patch file path).
	Note string
	// Finish is called after the run completes: it removes the isolation when
	// no work was produced and reports whether it was kept.
	Finish func() (kept bool)
}

// Isolator provisions isolated working copies for subagent runs when the
// caller passes isolation: "worktree".
type Isolator interface {
	// Isolate creates a fresh isolated checkout. label is a short slug used in
	// branch/dir naming (e.g. "t3" or "task-2").
	Isolate(label string) (*Isolation, error)
}

// isolationWorktree is the only accepted isolation mode.
const isolationWorktree = "worktree"

// validateIsolation checks the isolation input value. Returns a user-facing
// error message ("" = ok).
func (t *Tool) validateIsolation(mode string) string {
	switch mode {
	case "":
		return ""
	case isolationWorktree:
		if t.Isolator == nil {
			return "Isolation is not available here (no isolator configured — is this a git repository?)"
		}
		return ""
	default:
		return fmt.Sprintf("Unknown isolation mode %q. Available: %q", mode, isolationWorktree)
	}
}

// isolate wraps run so it executes inside a fresh isolated checkout.
func (t *Tool) isolate(label string, run func(ctx context.Context) (string, error)) func(ctx context.Context) (string, error) {
	return Isolate(t.Isolator, label, run)
}

// Isolate wraps run so it executes inside a fresh isolated checkout: the
// worktree is created lazily when the run starts (not at prepare time, so
// background runs don't hold a worktree while queued), the builtin tools are
// pointed at it via ctx, and after the run it is cleaned up when unchanged or
// kept (path + branch appended to the answer) when the subagent produced work.
// Exported so hosts (e.g. the Lua subagents API) can reuse the same isolation
// semantics as the builtin SubAgent tool.
func Isolate(isolator Isolator, label string, run func(ctx context.Context) (string, error)) func(ctx context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		iso, err := isolator.Isolate(label)
		if err != nil {
			return "", fmt.Errorf("worktree isolation: %w", err)
		}
		// Defer Finish so the worktree is cleaned up even if the run panics.
		var kept bool
		defer func() {
			if iso.Finish != nil {
				kept = iso.Finish()
			}
		}()
		answer, runErr := run(tool.WithWorkdir(ctx, iso.Dir, iso.MainRoot))
		if runErr != nil {
			// The subagent errored, but it may have produced work before
			// failing (e.g. partial edits, context cancellation). If the
			// worktree has changes, report its location so the user can
			// recover the work. The deferred Finish populates `kept`.
			if iso.Finish != nil {
				kept = iso.Finish()
				iso.Finish = nil // prevent double-call from defer
			}
			if kept {
				return "", fmt.Errorf("%w\n\n[worktree kept with partial work: %s on branch %s]%s",
					runErr, iso.Dir, iso.Branch, notesSuffix(iso.Note))
			}
			return "", runErr
		}
		if iso.Finish != nil {
			kept = iso.Finish()
			iso.Finish = nil // prevent double-call from defer
		}
		if kept {
			answer += fmt.Sprintf(
				"\n\n[worktree kept: the subagent made changes in %s on branch %s — review and merge or discard them]",
				iso.Dir, iso.Branch)
			if iso.Note != "" {
				answer += "\n" + iso.Note
			}
		} else {
			answer += "\n\n[worktree removed: the subagent made no changes]"
		}
		return answer, nil
	}
}

// notesSuffix prefixes a newline when the note is non-empty.
func notesSuffix(note string) string {
	if note == "" {
		return ""
	}
	return "\n" + note
}
