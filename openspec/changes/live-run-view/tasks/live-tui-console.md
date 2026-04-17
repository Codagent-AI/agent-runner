# Task: Live TUI as the workflow console

## Goal

Make the `runview` TUI the foreground console for every agent-runner workflow invocation. Today, workflow execution streams breadcrumbs and separator lines to stdout; this task replaces that stream with the existing `runview` TUI running concurrently with the workflow. Shell and headless subprocess output streams into the TUI detail pane in real time and persists on disk. Interactive agent steps hand the terminal off cleanly and re-enter afterward. The placeholder stdout output is deleted.

## Background

You MUST read these files before starting:
- `openspec/changes/live-run-view/proposal.md` for the "why"
- `openspec/changes/live-run-view/design.md` for the full design
- `openspec/changes/live-run-view/specs/live-run-view/spec.md` for the live-run-view capability scenarios
- `openspec/changes/live-run-view/specs/console-output-formatting/spec.md` for the removals
- `openspec/changes/live-run-view/specs/headless-progress-indication/spec.md` for the removals
- `openspec/changes/view-run/design.md` and `openspec/changes/view-run/specs/view-run/spec.md` for existing runview behavior you're extending

### The shape of the change

Today `cmd/agent-runner/main.go:handleRun()` calls `runner.RunWorkflow(...)` synchronously on the main goroutine, and the workflow prints to stdout via `realLogger`/`realProcessRunner`. This task moves the workflow to a background goroutine, puts the `runview` bubbletea program on the main goroutine, and coordinates them via a shared `*tea.Program` handle. Same change for `handleResume`, and TTY gating for `handleList` / `handleInspect`.

Key constraints from the design:
- **Concurrency model.** TUI on main goroutine (bubbletea requires this). Runner in a goroutine. They share a `*tea.Program` handle for `p.Send(msg)`, `p.ReleaseTerminal()`, `p.RestoreTerminal()`.
- **Runner refactor.** Split `runner.RunWorkflow` into `PrepareRun(workflow, params, opts) (*RunHandle, error)` (today's `initRunState` + `emitRunStart`) and `ExecuteFromHandle(h, opts) WorkflowResult` (today's `executeSteps` + `finalizeRun`). Keep `RunWorkflow` as a thin wrapper for existing tests and non-TUI callers. Same split for `ResumeWorkflow` → `PrepareResume(stateFilePath, opts)` + `ExecuteFromHandle`. The `RunHandle` MUST expose `SessionDir` and `ProjectDir` so `main` can size the runview before execution starts.
- **Interactive step suspension.** Add `SuspendHook func()` and `ResumeHook func()` to `runner.Options`. Thread them through `model.ExecutionContext` into `internal/exec/agent.go`, where they wrap the existing `interactiveRunnerFn(args, pty.Options{...})` call. Nil hooks = no-op, preserving existing test behavior. In TUI mode, main wires them to `p.ReleaseTerminal()` / `p.RestoreTerminal()`.
- **Output streaming.** New package `internal/liverun` with a `Coordinator` (holds `*tea.Program`, session dir) and a `TUIProcessRunner` that wraps `realProcessRunner`. For non-captured shell/headless steps, replace `os.Stdout`/`os.Stderr` with composite writers that:
  1. Strip ANSI and coalesce into ~4 KB batches with a 50 ms idle flush, then `p.Send(OutputChunkMsg{StepPrefix, Stream, Bytes})`.
  2. Tee raw bytes (including ANSI) to `<sessionDir>/output/<step-prefix>.out` / `.err`.
  3. Buffer into a `bytes.Buffer` so `ProcessResult.Stdout`/`Stderr` still populate with the final captured content — the Go-level return-value contract of `ProcessRunner.RunShell` / `RunAgent` is unchanged.
  The step-prefix is the audit log's event `prefix` field, filesystem-sanitized: replace `/` with `__`, replace `:` with `_`. Example: audit prefix `loop-b:2/step-c` → file `output/loop-b_2__step-c.out`.
  Interactive agent steps bypass `TUIProcessRunner` entirely (they go through `pty.RunInteractive` via the `SuspendHook`/`ResumeHook` path) and MUST NOT produce output files.
- **ANSI stripping.** Small state machine consuming CSI (`ESC [ ... final`), OSC (`ESC ] ... BEL`), and SGR sequences; pass text and newlines through. Use on the channel-delivery path only. The output file gets raw bytes. The stripper must buffer partial sequences across write boundaries.
- **Shell audit payload widened.** `internal/exec/shell.go` currently emits `stdout` in its `step_end` event only when the step has a `capture:` field. Widen the `endData` map in `ExecuteShellStep` to always include the `stdout` key, reusing `truncateForAudit` (defined at the top of `internal/exec/shell.go` with `const maxAuditValueLen = 4096`). This is a widening of the on-disk audit event payload; the Go-level `ProcessResult` contract is unchanged (it has always carried `Stdout` — we're just always emitting it into the event now). Full content lives in the output file, not the audit log.
- **TTY check.** Add a helper (e.g., `cmd/agent-runner/tty.go:requireTTY() error`) using `github.com/mattn/go-isatty` (likely already transitively available; add to go.mod if needed). Call at the top of `handleRun`, `handleResume`, `handleList`, `handleInspect`. Do NOT call for `--validate`, `--version`, `-v`, or `-C` alone. On failure: print one-line stderr error identifying the TTY requirement and exit non-zero before any side effects.
- **runview live mode.** Add a new `Entered` value `FromLiveRun` (sibling of existing `FromList` / `FromInspect` in `internal/runview/model.go`). Add model fields for `running bool` and `quitConfirming bool`. Add handlers for new bubbletea messages from `liverun`:
  - `OutputChunkMsg{StepPrefix, Stream, Bytes}` — locate target step by prefix, append to its in-memory output buffer (use the existing 2000-line / 256 KB tail-render threshold from view-run output rendering), re-render if selected.
  - `StepStateMsg{ActiveStepPrefix, Outcome}` — update the Model's `activeStep` pointer. (Cursor auto-follow logic is out of scope for this task.)
  - `ExecDoneMsg{Result WorkflowResult, Err error}` — flip `running=false`; breadcrumb status re-renders as `completed` / `failed`. From this point forward, the Model MUST behave identically to `FromInspect` — the user can navigate, drill in/out, scroll output, and trigger the resume action on agent steps (reusing the existing `view-run` resume-action path, no new code). (Failed-step auto-jump is out of scope for this task.)
  - `SuspendedMsg` / `ResumedMsg` — internal bookkeeping only. Do NOT paint an overlay during suspension; the TUI is fully released and the user sees the agent's own output.
- **Quit confirmation.** While `running`, pressing `q`, `Ctrl+C`, or Escape-at-top-level opens a modal with explicit orphan-warning copy:
  > The workflow is still running. Quitting will orphan the active step — it will continue running in the background. Quit anyway?  [y]es  [n]o
  On `y` → `tea.Quit`. On `n` / Esc → dismiss. No process killing; orphaning is deliberate. Escape while drilled-into a sub-workflow / loop / iteration is the normal drill-out key (handled by the existing `view-run` key handler) — the confirmation only fires at top level.
  The confirmation does NOT need special handling during an interactive agent step because while the TUI is suspended, keystrokes go to the agent, not to bubbletea.
  After `ExecDoneMsg`, `q`, `Ctrl+C`, and Escape-at-top-level exit immediately without confirmation.
- **main rewire.** After `requireTTY`, call `runner.PrepareRun(...)` (or `PrepareResume`), construct `runview.New(handle.SessionDir, handle.ProjectDir, runview.FromLiveRun)`, build a `*tea.Program`, construct a `liverun.Coordinator`, start a goroutine that runs `runner.ExecuteFromHandle(handle, opts)` with `opts.ProcessRunner = coord.TUIProcessRunner(realProcessRunner{})`, `opts.SuspendHook = coord.BeforeInteractive`, `opts.ResumeHook = coord.AfterInteractive`, `opts.Log = discardLogger{}`, wrapped in `defer recover() { coord.NotifyDone(Failed, panic) }`; after the runner returns, send `coord.NotifyDone(result, err)`. Then call `p.Run()` on the main goroutine.
- **Logger in TUI mode.** Inject a `discardLogger` (all methods no-op) in the TUI paths; the TUI surfaces status instead of stdout. The existing `realLogger` stays for non-TUI test/library callers.
- **Removals (required by the `console-output-formatting` and `headless-progress-indication` deltas).** Delete from `internal/textfmt`: `Separator()` and `StepHeading(...)`. Delete the headless spinner and pre-step headless prompt echo in `internal/exec/agent.go`. Delete the `rs.log.Println(Separator...)` / `StepHeading(...)` calls in `runner.executeSteps`. Delete `rs.log.Printf("--- step %q complete ---\n\n", ...)` and the `"agent-runner: workflow complete"` / `"to resume: ..."` log calls — they are superseded by the TUI's breadcrumb status and the resume action inside the run view.
- **Lockout note.** Second-process lockout on `--inspect` and list-Enter is explicitly out of scope for this task (covered separately). Do NOT add `runlock.CheckOwnedByOther` or its guards here.
- **Auto-follow note.** Cursor auto-follow tracking the active step, `l` key jump-to-live, detail-pane tail-follow with `End`/`G`, and failed-step auto-jump on `ExecDoneMsg` are out of scope for this task. Leave the cursor passively updated by user navigation only; the runview already works that way. Scenarios for those behaviors are not in this task.

### Files you'll touch

- `cmd/agent-runner/main.go` — restructure handleRun/handleResume/handleInspect/handleList
- `cmd/agent-runner/tty.go` (new) — requireTTY helper
- `internal/runner/runner.go` — split RunWorkflow → PrepareRun + ExecuteFromHandle
- `internal/runner/resume.go` — split ResumeWorkflow → PrepareResume + ExecuteFromHandle
- `internal/exec/agent.go` — invoke SuspendHook/ResumeHook around interactiveRunnerFn; delete spinner and prompt echo
- `internal/exec/shell.go` — widen step_end stdout preview
- `internal/liverun/*` (new package) — Coordinator, TUIProcessRunner, chunkWriter, ANSI stripper, message types
- `internal/runview/model.go` + `update.go` + `view.go` — FromLiveRun mode, message handlers, quit-confirm modal
- `internal/runview/output.go` — output-file reader fallback for post-run detail pane (when live channel isn't active)
- `internal/runner/runner.go` (Options struct) — add SuspendHook/ResumeHook
- `internal/model/execution_context.go` — thread SuspendHook/ResumeHook
- `internal/textfmt/*` — delete Separator, StepHeading (remove file if empty after)

### Constraints

- The `RunWorkflow` / `ResumeWorkflow` wrappers MUST remain callable with their existing signatures — existing tests call them directly.
- All new code paths under nil hooks / no TUI MUST behave exactly as today. No hidden TUI assumptions in `runner` or `exec`.
- `p.Send` is non-blocking; `chunkWriter` MUST cap message rate (size + idle flush) to avoid channel overflow.
- ANSI stripper MUST handle partial sequences across write boundaries (buffer the unfinished suffix).
- Step-prefix sanitization: replace `/`, `:`, whitespace with `_` (or similar); preserve uniqueness. No path traversal via crafted step IDs.
- Alt-screen interaction on `ReleaseTerminal` → `pty.RunInteractive` (claude-code's own alt-screen) → `RestoreTerminal` is a known unknown — do a minimal prototype first; if broken, fall back to quit-and-relaunch and note in the PR.
- The runner goroutine body MUST be wrapped with `defer recover()` that sends `ExecDoneMsg{Result: Failed, Err: panicErr}` so the TUI never hangs in `running=true`.

## Spec

### Requirement: Workflow invocation launches the run-view TUI

When agent-runner is invoked to run a workflow — either a fresh invocation or `--resume <session-id>` — the run-view TUI SHALL launch as the foreground display immediately and remain the sole interface for the run's duration. No streaming console output SHALL precede TUI initialization.

#### Scenario: Fresh workflow launches TUI
- **WHEN** agent-runner is invoked with a workflow name or path
- **THEN** the run-view TUI takes over the terminal with the step list populated from the workflow file, all rows in `pending`, before any step dispatches

#### Scenario: --resume launches TUI
- **WHEN** agent-runner is invoked with `--resume <session-id>` for an inactive run
- **THEN** the run-view TUI takes over with the step tree hydrated from audit.log and execution resumes from the recorded latest-step pointer

### Requirement: TTY required for TUI-launching invocations

Any agent-runner invocation that launches the TUI — workflow execution, `--resume`, `--list`, `--inspect` — SHALL require stdout to be an interactive terminal. If stdout is not a TTY, agent-runner SHALL print an error message to stderr identifying the TTY requirement and exit with a non-zero status without launching the TUI or executing the workflow. Non-TUI invocations (`--validate`, `--version`, `-v`) SHALL remain usable without a TTY.

#### Scenario: Piped stdout rejected for workflow run
- **WHEN** agent-runner is invoked to run a workflow with its stdout piped to another process
- **THEN** it prints a clear error to stderr and exits non-zero; no workflow steps dispatch

#### Scenario: Redirected stdout rejected for workflow run
- **WHEN** agent-runner is invoked to run a workflow with stdout redirected to a file
- **THEN** it prints a clear error to stderr and exits non-zero

#### Scenario: Interactive terminal proceeds
- **WHEN** agent-runner is invoked from an interactive terminal
- **THEN** the TUI launches and the workflow executes normally

#### Scenario: --validate does not require TTY
- **WHEN** `agent-runner --validate <workflow>` is invoked without a TTY (piped, redirected, or in CI)
- **THEN** validation runs and agent-runner exits with the validation outcome; the TTY check does not fire

#### Scenario: --version does not require TTY
- **WHEN** `agent-runner --version` (or `-v`) is invoked without a TTY
- **THEN** the version string is printed and agent-runner exits zero; the TTY check does not fire

### Requirement: TUI stays open after workflow completion

When the workflow reaches a terminal state (success or failure), the run-view TUI SHALL remain active. Exit SHALL require explicit user input (`q`, `Ctrl+C`, or Escape at the top level). Once in this post-completion state, the run view SHALL behave identically to a run opened via `--inspect` — the user can navigate the step list, drill in and out, scroll output, trigger the resume action on agent steps, and invoke the legend overlay.

#### Scenario: Successful completion keeps TUI open
- **WHEN** the last step in the workflow completes successfully
- **THEN** the TUI remains open with the breadcrumb status showing `completed`

#### Scenario: Failure keeps TUI open
- **WHEN** a step fails and the workflow halts
- **THEN** the TUI remains open with the breadcrumb status showing `failed`

#### Scenario: Post-completion navigation matches inspect mode
- **WHEN** the workflow has finished and the user navigates the step list, drills into sub-workflows or iterations, or scrolls the detail pane
- **THEN** the behavior is identical to a run opened via `--inspect` (per the `view-run` capability)

#### Scenario: Resume action available after completion
- **WHEN** the workflow has finished and the user triggers the resume action on an agent step with a known session ID
- **THEN** the TUI exits and agent-runner is relaunched with `--resume <session-id>`, exactly as the `view-run` capability's resume behavior specifies

### Requirement: Real-time step output

Shell and headless agent step stdout and stderr SHALL render in the detail pane as output is produced, not only after the step completes. Output is delivered to the TUI via an in-process channel (bubbletea `p.Send`) as bytes are produced by the subprocess, with ANSI escape sequences stripped before rendering. The same bytes (raw, unstripped) SHALL also be written to `<sessionDir>/output/<step-prefix>.out` and `<step-prefix>.err` for durable post-run inspection regardless of audit-log truncation, where `<step-prefix>` is the audit log event's `prefix` field with `/` replaced by `__` and `:` replaced by `_`. First-byte latency target: visible in the detail pane within 100 ms of being produced. Interactive agent steps are excluded — the agent owns the terminal directly during such steps, so agent-runner neither streams nor persists the agent's output itself.

#### Scenario: Long-running shell step output streams
- **WHEN** a shell step is executing and producing stdout
- **THEN** its detail pane reflects newly produced bytes without waiting for the step to finish

#### Scenario: Headless agent output streams
- **WHEN** a headless agent step is executing and its CLI is producing output
- **THEN** its detail pane reflects newly produced bytes without waiting for the step to finish

#### Scenario: ANSI sequences are stripped in the detail pane
- **WHEN** a shell step emits ANSI color or cursor-positioning sequences (e.g., `ls --color`, `git diff`)
- **THEN** the detail pane renders the text without those sequences and without visual corruption of the surrounding TUI layout

#### Scenario: Output persists past step completion
- **WHEN** a shell or headless agent step has completed
- **THEN** its full stdout and stderr (including any ANSI sequences, untruncated) are readable from `<sessionDir>/output/<step-prefix>.out` and `.err` in the session directory

#### Scenario: Post-completion detail pane reads from output files
- **WHEN** the workflow has finished and the user selects a shell or headless step (either in the same live-run TUI session or after re-opening via `--inspect` / list Enter)
- **THEN** the detail pane loads that step's output from the persisted output files, showing full content (subject only to the 2000-line / 256 KB tail-render threshold)

#### Scenario: Interactive agent step has no output files
- **WHEN** an interactive agent step runs and exits
- **THEN** no `<sessionDir>/output/<step-prefix>.out` or `.err` files are created for it; the detail pane shows the agent's profile, CLI, model, and session metadata as specified by the `view-run` detail-pane requirement

### Requirement: Interactive agent steps suspend the TUI

When the workflow dispatches an interactive agent step, the run-view TUI SHALL suspend, releasing the terminal so the agent process has full control. When the agent process exits, the TUI SHALL re-enter automatically without user input, regardless of the agent's exit status.

#### Scenario: Interactive step takes over terminal
- **WHEN** an interactive agent step starts
- **THEN** the run-view TUI suspends and the agent process owns the terminal

#### Scenario: Agent exits successfully and returns to TUI
- **WHEN** the interactive agent process exits with a successful outcome (continue-trigger received)
- **THEN** the run-view TUI re-enters automatically, the step's row reflects `success`, and workflow execution continues

#### Scenario: Agent exits abnormally and returns to TUI
- **WHEN** the interactive agent process exits without a continue-trigger (the session was abandoned or the CLI returned non-zero)
- **THEN** the run-view TUI re-enters automatically and the step's row reflects the recorded outcome (`aborted` or `failed`, per the existing interactive-agent behavior defined in the agent-runner engine)

### Requirement: Quit during live run requires confirmation

While the workflow is running, pressing `q`, `Ctrl+C`, or Escape at the top level SHALL prompt the user for confirmation before quitting. The confirmation prompt SHALL explicitly state that the active subprocess will be orphaned (continue running in the background) if the user proceeds. Confirming SHALL exit the TUI without killing the active subprocess. Declining SHALL dismiss the prompt and leave the workflow running. After the workflow has finished, `q`, `Ctrl+C`, and Escape-at-top-level SHALL exit immediately without confirmation. The confirmation does not fire while the TUI is suspended for an interactive agent step, because during that window keystrokes are received by the agent, not the TUI.

#### Scenario: Confirmation requested on q mid-run
- **WHEN** the user presses `q` while the workflow is running and the TUI is not suspended for an interactive step
- **THEN** a confirmation prompt is displayed stating that the active subprocess will be orphaned on confirm; the workflow continues while the prompt is open

#### Scenario: Confirmation requested on Ctrl+C mid-run
- **WHEN** the user presses `Ctrl+C` while the workflow is running and the TUI is not suspended for an interactive step
- **THEN** the same confirmation prompt as for `q` is displayed; the workflow continues while the prompt is open

#### Scenario: Confirmation requested on Escape at top level mid-run
- **WHEN** the user presses Escape at the top level of the run view while the workflow is running
- **THEN** the same confirmation prompt as for `q` is displayed; the workflow continues while the prompt is open

#### Scenario: Escape inside a drilled-in view drills out, not quit
- **WHEN** the user presses Escape while drilled into a sub-workflow, loop, or iteration (not at the top level) and the workflow is running
- **THEN** the view drills out one level as specified by the `view-run` capability; no confirmation is displayed

#### Scenario: Confirmation accepted exits TUI and orphans subprocess
- **WHEN** the user confirms quit
- **THEN** the TUI exits; any currently-running subprocess (shell command or headless agent) is not killed and continues executing in the background

#### Scenario: Confirmation declined keeps running
- **WHEN** the user declines quit
- **THEN** the prompt is dismissed and the workflow continues uninterrupted

#### Scenario: Keystrokes during interactive step reach the agent
- **WHEN** the user presses `q` or `Ctrl+C` while the TUI is suspended for an interactive agent step
- **THEN** the keystroke is delivered to the agent process (not the TUI); no confirmation is displayed by agent-runner

#### Scenario: No confirmation after completion
- **WHEN** the workflow has finished and the user presses `q`, `Ctrl+C`, or Escape at the top level
- **THEN** the TUI exits immediately

### REMOVED Requirement: Step separator lines

**Reason**: The `live-run-view` TUI is now the sole live workflow display; separator lines on stdout are no longer printed. The TUI renders step boundaries through its step-list layout.

**Migration**: None for end users — the behavior is replaced by the TUI. Tooling that parsed these separators from stdout must switch to tailing `audit.log` events.

### REMOVED Requirement: Breadcrumb step headings

**Reason**: The `live-run-view` TUI renders the full nesting path as a breadcrumb line and shows step identity in the step list; breadcrumb headings on stdout are no longer printed.

**Migration**: None for end users. Tooling that parsed these headings from stdout must switch to tailing `audit.log` events (which carry nesting via the `prefix` field).

### REMOVED Requirement: Headless prompt display

**Reason**: The `live-run-view` TUI shows the resolved prompt inline in the detail pane of a headless agent step; it is no longer printed to stdout.

**Migration**: None for end users — the prompt is visible in the TUI.

### REMOVED Requirement: Headless spinner animation

**Reason**: The `live-run-view` TUI indicates in-progress headless agent steps via the step-list status indicator (blinking running glyph) and real-time output streaming in the detail pane; a stdout spinner is no longer drawn.

**Migration**: None for end users — progress is visible in the TUI.

## Done When

All scenarios above are covered by tests and passing. End-to-end manual check: `./dev.sh openspec:change <name>` (or any workflow with a shell, a headless agent, and an interactive agent step) launches the TUI immediately, streams shell/headless output live into the detail pane with ANSI stripped, hands off the terminal cleanly on the interactive step and re-enters afterward, stays open on completion, and requires y/n confirmation on `q`/`Ctrl+C`/Esc-at-top-level mid-run. Piping stdout prints a TTY error and exits non-zero. `--validate` still works without a TTY. `textfmt.Separator`, `textfmt.StepHeading`, the headless spinner, and the pre-step headless prompt echo are deleted along with their call sites. The existing `runner.RunWorkflow` signature is preserved for non-TUI callers and existing tests still pass.

**Scope note for the change as a whole:** This task does NOT complete the `live-run-view` change. Two requirements owned by this change — `Cursor auto-follows the active step` and `Detail-pane tail-follow` (both in `specs/live-run-view/spec.md`) — are intentionally deferred to the follow-up navigation task. This task also does NOT deliver the second-process lockout deltas on `view-run` and `list-runs`. Do not mark the change complete after this task lands; the change is complete only after the follow-up task finishes and `openspec validate --type change live-run-view` passes with all scenarios implemented.
