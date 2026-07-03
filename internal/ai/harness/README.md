# harness

Reusable primitives for building AI coding agents whose **execution environment is
swappable**. File tools run over a narrow `fsys.Store` (local disk, S3, or *derived from a
shell*) and shell tools run over an `exec.Executor` (local shell, remote worker, …).
Everything else — providers, retry, routing, compaction, skills, subagents, tool search — is
an optional "lego" piece you add only when you need it. Unused pieces cost nothing.

```
import "github.com/Abraxas-365/manifesto/internal/ai/harness"
```

## The 30-second version

```go
fs, _ := fsxlocal.NewLocalFileSystem(".")
ex := exec.NewLocalExecutor(".")

registry := builtins.Default(fsxstore.New(fs), ex) // Read, Write, Edit, List, Glob, Grep, Bash
                                                   // (+ BashOutput, KillShell if ex supports background)
provider := openai.New(os.Getenv("OPENAI_API_KEY"))

agent := harness.New(provider, registry)
agent.System = "You are a helpful coding assistant."
agent.Model = "gpt-4o"

answer, err := agent.Run(ctx, "List the Go files and summarize main.go")
```

`Run` drives the full tool-calling loop and returns the final assistant text. Conversation
state persists on the agent across calls.

## Core concepts

| Piece | Package | What it is |
|-------|---------|------------|
| **Provider** | `llm` | `Chat`/`ChatStream` interface. Implemented by `llm/openai` and `llm/anthropic`. |
| **Tool** | `tool` | A capability the model can call. `Registry` holds them; `builtins` ships file/shell tools. |
| **Agent** | `harness` | The tool-calling loop: `New(provider, registry)`, set `System`/`Model`, call `Run`. |
| **Store** | `fsys` | Narrow filesystem backing the file tools (ReadFile/WriteFile/Stat/List/MkdirAll). Adapt an `fsx.FileSystem` with `fsxstore.New`, or derive one from an executor with `execstore.New`. |
| **Executor** | `exec` | Swappable shell backing the Bash tool (local, remote). |

## Choosing an environment

The file tools (`Read`/`Write`/`Edit`/`List`/`Glob`/`Grep`) run over a `fsys.Store`; `Bash`
runs over an `exec.Executor`. When they point at *different* places — say an S3 store with a
local shell — the agent edits one world and compiles another ("split-brain"). Three
constructors make the right wiring the easy wiring, so you pick by environment instead of
hand-assembling backends:

| Environment | Constructor | Why it's safe |
|-------------|-------------|---------------|
| **Local dev** | `builtins.Default(fsxstore.New(local), LocalExecutor)` | store and shell are the same disk |
| **Storage-only (S3)** | `builtins.Files(fsxstore.New(s3))` | no Bash registered — nothing to diverge |
| **Remote container / SSH** | `builtins.FromExecutor(remoteExec)` | file ops are *derived from the same executor* as Bash — split-brain is impossible by construction |

- **`Files(store)`** — the six file tools only. Use it when there's no shell (S3 is storage,
  not compute). This is why [`11_s3`](./examples/11_s3) omits Bash.
- **`Default(store, ex)`** — file tools + Bash. You supply both backends; use it for local dev
  or any pair you *know* shares an environment.
- **`FromExecutor(ex)`** — file tools + Bash, with the store **derived from `ex`** via
  `execstore` (file ops run as shell commands: `cat`/`base64`/`find`/… in the executor's
  environment). One backend powers everything, so the tools cannot point at different worlds.
  Use it for Docker, SSH, or any remote executor (see [`12_docker`](./examples/12_docker)).

Under the hood a `fsys.Store` is a small five-method interface (`ReadFile`, `WriteFile`,
`Stat`, `List`, `MkdirAll`). `fsxstore.New` adapts any `fsx.FileSystem` (local, S3) to it;
`execstore.New` implements it over an `exec.Executor` for POSIX shell environments.

### Compiling, running, and long-lived servers

With any of the constructors above, `Bash` compiles and runs code normally
(`go build ./...`, `pytest`, …) — these run to completion and return output.

A plain `Bash` call is **synchronous** and capped by a timeout (default 120s),
then the whole process group is killed. That is wrong for a server you want to
keep alive. When the Executor implements `exec.BackgroundExecutor` (as
`LocalExecutor` does), `builtins.Default` also registers **`BashOutput`** and
**`KillShell`**, and `Bash` accepts `run_in_background: true`:

```
Bash{ command: "npm start", run_in_background: true }  → "Started background shell shell_1"
BashOutput{ shell_id: "shell_1" }                      → incremental logs + running/exited status
KillShell{ shell_id: "shell_1" }                       → stops it
```

The detached process outlives the tool call and stops only on its own exit or
`KillShell`. An Executor that implements only the base `Executor` interface
simply won't expose these tools, and `run_in_background` returns a clear error.

## Optional lego pieces

Each is opt-in and composes with the others.

| Feature | How to enable | Package |
|---------|---------------|---------|
| **Retry / backoff** | `agent.EnableRetry()` | `llm/retry` |
| **Model routing** (many models → right provider) | `router.New().Handle("gpt-*", …)` | `llm/router` |
| **Prompt caching** (Anthropic) | `anthropic.NewWithOptions(key, []Option{anthropic.WithPromptCaching()})` | `llm/anthropic` |
| **Observability hooks** | `agent.Hooks = harness.Hooks{…}` | `harness` |
| **Context compaction** | `agent.Compactor = harness.TruncateCompactor{KeepRecent: 20}` | `harness` |
| **Tool search** (defer big schemas) | `registry.SetDeferred(name, hint)` + `agent.EnableToolSearch()` | `toolsearch` |
| **Skills** (on-demand instruction sets) | `registry.Register(&skill.Tool{Registry: skReg})` | `skill` |
| **Subagents** (delegate isolated subtasks) | `registry.Register(&subagent.Tool{NewAgent: …})` | `subagent` |
| **Todo list** (declarative task tracking) | `registry.Register(&todo.Tool{})` | `todo` |
| **Provider options / reasoning** | `agent.Reasoning = llm.ReasoningMedium`; `agent.ProviderOptions = …` | `llm` |

Provider-specific request configuration has three composable layers, each a no-op
when unset:

```go
// 1. Portable pointer knobs — nil = omit (temperature is dropped for models
//    that reject it, e.g. reasoning models).
temp := 0.2
agent.Temperature = &temp
agent.TopP = &temp

// 2. Unified reasoning — mapped per provider (OpenAI reasoning_effort,
//    Anthropic thinking budget) and omitted when the model can't reason.
agent.Reasoning = llm.ReasoningMedium

// 3. Raw per-provider escape hatch — merged straight into the request body.
agent.ProviderOptions = map[string]map[string]any{
    "openai": {"service_tier": "flex"},
}
```

Model capabilities (does it support temperature / reasoning, and how to map the
level) live in a small in-code table in `llm/capability.go` — extend it as new
model families appear.

## Examples

Runnable programs live in [`examples/`](./examples). Each is a standalone `main` package;
set the relevant API key and `go run` it.

| Example | Shows |
|---------|-------|
| [`01_minimal`](./examples/01_minimal) | Smallest possible agent: builtins + one provider + `Run`. |
| [`02_router`](./examples/02_router) | Route `gpt-*` to OpenAI and `claude-*` to Anthropic with one `Provider`. |
| [`03_retry_hooks`](./examples/03_retry_hooks) | Transparent retry plus observability hooks (turns, tools, usage). |
| [`04_toolsearch`](./examples/04_toolsearch) | Defer rarely-used tools so their schemas load on demand. |
| [`05_skills`](./examples/05_skills) | Load a skill (fsx / embedded / in-code) and let the agent read its references. |
| [`06_subagent`](./examples/06_subagent) | Delegate a subtask to a nested agent and pick its model. |
| [`07_compaction`](./examples/07_compaction) | Keep long conversations within the context window. |
| [`08_custom_tool`](./examples/08_custom_tool) | Implement `tool.Tool` and register your own capability. |
| [`09_provider_options`](./examples/09_provider_options) | The three layers: pointer temp/topP, unified reasoning, raw provider bag. |
| [`10_todo`](./examples/10_todo) | Declarative task list (in-memory by default, swappable `Store`). |
| [`11_s3`](./examples/11_s3) | Real agent over an S3 bucket, routing to multiple providers — `builtins.Files` (file tools only) + router. |
| [`12_docker`](./examples/12_docker) | `builtins.FromExecutor` over a `docker exec` executor — file tools and Bash both run inside the container, split-brain impossible. |
| [`13_fromexecutor`](./examples/13_fromexecutor) | The same one-backend pattern locally (no Docker): a `LocalExecutor` in a scratch dir backs both file tools and Bash. Includes a test that needs no API key. |

## Design principles

- **Optional, not baked in.** A nil field is a no-op. Adding a feature never changes the
  behaviour of code that doesn't use it.
- **Decorator providers.** `retry.Provider` and `router.Router` both implement `llm.Provider`,
  so they wrap each other and the agent holds whichever you like.
- **Swap the environment, keep the agent.** Point the same agent at S3 or a remote executor by
  changing only the registry constructor (`Files` / `Default` / `FromExecutor`).
- **Make the safe wiring structural.** `FromExecutor` derives the file store from the same
  executor as Bash, so an environment mismatch can't be expressed rather than merely discouraged.
- **Fail loud at wiring time.** e.g. `EnableToolSearch()` panics if `SetDeferred` names a tool
  that was never registered (a typo), rather than silently doing nothing.

## Verification

```
go build ./internal/ai/harness/...
go test  ./internal/ai/harness/... -count=1
```
