## Why

Agent Runner currently only supports Claude CLI as its agent backend, and uses raw `os/exec` to spawn it — meaning interactive steps hand off the terminal entirely and Agent Runner has no way to intercept user input or signal "continue" without a sideband file. Adding Codex CLI support and a pseudo-terminal layer solves both: workflows can choose which CLI runs each step, and Agent Runner can host any CLI inside a PTY while retaining host-level controls like `/next`.

## What Changes

- Add a `cli` field to workflow steps so each step can declare whether it runs under `claude` or `codex` (or a future CLI). When absent, the runner defaults to `claude`. The existing `agent` field on `Workflow` is removed (a project-level default will be added in a future change).
- Introduce a **CLI adapter** abstraction (`internal/cli`) that encapsulates how each CLI is invoked, how its args are built, how sessions are discovered/resumed, and what flags it supports. Claude and Codex each get an adapter implementation.
- Replace the current direct `exec.Command("claude", ...)` invocation in `internal/exec/agent.go` with adapter dispatch — the runner asks the adapter for the command, and the adapter returns it.
- Integrate the PTY proxy from `cmd/pty-poc` into the main runner for interactive steps. Interactive agent steps will spawn inside a PTY so Agent Runner can intercept `/next` and `Ctrl-]` without a signal file.
- Headless steps continue to use direct `exec.Command` (no PTY needed) but go through the CLI adapter for arg construction.
- **BREAKING**: The `agent` field on `Workflow` is removed. Existing workflows using `agent: claude` must remove that line. Steps that need a non-default CLI should set `cli:` explicitly.

## Capabilities

### New Capabilities

- `cli-adapter`: Abstraction layer for CLI backends (Claude, Codex). Defines how each CLI is invoked, what flags it accepts, how sessions are discovered and resumed, and how args are constructed for headless vs interactive modes.
- `codex-cli-support`: Support for running Codex CLI as a workflow step backend, including arg construction, session handling, and Codex-specific flags like `--no-alt-screen`.
- `pseudo-terminal`: PTY-based execution for interactive agent steps. Agent Runner creates a PTY, attaches the CLI to it, proxies I/O, intercepts `/next` and `Ctrl-]` for continue, handles escape sequences, terminal resize, and clean shutdown. Replaces the signal-file mechanism for interactive steps.

### Modified Capabilities

- `step-model`: The step schema gains a `cli` field (valid values: `claude`, `codex`). Validation rules extend to cover the new field. The `model` field behavior may differ per CLI adapter.
- `workflow-execution`: The runner's agent step executor delegates to CLI adapters instead of hardcoding Claude. Session discovery becomes adapter-specific. Interactive steps use PTY execution instead of direct terminal handoff.

## Impact

- **`internal/model/step.go`**: Add `CLI` field to `Step` and remove `Agent` from `Workflow`. Update `ApplyDefaults` and `Validate`.
- **`internal/exec/agent.go`**: Replace `buildAgentArgs` and session discovery with CLI adapter dispatch. Split interactive execution into a PTY path.
- **New `internal/cli/`**: Package with `Adapter` interface and implementations for `claude` and `codex`.
- **New `internal/pty/`**: Package extracted from `cmd/pty-poc` — PTY lifecycle, raw terminal mode, escape sequence parser, `/next` interception, idle hint, resize forwarding.
- **`internal/loader/loader.go`**: Parse the new `cli` field.
- **`internal/validate/`**: Validate `cli` values against known adapters.
- **`internal/engine/`**: `EnrichPrompt` may need the CLI identity to tailor prompts.
- **Workflow YAML schema**: New `cli` field at workflow and step level.
- **Dependencies**: `github.com/creack/pty` and `golang.org/x/sys/unix` move from POC-only to main module dependencies.
