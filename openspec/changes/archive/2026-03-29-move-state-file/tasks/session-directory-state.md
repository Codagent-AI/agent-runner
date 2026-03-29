# Task: Unified session directory and state relocation

## Goal

Move state and audit files into a unified session directory at `~/.agent-runner/projects/{encoded-cwd}/runs/{session-id}/`. Each workflow execution creates a session directory containing both `state.json` and `audit.log`. This replaces the current behavior where state files go to the working directory and audit logs go to a separate `logs/` directory.

## Background

**Current state file behavior:**
- `internal/stateio/stateio.go` writes `agent-runner-state.json` to a directory resolved by `resolveStateDir()` in `internal/runner/runner.go` (line 70-78). The resolution chain is: engine override via `GetStateDir()` → `opts.StateDir` → `os.Getwd()`.
- `stateio.WriteState()`, `ReadState()`, `DeleteState()`, and `GetStateFilePath()` all use a hardcoded `stateFileName = "agent-runner-state.json"`.

**Current audit log behavior:**
- `internal/audit/logger.go` has `CreateLogger(workflowName, cwd)` which constructs the path `~/.agent-runner/projects/{encoded-cwd}/logs/{workflow-name}-{timestamp}.log`.
- `encodePath()` replaces `/`, `.`, `_` with `-`. `sanitizeWorkflowName()` replaces `..` and file-unsafe chars with `-`.
- There is a bug: `CreateLogger` is called with empty string for `cwd` in `runner.go` line 156, losing project context.

**What changes:**
- The session directory replaces all state/audit path resolution. `initRunState()` in `runner.go` must create the session directory and pass it to both `stateio.WriteState()` and `audit.NewLogger()`.
- The engine override chain (`GetStateDir()`, `opts.StateDir`, `resolveStateDir()`) is removed entirely. The session directory is always `~/.agent-runner/projects/{encoded-cwd}/runs/{session-id}/`.
- `CreateLogger(workflowName, cwd)` is no longer needed. Instead, call the existing `NewLogger(filepath.Join(sessionDir, "audit.log"))` directly.
- The `encodePath()` and `sanitizeWorkflowName()` functions must be accessible from outside the audit package since they're needed to construct the session directory path. Move them to an appropriate shared location or export them.
- The session ID format is `{sanitized-workflow-name}-{timestamp}`, using `sanitizeWorkflowName()` for the name and RFC3339 timestamp with colons replaced by dashes.
- The state file is renamed from `agent-runner-state.json` to `state.json`.

**Key files to modify:**
- `internal/stateio/stateio.go` — rename state file constant to `state.json`
- `internal/audit/logger.go` — remove `CreateLogger`, move `encodePath` and `sanitizeWorkflowName` to a shared location (or export them)
- `internal/runner/runner.go` — replace `resolveStateDir()` with session directory creation, pass session dir to audit and state, remove `StateDir` from `Options`
- `internal/runner/resume.go` — update to work with new state file name (the resume logic itself doesn't change, just the paths)
- `cmd/agent-runner/main.go` — remove `StateDir` from runner options
- Tests in `internal/stateio/stateio_test.go`, `internal/audit/logger_test.go`, `internal/runner/runner_test.go`

**Key constraint:** The `finalizeRun()` function in `runner.go` (line 302-324) prints a resume hint on failure: `agent-runner: to resume: agent-runner resume %s`. This message should print the session ID instead of a state file path, since the `resume` subcommand is being replaced with a `--resume` flag and will no longer exist. Update the message to something like `agent-runner: to resume: agent-runner --resume --session <session-id>`.

## Spec

### Requirement: Unified session directory

Each workflow execution SHALL create a session directory at `~/.agent-runner/projects/{encoded-cwd}/runs/{session-id}/`. The directory SHALL contain both `state.json` and `audit.log`. The `{encoded-cwd}` SHALL use the same encoding as today's audit log paths (replacing `/`, `.`, and `_` with `-`).

#### Scenario: Session directory created on run start
- **WHEN** a workflow execution begins
- **THEN** the runner creates a session directory at `~/.agent-runner/projects/{encoded-cwd}/runs/{session-id}/` and writes both state and audit files there

#### Scenario: Encoding matches existing convention
- **WHEN** the working directory is `/Users/paul/codagent/agent-runner`
- **THEN** the encoded path is `-Users-paul-codagent-agent-runner`

### Requirement: Session ID format

The session ID SHALL be `{sanitized-workflow-name}-{timestamp}`, where the workflow name is sanitized using the existing `sanitizeWorkflowName` logic and the timestamp uses RFC3339 format with colons replaced by dashes.

#### Scenario: Session ID for a named workflow
- **WHEN** a workflow named `deploy-service` starts at `2026-03-29T10:30:00Z`
- **THEN** the session ID is `deploy-service-2026-03-29T10-30-00Z`

### Requirement: State file in session directory

The state file (`state.json`) SHALL be written to the session directory. The runner SHALL NOT write state files to the working directory or any engine-specified path.

#### Scenario: State file written to session directory
- **WHEN** the runner persists state during execution
- **THEN** `state.json` is written inside the session directory, not in the working directory

### Requirement: Audit log in session directory

The audit log SHALL be written as `audit.log` inside the session directory, replacing the current convention of `~/.agent-runner/projects/{encoded-cwd}/logs/{workflow-name}-{timestamp}.log`.

#### Scenario: Audit log written to session directory
- **WHEN** a workflow execution begins and audit logging is enabled
- **THEN** the audit log is created at `{session-directory}/audit.log`

### Requirement: No migration of existing state files

Existing state files in working directories or openspec change directories SHALL be abandoned. The runner SHALL NOT attempt to locate, migrate, or read state files from legacy paths.

#### Scenario: Old state file ignored
- **WHEN** an `agent-runner-state.json` exists in the working directory from a previous version
- **THEN** the runner does not read or acknowledge it

## Done When

Tests covering the above scenarios pass. State files are written as `state.json` inside session directories under `~/.agent-runner/projects/{encoded-cwd}/runs/{session-id}/`. Audit logs are written as `audit.log` in the same directory. The engine state dir override chain is removed.
