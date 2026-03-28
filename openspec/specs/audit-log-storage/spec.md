# audit-log-storage Specification

## Purpose
Define how baton persists audit log files to disk, including directory layout, file naming, format, retention policy, and flush-on-write guarantees.
## Requirements
### Requirement: Log directory location

Baton SHALL store audit logs in `~/.baton/projects/{encoded-path}/logs/` where `{encoded-path}` is the project directory path with `/`, `.`, and `_` replaced by `-`.

#### Scenario: Log directory created
- **WHEN** a workflow run begins and the log directory does not exist
- **THEN** baton creates `~/.baton/projects/{encoded-path}/logs/`

#### Scenario: Path encoding
- **WHEN** the project directory is `/Users/foo/my_project`
- **THEN** the log directory is `~/.baton/projects/-Users-foo-my-project/logs/`

### Requirement: Log file naming

Each workflow execution SHALL create a new log file named `{workflow-name}-{ISO-8601-timestamp}.log` in the log directory.

#### Scenario: New run creates log file
- **WHEN** workflow `deploy` starts at `2026-03-15T18:30:00Z`
- **THEN** baton creates `deploy-2026-03-15T18-30-00Z.log`

#### Scenario: Resumed run creates new log file
- **WHEN** workflow `deploy` is resumed at `2026-03-15T19:00:00Z`
- **THEN** baton creates a new file `deploy-2026-03-15T19-00-00Z.log`

### Requirement: Log file format

Each line in the log file SHALL be a hybrid format: ISO-8601 timestamp, nesting prefix, event type, followed by a JSON payload. The JSON payload contains all structured data for the event.

#### Scenario: Log line format
- **WHEN** a `step_start` event is emitted for step `validate` at `2026-03-15T18:30:00Z`
- **THEN** the log line is formatted as `2026-03-15T18:30:00Z [validate] step_start {...}`

### Requirement: Log persistence

Audit log files SHALL never be automatically deleted. No rotation or cleanup is performed by baton.

#### Scenario: Logs accumulate
- **WHEN** a workflow is run 100 times
- **THEN** 100 log files exist in the log directory

### Requirement: Flush on write

Each log entry SHALL be flushed to disk immediately after being written, to ensure the log is complete even if the process crashes.

#### Scenario: Crash preserves log
- **WHEN** baton crashes mid-execution
- **THEN** all entries written before the crash are present in the log file

