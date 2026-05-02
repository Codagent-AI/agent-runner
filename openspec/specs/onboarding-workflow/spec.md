# onboarding-workflow Specification

## Purpose
TBD - created by archiving change onboarding. Update Purpose after archive.
## Requirements
### Requirement: Embedded onboarding namespace contents

The `onboarding` builtin workflow namespace SHALL contain at minimum:
- `welcome` — the top-level workflow entered by the first-run dispatcher and by direct invocation;
- `setup-agent-profile` — the sub-workflow used by Phase 2 (agent-profile editor);
- the bundled scripts these workflows reference, including adapter detection, model-list-for-cli, and profile-writer scripts.

#### Scenario: Welcome workflow resolves
- **WHEN** the user runs `agent-runner run onboarding:welcome`
- **THEN** the workflow loads from the embedded namespace and starts executing

#### Scenario: Setup sub-workflow resolves within the namespace
- **WHEN** `onboarding:welcome` references `workflow: setup-agent-profile.yaml`
- **THEN** the sub-workflow loads from the embedded `onboarding/setup-agent-profile.yaml` and SHALL NOT fall back to user-authored workflows

### Requirement: First-run dispatcher trigger condition

Before entering any interactive TUI (listview, live-run, run-view, resume-TUI), the runner SHALL evaluate the dispatcher condition. The condition SHALL fire (offer onboarding) when ALL of the following hold:
- `settings.onboarding.completed_at` is unset;
- `settings.onboarding.dismissed` is unset;
- both stdin and stdout are TTYs.

When any condition is false, the runner SHALL proceed to its normal entry point without modifying settings.

#### Scenario: Fresh first run with TTY fires
- **WHEN** the user runs `agent-runner` with no command on a TTY, and `settings.onboarding.completed_at` and `settings.onboarding.dismissed` are both unset
- **THEN** the dispatcher fires and launches `onboarding:welcome`

#### Scenario: Already completed does not fire
- **WHEN** the user runs `agent-runner` with `settings.onboarding.completed_at` set
- **THEN** the dispatcher does not fire and the runner proceeds to its normal entry point

#### Scenario: Already dismissed does not fire
- **WHEN** the user runs `agent-runner` with `settings.onboarding.dismissed` set
- **THEN** the dispatcher does not fire

#### Scenario: Non-TTY does not fire
- **WHEN** the user runs `agent-runner` from a CI job where stdout is a pipe, and onboarding settings are unset
- **THEN** the dispatcher does not fire and SHALL NOT modify settings (no auto-dismissal)

#### Scenario: Non-TUI command does not fire
- **WHEN** the user runs `agent-runner -version` or `agent-runner run my-workflow`
- **THEN** the dispatcher does not fire even when conditions would otherwise be satisfied

### Requirement: Dispatcher launches `onboarding:welcome` via the normal workflow path

When the dispatcher condition fires, the runner SHALL launch the embedded `onboarding:welcome` workflow using the standard workflow loader, runner, state, and audit machinery. The dispatcher SHALL NOT bypass the workflow loader or the runner; it SHALL NOT introduce a parallel execution path.

#### Scenario: Dispatcher uses standard machinery
- **WHEN** the dispatcher fires
- **THEN** `onboarding:welcome` runs through the same loader / runner / audit pipeline as any other invocation, and a normal run record is produced

### Requirement: Welcome screen actions

The first step of `onboarding:welcome` SHALL be a `mode: ui` informational screen with exactly three actions: `continue`, `not_now`, and `dismiss`.

#### Scenario: Welcome offers three actions
- **WHEN** the welcome screen renders
- **THEN** the user sees actions labelled to indicate continue, not-now, and dismiss; the underlying outcomes are exactly `continue`, `not_now`, `dismiss`

### Requirement: Dismiss action records dismissal and exits

When the user selects the `dismiss` action, the workflow SHALL set `settings.onboarding.dismissed` to the current RFC3339 timestamp via the existing `usersettings` atomic-write path. After the write, the workflow SHALL exit successfully without proceeding to setup.

#### Scenario: Dismiss writes timestamp
- **WHEN** the user selects `dismiss` on the welcome screen
- **THEN** `~/.agent-runner/settings.yaml` is written atomically with `onboarding.dismissed: <RFC3339 timestamp>` and the workflow exits successfully

#### Scenario: Dismiss does not run setup
- **WHEN** the user selects `dismiss`
- **THEN** the setup sub-workflow does not run; no config files are read or written

### Requirement: Not-now action exits without modifying settings

When the user selects the `not_now` action, the workflow SHALL exit successfully without writing to settings or running setup. The dispatcher SHALL fire again on the next TUI entry.

#### Scenario: Not-now leaves settings unchanged
- **WHEN** the user selects `not_now`
- **THEN** no settings keys are written and the workflow exits successfully

#### Scenario: Not-now does not suppress future prompts
- **WHEN** the user selects `not_now` and re-runs `agent-runner` later on a TTY
- **THEN** the dispatcher fires again

### Requirement: Continue action invokes setup

When the user selects the `continue` action, the workflow SHALL invoke `setup-agent-profile.yaml` as a sub-workflow. The setup sub-workflow runs the agent-profile-editor flow defined by the `agent-profile-editor` capability.

#### Scenario: Continue runs setup
- **WHEN** the user selects `continue`
- **THEN** `setup-agent-profile.yaml` runs as a sub-workflow

### Requirement: Successful completion records `completed_at`

On successful return from the setup sub-workflow, `onboarding:welcome` SHALL set `settings.onboarding.completed_at` to the current RFC3339 timestamp via the existing `usersettings` atomic-write path. Successful completion is determined by the setup sub-workflow's normal success/failure result.

#### Scenario: Successful setup writes completed_at
- **WHEN** the user runs through welcome → continue → setup, completes the editor write successfully
- **THEN** `settings.onboarding.completed_at` is written and the workflow exits successfully

### Requirement: Cancellation or failure inside setup leaves settings unchanged

When the setup sub-workflow fails or the user cancels (e.g., presses Cancel on a confirmation screen, or Ctrl-C interrupts a step), `onboarding:welcome` SHALL NOT modify settings. The dispatcher SHALL fire again on the next TUI entry.

#### Scenario: Cancel at confirmation does not record completion
- **WHEN** the user reaches the editor's confirmation screen and selects cancel
- **THEN** `settings.onboarding.completed_at` is not written and `settings.onboarding.dismissed` is not written

#### Scenario: Re-prompted after cancellation
- **WHEN** the user cancels mid-setup and later runs `agent-runner` on a TTY
- **THEN** the dispatcher fires again and offers the welcome screen

### Requirement: Re-entry by direct invocation

The user MAY re-run onboarding at any time via `agent-runner run onboarding:welcome`. The workflow SHALL execute regardless of the current state of `settings.onboarding.completed_at` or `settings.onboarding.dismissed`. The same actions, behaviors, and settings writes SHALL apply as on a dispatcher-triggered run.

#### Scenario: Run after completion
- **WHEN** the user runs `agent-runner run onboarding:welcome` with `settings.onboarding.completed_at` already set
- **THEN** the workflow executes normally; the user can choose continue, not_now, or dismiss

#### Scenario: Re-running after dismissal
- **WHEN** the user runs `agent-runner run onboarding:welcome` after previously dismissing
- **THEN** the workflow executes normally; selecting `continue` proceeds to setup despite the existing `dismissed` timestamp

### Requirement: Resume via standard machinery

Onboarding workflows SHALL participate in the standard resume-by-session-id and resume-TUI mechanisms without special-casing. A user who exits mid-flow (network drop, Ctrl-C, terminal close) MAY resume via the same commands and TUI as for any other workflow.

#### Scenario: Resume after mid-flow exit
- **WHEN** the user starts onboarding, completes the welcome screen and the adapter-detection step, then closes the terminal; later they run `agent-runner -resume`
- **THEN** the resume TUI lists the onboarding session and resumption picks up at the next pending step

### Requirement: No bespoke onboarding state

Onboarding SHALL be implemented entirely with existing workflow primitives (`mode: ui`, `script:`, sub-workflow, captures, settings writes via `agent-runner internal` subcommands). The runner SHALL NOT introduce an onboarding-specific state file or runtime path beyond the dispatcher trigger described above.

#### Scenario: No onboarding-only state file
- **WHEN** an onboarding session runs
- **THEN** workflow state is recorded in the standard run-state location with no additional onboarding-specific file

