# Task: Flatten CLI and add resume-by-session

## Goal

Replace Cobra with Go's `flag` stdlib, remove `run`/`resume`/`validate` subcommands, and implement `--resume`/`--session`/`--validate` flags. Users invoke the runner as `agent-runner <workflow.yaml> [params...]` with optional flags instead of subcommands.

## Background

**Current CLI structure:**
- `cmd/agent-runner/main.go` uses Cobra (`github.com/spf13/cobra`) with three subcommands: `run`, `resume`, `validate`.
- `run` takes `<workflow.yaml> [params...]`, loads the workflow, parses params via `parseParams()` and `matchParams()`, optionally creates an engine, and calls `runner.RunWorkflow()`.
- `validate` takes `<workflow.yaml>`, calls `loader.LoadWorkflow()`, prints "workflow is valid".
- `resume` takes `<state-file>`, calls `runner.ResumeWorkflow(stateFilePath, opts)`.
- The helper functions `parseParams()` and `matchParams()` are reusable and should be preserved.
- Process runner types (`realProcessRunner`, `realAgentProcess`, `realGlobExpander`, `realLogger`) are unchanged.

**What changes:**
- Drop Cobra entirely. Remove the `github.com/spf13/cobra` dependency from `go.mod`. Use Go's `flag` stdlib for flag parsing.
- The CLI becomes: `agent-runner [--resume] [--session <id>] [--validate] <workflow.yaml> [params...]`. Flags must precede positional args (Go `flag` convention). Positional args come from `flag.Args()` after flag parsing.
- `--resume` is a boolean flag. `--validate` is a boolean flag. `--session` is a string flag.
- `--resume` alone: scan `~/.agent-runner/projects/{encoded-cwd}/runs/*/state.json` for the most recently modified file. Use its path with `runner.ResumeWorkflow()`.
- `--resume --session <id>`: look for `~/.agent-runner/projects/{encoded-cwd}/runs/<id>/state.json` directly. Error if not found.
- `--session` without `--resume`: exit with error.
- `--validate` and `--resume` together: exit with error (mutually exclusive).
- `--resume` with any positional arguments (including workflow file): exit with error. Resume mode accepts no positional arguments — the workflow file and params are restored from the saved state.
- `--validate`: load and validate the workflow file, print "workflow is valid", exit.
- No flags: execute the workflow as the former `run` subcommand did.
- Write a manual `flag.Usage` function for help output.

**Session directory path resolution:**
- The `encodePath()` function (which replaces `/`, `.`, `_` with `-`) is available in a shared location outside the audit package (moved from `internal/audit/logger.go` to be importable by the CLI). Use it to construct `~/.agent-runner/projects/{encoded-cwd}/runs/`.
- For "most recent" lookup: list directories under `runs/`, find `state.json` in each, pick the one with the most recent modification time (use `os.Stat().ModTime()`).

**Key files to modify:**
- `cmd/agent-runner/main.go` — rewrite CLI setup, replacing Cobra with `flag` stdlib
- `go.mod` — remove `github.com/spf13/cobra` dependency (run `go mod tidy` after)
- Tests in `cmd/agent-runner/` — `script_test.go` and `version_test.go` will need updates for the new CLI interface

**The `runner.ResumeWorkflow()` function** (in `internal/runner/resume.go`) takes a state file path and runner options — this interface doesn't change. The only change is how the CLI resolves the state file path before calling it.

## Spec

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

## Done When

Tests covering the above scenarios pass. Cobra is removed from dependencies. The CLI works as `agent-runner [--resume] [--session <id>] [--validate] <workflow.yaml> [params...]` with proper mutual exclusivity enforcement and session lookup logic.
