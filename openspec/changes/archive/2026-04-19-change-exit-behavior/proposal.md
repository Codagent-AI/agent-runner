## Why

When the user resumes an agent CLI session from the run view, today's implementation exec-replaces the agent-runner process with the CLI. Typing `/exit` or `/quit` inside the resumed CLI therefore drops the user out of agent-runner entirely, even though they had just been interacting with a run-view TUI. The natural mental model is "I peeked into that session" — returning to the run view on CLI exit preserves that affordance.

## What Changes

- Run view resume action SHALL spawn the agent CLI as a subprocess and re-enter the run view when the CLI exits, regardless of how the run view was reached (live-run completion, `--list`, or `--inspect`).
- On re-entry, audit/state files SHALL be re-read so any new events written by the resumed CLI session appear.
- Re-entry SHALL preserve the original entry path so back-navigation (esc to the run list from a `--list` entry, etc.) still works.

## Capabilities

### Modified Capabilities
- `view-run`: the resume action switches from exec-replace to spawn-and-reenter across all entry paths.

## Out of Scope

- Behavior of `/exit` or `/quit` inside an agent CLI spawned as part of a live workflow step (covered by the existing pseudo-terminal spec — unchanged).
- Resume of the overall agent-runner workflow run (`agent-runner --resume <run-id>`) — unchanged.
- Changes to which CLIs are allowlisted for resume.

## Impact

- `cmd/agent-runner/main.go`: both `runLiveTUI` and `runSwitcher` switch from `syscall.Exec` to a subprocess spawn that waits on CLI exit and then re-enters the TUI. The unused `execAgentResume` helper is removed; `spawnAgentResume` is the single resume mechanism.
- `internal/runview/`: `NewForReentry` accepts the original entry mode so the rebuilt run view preserves it; getters (`SessionDir`, `ProjectDir`, `Entered`) are added so the switcher can rebuild itself without threading extra state through tea messages.
- Terminal handoff: reuses the same release/reclaim pattern already used for interactive agent steps during live runs.
