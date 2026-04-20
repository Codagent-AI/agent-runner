## Context

Today `handleRun` and `handleResume` in `cmd/agent-runner/main.go` execute workflows synchronously on the main goroutine. A `realLogger` prints separators (`━━━`), breadcrumb step headings (`━━ step N/M: path > step [type] ━━`), a headless spinner, and the pre-step headless prompt echo. A `realProcessRunner` wires subprocess stdout/stderr to `os.Stdout`/`os.Stderr` for non-captured steps. This stream is the temporary placeholder this change removes.

The `runview` TUI (`internal/runview`) already renders runs from `state.json` + `audit.log` + `run.lock`, supports live refresh (`runlock.Check` + audit tailing every 2s), and has `FromList` / `FromInspect` entry modes. This change adds a third mode — `FromLiveRun` — and rewires the workflow-execution paths so the TUI is the foreground console while the workflow runs.

## Goals / Non-Goals

**Goals:**
- `runview` is the sole live console during workflow execution; the `━━` breadcrumb + separator stream is deleted.
- Shell and headless-agent output renders in the TUI detail pane in real time.
- Interactive agent steps hand the terminal off cleanly and re-enter without flash.
- A TTY is required for any path that launches the TUI; non-TTY invocations of non-TUI flags (`--validate`, `--version`) still work.
- A run locked by another live process cannot be opened from a different process.

**Non-Goals:**
- Subprocess cancellation. Confirmed quit orphans whatever was running; this is documented in the confirmation modal.
- Structured plain-text fallback for CI / piped invocations — explicitly not supported.
- Multiple simultaneous viewers of the same live run.
- Changes to the audit log schema beyond widening which `step_end` events carry a `stdout` preview.

## Approach

### Concurrency model

TUI owns the main goroutine (bubbletea's requirement). The workflow runner runs in a background goroutine. Coordination happens through a shared `*tea.Program` handle.

```
 main goroutine                              runner goroutine
 ──────────────                              ────────────────
 requireTTY()
 handle := runner.PrepareRun(...)
 p := tea.NewProgram(runview.New(
        handle.SessionDir,
        handle.ProjectDir,
        runview.FromLiveRun))
 coord := liverun.NewCoordinator(p, handle.SessionDir)

 go func() {
   defer recover()→NotifyDone(Failed,panic)
   result := runner.ExecuteFromHandle(handle,
     opts + SuspendHook/ResumeHook +
     TUIProcessRunner(coord) + discardLogger)
   coord.NotifyDone(result, nil)
 }()                                         ───► chunkWriter ──► p.Send(OutputChunkMsg)
                                              │   + <sessionDir>/output/<prefix>.out
                                              │
 p.Run()   ◄───────────────────────────────────┤   p.Send(StepStateMsg) on step boundaries
   ◄── ReleaseTerminal / pty.RunInteractive /  │   around interactive agent steps
       RestoreTerminal ─────────────────────────┤
                                              │
   ◄── p.Send(ExecDoneMsg{result,err}) ───────◄┘  (always — defer/recover guarantees it)
 p.Run() returns when the user quits
```

`p.ReleaseTerminal` and `p.RestoreTerminal` are safe to call from any goroutine (bubbletea handles its own locking). `p.Send` is a channel send behind a lock; the `chunkWriter` coalesces into ~4 KB batches with a 50 ms idle flush to avoid high-rate traffic.

### New package: `internal/liverun`

```
internal/liverun/
  coordinator.go   — Coordinator{program, sessionDir}; OnOutput, BeforeInteractive,
                     AfterInteractive, NotifyStepChange, NotifyDone.
  messages.go      — OutputChunkMsg, StepStateMsg, SuspendedMsg, ResumedMsg, ExecDoneMsg.
  process_runner.go— TUIProcessRunner; wraps realProcessRunner, replaces os.Stdout/Stderr
                     with chunkWriters, mirrors to output files.
  chunk_writer.go  — io.Writer that batches by size/time; sends via Coordinator.
  ansi.go          — stripping state machine (CSI/OSC/SGR consumed; text/newlines passed).
```

### Runner refactor

`runner.RunWorkflow` is split so main can get the session directory *before* starting the TUI:

```go
// PrepareRun does today's initRunState: creates session dir, writes lock file,
// opens audit logger, emits run_start. Returns a handle main uses to size the TUI.
func PrepareRun(wf *model.Workflow, params map[string]string, opts *Options) (*RunHandle, error)

// ExecuteFromHandle runs executeSteps + finalizeRun on an already-prepared handle.
// Safe to call from a background goroutine.
func ExecuteFromHandle(h *RunHandle, opts *Options) WorkflowResult

// Existing RunWorkflow becomes a thin wrapper for tests / non-TUI callers:
func RunWorkflow(wf *model.Workflow, params map[string]string, opts *Options) (WorkflowResult, error) {
    h, err := PrepareRun(wf, params, opts)
    if err != nil { return ResultFailed, err }
    return ExecuteFromHandle(h, opts), nil
}
```

Same split for `ResumeWorkflow`: `PrepareResume(stateFilePath, opts) (*RunHandle, error)` + `ExecuteFromHandle`.

`runner.Options` gains two optional hooks:

```go
type Options struct {
    // ... existing fields ...
    SuspendHook func() // called before pty.RunInteractive; nil = no-op
    ResumeHook  func() // called after  pty.RunInteractive; nil = no-op
}
```

These are threaded into `model.ExecutionContext` and invoked in `exec/agent.go` around the `interactiveRunnerFn(...)` call. Existing test paths pass nil and behave identically.

### Output streaming

`TUIProcessRunner` intercepts both shell and headless-agent subprocess output. For non-captured steps its `Stdout`/`Stderr` fields become a composite writer:

```
subprocess stdout ──► composite writer ──┬── ANSI-strip → chunkWriter → p.Send(OutputChunkMsg{stepPrefix, stream, bytes})
                                         ├── output/<prefix>.out (raw, no strip)
                                         └── bytes.Buffer → ProcessResult.Stdout (unchanged contract)
```

- **Channel delivery:** the strip-then-chunk path gives the TUI clean text it can safely render. Coalescing at 4 KB or 50 ms trades worst-case latency for message-channel health.
- **Output files:** raw bytes (including ANSI) go to `<sessionDir>/output/<step-prefix>.out` / `.err`. The prefix is the audit log's event `prefix` string, filesystem-sanitized by replacing `/` with `--` (nesting), `:` with `_` (iteration), and any other character outside `[A-Za-z0-9._\-]` with a single `_` (e.g., audit prefix `loop-b:2/step-c` → filename stem `loop-b_2--step-c`). Sanitization is total and deterministic; no path traversal is possible via crafted step IDs.
- **ProcessResult contract (Go-level, unchanged):** the composite writer's buffered content still populates `ProcessResult.Stdout` / `Stderr` — existing callers of `ProcessRunner.RunShell` / `RunAgent` see the same return values.
- **Audit event payload (widened):** `internal/exec/shell.go` previously only included `stdout` in `step_end` when the step used `capture:`. This change widens it to always include the 4 KB `truncateForAudit`-capped preview. The audit log gets a greppable summary; the output file holds the full content.

For interactive steps, `TUIProcessRunner` is bypassed (they go through `pty.RunInteractive`). The `SuspendHook`/`ResumeHook` pair covers them. Interactive steps do NOT write `output/<step-prefix>.out` files — the agent's own PTY output goes to the terminal directly and is not captured or streamed by agent-runner. The detail pane for an interactive step shows the profile/CLI/session metadata only (consistent with the `view-run` detail-pane spec for interactive steps).

### runview — live mode

New `Entered` value `FromLiveRun`. Model fields added:

```go
type Model struct {
    // ... existing fields ...
    running        bool   // true until ExecDoneMsg arrives
    autoFollow     bool   // step-list cursor tracks the active step
    tailFollow     bool   // detail-pane viewport pinned to tail
    activeStep     *StepNode
    quitConfirming bool
    result         WorkflowResult // set on ExecDoneMsg
}
```

New message handlers:

| Message | Effect |
|---|---|
| `OutputChunkMsg` | Locate target step by prefix; append bytes to its buffer (tail-truncate at 2000 lines / 256 KB); if selected and `tailFollow`, scroll to bottom; re-render. |
| `StepStateMsg` | Update `activeStep`; if `autoFollow`, rewrite cursor + drill path to reach it (drilling into sub-workflows / iterations as needed). |
| `SuspendedMsg` / `ResumedMsg` | Bookkeeping only — no additional overlay rendered. The TUI is fully released during suspension, so the user sees the agent's own output. |
| `ExecDoneMsg` | Flip `running=false`; if `Failed`, drill the cursor to the failed step; breadcrumb status re-renders (`completed` / `failed`). Model stays in `FromLiveRun` mode but behaves identically to `FromInspect` from this point forward — resume action, drill-in, output file reads, all available. |

Auto-follow rules:
- `autoFollow` starts `true`, `tailFollow` starts `true`.
- Manual navigation (`↑`/`↓`/`k`/`j`/Enter/Esc) sets `autoFollow=false`. PgUp / mouse-wheel-up inside the detail pane sets `tailFollow=false`.
- `l` → jump cursor to `activeStep` (drilling in/out as needed) and set `autoFollow=true`.
- `End` or `G` → scroll detail pane to tail and set `tailFollow=true`.

Quit confirmation (only while `running`):

```
┌─────────────────────────────────────────────────────────────┐
│ The workflow is still running. Quitting will orphan the     │
│ active step — it will continue running in the background.   │
│                                                             │
│ Quit anyway?  [y]es  [n]o                                   │
└─────────────────────────────────────────────────────────────┘
```

`q`, `Ctrl+C`, and Escape-at-top-level all open this modal while `running`. On `y`: `tea.Quit`. On `n` or Esc: dismiss the modal. (Esc inside a drilled-in view is the normal drill-out key — the confirmation only fires at top level.) After `ExecDoneMsg`, `q` / `Ctrl+C` / Esc-at-top-level exit immediately without confirmation, consistent with the `view-run` capability's exit behavior.

**Quit confirmation during an interactive step.** While the TUI is suspended for an interactive agent step, the terminal is owned by the agent — keystrokes go to the agent, not the TUI — so the quit confirmation cannot fire during that window. The user's escape hatches during an interactive step are the agent's own quit/Ctrl+C (which terminates the agent and returns control to the TUI via `ResumeHook`, after which the confirmation is again reachable). This is a direct consequence of the `ReleaseTerminal` handoff and is not a separate requirement.

### listview / runlock — second-process lockout

New helper in `internal/runlock`:

```go
// CheckOwnedByOther returns true iff the lock is Active AND the lock's PID
// differs from selfPID. Stale and absent locks return false.
func CheckOwnedByOther(sessionDir string, selfPID int) bool
```

Callers:
- `main.handleInspect`: before `runview.New`, check the lock; if another process owns it, print stderr error naming the run and exit non-zero; no TUI launched.
- `listview.Update`: Enter on a selected run → `CheckOwnedByOther(sessionDir, os.Getpid())`; if true, set an inline error on the model (bottom of the list, auto-cleared on the next keystroke); do not emit `ViewRunMsg`.

Stale locks (PID dead) and same-process locks are treated identically to no-lock (handled by existing paths).

### TTY check

```go
func requireTTY() error {
    if !isatty.IsTerminal(os.Stdout.Fd()) {
        return errors.New("agent-runner: an interactive terminal is required; stdout is not a TTY")
    }
    return nil
}
```

Called at the top of `handleRun`, `handleResume`, `handleList`, `handleInspect`. Not called for `--validate`, `--version`, or `-v`.

### Code removals

- `textfmt.Separator`, `textfmt.StepHeading` — deleted; no replacement.
- Headless spinner + pre-step prompt echo in `exec/agent.go` — deleted.
- `runner.executeSteps`'s `rs.log.Println(Separator...)` / `StepHeading(...)` calls — deleted. Remaining `rs.log.Printf` calls (step-failed / continue-on-failure messages, workflow-complete banner, resume hint) are silenced by injecting a `discardLogger` in TUI mode. Their information is surfaced in the TUI (breadcrumb status, detail pane error, resume action).

## Decisions

- **Single process, goroutine-backed concurrency** over a subprocess runner. Keeps session + lock + audit lifecycle in one place; makes `p.ReleaseTerminal` handoff for interactive steps a one-line primitive.
- **In-process channel for live streaming + per-step output files for persistence** (both, not either/or). Channel gives immediate feedback; files give durable post-run inspection without inflating `audit.log` beyond its 4 KB preview.
- **`p.ReleaseTerminal` / `p.RestoreTerminal`** over quit-and-relaunch. No flash, TUI state preserved. Alt-screen stacking is an unknown; treated as a spike risk (fallback: quit-and-relaunch).
- **Strip ANSI on input** over render-as-color. Eliminates the risk of child output corrupting the TUI. Color loss is acceptable for v1.
- **Orphan on confirmed quit** over subprocess kill. No context threading needed; confirmation modal explicitly tells the user the consequence.
- **Jump-to-live is `l`**, detail-pane tail-jump is `End` / `G`. Two separate follow modes, two separate keys.
- **TTY requirement scoped to TUI-launching entry points.** `--validate`, `--version`, `-v`, `-C <dir>` alone remain usable without a TTY.
- **Split `RunWorkflow` into `PrepareRun` + `ExecuteFromHandle`**, preserving the existing `RunWorkflow` as a wrapper. Lets main size the TUI from the real session dir before execution starts; keeps existing tests unchanged.
- **Hooks on `runner.Options`** rather than extending `exec.Logger` for interactive suspend/resume. Logger is about text output; terminal handoff is a distinct concern.
- **Failed-step auto-jump on `ExecDoneMsg{Failed}`**. Where the user needs to look; overrides the current drilled-in position if any.

## Risks / Trade-offs

- **Alt-screen interaction on `ReleaseTerminal` → claude-code's own alt-screen → `RestoreTerminal`** → Nested alt-screen sequences can misbehave on some terminals (cursor position lost, screen buffer garbled). Mitigation: run a focused spike in step 1 of implementation before anything else ships; fallback is quit-and-relaunch for interactive steps.
- **`p.Send` back-pressure from a chatty step** → `chunkWriter` caps to ~4 KB or 50 ms whichever first; receiver tail-truncates per step buffer at 2000 lines / 256 KB; overflow shows a banner indicating bytes were dropped (reuses view-run's `g` load-full pattern for the output file on post-run).
- **Confirmed quit orphans the subprocess** → Documented to the user in the confirmation modal. No zombie reaping concerns — init reaps. For interactive agents, orphaning is benign (they have their own PTY and will SIGHUP on terminal loss or keep running if detached).
- **Non-captured shell stdout in `audit.log` previews is capped at 4 KB** → Full output lives in `output/*.out`; view-run reads the file for any length. Users who `grep audit.log` see only the preview.
- **Panic in runner goroutine** → `defer recover() → NotifyDone(Failed, panic)` is mandatory, not optional; the TUI must never hang in live state because of a runner panic.
- **Tailing output file vs. channel on post-run inspection** → Minor ambiguity: live run uses channel (in-memory buffer); post-run `--inspect` reads the file. The two paths converge on the same bytes because the file was written from the same subprocess stream the channel consumed. No divergence possible because we tee from the same writer.

## Migration Plan

**Archive ordering.** The `view-run` and `list-runs` spec deltas in this change modify requirements introduced by the in-flight `view-run` change. The `view-run` change MUST be archived (into `openspec/specs/`) before this one so the deltas apply against a real baseline. The `live-run-view` new capability and the `console-output-formatting` / `headless-progress-indication` removals are independent of that ordering.

1. **Alt-screen spike.** Implement a minimal `ReleaseTerminal` → `pty.RunInteractive` → `RestoreTerminal` prototype in isolation and verify with claude-code. If broken, commit to quit-and-relaunch and revise this design.
2. Add `runlock.CheckOwnedByOther`.
3. Add `requireTTY` helper; call in the four TUI-launching entry points.
4. Scaffold `internal/liverun`: `Coordinator`, `chunkWriter`, ANSI stripper, messages.
5. Split `runner.RunWorkflow` → `PrepareRun` + `ExecuteFromHandle`; same for `ResumeWorkflow`.
6. Add `SuspendHook`/`ResumeHook` to `runner.Options`; thread via `ExecutionContext` into `exec/agent.go`.
7. Implement `TUIProcessRunner` (channel + files + buffer tee). Widen shell `step_end` stdout to always include the 4 KB preview.
8. Extend `runview`: `FromLiveRun`, new message handlers, `autoFollow`, `tailFollow`, `l` / `End` / `G` keys, quit-confirm modal, failed-step jump on `ExecDoneMsg`.
9. `listview` Enter → `CheckOwnedByOther` guard + inline error.
10. Rewire `handleRun` / `handleResume` / `handleInspect` / `handleList` in `main.go` to the new flow.
11. Delete `textfmt.Separator`, `textfmt.StepHeading`, headless spinner, pre-step prompt echo, and their call sites.

No disk-format migrations. No audit schema break (additive). Rollback is a package revert.

## Resolved Design Decisions

- **Output-file prefix format.** Mirror the audit log's event `prefix` field, sanitized for the filesystem by replacing `/` with `--` (nesting) and `:` with `_` (iteration). Underscores in step IDs pass through unchanged so they do not collide with the nesting separator. Example: audit prefix `loop-b:2/step-c` → `output/loop-b_2--step-c.out`. Deterministic, one-to-one, reversible enough for debugging. Documented in the `TUIProcessRunner` section above.
- **Auto-follow on instantaneous multi-step completions.** When several `StepStateMsg` events arrive in the same render tick (batched skipped steps, rapid-fire short shells), auto-follow lands the cursor on the last-emitted active step; intermediate positions are not rendered. Acceptable because the user sees the tree update holistically on the next render.
- **"Bytes dropped" banner persistence.** When a step's in-memory output buffer tail-truncates due to the 2000-line / 256 KB cap, the banner is persistent (not dismissible) for that step. The post-run `output/*.out` file holds the full content; the TUI's `g`-load-full path reads directly from the file when available. The banner escape-hatches users to the file.

## Open Questions

None outstanding.
