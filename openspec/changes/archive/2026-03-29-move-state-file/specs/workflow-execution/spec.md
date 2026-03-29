## ADDED Requirements

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
