## Why

Several small TUI rough edges in the run view have accumulated: agent blocks omit the `model:` line when a step doesn't override the profile default, the top-of-screen status label blinks unnecessarily, a streaming headless agent has no visible progress cue once output starts, the left screen margin is hardcoded across many files, and iteration rows sit too far to the right of their parent loop.

## What Changes

- Agent log blocks always show `model:` (with an `(unknown)` fallback), positioned directly under `cli:`. The current rendering places `model:` above `cli:` and omits the line entirely when the step doesn't set `model:` inline.
- `step_start.model` audit event data is populated with the *resolved* model (step override composed over profile default, and for `session: resume` / `session: inherit` steps, the profile of the session-originating step) instead of just the raw step-level override. This is what restores the `model:` line in the run view for steps that rely on profile defaults.
- The top-of-screen breadcrumb status label ("running" / "active") renders static. Only the step-list `●` indicator blinks.
- Headless-agent log blocks render a single-character braille spinner on a line below the streaming output while the step is in progress. The existing multi-line spinner (rendered when no output has arrived yet) is retained unchanged.
- The left screen margin is reduced from 2 to 1 character and centralized behind a single constant (new in `internal/tuistyle`). The outer `"  "` prefix currently hardcoded across `internal/runview/*.go` and `internal/listview/*.go` is replaced with a reference to this constant. Inner column-separator `"  "` between row cells is unchanged.
- Iteration expansion rows in the step list sit one visible step to the right of the parent loop row (today they carry an additional two-character indent past that position).

## Capabilities

### Modified Capabilities
- `view-run`: agent detail block always shows model; ordering changes so model renders directly under cli.
- `audit-log-entries`: `step_start.model` captures resolved model.

### New Capabilities
- None.

## Out of Scope

- Capturing which model the CLI *actually* selected at runtime (e.g. Claude's plan-mode model pick). We only report the model agent-runner launched the CLI with.
- Reworking inner column-separator `"  "` spacing inside rows. Only the outer screen margin is centralized and reduced.
- Changes to list-view / breadcrumb behavior beyond the status-label blink removal.

## Impact

- **Code**:
  - `internal/exec/agent.go` — emit `resolved.Model` in `step_start`.
  - `internal/runview/detail.go` — reorder agent header (profile → cli → model → session), always render `model:` with `(unknown)` fallback, add single-char braille spinner when agent output is non-empty and the step is in progress.
  - `internal/runview/breadcrumb.go` — remove blink from the `m.running` / `m.active` status-label branches.
  - `internal/runview/view.go` — reference the new `tuistyle` margin constant; reduce the extra depth-multiplier in `renderExpansionRow`.
  - `internal/listview/view.go` — reference the new `tuistyle` margin constant.
  - `internal/tuistyle/` — new `ScreenMargin` (or equivalent) constant; braille spinner frame helper.
- **Audit log consumers**: `step_start.model` values become accurate for steps that relied on profile defaults (previously `""`). No schema change.
- **Docs**: none.
