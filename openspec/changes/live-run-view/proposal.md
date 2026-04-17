## Why

The current live workflow output — `━` separator lines and `━━ step N/M: path > step [type] ━━` breadcrumb headings — is an explicitly temporary placeholder. The `runview` TUI, built in the `view-run` change, already handles live audit-log tailing, step-tree rendering, and per-step detail panes. Using it as the live execution experience replaces the placeholder with a polished, consistent UI and removes a parallel rendering surface entirely.

## What Changes

- Launch the `runview` TUI immediately when a workflow starts, replacing all streaming console output.
- TUI stays open after the workflow completes (success or failure); user exits explicitly with `q`, `Ctrl+C`, or Escape.
- Interactive agent steps suspend the TUI, run inline on the terminal, and re-enter after completion.
- TUI-only mode: require a TTY; print a clear error and exit if stdout is not an interactive terminal. No plain-text fallback.
- Shell and headless agent step output renders in the TUI detail pane in real time as produced, and persists to per-step output files under the session directory for post-run inspection.
- **BREAKING**: Disallow opening an active run from a second process — `--inspect` and list-runs Enter on a run locked by another process are rejected with a clear error.
- **BREAKING**: Remove step separator lines, breadcrumb headings, headless spinner, and pre-step headless prompt echo from the terminal output path entirely.

## Capabilities

### New Capabilities

- `live-run-view`: The contract that `runview` is the live workflow console — launched on workflow start, real-time output in the detail pane, TUI suspension for interactive steps, TTY requirement and error behavior, and second-process lockout.

### Modified Capabilities

- `console-output-formatting`: Step separator and breadcrumb requirements removed entirely (superseded by `live-run-view`).
- `headless-progress-indication`: Spinner animation and pre-step prompt display requirements removed entirely (superseded by `live-run-view`).
- `view-run`: Second-process lockout added — `--inspect` on a run with an active process lock SHALL be rejected.
- `list-runs`: Enter on a run with an active process lock SHALL be rejected.

## Out of Scope

- Process model choice (subprocess vs in-process) — deferred to design.
- Non-TTY / CI use cases — explicitly not supported in this change.
- Full output streaming mechanism — deferred to design.
- Multiple simultaneous viewers of the same run.
- Detaching from a live run without stopping it.

## Impact

- `cmd/agent-runner/main.go`: TTY gate on TUI-launching paths; `handleRun`/`handleResume` rewired to launch the TUI + run the workflow in a background goroutine; `handleInspect` gets the second-process lockout guard.
- `internal/runner`: `RunWorkflow` / `ResumeWorkflow` split into `PrepareRun`/`PrepareResume` + `ExecuteFromHandle` so main can size the TUI from a real session directory before execution starts; `Options` gains `SuspendHook` / `ResumeHook`.
- `internal/exec/agent.go`: invoke `SuspendHook` / `ResumeHook` around `interactiveRunnerFn`; delete the headless spinner and pre-step headless prompt echo.
- `internal/exec/shell.go`: widen `step_end` to always include a `stdout` preview (not only when the step has a `capture:` field).
- `internal/liverun` (new package): `Coordinator`, `TUIProcessRunner`, `chunkWriter`, ANSI stripper, bubbletea message types for streaming output / step-state updates / TUI suspension.
- `internal/runview`: new `FromLiveRun` entered mode; message handlers for live updates; quit-confirmation modal; cursor auto-follow + jump-to-live (`l`); detail-pane tail-follow + `End`/`G`; failed-step auto-jump on workflow failure; reads per-step output files for post-run detail.
- `internal/listview`: Enter handler rejects runs locked by another process with an inline error.
- `internal/runlock`: new `CheckOwnedByOther(sessionDir, selfPID) bool` helper.
- `internal/textfmt`: delete `Separator` and `StepHeading`.
- Session-on-disk layout: new `<sessionDir>/output/<step-prefix>.out` / `.err` files for durable per-step output.
- **Dependency**: this change's `view-run` and `list-runs` spec deltas assume the `view-run` change is archived first, since they build on requirements introduced there.
- **BREAKING**: any tooling parsing agent-runner stdout loses structured output; non-TTY invocations of TUI-launching paths are rejected.
