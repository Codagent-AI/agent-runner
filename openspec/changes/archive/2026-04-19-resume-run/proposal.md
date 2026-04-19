## Why

The run view can exec `<agent-cli> --resume <session-id>` on Enter against an agent step — but there is no way to resume the agent-runner **workflow run itself** from inside the TUI. Today the user has to exit, find the run ID, and type `agent-runner --resume <run-id>` in the shell. A single keyboard shortcut on an inactive run closes that gap.

Two small keyboard ergonomics issues are fixed in the same pass: `PgUp`/`PgDown` are unused muscle-memory keys in a terminal where `j`/`k` are already the established scroll idiom, and `End`/`G` for tail-follow duplicate a single behavior across two keys where one lowercase letter suffices.

## What Changes

- Add `r` to the run view: when the run's status is `inactive` and the TUI is not currently running a workflow live, pressing `r` exits the TUI and execs `agent-runner --resume <run-id>` in-place. Available at any drill depth.
- When the gate is satisfied, the top-level breadcrumb SHALL render a `(r to resume)` affordance adjacent to the `inactive` status token, and the help bar SHALL list the `r` binding.
- **BREAKING**: Detail-pane scroll keys change from `PgUp`/`PgDown` to `j`/`k`. Step-list cursor movement is now arrows-only (`↑`/`↓`).
- **BREAKING**: Tail-follow re-engage key changes from `End`/`G` to `t`. Both `End` and uppercase `G` are removed.

## Capabilities

### Modified Capabilities

- `view-run`: adds the `r` resume-run action, the breadcrumb affordance, and the help-bar entry; replaces `PgUp`/`PgDown` detail-pane scrolling with `j`/`k`.
- `live-run-view` (in-flight): replaces the `End`/`G` tail-follow re-engage binding with `t`.

## Out of Scope

- Resuming `active` runs (already running — by definition, nothing to resume).
- Resuming `completed` or `failed` runs (not interrupted; `failed` semantics would need a separate decision).
- Offering `r` during the live-run-view's own execution window (`m.running == true`). The run view stays open after completion but `r` is only meaningful for interrupted (inactive) runs, not the one that just finished.
- Multi-key chords, vim-mode toggles, or broader remapping schemes.

## Impact

- `internal/runview/model.go`: `handleKey` gains a `case "r"` branch gated on `runStatus == inactive && !m.running`; emits a `ResumeRunMsg` (new message type) that main handles by exec'ing `agent-runner --resume <run-id>`. Existing `pgup`/`pgdown` cases deleted; `j`/`k` cases switched from cursor movement to `scrollDetail(±detailPageSize())` and no longer set `autoFollow = false`. `up`/`k` and `down`/`j` handlers split: cursor movement is now only triggered by `up`/`down`. `end`/`G` case deleted; `t` case added that sets `tailFollow = true` and snaps `detailOffset = math.MaxInt32`.
- `internal/runview/breadcrumb.go`: top-level status renderer appends ` (r to resume)` when status is `inactive` and `!m.running`.
- `internal/runview/view.go` (help bar): conditionally inserts `r resume` when the same gate holds; removes `PgUp/PgDn` and `End`/`G` entries; adds `j/k scroll` and `t tail`.
- `cmd/agent-runner/main.go`: `ResumeRunMsg` handler mirrors the existing agent-CLI `ResumeMsg` path — TUI exits cleanly, then `syscall.Exec` replaces the current process with `agent-runner --resume <run-id>`.
- `internal/liverun/coordinator.go` / runview FromLiveRun wiring: make `m.running` observable to the key handler (already is; just called out).
- Depends on `live-run-view` change's ordering: the `t` binding change is written as a MODIFIED delta against `live-run-view`'s in-flight spec. Implementer lands this alongside or after `live-run-view` archives.
- **BREAKING**: users with muscle memory for `PgUp`/`PgDown`/`End`/`G` will need to re-learn `j`/`k`/`t`. No compatibility shim.
