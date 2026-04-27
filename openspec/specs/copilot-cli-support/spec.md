# copilot-cli-support Specification

## Purpose

Defines how the runner integrates with the GitHub Copilot CLI (`copilot`) as a headless agent backend.
## Requirements
### Requirement: Copilot headless invocation

The Copilot adapter SHALL construct headless invocations using:

```
copilot -p <prompt> --allow-all --autopilot -s [--model <m>] [--reasoning-effort <e>]
```

- `--allow-all` grants tool, file-path, and URL permissions required for autonomous operation.
- `--autopilot` keeps the agent running until the task is complete.
- `-s` suppresses interactive output, emitting only the agent response text.

#### Scenario: Fresh headless Copilot step
- **WHEN** the runner executes a headless step with `cli: copilot` and session strategy `new`
- **THEN** the adapter returns args beginning with `copilot` and including `-p`, the prompt, `--allow-all`, `--autopilot`, and `-s`, and does not include `--resume`

#### Scenario: Headless flag required
- **WHEN** the runner constructs args for a Copilot headless step
- **THEN** the args include `--allow-all` regardless of other options

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

After a headless Copilot process exits, the adapter SHALL discover the session ID by scanning `~/.copilot/session-state/` for the most recently modified session directory created after the process spawn time, matching on the working directory recorded in that session's `workspace.yaml`. If no matching session is found, the adapter SHALL return the empty string.

The effective working directory used for matching SHALL be the working directory of the Copilot process (`opts.Workdir`); when that is not provided, the runner process's own working directory (`os.Getwd()`) is used as a fallback.

#### Scenario: Session ID discovered from filesystem
- **WHEN** a headless Copilot step completes and a matching session directory exists in `~/.copilot/session-state/` with a `workspace.yaml` whose `cwd` equals the step's working directory
- **THEN** the adapter returns the session directory name as the session ID

#### Scenario: Session ID uses step workdir for matching
- **WHEN** a step specifies `workdir` and the Copilot process ran in that directory
- **THEN** the adapter matches the session using the step workdir, not the runner's own CWD

#### Scenario: No matching session found
- **WHEN** no session directory in `~/.copilot/session-state/` matches the working directory or was created after the spawn time
- **THEN** the adapter returns the empty string

