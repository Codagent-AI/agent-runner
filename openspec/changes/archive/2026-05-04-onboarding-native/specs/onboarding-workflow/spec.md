## ADDED Requirements

### Requirement: Onboarding demo intro actions

The first step of `onboarding:onboarding` SHALL be a `mode: ui` informational screen that introduces the onboarding demo and offers exactly three outcomes: continue to the demo, defer the demo until later, and dismiss the demo. The step SHALL NOT be named or presented as the setup welcome screen.

#### Scenario: Intro offers three actions
- **WHEN** `onboarding:onboarding` starts
- **THEN** the first step offers actions whose outcomes are exactly `continue`, `not_now`, and `dismiss`

#### Scenario: Intro is not setup welcome
- **WHEN** the onboarding demo intro renders
- **THEN** the copy describes the optional workflow demo rather than mandatory setup

#### Scenario: Not-now leaves demo eligible
- **WHEN** the user selects the not-now action
- **THEN** no onboarding completion or dismissal setting is written

#### Scenario: Dismiss records demo dismissal
- **WHEN** the user selects the dismiss action
- **THEN** `settings.onboarding.dismissed` is written with the current RFC3339 timestamp

## MODIFIED Requirements

### Requirement: Embedded onboarding namespace contents

The `onboarding` builtin workflow namespace SHALL contain at minimum:
- `onboarding` - the top-level demo workflow started after successful native setup and by direct invocation;
- `step-types-demo` - the workflow used to demonstrate UI, interactive agent, headless agent, shell, and capture behavior;
- the packaged documentation needed by the Q&A agent.

The onboarding namespace SHALL NOT own first-run setup or setup completion tracking. It SHALL own onboarding demo defer and dismissal behavior.

#### Scenario: Onboarding demo workflow resolves
- **WHEN** the user runs `agent-runner run onboarding:onboarding`
- **THEN** the workflow loads from the embedded namespace and starts executing

#### Scenario: Step types demo workflow resolves
- **WHEN** the user runs `agent-runner run onboarding:step-types-demo`
- **THEN** the workflow loads from the embedded namespace and starts executing

#### Scenario: Welcome workflow is not the demo entry
- **WHEN** the user runs `agent-runner run onboarding:welcome`
- **THEN** the runner fails with a workflow-not-found error

#### Scenario: Setup workflow is not an onboarding workflow
- **WHEN** the user runs `agent-runner run onboarding:setup-agent-profile`
- **THEN** the runner fails with a workflow-not-found error

### Requirement: First-run dispatcher trigger condition

Before entering the bare/list TUI entry point, the runner SHALL evaluate native setup before onboarding demo dispatch. The onboarding demo dispatcher SHALL fire when all of the following hold:
- `settings.setup.completed_at` is set;
- `settings.onboarding.completed_at` is unset;
- `settings.onboarding.dismissed` is unset;
- both stdin and stdout are TTYs.

When any condition is false, the runner SHALL proceed to its normal entry point without modifying onboarding settings.

#### Scenario: Setup runs before onboarding demo
- **WHEN** setup settings and onboarding settings are unset
- **THEN** the runner opens native setup before any onboarding demo workflow

#### Scenario: Completed setup starts demo
- **WHEN** `settings.setup.completed_at` is set and `settings.onboarding.completed_at` and `settings.onboarding.dismissed` are unset
- **THEN** the dispatcher launches `onboarding:onboarding`

#### Scenario: Completed onboarding demo does not fire
- **WHEN** `settings.onboarding.completed_at` is set
- **THEN** the onboarding demo dispatcher does not fire and the runner proceeds to its normal entry point

#### Scenario: Dismissed onboarding demo does not fire
- **WHEN** `settings.onboarding.dismissed` is set
- **THEN** the onboarding demo dispatcher does not fire

#### Scenario: Non-TTY does not fire
- **WHEN** the runner starts with stdin or stdout connected to a pipe
- **THEN** the onboarding demo dispatcher does not fire and SHALL NOT modify settings

#### Scenario: Non-TUI command does not fire
- **WHEN** the user runs `agent-runner -version` or `agent-runner run my-workflow`
- **THEN** the onboarding demo dispatcher does not fire even when conditions would otherwise be satisfied

#### Scenario: Resume does not fire
- **WHEN** the user runs `agent-runner --resume <id>`
- **THEN** the onboarding demo dispatcher does not fire even when conditions would otherwise be satisfied

### Requirement: Continue action invokes setup

The onboarding demo workflow SHALL NOT invoke setup. Setup is native TUI functionality that runs before the onboarding demo. When the user selects the continue action in `onboarding:onboarding`, the workflow SHALL invoke the `step-types-demo` workflow or otherwise run the step-types demo sequence.

#### Scenario: Demo skips setup
- **WHEN** the user selects continue in `onboarding:onboarding`
- **THEN** it does not invoke `setup-agent-profile.yaml`

#### Scenario: Demo runs step types
- **WHEN** the user selects continue in `onboarding:onboarding`
- **THEN** it runs the step-types demo workflow sequence

### Requirement: Successful completion records `completed_at`

When the continue path of `onboarding:onboarding` completes successfully, the runner SHALL set `settings.onboarding.completed_at` to the current RFC3339 timestamp via the existing user settings atomic-write path. Successful onboarding demo completion SHALL NOT write setup completion settings.

#### Scenario: Demo completion records onboarding completion
- **WHEN** `onboarding:onboarding` completes its continue path successfully
- **THEN** `settings.onboarding.completed_at` is written

#### Scenario: Demo completion does not write setup completion
- **WHEN** `onboarding:onboarding` completes successfully
- **THEN** `settings.setup.completed_at` is not modified

### Requirement: Re-entry by direct invocation

The user MAY re-run the onboarding demo at any time via `agent-runner run onboarding:onboarding`. The workflow SHALL execute regardless of the current state of `settings.onboarding.completed_at`, `settings.onboarding.dismissed`, or `settings.setup.completed_at`. Direct invocation of `onboarding:onboarding` SHALL use the standard direct-run post-run behavior.

#### Scenario: Run after demo completion
- **WHEN** the user runs `agent-runner run onboarding:onboarding` with `settings.onboarding.completed_at` already set
- **THEN** the workflow executes normally

#### Scenario: Run after demo dismissal
- **WHEN** the user runs `agent-runner run onboarding:onboarding` with `settings.onboarding.dismissed` already set
- **THEN** the workflow executes normally

#### Scenario: Run without setup completion
- **WHEN** the user runs `agent-runner run onboarding:onboarding` with `settings.setup.completed_at` unset
- **THEN** the workflow executes normally

### Requirement: Step types demo failure leaves onboarding incomplete

When `onboarding:onboarding` or `onboarding:step-types-demo` fails or is cancelled before completing the continue path, the runner SHALL NOT write `settings.onboarding.completed_at`. Native setup completion settings SHALL remain unchanged.

#### Scenario: Demo failure does not write onboarding settings
- **WHEN** `onboarding:onboarding` starts and the step-types demo fails before completion
- **THEN** `settings.onboarding.completed_at` is not written

#### Scenario: Demo failure does not change setup state
- **WHEN** `onboarding:onboarding` fails after native setup completed
- **THEN** `settings.setup.completed_at` remains unchanged

#### Scenario: Direct invocation with demo failure keeps standard post-run behavior
- **WHEN** the user runs `agent-runner run onboarding:onboarding` directly and the demo reaches a terminal failure or cancellation
- **THEN** the runner SHALL behave per the existing direct-run view rules and SHALL NOT auto-transition to the list-runs TUI

## REMOVED Requirements

### Requirement: Dispatcher launches `onboarding:welcome` via the normal workflow path

**Reason**: First-run setup is now native TUI behavior, and onboarding workflow execution starts only for the demo after setup succeeds.
**Migration**: Use native setup dispatch for setup behavior and `onboarding:onboarding` for the workflow demo.

### Requirement: Welcome screen actions

**Reason**: The setup welcome surface is now native and mandatory, while the workflow-owned optional screen is an onboarding demo intro with different copy and purpose.
**Migration**: Use native setup for mandatory setup progression and the `Onboarding demo intro actions` requirement for optional demo deferral/dismissal.

### Requirement: Dismiss action records dismissal and exits

**Reason**: Setup can no longer be dismissed. Dismissal now applies only to the optional onboarding demo.
**Migration**: Use the `Onboarding demo intro actions` requirement and `settings.onboarding.dismissed`.

### Requirement: Not-now action exits without modifying settings

**Reason**: Setup can no longer be deferred. Not-now now applies only to the optional onboarding demo.
**Migration**: Use the `Onboarding demo intro actions` requirement.

### Requirement: Cancellation or failure inside setup leaves settings unchanged

**Reason**: Setup cancellation and failure are now native setup outcomes instead of sub-workflow outcomes.
**Migration**: Use the native setup cancellation, interruption, and failure requirements.

### Requirement: Resume via standard machinery

**Reason**: Native setup does not persist partial wizard state and does not resume through workflow run state.
**Migration**: Restart native setup from the beginning on the next eligible launch.

### Requirement: No bespoke onboarding state

**Reason**: Setup tracking now requires native settings state separate from workflow run state.
**Migration**: Store setup completion in `settings.setup.completed_at`; workflow demo state remains normal workflow run state plus `settings.onboarding.completed_at` or `settings.onboarding.dismissed`.

### Requirement: Dispatcher resumes incomplete prior onboarding runs

**Reason**: The first-run setup dispatcher no longer launches or resumes `onboarding:welcome`.
**Migration**: Restart interrupted native setup from the beginning; run `onboarding:onboarding` after setup completes.

### Requirement: Post-onboarding handoff to the home screen

**Reason**: Handoff is now split between native setup and standard workflow run behavior for the demo.
**Migration**: Use native setup handoff for setup outcomes and standard direct-run/live-run behavior for `onboarding:onboarding`.
