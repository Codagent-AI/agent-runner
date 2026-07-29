## MODIFIED Requirements

### Requirement: Resume by session ID

The CLI SHALL accept a `--resume` flag that optionally takes a session ID. When `--resume` is passed without a session ID, it SHALL launch the run list TUI. When `--resume <id>` is passed with a session ID for an unfinished run, it SHALL resume workflow execution from that session's saved state using the exact recorded workflow version. A newer workflow version MUST NOT replace the recorded version.

For an unfinished run, a changed workflow hash SHALL retain the existing warning behavior and SHALL NOT block resume. A missing recorded versioned file SHALL fail resume without selecting another version. An unversioned recorded on-disk workflow file SHALL fail with actionable filename migration guidance and MUST NOT fall back to a versioned file. An unversioned recorded `builtin:` workflow reference SHALL instead explain that the run predates workflow versioning and cannot resume with the current binary, and SHALL guide the user to restart it or finish it using the older binary.

When `--resume <id>` is passed with a session ID for a completed run, it SHALL open the run view for that session in inspect mode (the same read-only view as `--inspect <id>`) instead of validating the workflow filename for execution or resuming. Completed unversioned runs and completed runs whose workflow files are missing SHALL remain inspectable from recoverable saved run evidence.

#### Scenario: Resume with explicit session ID on an unfinished run
- **WHEN** `--resume <id>` is passed and the matching session's saved state indicates the workflow is not yet complete
- **THEN** the runner resumes workflow execution from that session's saved state

#### Scenario: Unfinished run resumes exact recorded version
- **WHEN** an unfinished run records `deploy-v1.0.yaml`, `deploy-v2.0.yaml` also exists, and the user resumes the run
- **THEN** the runner loads `deploy-v1.0.yaml` and does not select `deploy-v2.0.yaml`

#### Scenario: Edited recorded version warns and resumes
- **WHEN** an unfinished run's recorded versioned file differs from its saved workflow hash
- **THEN** Agent Runner emits the existing changed-file warning and continues resume from that recorded file

#### Scenario: Missing recorded version fails resume
- **WHEN** an unfinished run records `deploy-v1.0.yaml` and that file is missing
- **THEN** resume fails with an error naming the missing recorded version and does not select another version

#### Scenario: Unfinished legacy run fails migration validation
- **WHEN** an unfinished run records unversioned `deploy.yaml` and the user resumes the run
- **THEN** resume fails with actionable guidance to migrate to a versioned filename and does not fall back to `deploy-v1.0.yaml`

#### Scenario: Unfinished legacy builtin run requires restart or older binary
- **WHEN** an unfinished run records `builtin:onboarding/onboarding.yaml` and the user resumes the run
- **THEN** resume fails with an error that the run predates workflow versioning
- **AND** the error guides the user to restart with the current binary or finish with the older binary instead of instructing them to rename an embedded file

#### Scenario: Resume with explicit session ID on a completed run opens run view
- **WHEN** `--resume <id>` is passed and the matching session's saved state indicates the workflow has already completed (either because `completed` is true, or because no incomplete steps remain)
- **THEN** the runner opens the run view for that session in inspect mode so the user can read its recorded output, and exits when the view is dismissed

#### Scenario: Completed unversioned run remains inspectable
- **WHEN** a completed run records unversioned `deploy.yaml` and the user invokes `--resume <id>`
- **THEN** the runner opens the run in inspect mode and displays its workflow version as `unversioned`

#### Scenario: Completed run with missing definition remains inspectable
- **WHEN** a completed run's recorded workflow file is missing but its saved run evidence can reconstruct the view
- **THEN** `--resume <id>` opens the run in inspect mode without substituting another workflow version

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

The former command framework's `run`, `resume`, and `validate` subcommands SHALL remain removed. For fresh execution, the primary CLI form SHALL accept a version-free logical workflow name as a positional argument: `agent-runner [flags] <workflow-name> [params...]`. The lightweight `agent-runner run <workflow-name> ...` command form SHALL remain an alias for fresh execution. A workflow filename or filesystem path MUST NOT start a fresh run through either form. Global flags in the primary form MUST precede positional arguments. The `--resume` and `--validate` flags replace the former resume and validate subcommands.

The `--validate` flag SHALL accept either a version-free logical workflow name or an existing exact versioned `.yaml` or `.yml` file path. Accepting an exact path for validation MUST NOT make that path executable as a fresh run.

#### Scenario: Run workflow without subcommand
- **WHEN** `agent-runner deploy` is invoked without any subcommand
- **THEN** the runner resolves the logical workflow name to its latest version and executes it

#### Scenario: Run command alias resolves logical workflow
- **WHEN** `agent-runner run deploy` is invoked
- **THEN** the runner resolves the logical workflow name to its latest version and executes it

#### Scenario: Workflow path cannot start fresh run
- **WHEN** `agent-runner deploy-v1.0.yaml` is invoked without `--validate`
- **THEN** the runner rejects the workflow filename and instructs the user to launch logical workflow `deploy`

#### Scenario: Run alias also rejects workflow path
- **WHEN** `agent-runner run deploy-v1.0.yaml` is invoked
- **THEN** the runner rejects the workflow filename and instructs the user to launch logical workflow `deploy`

#### Scenario: Validate via flag
- **WHEN** `agent-runner --validate deploy` is invoked
- **THEN** the runner resolves and validates the latest `deploy` workflow without executing it

#### Scenario: Validate exact versioned path via flag
- **WHEN** `agent-runner --validate deploy-v1.0.yaml` is invoked and the file exists
- **THEN** the runner validates that exact workflow file and exits without executing it

#### Scenario: Validate and resume are mutually exclusive
- **WHEN** both `--validate` and `--resume` are passed
- **THEN** the runner exits with an error indicating the flags are mutually exclusive
