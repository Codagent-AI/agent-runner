# onboarding-workflow Specification

## Purpose
Define the first-run onboarding experience that guides new users through initial agent profile setup via a dispatcher-triggered embedded workflow with welcome, dismiss, and setup-continuation flows.
## Requirements
### Requirement: Embedded onboarding namespace contents

The `onboarding` builtin workflow namespace SHALL contain at minimum:
- `welcome` — the top-level workflow entered by the first-run dispatcher and by direct invocation;
- `setup-agent-profile` — the sub-workflow used by Phase 2 (agent-profile editor);
- `step-types-demo` — the sub-workflow used by Phase 3 to demonstrate UI, interactive agent, headless agent, shell, and capture behavior;
- the bundled scripts and packaged documentation these workflows reference, including adapter detection, model-list-for-cli, profile-writer scripts, and onboarding documentation needed by the Q&A agent.

#### Scenario: Welcome workflow resolves
- **WHEN** the user runs `agent-runner run onboarding:welcome`
- **THEN** the workflow loads from the embedded namespace and starts executing

#### Scenario: Setup sub-workflow resolves within the namespace
- **WHEN** `onboarding:welcome` references `workflow: setup-agent-profile.yaml`
- **THEN** the sub-workflow loads from the embedded `onboarding/setup-agent-profile.yaml` and SHALL NOT fall back to user-authored workflows

#### Scenario: Step types demo workflow resolves
- **WHEN** the user runs `agent-runner run onboarding:step-types-demo`
- **THEN** the workflow loads from the embedded namespace and starts executing

#### Scenario: Step types demo sub-workflow resolves within the namespace
- **WHEN** `onboarding:welcome` references `workflow: step-types-demo.yaml`
- **THEN** the sub-workflow loads from the embedded `onboarding/step-types-demo.yaml` and SHALL NOT fall back to user-authored workflows

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

When the user selects the `continue` action, the workflow SHALL invoke `setup-agent-profile.yaml` as a sub-workflow. After setup succeeds, the workflow SHALL invoke `step-types-demo.yaml` as a sub-workflow. The setup sub-workflow runs the agent-profile-editor flow defined by the `agent-profile-editor` capability. The step types demo sub-workflow orients the user to core workflow step primitives.

#### Scenario: Continue runs setup
- **WHEN** the user selects `continue`
- **THEN** `setup-agent-profile.yaml` runs as a sub-workflow

#### Scenario: Demo runs after successful setup
- **WHEN** the user selects `continue` and `setup-agent-profile.yaml` completes successfully
- **THEN** `step-types-demo.yaml` runs as the next sub-workflow

### Requirement: Successful completion records `completed_at`

When the `continue` path reaches the end of `onboarding:welcome` successfully, the workflow SHALL set `settings.onboarding.completed_at` to the current RFC3339 timestamp via the existing `usersettings` atomic-write path. Successful completion is determined by the normal success/failure result of the workflow steps on the selected path.

#### Scenario: Successful continue path writes completed_at
- **WHEN** the user runs through welcome, selects `continue`, and the selected onboarding path reaches its end successfully
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

Onboarding SHALL be implemented entirely with existing workflow primitives (`mode: ui`, agent steps, shell steps, `script:`, sub-workflow, captures, settings writes via `agent-runner internal` subcommands, and packaged workflow assets). The runner SHALL NOT introduce an onboarding-specific state file or runtime path beyond the dispatcher trigger described above.

#### Scenario: No onboarding-only state file
- **WHEN** an onboarding session runs
- **THEN** workflow state is recorded in the standard run-state location with no additional onboarding-specific file

### Requirement: Dispatcher resumes incomplete prior onboarding runs

When the dispatcher condition fires and a prior `onboarding:welcome` run exists in a non-terminal state (i.e., the workflow was started but did not reach success, dismiss, or fatal failure), the dispatcher SHALL resume that run via the standard resume machinery rather than starting a new run. Only when there is no incomplete prior run SHALL the dispatcher start a fresh `onboarding:welcome`.

The dispatcher SHALL detect incomplete runs by inspecting the standard run-state location for runs whose workflow id is `onboarding:welcome` and whose state is not terminal. If multiple incomplete runs exist (an unexpected state), the dispatcher SHALL resume the most recent and SHALL NOT start a new run.

#### Scenario: Resume after Ctrl-C mid-flow
- **WHEN** the user starts onboarding via the dispatcher, advances past the welcome screen and the adapter-detection step, presses Ctrl-C to exit, and then re-runs `agent-runner` on a TTY with onboarding settings still unset
- **THEN** the dispatcher SHALL fire and SHALL resume the existing run at the next pending step rather than re-rendering the welcome screen

#### Scenario: No incomplete run starts a fresh one
- **WHEN** the dispatcher fires and there is no `onboarding:welcome` run in a non-terminal state
- **THEN** the dispatcher SHALL start a fresh `onboarding:welcome` run

#### Scenario: Multiple incomplete runs resume the most recent
- **WHEN** the dispatcher fires and two `onboarding:welcome` runs exist in non-terminal states
- **THEN** the dispatcher SHALL resume the most recent run and SHALL NOT start a new run

### Requirement: Post-onboarding handoff to the home screen

When a dispatcher-launched `onboarding:welcome` run reaches a terminal state — successful `continue` path completion, `dismiss`, `not_now`, cancellation, or failure — the runner SHALL transition to its normal entry point for the bare `agent-runner` invocation: the list-runs ("home") TUI. The runner SHALL NOT leave the user on the post-completion run view of the onboarding run.

This handoff applies only to the dispatcher-launched path. Direct invocation via `agent-runner run onboarding:welcome` SHALL retain its current post-run behavior (the user remains on the run-view per the standard `view-run` rules) so that scripted or explicit invocations are not surprised by an entry-point switch.

#### Scenario: Dismiss returns to home
- **WHEN** the dispatcher launches onboarding and the user selects `dismiss` on the welcome screen
- **THEN** after the dismissal timestamp is written, the runner SHALL transition to the list-runs TUI

#### Scenario: Not-now returns to home
- **WHEN** the dispatcher launches onboarding and the user selects `not_now` on the welcome screen
- **THEN** the runner SHALL transition to the list-runs TUI without writing settings

#### Scenario: Successful continue path returns to home
- **WHEN** the dispatcher launches onboarding, the user selects `continue`, and the selected onboarding path reaches its end successfully
- **THEN** after `completed_at` is written, the runner SHALL transition to the list-runs TUI

#### Scenario: Setup cancellation returns to home
- **WHEN** the dispatcher launches onboarding, the user selects `continue`, and the user cancels at the editor's confirmation screen
- **THEN** the runner SHALL transition to the list-runs TUI without writing settings (dispatcher will fire again on next entry per existing rules)

#### Scenario: Direct invocation does not switch entry points
- **WHEN** the user runs `agent-runner run onboarding:welcome` directly and the workflow reaches a terminal state
- **THEN** the runner SHALL behave per the existing `view-run` rules (remain on the run view) and SHALL NOT auto-transition to the list-runs TUI

### Requirement: Step types demo instructional flow

The `step-types-demo` onboarding workflow SHALL teach workflow step primitives by running real workflow steps in a fixed instructional sequence:
- a `mode: ui` step that explains the demo is itself beginning with a UI step;
- a `mode: ui` explanation immediately before an interactive agent demonstration;
- an interactive agent step using the `planner` profile;
- a `mode: ui` explanation immediately before a headless agent demonstration;
- a headless agent step using the `implementor` profile;
- a `mode: ui` explanation immediately before a shell demonstration;
- a shell step that produces a captured value;
- a final `mode: ui` summary screen that displays the captured shell value.

The workflow SHALL NOT require separate recap screens after each runtime demonstration step.

#### Scenario: Demo starts by teaching UI steps
- **WHEN** `onboarding:step-types-demo` starts
- **THEN** the first demo step is a `mode: ui` step explaining that the workflow is beginning and that the current step type is UI

#### Scenario: Interactive demo is framed by UI
- **WHEN** the opening UI step completes
- **THEN** the workflow renders a `mode: ui` explanation screen before starting the interactive agent demonstration

#### Scenario: Interactive demo uses planner profile
- **WHEN** the user continues from the interactive explanation screen
- **THEN** the workflow starts an interactive agent step using the `planner` profile

#### Scenario: Headless demo is framed by UI
- **WHEN** the interactive agent demonstration completes
- **THEN** the workflow renders a `mode: ui` explanation screen before starting the headless agent demonstration

#### Scenario: Headless demo uses implementor profile
- **WHEN** the user continues from the headless explanation screen
- **THEN** the workflow starts a headless agent step using the `implementor` profile

#### Scenario: Shell demo is framed by UI
- **WHEN** the headless agent demonstration completes
- **THEN** the workflow renders a `mode: ui` explanation screen before starting the shell demonstration

#### Scenario: Demo ends with summary UI
- **WHEN** the shell demonstration completes and captures its value
- **THEN** the workflow renders a final `mode: ui` summary screen that displays the captured shell value

### Requirement: Interactive demo provides light Q&A

The interactive agent demonstration SHALL teach the user how to advance to the next workflow step and SHALL support light Q&A about Agent Runner. The interactive step SHALL support completion either through the normal user-initiated continue mechanism or through agent-driven completion after a short exchange.

The Q&A agent SHALL have access to the Agent Runner documentation packaged with the installed or built product. The workflow SHALL provide the documentation location or documentation content to the agent through normal workflow prompt/context mechanisms.

#### Scenario: Interactive demo explains continuation
- **WHEN** the interactive agent demonstration starts
- **THEN** the prompt tells the agent to explain how the user advances the workflow to the next step

#### Scenario: User can ask questions
- **WHEN** the interactive agent demonstration is running
- **THEN** the user can ask lightweight Agent Runner questions before continuing the workflow

#### Scenario: Agent can use packaged documentation
- **WHEN** the Q&A agent needs reference material during the interactive demonstration
- **THEN** the packaged Agent Runner documentation is available to it through the workflow-provided context

#### Scenario: Interactive demo can finish after short exchange
- **WHEN** the user has completed a short Q&A exchange and does not manually trigger continuation
- **THEN** the workflow SHALL support completing the interactive demonstration through the normal agent-driven completion path

### Requirement: Step types demo demonstrates capture data flow

The shell demonstration SHALL emit a deterministic, non-sensitive value and capture it into a workflow variable. The final summary UI SHALL interpolate that captured value so the user sees data flow from one workflow step into a later step.

#### Scenario: Shell step captures deterministic value
- **WHEN** the shell demonstration runs
- **THEN** it emits and captures a deterministic, non-sensitive value

#### Scenario: Final summary displays captured value
- **WHEN** the final summary UI renders
- **THEN** it displays the value captured from the shell demonstration

### Requirement: Final summary can launch Q&A

The final summary UI in `step-types-demo` SHALL offer exactly two actions: `continue` and `learn_more`. Selecting `continue` SHALL allow the demo workflow to complete successfully. Selecting `learn_more` SHALL launch the interactive Q&A agent one additional time; after that Q&A agent completes, the demo workflow SHALL complete successfully.

#### Scenario: Continue completes demo
- **WHEN** the final summary UI renders and the user selects `continue`
- **THEN** the `step-types-demo` workflow completes successfully

#### Scenario: Learn More launches one additional Q&A
- **WHEN** the final summary UI renders and the user selects `learn_more`
- **THEN** the workflow launches the interactive Q&A agent one additional time

#### Scenario: Q&A completes demo
- **WHEN** the Q&A agent launched from the final summary completes
- **THEN** the `step-types-demo` workflow completes successfully

### Requirement: Step types demo is non-destructive

The `step-types-demo` onboarding workflow SHALL NOT create, modify, or delete user project files, global configuration, project configuration, user settings, temporary demo files, or external services. Its allowed side effects are limited to terminal output and the standard workflow run state, session state, and audit records produced by normal workflow execution.

#### Scenario: Demo leaves project files unchanged
- **WHEN** `step-types-demo` completes successfully
- **THEN** no files in the user's project are created, modified, or deleted by the demo steps

#### Scenario: Demo does not write configuration or settings
- **WHEN** `step-types-demo` runs
- **THEN** it does not write `~/.agent-runner/config.yaml`, project `.agent-runner/config.yaml`, or `~/.agent-runner/settings.yaml`

#### Scenario: Demo does not use temporary demo files
- **WHEN** `step-types-demo` runs
- **THEN** it does not create temporary files for demonstration purposes

#### Scenario: Demo side effects are normal run records
- **WHEN** `step-types-demo` runs
- **THEN** any persisted records are limited to standard workflow run state, session state, and audit entries

### Requirement: Step types demo failure leaves onboarding incomplete

When the `step-types-demo` sub-workflow fails or is cancelled before completing, `onboarding:welcome` SHALL NOT write `settings.onboarding.completed_at` or `settings.onboarding.dismissed`. Dispatcher-launched terminal failure or cancellation SHALL use the existing post-onboarding handoff behavior. Direct invocation SHALL use the standard direct-run post-run behavior.

#### Scenario: Demo failure does not write onboarding settings
- **WHEN** the user selects `continue`, `setup-agent-profile.yaml` completes successfully, and `step-types-demo.yaml` fails before completion
- **THEN** `settings.onboarding.completed_at` is not written and `settings.onboarding.dismissed` is not written

#### Scenario: Dispatcher-launched demo failure returns to home
- **WHEN** the dispatcher launches onboarding, the user selects `continue`, setup completes successfully, and `step-types-demo.yaml` reaches a terminal failure or cancellation
- **THEN** the runner SHALL transition to the list-runs TUI without writing onboarding settings

#### Scenario: Direct invocation with demo failure keeps standard post-run behavior
- **WHEN** the user runs `agent-runner run onboarding:welcome` directly, selects `continue`, setup completes successfully, and `step-types-demo.yaml` reaches a terminal failure or cancellation
- **THEN** the runner SHALL behave per the existing `view-run` rules and SHALL NOT auto-transition to the list-runs TUI

