## MODIFIED Requirements

### Requirement: Resume by session ID

The CLI SHALL accept a `--resume` flag that optionally takes a session ID. When `--resume` is passed without a session ID, it SHALL launch the run list TUI. When `--resume <id>` is passed with a session ID, it SHALL resume workflow execution from that session's saved state. The separate `--session` flag is removed.

#### Scenario: Resume with explicit session ID
- **WHEN** `--resume <id>` is passed and a session with that ID exists
- **THEN** the runner resumes workflow execution from that session's saved state

#### Scenario: Resume without session ID launches TUI
- **WHEN** `--resume` is passed without a session ID
- **THEN** the run list TUI is launched

#### Scenario: Resume with nonexistent session ID
- **WHEN** `--resume <id>` is passed and no session matches that ID
- **THEN** the runner exits with an error indicating the session was not found

#### Scenario: Resume rejects extra positional arguments
- **WHEN** `--resume` is passed with more than one positional argument
- **THEN** the runner exits with an error indicating resume mode accepts at most one argument (the session ID)

## REMOVED Requirements

### Requirement: --session flag
**Reason**: `--session` merged into `--resume` as an optional value. The separate flag was redundant and added unnecessary verbosity.
**Migration**: Replace `--resume --session <id>` with `--resume <id>`.
