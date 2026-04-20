## Why

Shell steps today always run through the TUI-aware process runner, which does not wire stdin or allocate a PTY. Any shell command that reads from stdin (a `read` prompt, `gh auth login`, an `ssh` passphrase, a REPL, etc.) hangs. Workflow authors have no way to drop into an interactive shell command mid-run.

## What Changes

- Extend the existing `mode: interactive | headless` attribute to shell steps. Agent steps are unchanged.
- When a shell step has `mode: interactive`, the runner suspends the TUI, runs the command in a PTY with the user's terminal attached, and resumes the TUI on exit — reusing the suspend/resume pattern already used by interactive agent steps.
- Map the shell exit code to step outcome using the normal shell rules (0 → success, nonzero → failed). No continue-trigger detection, no sentinel scanning, no resume-on-exit behavior.
- Reject `mode: interactive` combined with `capture` at validation time (stdout belongs to the user's terminal).
- Headless shell steps behave exactly as they do today.

## Capabilities

### New Capabilities
- `interactive-shell-steps`: adds the `mode: interactive` attribute to shell steps with PTY-based execution and shell-native exit semantics.

### Modified Capabilities
- None. Agent-profile `mode` override and the agent-focused PTY requirements in `pseudo-terminal` are unaffected.

## Out of Scope

- Changes to agent interactive behavior (continue triggers, idle hint, sentinel, SIGTERM/SIGKILL grace period).
- Piping captured variables into or out of an interactive shell step.
- A separate `interactive: true` boolean flag — the existing `mode` field is reused.
- Background/detached interactive steps or concurrent interactive shells.

## Impact

- `internal/model/step.go` — relax `validateAgentOnlyField` so `mode` is allowed on shell steps; add validation for `capture` + `mode: interactive`.
- `internal/exec/shell.go` — branch on `step.Mode == ModeInteractive` and dispatch via a PTY entrypoint instead of `runner.RunShell`.
- `internal/pty` — add a thin shell entrypoint (`sh -c <command>`) that does not install continue-trigger detection used by agent interactive.
- Audit events for shell steps gain no new fields; exit code already appears in `EventStepEnd`.
- No changes to workflow YAML schema beyond widening where `mode` is allowed.
