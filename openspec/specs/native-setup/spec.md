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

The native setup TUI SHALL begin with a setup surface that offers progression into profile setup. It SHALL NOT offer skip, not-now, or dismiss actions. A user who cancels or interrupts setup leaves setup incomplete, and native setup SHALL be offered again on the next eligible launch.

#### Scenario: Continue enters setup
- **WHEN** the user chooses the continue action
- **THEN** the runner proceeds to the native agent profile setup flow

#### Scenario: Setup cannot be skipped
- **WHEN** native setup renders its first surface
- **THEN** no skip, not-now, or dismiss action is available

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

After native setup reaches a terminal state, the runner SHALL continue to the appropriate next application surface. A successful setup SHALL start or resume `onboarding:onboarding` when onboarding demo completion or dismissal has not been recorded. First-run onboarding demo run state SHALL be stored in a user-global onboarding run scope rather than under the current project's run history. Cancellation, interruption, or failure SHALL transition to the normal TUI entry point without starting the demo.

#### Scenario: Successful setup starts onboarding demo
- **WHEN** native setup completes successfully and `settings.onboarding.completed_at` and `settings.onboarding.dismissed` are unset
- **THEN** the runner starts `onboarding:onboarding`

#### Scenario: Interrupted onboarding demo resumes
- **WHEN** `settings.onboarding.completed_at` and `settings.onboarding.dismissed` are unset
- **AND** the user-global onboarding run scope has an incomplete persisted `onboarding:onboarding` run
- **THEN** the runner resumes the most recently updated incomplete onboarding run instead of starting a new onboarding run

#### Scenario: Onboarding resume is independent of current directory
- **WHEN** first-run onboarding starts from one working directory and exits before completion
- **AND** Agent Runner later starts from a different working directory with `settings.onboarding.completed_at` and `settings.onboarding.dismissed` unset
- **THEN** onboarding resumes from the incomplete user-global onboarding run

#### Scenario: Completed onboarding demo returns home
- **WHEN** the onboarding demo workflow exits successfully without an explicit app-quit request
- **THEN** the runner proceeds to the normal TUI entry point

#### Scenario: Quitting onboarding demo exits app
- **WHEN** the user explicitly quits the onboarding demo workflow with `q`, `Ctrl+C`, or confirmed top-level Escape
- **THEN** the runner exits the app instead of proceeding to the normal TUI entry point

#### Scenario: Completed onboarding demo is not repeated
- **WHEN** native setup completes successfully and `settings.onboarding.completed_at` is already set
- **THEN** the runner proceeds to the normal TUI entry point without starting `onboarding:onboarding`

#### Scenario: Dismissed onboarding demo is not repeated
- **WHEN** native setup completes successfully and `settings.onboarding.dismissed` is already set
- **THEN** the runner proceeds to the normal home TUI without starting `onboarding:onboarding`

#### Scenario: Cancelled setup goes home
- **WHEN** native setup is cancelled or fails
- **THEN** the runner proceeds to the normal home TUI without marking setup complete

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

