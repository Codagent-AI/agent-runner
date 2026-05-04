# onboarding-workflow Specification

## Purpose
Define the first-run onboarding experience that guides new users through initial agent profile setup via a dispatcher-triggered embedded workflow with welcome, dismiss, and setup-continuation flows.
## Requirements
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

