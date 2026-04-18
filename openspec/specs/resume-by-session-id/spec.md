# resume-by-session-id Specification

## Purpose
TBD - created by archiving change move-state-file. Update Purpose after archive.
## Requirements
### Requirement: Resume by session ID

The CLI SHALL accept a `--resume` flag that optionally takes a session ID. When `--resume` is passed without a session ID, it SHALL launch the run list TUI. When `--resume <id>` is passed with a session ID, it SHALL resume workflow execution from that session's saved state.

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

### Requirement: Flatten CLI to single command

The `run`, `resume`, and `validate` subcommands SHALL be removed. The CLI SHALL accept a workflow file as a positional argument directly: `agent-runner [flags] <workflow.yaml> [params...]`. Flags MUST precede positional arguments. The `--resume` and `--validate` flags replace the former subcommands.

#### Scenario: Run workflow without subcommand
- **WHEN** `agent-runner workflow.yaml` is invoked without any subcommand
- **THEN** the runner executes the workflow as the former `run` subcommand did

#### Scenario: Validate via flag
- **WHEN** `--validate` is passed
- **THEN** the runner validates the workflow file and exits without executing

#### Scenario: Validate and resume are mutually exclusive
- **WHEN** both `--validate` and `--resume` are passed
- **THEN** the runner exits with an error indicating the flags are mutually exclusive
