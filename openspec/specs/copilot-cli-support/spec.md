# copilot-cli-support Specification

## Purpose
TBD - created by archiving change support-copilot-headless. Update Purpose after archive.
## Requirements
### Requirement: Copilot headless invocation

The Copilot adapter SHALL construct headless invocations using `copilot -p <prompt> --allow-all-tools --output-format json`. The `--allow-all-tools` flag is required because copilot refuses non-interactive execution without it. The `--output-format json` flag is required so that the runner can parse the session ID from stdout.

#### Scenario: Fresh headless Copilot step
- **WHEN** the runner executes a headless step with `cli: copilot` and session strategy `new`
- **THEN** the adapter returns args beginning with `copilot` and including `-p`, the prompt, `--allow-all-tools`, and `--output-format json`, and does not include `--resume`

#### Scenario: Headless flag required
- **WHEN** the runner constructs args for a Copilot headless step
- **THEN** the args include `--allow-all-tools` regardless of other options

### Requirement: Copilot session resume

The Copilot adapter SHALL support session resume in headless mode by emitting `--resume=<session-id>`. On resume, the adapter SHALL NOT emit `--model` or `--reasoning-effort` (a resumed copilot thread keeps the model and effort it was started with).

#### Scenario: Headless Copilot step resumes prior session
- **WHEN** a Copilot headless step has session strategy `resume` and a session ID exists in state
- **THEN** the adapter invocation includes `--resume=<session-id>` and does not include a `--model` flag even if one is set on the profile

#### Scenario: Model specified on fresh Copilot step
- **WHEN** a fresh headless Copilot step has `model: gpt-5.2`
- **THEN** the adapter includes `--model gpt-5.2` in the invocation args

#### Scenario: Model specified on resumed Copilot step
- **WHEN** a resumed headless Copilot step has `model: gpt-5.2`
- **THEN** the adapter does NOT include `--model` in the invocation args

### Requirement: Copilot effort mapping

When effort is provided, the Copilot adapter SHALL emit `--reasoning-effort <level>` on fresh sessions only. Values `low`, `medium`, and `high` pass through unchanged. Effort is omitted entirely when unset.

#### Scenario: Effort level specified on fresh Copilot step
- **WHEN** a fresh headless Copilot step has `effort: high`
- **THEN** the adapter includes `--reasoning-effort high` in the invocation args

#### Scenario: Effort level unset
- **WHEN** a Copilot step has no effort value
- **THEN** the adapter does not include `--reasoning-effort` in the invocation args

### Requirement: Copilot disallowed tool mapping

When the runner passes `DisallowedTools` containing `"AskUserQuestion"`, the Copilot adapter SHALL emit `--no-ask-user`. Other entries in `DisallowedTools` have no copilot equivalent and are silently ignored.

#### Scenario: AskUserQuestion disallowed
- **WHEN** the runner provides `DisallowedTools: ["AskUserQuestion"]` to the Copilot adapter
- **THEN** the adapter includes `--no-ask-user` in the invocation args

#### Scenario: No disallowed tools
- **WHEN** the runner provides an empty `DisallowedTools` list
- **THEN** the adapter does not include `--no-ask-user`

### Requirement: Copilot session ID discovery

After a headless Copilot process exits, the adapter SHALL parse stdout as JSONL and return the `sessionId` field from the first line whose `type` is `"result"`. If no such line is found, the adapter SHALL return the empty string.

#### Scenario: Session ID parsed from result event
- **WHEN** a headless Copilot step completes with stdout containing a `{"type":"result","sessionId":"<id>",...}` JSONL line
- **THEN** the adapter returns `<id>` from `DiscoverSessionID`

#### Scenario: Result event missing
- **WHEN** a headless Copilot step's stdout contains no `type:"result"` line (e.g., the process failed before emitting one)
- **THEN** the adapter returns the empty string

#### Scenario: Resumed session ID unchanged
- **WHEN** a headless Copilot step resumes session `<id>` and completes successfully
- **THEN** the adapter returns `<id>` (the same session ID observed in the resumed `result` event)

### Requirement: Copilot interactive mode rejected at runtime

Interactive mode for Copilot is not supported in this release. When a step with `cli: copilot` is invoked in interactive mode (headless=false), the runner SHALL fail the step at runtime with an error message indicating that copilot interactive mode is not supported.

#### Scenario: Interactive Copilot step fails at runtime
- **WHEN** an agent step resolves to `cli: copilot` and `mode: interactive`
- **THEN** the runner marks the step as failed and emits an error message stating that interactive mode is not supported for the copilot CLI

#### Scenario: Copilot interactive rejection does not block load
- **WHEN** a workflow or profile declares `cli: copilot` with `default_mode: interactive`
- **THEN** configuration and workflow loading succeed without error (the failure surfaces only when such a step is actually executed)

