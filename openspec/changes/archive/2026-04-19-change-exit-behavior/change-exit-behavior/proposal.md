## Why

When the user resumes an agent CLI session from the run view after a live workflow run, today's implementation exec-replaces the agent-runner process with the CLI. Typing `/exit` or `/quit` inside the resumed CLI therefore drops the user out of agent-runner entirely, even though they had just been interacting with the live-run's run view. The natural mental model is "I peeked into that session" — returning to the run view on CLI exit preserves that affordance.

## What Changes

- Live-run run view: resume action SHALL spawn the agent CLI as a subprocess and re-enter the run view when the CLI exits.
- On re-entry, audit/state files SHALL be re-read so any new events written by the resumed CLI session appear.
- `--list` / `--inspect` run view: resume action keeps the existing exec-replace behavior (agent-runner was invoked as a one-shot inspector; there is nothing meaningful to return to).

## Capabilities

### Modified Capabilities
- `view-run`: split the resume action into two variants based on entry path — spawn-and-return for live-run, exec-replace for snapshot inspection.

## Out of Scope

- Behavior of `/exit` or `/quit` inside an agent CLI spawned as part of a live workflow step (covered by the existing pseudo-terminal spec — unchanged).
- Resume of the overall agent-runner workflow run (`agent-runner --resume <run-id>`) — unchanged.
- Changes to which CLIs are allowlisted for resume.

## Impact

- `cmd/agent-runner/main.go`: `runLiveTUI` switches from `execAgentResume` (syscall.Exec) to a subprocess spawn that waits on CLI exit and then re-enters the TUI. `runSwitcher` continues to call `execAgentResume` unchanged.
- `internal/runview/`: needs a reload entry point (re-read audit/state for the current run) invoked after subprocess exit.
- Terminal handoff: reuses the same release/reclaim pattern already used for interactive agent steps during live runs.
