## Purpose

Define Codex CLI adapter behavior for interactive/headless invocation, model overrides, session resume, and session discovery.

## Requirements

### Requirement: Codex interactive invocation

The Codex adapter SHALL always include the `--no-alt-screen` flag when constructing args for interactive mode steps. This prevents Codex from using the alternate screen buffer.

In headless Codex invocations, the Codex adapter SHALL include `--skip-git-repo-check` after the `exec` subcommand so autonomous steps can run in workflow work directories that are not git repositories. The adapter SHALL NOT include this flag before `exec` or for non-headless invocations.

#### Scenario: Interactive Codex step
- **WHEN** the runner executes an interactive step with `cli: codex`
- **THEN** the adapter includes `--no-alt-screen` in the invocation args

#### Scenario: Headless Codex step
- **WHEN** the runner executes a headless step with `cli: codex`
- **THEN** the adapter does not include `--no-alt-screen`

#### Scenario: Headless Codex step skips git repository check
- **WHEN** the runner executes a headless step with `cli: codex`
- **THEN** the adapter includes `exec --skip-git-repo-check` in the invocation args

#### Scenario: Non-headless Codex step keeps git repository check
- **WHEN** the runner executes a non-headless step with `cli: codex`
- **THEN** the adapter does not include `--skip-git-repo-check` in the invocation args

### Requirement: Codex model override

The Codex adapter SHALL support per-step model overrides. When a step specifies a `model` field, the adapter SHALL pass it to the Codex CLI.

#### Scenario: Model specified on Codex step
- **WHEN** a Codex step has `model: o3`
- **THEN** the adapter passes the model flag to the Codex invocation

#### Scenario: No model on Codex step
- **WHEN** a Codex step has no `model` field
- **THEN** the adapter invokes Codex without a model flag, using its default

### Requirement: Codex session resume

The Codex adapter SHALL support session resume. For interactive mode, the adapter SHALL use `codex resume --no-alt-screen <session-id> <prompt>`. For headless mode, the adapter SHALL use `codex exec --skip-git-repo-check resume <session-id> <prompt>`.

#### Scenario: Codex interactive step resumes prior session
- **WHEN** a Codex interactive step has session strategy `resume` and a session ID exists in state
- **THEN** the adapter invokes `codex resume --no-alt-screen <session-id> <prompt>`

#### Scenario: Codex headless step resumes prior session
- **WHEN** a Codex headless step has session strategy `resume` and a session ID exists in state
- **THEN** the adapter invokes `codex exec --skip-git-repo-check resume <session-id> <prompt>`

### Requirement: Codex session discovery

After a Codex step completes, the adapter SHALL return a session ID. For headless mode, the adapter SHALL parse the `thread_id` from the `thread.started` JSONL event emitted by `codex exec --json`. For interactive mode, the adapter SHALL resolve the session directory from the `CODEX_HOME` environment variable (falling back to `~/.codex` if unset) and scan `$CODEX_HOME/sessions/` for the most recent session file created after the step's spawn time, matching on CWD from the `session_meta` payload.

#### Scenario: Codex headless session ID from JSONL
- **WHEN** a headless Codex step completes
- **THEN** the adapter parses the `thread_id` from the `thread.started` event in the JSONL output

#### Scenario: Codex interactive session ID from filesystem
- **WHEN** an interactive Codex step completes
- **THEN** the adapter scans `$CODEX_HOME/sessions/` for the most recent file created after spawn time and extracts the session ID from the `session_meta` payload, matching on CWD
