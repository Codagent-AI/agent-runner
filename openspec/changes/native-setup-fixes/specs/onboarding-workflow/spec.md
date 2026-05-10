## MODIFIED Requirements

### Requirement: First-run dispatcher trigger condition

The first-run dispatcher SHALL NOT auto-launch `onboarding:onboarding`. The demo prompt (Continue / Not now / Dismiss) is now owned by the native setup TUI and the demo-prompt re-show trigger (see native-setup spec). When the demo prompt's Continue action fires, the caller SHALL launch `onboarding:onboarding` as a workflow run.

#### Scenario: Dispatcher does not auto-launch onboarding
- **WHEN** `settings.setup.completed_at` is set and onboarding is eligible
- **THEN** the runner does not auto-launch `onboarding:onboarding`; the demo prompt (owned by native setup) handles the user's choice

#### Scenario: Continue from demo prompt launches workflow
- **WHEN** the demo prompt fires (from native setup or re-show trigger) and the user selects Continue
- **THEN** the caller launches `onboarding:onboarding` as a workflow run

### Requirement: Onboarding demo intro actions

The `onboarding:onboarding` workflow SHALL NOT contain an intro step with Continue / Not now / Dismiss actions. The demo prompt is owned by the native setup TUI. The onboarding workflow SHALL begin directly with the `step-types-demo` sub-workflow invocation.

#### Scenario: Workflow starts at step-types-demo
- **WHEN** `onboarding:onboarding` starts
- **THEN** the first step invokes the `step-types-demo` sub-workflow (no intro UI step)

#### Scenario: Workflow does not handle dismiss or not-now
- **WHEN** `onboarding:onboarding` runs
- **THEN** the workflow contains no dismiss or not-now action handling; those are handled before the workflow is launched

## REMOVED Requirements

### Requirement: Onboarding demo intro actions — dismiss and not-now steps

**Reason:** The `set-dismissed` shell step and the `skip_if` conditionals based on `demo_action` are no longer needed. Dismiss is handled by native setup before the workflow is launched. Not-now prevents the workflow from launching entirely.

**Migration:** Remove the `intro`, `set-dismissed` steps and `demo_action` capture from `workflows/onboarding/onboarding.yaml`. The workflow becomes: `step-types-demo` → `set-completed`.
