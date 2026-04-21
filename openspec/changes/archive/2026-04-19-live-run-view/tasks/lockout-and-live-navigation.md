# Task: Lockout guards and live-run navigation

## Goal

Add two independent layers on top of an already-working live TUI:

1. **Second-process lockout** — prevent two agent-runner processes from ever opening the same run simultaneously. `--inspect` and list-TUI Enter on a run whose lock belongs to another live process are rejected.
2. **Live-run navigation polish** — cursor auto-follows the active step (drilling into and out of sub-workflows and iterations as execution proceeds), manual navigation pauses the follow, `l` jumps back and re-engages it; detail-pane viewport tails streaming output, scrolling up pauses tailing, `End`/`G` jumps to the tail; on run failure the cursor auto-jumps to the failed step.

These two layers have no code overlap but are both small additive follow-ups to the live-run-view core.

## Background

You MUST read these files before starting:
- `openspec/changes/live-run-view/proposal.md` for the "why"
- `openspec/changes/live-run-view/design.md` for the full design
- `openspec/changes/live-run-view/specs/live-run-view/spec.md` — requirements "Cursor auto-follows the active step", "Detail-pane tail-follow"
- `openspec/changes/live-run-view/specs/view-run/spec.md` — the `Run-view entry points` MODIFIED delta (adds `--inspect` lockout)
- `openspec/changes/live-run-view/specs/list-runs/spec.md` — the `Open run from TUI` MODIFIED delta (adds list Enter lockout)
- `openspec/changes/view-run/specs/view-run/spec.md` for the baseline runview behavior you're extending
- `openspec/changes/view-run/design.md` for the existing runview tree / drill-in / selection model

This task ASSUMES the live-run-view core is already in place: `internal/liverun` exists, `runview` has a `FromLiveRun` entered mode, `StepStateMsg` / `ExecDoneMsg` / `OutputChunkMsg` flow through the TUI, and the runview's Model has `running`/`activeStep` fields. Do NOT re-implement that plumbing.

### Lockout — what to build

New helper in `internal/runlock/runlock.go`:

```go
// CheckOwnedByOther returns true iff the lock is Active AND the lock file's
// PID differs from selfPID. Stale (dead PID) and absent locks return false.
func CheckOwnedByOther(sessionDir string, selfPID int) bool
```

Implementation reuses the existing liveness logic that `Check(sessionDir string) LockStatus` in `internal/runlock/runlock.go` already encapsulates (reads `<sessionDir>/lock`, parses the PID, does a `syscall.Kill(pid, 0)` liveness probe). Factor out the "read + parse + liveness-check + return (status, pid)" part if helpful, or read the file once inside `CheckOwnedByOther` and compare `pid != selfPID`. Everything that isn't "active AND other process" → false (including no lock file, unreadable, non-numeric PID, dead PID, same PID).

Two call sites:

- `cmd/agent-runner/main.go:handleInspect(runID string)` — after `resolveInspectSession` resolves the session dir, before `runview.New`, call `runlock.CheckOwnedByOther(sessionDir, os.Getpid())`. If true, print to stderr `agent-runner: run %q is active in another process\n` and return 1. No TUI launches.
- `internal/listview` — in the model's Update handler for Enter, before emitting `ViewRunMsg`, call `CheckOwnedByOther(sessionDir, os.Getpid())`. If true, set an inline error on the list Model (new field, e.g., `errMsg string`) with copy like `"run is active in another process"`. View() renders the error on a line at the bottom of the list (adjacent to the existing help bar style). The error auto-clears on the next keypress so the user can keep navigating.

### Navigation — what to build

Extend `internal/runview/model.go` with new Model fields:

```go
type Model struct {
    // ... existing ...
    autoFollow bool // cursor tracks activeStep; true by default in FromLiveRun
    tailFollow bool // detail-pane viewport pinned to tail; true by default
}
```

Initialize both `true` when `entered == FromLiveRun`; leave both `false` for `FromList` / `FromInspect`.

**Cursor auto-follow behavior.** The existing `StepStateMsg` handler already updates `activeStep`. Extend it: if `autoFollow`, navigate the view to the active step, drilling into sub-workflows / iterations as needed, or drilling out when execution returns to a higher level. Use the existing tree/path/cursor manipulation primitives in `runview` (breadcrumb.go, model.go). Auto-drill must use the same auto-flatten rules already in the tree (see `view-run/design.md` — iteration-with-single-subworkflow-child drills past the degenerate level).

Any user-initiated navigation key (`↑`/`↓`/`k`/`j` cursor; Enter drill-in; Esc drill-out) sets `autoFollow = false`. Add a new key `l` (lowercase, for "live"): when pressed, navigate the view to the current `activeStep` (same drilling logic as above) and set `autoFollow = true`.

Failure jump: when `ExecDoneMsg{Result: Failed}` arrives, the cursor SHALL drill to the failed step (including into sub-workflows / iterations) and show its detail pane, regardless of `autoFollow`'s state. You can determine the failed step by scanning the tree for the step whose `Status == StatusFailed` (there will be exactly one, since execution halts on failure). This is a one-shot override; after it fires, `autoFollow` can be left as-is (the user's next keypress decides).

**Detail-pane tail-follow behavior.** The existing detail pane uses a `detailOffset` scroll offset (see `runview/model.go`). When `tailFollow` is true and `OutputChunkMsg` arrives for the selected step, keep the viewport pinned at the bottom (set `detailOffset` to scroll-to-end). PgUp, mouse-wheel-up (and any existing scroll-up keybinding) set `tailFollow = false`. New keys: `End` and `G` (uppercase) jump the viewport to the tail and set `tailFollow = true`.

### Help bar

The runview's help bar (bottom line) adjusts based on state. While `running && !autoFollow`, show `l` in the hint list ("l live"). While `!tailFollow`, show `End` in the hint list ("End tail"). Keep the existing hints otherwise. The legend overlay (`?`) gains two rows: `l` — jump to active step, `End`/`G` — jump to output tail.

### Files you'll touch

- `internal/runlock/runlock.go` — add `CheckOwnedByOther`
- `internal/runlock/runlock_test.go` — test new helper
- `cmd/agent-runner/main.go` — `handleInspect` guard
- `internal/listview/model.go` + `update.go` + `view.go` — Enter guard, inline error field, render
- `internal/runview/model.go` — `autoFollow`, `tailFollow` fields + initialization
- `internal/runview/update.go` — extended `StepStateMsg` handler; `OutputChunkMsg` handler tail-pin; `ExecDoneMsg` failed-step jump; new `l` / `End` / `G` key handlers; navigation keys clear follow flags
- `internal/runview/view.go` — help bar adjustments, legend overlay updates
- `internal/runview/breadcrumb.go` or similar — a helper to "navigate to a given StepNode" (drill in/out to reach it) if one doesn't already exist; reuse auto-flatten rules

### Constraints

- `CheckOwnedByOther` MUST treat stale locks, absent locks, and same-process locks identically to "not locked by other" — callers MUST NOT be surprised by edge cases.
- The inline error on list TUI MUST NOT exit the TUI; it MUST auto-clear on the next keystroke so the user can retry or navigate away.
- Auto-follow navigation MUST reuse existing drill-in/out primitives; do not duplicate path-manipulation logic.
- Manual navigation detection: set `autoFollow = false` on any ↑/↓/k/j/Enter/Esc, regardless of whether the navigation actually changed position. Do NOT treat `l`, `End`, `G`, `?`, PgDn as manual navigation for auto-follow purposes. PgUp / mouse-wheel-up DO clear `tailFollow` but NOT `autoFollow`.
- Failed-step jump is a one-shot on `ExecDoneMsg{Failed}` — don't re-trigger on subsequent re-renders.
- `End` / `G` work only when the detail pane has a scrollable region; they're no-ops when output fits in the viewport. This is consistent with existing PgUp/PgDn behavior.
- The `l` key must not collide with any existing view-run keybinding. (Verified against the view-run spec: it doesn't.)

## Spec

### Requirement: Run-view entry points
The CLI SHALL provide two entry points to the run view: a `--inspect <run-id>` flag for direct entry, and an Enter action from the list TUI (covered by the `list-runs` delta). Direct entry SHALL require a full run ID (no prefix matching). When the target run's run-lock is held by another live process, `--inspect` SHALL reject the entry with an error and not launch the TUI.

#### Scenario: --inspect launches run view
- **WHEN** `agent-runner --inspect <run-id>` is invoked and the run exists
- **THEN** the run-view TUI launches for that run

#### Scenario: --inspect with unknown run ID
- **WHEN** `agent-runner --inspect <run-id>` is invoked and the run does not exist
- **THEN** agent-runner prints an error message naming the missing run ID and exits with a non-zero status

#### Scenario: --inspect requires full run ID
- **WHEN** `agent-runner --inspect <prefix>` is invoked with a prefix that is not a complete run ID
- **THEN** agent-runner treats it as "not found" and exits non-zero

#### Scenario: --inspect is mutually exclusive with --list and --resume
- **WHEN** `agent-runner --inspect <run-id>` is invoked together with `--list` or `--resume`
- **THEN** agent-runner prints an error indicating the flags are mutually exclusive and exits non-zero

#### Scenario: --inspect rejects a run locked by another process
- **WHEN** `agent-runner --inspect <run-id>` is invoked and the target run's run-lock belongs to another live process
- **THEN** agent-runner prints an error to stderr identifying the run as active in another process and exits non-zero; no TUI is launched

#### Scenario: --inspect proceeds past a stale lock
- **WHEN** `agent-runner --inspect <run-id>` is invoked and the target run's run-lock PID is dead
- **THEN** the lock is treated as stale and the run-view TUI launches normally

### Requirement: Open run from TUI
Pressing Enter on a run in the TUI SHALL navigate from the list view to the run view for that run. The list view's state (cursor, tab, scroll offsets) SHALL be preserved so that returning from the run view restores it. Runs of any status (active, inactive, completed) SHALL be selectable. Resume is no longer triggered directly from the list — it becomes an action inside the run view (see `view-run` spec).

When the target run's run-lock is held by another live process, the list TUI SHALL reject the Enter action with an inline error and SHALL NOT navigate away from the list.

#### Scenario: Enter on inactive run opens run view
- **WHEN** the user presses Enter on an inactive run
- **THEN** the view switches from the list to the run view for that run

#### Scenario: Enter on active run opens run view
- **WHEN** the user presses Enter on an active run whose run-lock belongs to the current process
- **THEN** the view switches from the list to the run view for that run, with live refresh enabled

#### Scenario: Enter on completed run opens run view
- **WHEN** the user presses Enter on a completed run
- **THEN** the view switches from the list to the run view for that run in read-only mode

#### Scenario: Enter on run locked by another process is rejected
- **WHEN** the user presses Enter on a run whose run-lock belongs to another live process
- **THEN** the list TUI displays an inline error message identifying the run as active in another process; the list remains on screen and navigable

#### Scenario: Enter proceeds past a stale lock
- **WHEN** the user presses Enter on a run whose run-lock PID is dead
- **THEN** the lock is treated as stale and the run view opens normally

### Requirement: Cursor auto-follows the active step

While the workflow is running and the user has not manually navigated away, the step-list cursor and drill depth SHALL auto-follow the currently active step — moving to peer steps at the same level, drilling into newly entered sub-workflows and loop iterations, and drilling out when execution leaves them.

Manual cursor movement, drill-in, or drill-out SHALL pause auto-follow. A dedicated keyboard action SHALL jump the cursor to the active step and re-engage auto-follow.

#### Scenario: Active step advances to peer
- **WHEN** the cursor is auto-following and execution moves to the next peer step
- **THEN** the cursor moves to the new active step and its detail pane is shown

#### Scenario: Active step enters a sub-workflow
- **WHEN** the cursor is auto-following and execution enters a sub-workflow
- **THEN** the view drills into the sub-workflow and the cursor lands on the new active child step

#### Scenario: Active step enters a loop iteration
- **WHEN** the cursor is auto-following and execution enters a new loop iteration
- **THEN** the view drills into the iteration and the cursor lands on the new active child step

#### Scenario: Active step leaves a sub-workflow
- **WHEN** the cursor is auto-following and execution returns from a sub-workflow or iteration to a higher level
- **THEN** the view drills out to the level of the new active step

#### Scenario: Manual navigation pauses auto-follow
- **WHEN** the user moves the cursor, drills in, or drills out manually
- **THEN** auto-follow is paused and the cursor stays where the user placed it regardless of execution progress

#### Scenario: Jump-to-live re-engages auto-follow
- **WHEN** the user presses the jump-to-live key (`l`) with auto-follow paused
- **THEN** the view navigates to the currently active step (drilling in/out as needed) and auto-follow resumes

#### Scenario: Failure jumps cursor to the failed step
- **WHEN** the workflow reaches a terminal failed state
- **THEN** the cursor drills to the failed step (including into sub-workflows or loop iterations as needed) and its detail pane is shown, regardless of where auto-follow last placed the cursor

### Requirement: Detail-pane tail-follow

While output is streaming into the selected step's detail pane, the viewport SHALL remain pinned at the tail (newest content visible) unless the user has manually scrolled up. Scrolling up (via PgUp or mouse wheel) SHALL pause tail-follow. Pressing `End` or `G` SHALL jump the viewport to the tail and re-engage tail-follow.

#### Scenario: Streaming output auto-tails
- **WHEN** new bytes arrive for the currently selected step and tail-follow is engaged
- **THEN** the detail pane viewport stays at the bottom, showing the newest content

#### Scenario: User scroll pauses tail-follow
- **WHEN** the user scrolls the detail pane up (PgUp or mouse-wheel-up)
- **THEN** tail-follow is paused; subsequent output does not move the viewport

#### Scenario: End / G re-engages tail-follow
- **WHEN** the user presses `End` or `G` with tail-follow paused
- **THEN** the viewport jumps to the bottom of the output and tail-follow resumes

## Done When

All scenarios above are covered by tests and passing.

End-to-end manual check (lockout): Start a long-running workflow in terminal A. In terminal B, `agent-runner --inspect <runID>` prints an "active in another process" error and exits non-zero. In terminal B, `agent-runner --list` + Enter on that same run shows the inline error in the list TUI without leaving the list.

End-to-end manual check (navigation): Start a workflow with a sub-workflow and a loop. The cursor follows execution through the tree — drilling in as it enters the sub-workflow and the loop, drilling out as it leaves. Press ↑; cursor stops following. Press `l`; cursor jumps back to the active step and resumes following. In a step with lots of output, press PgUp; the viewport stays put as more output arrives. Press `End`; viewport snaps to the tail and resumes auto-scrolling. Kill a step (e.g., by making a shell command fail); the cursor auto-drills to the failed step when the run terminates.
