## Why

Workflow execution artifacts—state and audit logs—are currently scattered: state files live in openspec change directories (or whatever the caller specifies), while audit logs live in `~/.agent-runner/`. This split makes it harder to manage, inspect, and reason about a single workflow execution. Bringing state and logs under a unified session directory simplifies lifecycle management, cleanup, and future observability tooling.

The resume experience also needs improvement. Currently, resuming requires passing an explicit state file path; this ties resume to filesystem knowledge and doesn't scale well when workflows have multiple interrupted runs.

## What Changes

- **Relocate state file**: `agent-runner-state.json` moves from the working directory (or engine-specified path) to `~/.agent-runner/projects/{encoded-cwd}/runs/{session-id}/` alongside audit logs
- **Unified session directory**: Each workflow execution gets a single session directory containing both `audit.log` and `state.json`
- **Flatten CLI**: Remove `run`, `resume`, and `validate` subcommands; the CLI becomes `agent-runner [flags] <workflow.yaml> [params...]` with `--resume` (boolean), `--session <id>` (optional), and `--validate` flags; drop Cobra in favor of Go's `flag` stdlib
- **Add resume by session ID**: `--resume` resumes the most recent session for the current project directory; `--resume --session <id>` resumes a specific session. Resume mode does not require a workflow file argument.
- **Remove explicit state file resumption**: The `resume` command (which takes a state file path) is removed in favor of `--resume`

## Capabilities

### New Capabilities

- `resume-by-session-id`: Resume a workflow execution by session identifier or most recent session for the current project directory; flatten CLI by removing `run`/`resume`/`validate` subcommands in favor of `--resume`/`--session`/`--validate` flags; drop Cobra for Go's `flag` stdlib

### Modified Capabilities

- `workflow-execution`: State persistence location changes from caller-specified/engine-specified directory to unified session directory under `~/.agent-runner/projects/`

## Impact

- **Runner state management**: State writing and reading logic updates to use session directory paths instead of external paths
- **CLI interface**: `run`, `resume`, and `validate` subcommands are removed; the CLI is flattened to a single command with `--resume` (boolean), `--session <id>`, and `--validate` flags; Cobra is replaced with Go's `flag` stdlib
- **User experience**: Users no longer need to know or manage state file paths; workflow resumption is simpler and more discoverable
- **Backward compatibility**: No migration of existing state files; old state in openspec directories is abandoned

