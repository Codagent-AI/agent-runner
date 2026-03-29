## ADDED Requirements

### Requirement: Resume by session ID

The CLI SHALL accept a `--resume` boolean flag and a `--session <id>` string flag. When `--resume` is passed with `--session <id>`, it SHALL resume the workflow execution from that session's saved state. When `--resume` is passed without `--session`, it SHALL resume the most recent session for the current project directory, determined by filesystem modification time of the state file. `--session` without `--resume` SHALL be an error.

#### Scenario: Resume with explicit session ID
- **WHEN** `--resume --session <id>` is passed and a session directory with that ID exists
- **THEN** the runner resumes workflow execution from that session's saved state

#### Scenario: Resume most recent session
- **WHEN** `--resume` is passed without `--session`
- **THEN** the runner locates the most recently modified state file across all sessions for the current project directory and resumes from it

#### Scenario: Resume with nonexistent session ID
- **WHEN** `--resume --session <id>` is passed and no session directory matches
- **THEN** the runner exits with an error indicating the session was not found

#### Scenario: Resume with no previous sessions
- **WHEN** `--resume` is passed without `--session` and no previous sessions exist for the project directory
- **THEN** the runner exits with an error indicating no previous sessions were found

#### Scenario: Session flag without resume
- **WHEN** `--session <id>` is passed without `--resume`
- **THEN** the runner exits with an error indicating `--session` requires `--resume`

#### Scenario: Resume rejects positional arguments
- **WHEN** `--resume` is passed alongside any positional arguments (workflow file or parameters)
- **THEN** the runner exits with an error indicating that resume mode does not accept positional arguments

#### Scenario: Resume without positional arguments
- **WHEN** `--resume` is passed without any positional arguments
- **THEN** the runner proceeds with resume using the workflow file stored in the saved state

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
