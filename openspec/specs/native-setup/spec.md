# native-setup Specification

## Purpose
Define the native first-run setup flow that writes required agent profiles before optionally handing off to the onboarding demo.
## Requirements
### Requirement: Native setup trigger condition

Before entering the bare/list TUI entry point, the runner SHALL evaluate whether native setup should be offered. The native setup trigger SHALL fire when all of the following hold:
- `settings.setup.completed_at` is unset;
- both stdin and stdout are TTYs.

When any condition is false, the runner SHALL proceed to its normal entry point without modifying setup settings.

#### Scenario: Fresh first run starts native setup
- **WHEN** the user runs `agent-runner` with no command on a TTY and setup completion is unset
- **THEN** the runner opens the native setup TUI before starting the onboarding demo workflow or normal home screen

#### Scenario: Completed setup suppresses native setup
- **WHEN** `settings.setup.completed_at` is set
- **THEN** the native setup trigger does not fire

#### Scenario: Non-TTY does not start setup
- **WHEN** the runner starts with stdin or stdout connected to a pipe
- **THEN** native setup does not start and no setup settings are written

#### Scenario: Direct workflow run does not start setup
- **WHEN** the user runs `agent-runner run my-workflow` and setup completion is unset
- **THEN** native setup does not start before the direct workflow run

#### Scenario: Resume does not start setup
- **WHEN** the user runs `agent-runner --resume <id>` and setup completion is unset
- **THEN** native setup does not start before resume handling

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

### Requirement: Native setup implementor CLI billing disclosure

The native setup implementor CLI selection SHALL disclose Claude programmatic usage billing next to the Claude option. The disclosure SHALL appear before the user confirms the implementor CLI choice so users can choose the default implementor backend with that cost context visible.

#### Scenario: Claude implementor option shows programmatic billing disclosure
- **WHEN** native setup renders the implementor CLI selection and `claude` is an available option
- **THEN** the `claude` option includes a visible programmatic credits/API-rate billing disclosure

### Requirement: Native setup completion tracking

The runner SHALL write `settings.setup.completed_at` only after the native setup flow successfully finishes all required setup actions, including writing the selected profile configuration and running the skill installation step described in `Skill installation during native setup`.

Per-CLI failures reported by `agent-plugin` during the skill installation step SHALL NOT block the completion write. A missing `agent-plugin` binary, a failed dry-run invocation, or user cancellation at the dry-run confirmation stage SHALL block the completion write.

#### Scenario: Successful setup records completion
- **WHEN** the user completes native setup, the profile configuration write succeeds, and the skill installation step completes (including the per-CLI warning case)
- **THEN** the runner writes `settings.setup.completed_at` with the current RFC3339 timestamp using the settings atomic-write path

#### Scenario: Cancel leaves setup incomplete
- **WHEN** the user cancels native setup before the profile configuration write or at the dry-run confirmation stage
- **THEN** the runner does not write `settings.setup.completed_at`

#### Scenario: Failed write leaves setup incomplete
- **WHEN** the profile configuration write fails
- **THEN** the runner surfaces the failure and does not write `settings.setup.completed_at`

#### Scenario: Missing agent-plugin binary leaves setup incomplete
- **WHEN** the `agent-plugin` binary is not present on PATH at the skill installation step
- **THEN** the runner surfaces the missing-binary error and does not write `settings.setup.completed_at`

### Requirement: Interrupted setup restarts

Native setup SHALL NOT persist partially completed wizard progress. If setup is interrupted before completion, the next eligible launch SHALL start native setup from the beginning.

#### Scenario: Interrupted setup restarts from setup start
- **WHEN** the user starts native setup, makes one or more choices, exits before completion, and later starts Agent Runner on an eligible TTY
- **THEN** native setup starts again from the first setup surface

#### Scenario: Interrupted setup writes no tracking state
- **WHEN** native setup is interrupted before completion
- **THEN** the runner does not write `settings.setup.completed_at`

### Requirement: Native setup handoff to onboarding demo

After native setup writes the profile configuration and records `settings.setup.completed_at`, the native setup TUI SHALL present a demo prompt screen with exactly three actions: Continue, Not now, and Dismiss.

The demo prompt actions SHALL render as a horizontal button row rather than as the setup TUI's vertical option list. The Continue button SHALL be left-aligned, the Dismiss button SHALL be right-aligned, and intermediate buttons SHALL be distributed between them when terminal width allows. Left and Right keys SHALL change the focused demo prompt button. Enter SHALL activate the focused button.

- **Continue** SHALL cause native setup to return a result indicating the onboarding demo should run. The caller SHALL then launch `onboarding:onboarding`.
- **Not now** SHALL leave `settings.onboarding.completed_at` and `settings.onboarding.dismissed` unset. Native setup SHALL return normally without launching the demo.
- **Dismiss** SHALL write `settings.onboarding.dismissed` with the current RFC3339 timestamp via the settings atomic-write path. Native setup SHALL return normally without launching the demo.

The demo prompt screen SHALL only appear after a successful profile write. Cancelled, interrupted, or failed setup SHALL NOT show the demo prompt.

When native setup completes but onboarding demo completion or dismissal is already recorded in settings, the demo prompt screen SHALL be skipped.

#### Scenario: Demo prompt appears after successful write
- **WHEN** native setup writes the profile configuration and records setup.completed_at successfully
- **THEN** the native setup TUI shows the demo prompt with Continue, Not now, and Dismiss actions as horizontal buttons

#### Scenario: Demo prompt buttons support horizontal navigation
- **WHEN** the demo prompt is visible
- **AND** the user presses Left or Right
- **THEN** focus moves between the Continue, Not now, and Dismiss buttons
- **AND** Enter activates the focused button

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

### Requirement: Skill installation during native setup

After a successful profile configuration write and before recording `settings.setup.completed_at`, the runner SHALL invoke `agent-plugin` to install the Codagent skills repository for every agent CLI usable in the merged Agent Runner configuration.

The set of CLIs SHALL be derived as the union of `cli` values across every agent entry in every profile of both the user-level `~/.agent-runner/config.yaml` and the project-level `<project>/.agent-runner/config.yaml`, after the profile-write step writes the freshly selected profile. The skills repository source SHALL be `Codagent-AI/agent-skills`. The install scope flag passed to `agent-plugin` SHALL match the scope the user selected for the profile write: when the user selected `project`, the runner SHALL pass `--project`; when the user selected `user`, the runner SHALL omit `--project`.

The runner SHALL first invoke `agent-plugin` with `--dry-run` and render the planned changes in a confirmation stage of the native setup TUI. The user SHALL be able to either confirm and proceed with the real install, or cancel. Cancellation at this stage SHALL be treated as cancellation of native setup.

If `agent-plugin` is not installed on the system PATH, native setup SHALL treat the situation as a setup failure: the runner SHALL surface the error, SHALL NOT write `settings.setup.completed_at`, and the user SHALL be returned to the next eligible launch flow. If any individual CLI install fails after a successful dry-run and confirmation, the runner SHALL surface a per-CLI warning, continue with the remaining CLIs, and still record `settings.setup.completed_at`.

#### Scenario: Skills install runs between profile write and completion
- **WHEN** the user completes the scope and overwrite stages of native setup and the profile write succeeds
- **THEN** the runner invokes `agent-plugin add Codagent-AI/agent-skills` with the derived CLI list before writing `settings.setup.completed_at`

#### Scenario: CLI set derived from merged user and project config
- **WHEN** the runner prepares the `agent-plugin add` invocation during native setup
- **THEN** the CLI list SHALL be the deduplicated union of `cli` values from every agent in every profile across the user-level and project-level `config.yaml` files, including the just-written profile

#### Scenario: Plugin scope matches setup scope
- **WHEN** the user selected `project` as the setup scope and the runner invokes `agent-plugin`
- **THEN** the invocation SHALL include `--project`

#### Scenario: User scope omits project flag
- **WHEN** the user selected `user` as the setup scope and the runner invokes `agent-plugin`
- **THEN** the invocation SHALL NOT include `--project`

#### Scenario: Dry-run preview precedes real install
- **WHEN** the runner reaches the skill installation step
- **THEN** the runner SHALL invoke `agent-plugin` with `--dry-run` first and render the planned changes in a confirmation stage of the native setup TUI before invoking it without `--dry-run`

#### Scenario: User confirms install
- **WHEN** the user confirms the dry-run preview
- **THEN** the runner SHALL invoke `agent-plugin add Codagent-AI/agent-skills` with `--yes` and the derived CLI list

#### Scenario: User cancels install
- **WHEN** the user cancels at the dry-run confirmation stage
- **THEN** the runner SHALL treat the result as native setup cancellation, SHALL NOT invoke the real install, and SHALL NOT write `settings.setup.completed_at`

#### Scenario: Missing agent-plugin binary fails setup
- **WHEN** the `agent-plugin` binary is not present on PATH at the skill installation step
- **THEN** the runner SHALL surface the missing-binary error, SHALL NOT write `settings.setup.completed_at`, and native setup SHALL be offered again on the next eligible launch

#### Scenario: Per-CLI install failure is non-fatal
- **WHEN** the real `agent-plugin add` invocation reports a failure for one or more CLIs while succeeding for at least one other CLI
- **THEN** the runner SHALL surface a per-CLI warning for each failure, SHALL continue past the skill installation step, and SHALL write `settings.setup.completed_at`

#### Scenario: Total install failure does not block completion
- **WHEN** the real `agent-plugin add` invocation reports a failure for every CLI in the derived list
- **THEN** the runner SHALL surface the warnings, SHALL continue past the skill installation step, and SHALL write `settings.setup.completed_at`

### Requirement: Autonomous backend selection during setup

After the implementor CLI selection step (where the programmatic billing disclosure is shown), native setup SHALL present an "Autonomous Backend" selection screen. The screen SHALL display the three `autonomous_backend` options — Headless, Interactive, and Interactive for Claude — each with a one-sentence explanation of what the option means. The `Interactive for Claude` option SHALL be pre-selected as the recommended default. The selected value SHALL be written to `~/.agent-runner/settings.yaml` as `autonomous_backend` when setup completes successfully.

#### Scenario: Autonomous backend screen appears after implementor CLI selection
- **WHEN** the user completes the implementor CLI selection step of native setup
- **THEN** the setup presents an autonomous backend selection screen before proceeding to the next setup step

#### Scenario: Interactive for Claude is pre-selected
- **WHEN** the autonomous backend selection screen is presented
- **THEN** the `Interactive for Claude` option is pre-selected

#### Scenario: Each option has an explanation
- **WHEN** the autonomous backend selection screen is presented
- **THEN** each of the three options displays a one-sentence explanation of what the option means

#### Scenario: Selected backend is persisted on setup completion
- **WHEN** the user selects an autonomous backend value and setup completes successfully
- **THEN** `~/.agent-runner/settings.yaml` contains the selected `autonomous_backend` value

#### Scenario: Cancelled setup does not persist backend
- **WHEN** the user selects an autonomous backend value but cancels setup before completion
- **THEN** `~/.agent-runner/settings.yaml` does not contain an `autonomous_backend` key from this setup attempt

### Requirement: Autonomous permission mode selection during setup

After the autonomous backend selection step, native setup SHALL present an "Autonomous Permission Mode" selection screen. The screen SHALL display the two `autonomous_permission_mode` options — Conservative and YOLO — each with explanatory copy. The `Conservative` option SHALL be pre-selected as the recommended default. The `YOLO` option SHALL additionally display risk copy. The selected value SHALL be written to `~/.agent-runner/settings.yaml` as `autonomous_permission_mode` when setup completes successfully.

The Permission Mode screen SHALL appear before the skill installation step. Cancellation on this screen SHALL be treated identically to cancellation on the Autonomous Backend screen: no permission-mode value is persisted, no setup completion is recorded, and native setup SHALL be offered again on the next eligible launch.

#### Scenario: Permission mode screen appears after autonomous backend selection

- **WHEN** the user completes the Autonomous Backend selection step of native setup
- **THEN** the setup presents an Autonomous Permission Mode selection screen before the skill installation step

#### Scenario: Conservative is pre-selected

- **WHEN** the Autonomous Permission Mode selection screen is presented
- **THEN** the `Conservative` option is pre-selected

#### Scenario: Each option has explanatory copy

- **WHEN** the Autonomous Permission Mode selection screen is presented
- **THEN** each of the two options displays explanatory copy

#### Scenario: YOLO option shows risk copy

- **WHEN** the Autonomous Permission Mode selection screen is presented
- **THEN** the `YOLO` option additionally displays risk copy

#### Scenario: Selected permission mode is persisted on setup completion

- **WHEN** the user selects an Autonomous Permission Mode value and setup completes successfully
- **THEN** `~/.agent-runner/settings.yaml` contains the selected `autonomous_permission_mode` value

#### Scenario: Cancelled setup does not persist permission mode

- **WHEN** the user selects an Autonomous Permission Mode value but cancels setup before completion
- **THEN** `~/.agent-runner/settings.yaml` does not contain an `autonomous_permission_mode` key from this setup attempt

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

