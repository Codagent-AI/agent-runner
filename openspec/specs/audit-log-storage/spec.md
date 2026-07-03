# audit-log-storage Specification

## Purpose
Define how Agent Runner persists audit log files to disk, including directory layout, file naming, format, retention policy, and close-on-exit guarantees.
## Requirements
### Requirement: Log directory location

Agent Runner SHALL store each run's audit log in the run session directory:

```text
~/.agent-runner/projects/{encoded-path}/runs/{run-id}/audit.log
```

`{encoded-path}` is the project directory path with `/`, `.`, and `_` replaced by `-`.

#### Scenario: Log directory created
- **WHEN** a workflow run begins and the run session directory does not exist
- **THEN** Agent Runner creates `~/.agent-runner/projects/{encoded-path}/runs/{run-id}/`

#### Scenario: Path encoding
- **WHEN** the project directory is `/Users/foo/my_project`
- **THEN** the project storage directory is `~/.agent-runner/projects/-Users-foo-my-project/`

### Requirement: Log file naming

Each workflow execution SHALL create an `audit.log` file in its run session directory. The run id is represented by the session directory name, not by the audit log filename.

#### Scenario: New run creates log file
- **WHEN** workflow `deploy` starts and receives run id `deploy-2026-03-15T18-30-00Z`
- **THEN** Agent Runner creates `~/.agent-runner/projects/{encoded-path}/runs/deploy-2026-03-15T18-30-00Z/audit.log`

#### Scenario: Resumed run creates new log file
- **WHEN** workflow run `deploy-2026-03-15T18-30-00Z` is resumed
- **THEN** Agent Runner appends audit events to that run's `audit.log`

### Requirement: Log file format

Each line in the log file SHALL be a hybrid format: ISO-8601 timestamp, optional nesting prefix, event type, followed by a JSON payload. The JSON payload contains all structured data for the event.

#### Scenario: Log line format
- **WHEN** a `step_start` event is emitted for step `validate` at `2026-03-15T18:30:00Z`
- **THEN** the log line is formatted as `2026-03-15T18:30:00Z [validate] step_start {...}`

### Requirement: Log persistence

Audit log files SHALL never be automatically deleted. No rotation or cleanup is performed by Agent Runner.

#### Scenario: Logs accumulate
- **WHEN** a workflow is run 100 times
- **THEN** 100 log files exist in the log directory

### Requirement: Close on exit

The audit logger SHALL close the log file when workflow execution exits. Entries written before an abrupt process crash may depend on operating-system buffering.

#### Scenario: Completed run closes log
- **WHEN** Agent Runner exits workflow execution normally
- **THEN** the audit log file is closed
