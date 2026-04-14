## Why

There is no way to see what workflow runs exist, what state they're in, or whether one is currently executing — without manually inspecting `~/.agent-runner/projects/`. When `--resume` picks the most recent session blindly, users have no informed choice about which run to resume. A list view gives visibility into run state and turns resume into a deliberate action.

## What Changes

- New `--list` flag that launches a TUI showing all workflow runs for the current project directory, showing workflow name, current step, and status (active/inactive/completed).
- **BREAKING**: `--resume` without a session ID now launches the TUI instead of auto-resuming the most recent session. `--session` flag is removed; use `--resume <id>` instead.
- **BREAKING**: Bare `agent-runner` with no arguments launches the TUI instead of printing a usage error.
- PID lock file written on run start and cleaned up on run end, enabling reliable active-run detection. Stale locks (PID no longer alive) are treated as inactive.
- TUI provides scoped views: current directory, sibling git worktrees, and all known project directories (supersedes the originally considered `--dir` and `--worktree` CLI flags — navigation happens within the TUI instead).

## Capabilities

### New Capabilities

- `list-runs`: The list command — reads run state, determines run status, and renders a bubbletea/lipgloss TUI with three scoped tabs (current directory, worktrees, all directories). Covers `--list` flag, worktree detection, and TUI formatting.
- `run-lock`: PID lock file lifecycle — creation on run start, cleanup on completion, stale lock detection via PID-alive checks. Consumed by list-runs for active status, but managed independently as part of the run lifecycle.

### Modified Capabilities

- `resume-by-session-id`: The "Resume most recent session" scenario changes. `--resume` without a session ID no longer auto-resumes — it shows the run list instead. The `--session` flag is removed; resuming a specific session is now spelled `--resume <id>`.

## Out of Scope

- **Inspect UI**: Interactive drill-down into a specific run's details (planned as a follow-up change).
- **Run deletion/cleanup**: No ability to delete old runs from this command.
- **Remote/multi-machine state**: Only local filesystem state is considered.
- **Filtering or sorting options**: The initial list is a simple dump of all runs, most recent first.

## Impact

- **CLI entry point** (`cmd/agent-runner/main.go`): New `--list` flag. Changed `--resume` behavior. `--session` removed. No-args launches TUI.
- **New runlock package**: PID lock file read/write/check functions.
- **runner package**: Lock file acquisition on run start, release on run end (including crash paths).
- **New runs package**: Reads state files and lock files to assemble run info for the TUI.
- **New tui package**: bubbletea + lipgloss terminal UI.
- **Existing `--resume` users**: Breaking change — `--resume` without a session ID now launches the TUI. Use `--resume <id>` to resume a specific session.
