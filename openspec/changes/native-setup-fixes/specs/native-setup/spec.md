## MODIFIED Requirements

### Requirement: Native setup is mandatory

Native setup SHALL NOT present a welcome or intro screen. When the native setup trigger fires, the runner SHALL immediately detect available CLI adapters and present the first selection screen (planner CLI). If adapter detection fails, the failure surface SHALL be the first thing the user sees. A user who cancels or interrupts setup leaves setup incomplete, and native setup SHALL be offered again on the next eligible launch.

#### Scenario: Native setup starts at CLI selection
- **WHEN** native setup fires and adapter detection succeeds
- **THEN** the first surface is the planner CLI selection screen with detected adapters as options

#### Scenario: No welcome or intro screen
- **WHEN** native setup fires
- **THEN** no introductory, welcome, or continue-gate screen is shown before the first selection

#### Scenario: Adapter detection failure is the first surface
- **WHEN** native setup fires and adapter detection fails (no CLIs found)
- **THEN** the failure message is the first surface the user sees

### Requirement: Native setup handoff to onboarding demo

After native setup writes the profile configuration and records `settings.setup.completed_at`, the native setup TUI SHALL present a demo prompt screen with exactly three actions: Continue, Not now, and Dismiss.

- **Continue** SHALL cause native setup to return a result indicating the onboarding demo should run. The caller SHALL then launch `onboarding:onboarding`.
- **Not now** SHALL leave `settings.onboarding.completed_at` and `settings.onboarding.dismissed` unset. Native setup SHALL return normally without launching the demo.
- **Dismiss** SHALL write `settings.onboarding.dismissed` with the current RFC3339 timestamp via the settings atomic-write path. Native setup SHALL return normally without launching the demo.

The demo prompt screen SHALL only appear after a successful profile write. Cancelled, interrupted, or failed setup SHALL NOT show the demo prompt.

When native setup completes but onboarding demo completion or dismissal is already recorded in settings, the demo prompt screen SHALL be skipped.

#### Scenario: Demo prompt appears after successful write
- **WHEN** native setup writes the profile configuration and records setup.completed_at successfully
- **THEN** the native setup TUI shows the demo prompt with Continue, Not now, and Dismiss actions

#### Scenario: Continue launches onboarding demo
- **WHEN** the user selects Continue on the demo prompt
- **THEN** the caller launches `onboarding:onboarding`

#### Scenario: Not now leaves onboarding settings unset
- **WHEN** the user selects Not now on the demo prompt
- **THEN** neither `settings.onboarding.completed_at` nor `settings.onboarding.dismissed` is written, and the runner proceeds to the normal home TUI

#### Scenario: Dismiss writes onboarding.dismissed
- **WHEN** the user selects Dismiss on the demo prompt
- **THEN** `settings.onboarding.dismissed` is written with the current RFC3339 timestamp and the runner proceeds to the normal home TUI

#### Scenario: Cancelled setup does not show demo prompt
- **WHEN** native setup is cancelled before the profile write
- **THEN** no demo prompt is shown and the runner proceeds to the normal home TUI

#### Scenario: Demo prompt skipped when onboarding already completed
- **WHEN** native setup completes and `settings.onboarding.completed_at` is already set
- **THEN** the demo prompt is skipped and the runner proceeds to the normal home TUI

#### Scenario: Demo prompt skipped when onboarding already dismissed
- **WHEN** native setup completes and `settings.onboarding.dismissed` is already set
- **THEN** the demo prompt is skipped and the runner proceeds to the normal home TUI

### Requirement: Demo prompt re-show on launch

When entering the bare/list TUI entry point, the runner SHALL evaluate whether the demo prompt should be re-shown. The demo prompt re-show trigger SHALL fire when all of the following hold:
- `settings.setup.completed_at` is set;
- `settings.onboarding.completed_at` is unset;
- `settings.onboarding.dismissed` is unset;
- both stdin and stdout are TTYs.

When the trigger fires, the runner SHALL present the same demo prompt screen (Continue / Not now / Dismiss) with the same behavior as during native setup. This replaces the previous behavior of auto-launching `onboarding:onboarding` directly.

#### Scenario: Demo prompt re-shown after Not now
- **WHEN** the user previously selected Not now, and the runner starts on an eligible TTY
- **THEN** the demo prompt screen is shown again with Continue, Not now, and Dismiss

#### Scenario: Demo prompt not re-shown after Dismiss
- **WHEN** the user previously selected Dismiss
- **THEN** the demo prompt does not appear on subsequent launches

#### Scenario: Demo prompt not re-shown after completed demo
- **WHEN** the user previously completed the onboarding demo
- **THEN** the demo prompt does not appear on subsequent launches

## REMOVED Requirements

### Requirement: Setup cannot be skipped (scenario removed)

**Reason:** The welcome screen with its Continue-only action list no longer exists. The "cannot be skipped" intent is preserved by the fact that there is no skip, not-now, or dismiss action on any of the CLI/model/scope selection screens — only selection and cancel/esc.

**Migration:** Remove the `TestWelcomeSurfaceHasNoSkipDismissOrNotNow` test or rewrite it to assert that selection screens have no skip/dismiss actions.
