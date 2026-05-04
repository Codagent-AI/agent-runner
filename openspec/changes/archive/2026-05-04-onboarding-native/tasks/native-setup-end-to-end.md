# Task: Native Setup End-to-End

## Goal

Implement mandatory native first-run setup outside workflow execution. This task delivers setup tracking, reusable Go setup services, the native Bubble Tea setup flow, and startup orchestration through setup completion and demo handoff.

## Background

You MUST read these files before starting:
- `openspec/changes/onboarding-native/proposal.md` sections `Why`, `What Changes`, and `Capabilities`
- `openspec/changes/onboarding-native/design.md` sections `Approach`, `Decisions`, and `Migration Plan`
- `openspec/changes/onboarding-native/specs/native-setup/spec.md` all requirements
- `openspec/changes/onboarding-native/specs/agent-profile-editor/spec.md` requirements `User chooses CLI and model for each base agent`, `User chooses scope`, `Confirmation before write`, `Overwrite confirmation when entries already exist`, `Profile write uses shared Go writer`, and `User-initiated, never auto-generated`
- `openspec/changes/onboarding-native/specs/user-settings-file/spec.md` requirements `Native setup settings` and `Onboarding demo settings`
- `openspec/changes/onboarding-native/specs/onboarding-workflow/spec.md` requirement `First-run dispatcher trigger condition`
- `openspec/specs/agent-profile-editor/spec.md` requirements `Editor produces a fixed four-agent shape` and `One profile pass per editor session`
- `cmd/agent-runner/main.go` functions `handleListWithTab` and current onboarding dispatch wiring
- `cmd/agent-runner/internal_cmd.go` `write-profile` implementation and `writeSetting`
- `internal/usersettings/settings.go` load/save/marshal behavior
- `internal/uistep/handler.go` for existing simple picker/key handling patterns
- `internal/tuistyle/styles.go` for shared TUI styling

The design chooses a dedicated native setup package rather than embedding setup internals in `cmd/agent-runner` or the existing switcher. Use a package such as `internal/onboarding/native` for the Bubble Tea setup model, setup result types, adapter/model discovery interfaces, collision checks, and profile write orchestration. `cmd/agent-runner` should remain startup orchestration: theme gate, mandatory setup gate, optional onboarding demo gate, then home/list TUI.

Move reusable setup primitives from shell scripts into Go:
- adapter detection with `exec.LookPath` over `claude`, `codex`, `copilot`, `cursor`, `opencode`;
- model discovery subprocess wrappers for the currently supported commands, treating failure or empty output as no model choices;
- target path resolution for global/project scope;
- collision detection by parsing YAML with `yaml.v3`;
- profile writing by extracting the existing `writeProfile`/merge/atomic-write logic from `cmd/agent-runner/internal_cmd.go` into a reusable internal package used by both native setup and `agent-runner internal write-profile`.

Native setup is mandatory. It must not offer skip, not-now, or dismiss. It does not persist partial wizard state. Cancel, Ctrl-C, interruption, or write failure must leave `settings.setup.completed_at` unset, so the next eligible TUI launch starts setup from the beginning.

## Spec

### Requirement: Native setup trigger condition

Before entering any interactive TUI entry point, the runner SHALL evaluate whether native setup should be offered. The native setup trigger SHALL fire when all of the following hold:
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

### Requirement: Native setup is mandatory

The native setup TUI SHALL begin with a setup surface that offers progression into profile setup. It SHALL NOT offer skip, not-now, or dismiss actions. A user who cancels or interrupts setup leaves setup incomplete, and native setup SHALL be offered again on the next eligible launch.

#### Scenario: Continue enters setup
- **WHEN** the user chooses the continue action
- **THEN** the runner proceeds to the native agent profile setup flow

#### Scenario: Setup cannot be skipped
- **WHEN** native setup renders its first surface
- **THEN** no skip, not-now, or dismiss action is available

### Requirement: Native setup completion tracking

The runner SHALL write `settings.setup.completed_at` only after the native setup flow successfully finishes all required setup actions and writes the selected profile configuration.

#### Scenario: Successful setup records completion
- **WHEN** the user completes native setup and the profile configuration write succeeds
- **THEN** the runner writes `settings.setup.completed_at` with the current RFC3339 timestamp using the settings atomic-write path

#### Scenario: Cancel leaves setup incomplete
- **WHEN** the user cancels native setup before the profile configuration write
- **THEN** the runner does not write `settings.setup.completed_at`

#### Scenario: Failed write leaves setup incomplete
- **WHEN** the profile configuration write fails
- **THEN** the runner surfaces the failure and does not write `settings.setup.completed_at`

### Requirement: Interrupted setup restarts

Native setup SHALL NOT persist partially completed wizard progress. If setup is interrupted before completion, the next eligible launch SHALL start native setup from the beginning.

#### Scenario: Interrupted setup restarts from setup start
- **WHEN** the user starts native setup, makes one or more choices, exits before completion, and later starts Agent Runner on an eligible TTY
- **THEN** native setup starts again from the first setup surface

#### Scenario: Interrupted setup writes no tracking state
- **WHEN** native setup is interrupted before completion
- **THEN** the runner does not write `settings.setup.completed_at`

### Requirement: Native setup handoff to onboarding demo

After native setup reaches a terminal state, the runner SHALL continue to the appropriate next application surface. A successful setup SHALL start `onboarding:onboarding` when onboarding demo completion or dismissal has not been recorded. Cancellation, interruption, or failure SHALL transition to the normal TUI entry point without starting the demo.

#### Scenario: Successful setup starts onboarding demo
- **WHEN** native setup completes successfully and `settings.onboarding.completed_at` and `settings.onboarding.dismissed` are unset
- **THEN** the runner starts `onboarding:onboarding`

#### Scenario: Completed onboarding demo is not repeated
- **WHEN** native setup completes successfully and `settings.onboarding.completed_at` is already set
- **THEN** the runner proceeds to the normal TUI entry point without starting `onboarding:onboarding`

#### Scenario: Dismissed onboarding demo is not repeated
- **WHEN** native setup completes successfully and `settings.onboarding.dismissed` is already set
- **THEN** the runner proceeds to the normal home TUI without starting `onboarding:onboarding`

#### Scenario: Cancelled setup goes home
- **WHEN** native setup is cancelled or fails
- **THEN** the runner proceeds to the normal home TUI without marking setup complete

### Requirement: User chooses CLI and model for each base agent

The native setup profile editor SHALL prompt the user to choose a CLI adapter and a model for `interactive_base`, and separately for `headless_base`. CLI options SHALL be drawn at runtime from the existing adapter detection behavior. Model options SHALL be discovered at runtime by querying the chosen CLI adapter for available models. When the CLI does not support model listing or model discovery returns an empty list, the model selection step SHALL be skipped and the model field SHALL be written as empty, meaning adapter default.

#### Scenario: CLI options reflect detected adapters
- **WHEN** the host has `claude` and `codex` on `$PATH` but not `cursor`
- **THEN** the native setup CLI selection screens for both `interactive_base` and `headless_base` present `claude` and `codex` as the only options

#### Scenario: Model options reflect chosen CLI
- **WHEN** the user picks `cli: claude` for `interactive_base` and model discovery returns `["opus", "sonnet", "haiku"]`
- **THEN** the native setup model selection screen presents only those discovered models

#### Scenario: Model discovery returns empty
- **WHEN** the user picks a CLI adapter whose model discovery returns an empty list
- **THEN** the model selection step is skipped and the model field is written as empty

#### Scenario: No detected adapters
- **WHEN** adapter detection returns an empty list
- **THEN** native setup fails the CLI selection step with an error indicating no supported CLI adapters were found on `$PATH`

### Requirement: User chooses scope

The native setup profile editor SHALL prompt the user to choose `global` or `project`. `global` SHALL write to `~/.agent-runner/config.yaml`. `project` SHALL write to `.agent-runner/config.yaml` resolved against the runner's working directory. The runner SHALL NOT inspect the cwd for project markers; whichever cwd the user invoked from is the project location.

#### Scenario: Global scope writes to home
- **WHEN** the user picks scope `global`
- **THEN** the write target is `~/.agent-runner/config.yaml`

#### Scenario: Project scope writes to cwd
- **WHEN** the user picks scope `project` and the runner was invoked from `/path/to/project`
- **THEN** the write target is `/path/to/project/.agent-runner/config.yaml`

#### Scenario: Project scope without project markers
- **WHEN** the user picks scope `project` and the cwd has no `.git` directory or other project marker
- **THEN** the write proceeds without warning and the file is created at `<cwd>/.agent-runner/config.yaml`

### Requirement: Confirmation before write

The native setup profile editor SHALL present a confirmation screen showing the chosen CLIs, models, and scope before invoking the write path. The user MAY cancel from this screen; cancellation SHALL leave profile configuration unchanged and leave native setup incomplete.

#### Scenario: Confirm proceeds to write
- **WHEN** the user reviews the confirmation screen and selects confirm
- **THEN** native setup invokes the profile write path

#### Scenario: Cancel from confirmation
- **WHEN** the user reviews the confirmation screen and selects cancel
- **THEN** no profile file is created or modified and native setup is not marked complete

### Requirement: Overwrite confirmation when entries already exist

Before writing, the native setup profile editor SHALL inspect the chosen scope's config file, if it exists, for any of the four entries `interactive_base`, `headless_base`, `planner`, or `implementor` under `profiles.default.agents`. If any are present, the editor SHALL display an additional confirmation screen naming the colliding entries and offering an `overwrite` action and a `cancel` action. The `overwrite` action SHALL proceed with the write, replacing those entries. The `cancel` action SHALL leave the file unchanged and leave native setup incomplete.

#### Scenario: No collisions, no overwrite screen
- **WHEN** the chosen config file does not exist or contains no entry named `interactive_base`, `headless_base`, `planner`, or `implementor` under `profiles.default.agents`
- **THEN** the editor proceeds directly from confirmation to write with no overwrite screen

#### Scenario: Existing planner triggers overwrite screen
- **WHEN** the chosen config file already contains a `planner` entry under `profiles.default.agents`
- **THEN** native setup shows an overwrite confirmation naming `planner` and any other colliding entries

#### Scenario: User cancels overwrite
- **WHEN** the overwrite confirmation screen is shown and the user selects cancel
- **THEN** no profile file is modified and native setup is not marked complete

#### Scenario: User overwrites
- **WHEN** the overwrite confirmation screen is shown and the user selects overwrite
- **THEN** the write proceeds and the four editor-managed entries replace any pre-existing entries of the same name

### Requirement: Profile write uses shared Go writer

The native setup profile editor SHALL use the tested internal Go profile-writing path directly. The existing `agent-runner internal write-profile` subcommand SHALL remain a wrapper around that same shared writer. User-selected values SHALL NOT enter a YAML emitter inside a shell script. The shared writer SHALL:
- Accept its inputs as structured data for interactive cli/model, headless cli/model, and target path.
- Read the existing file, if any, parse it, and merge the four entries into `profiles.default.agents` while preserving any other agents, other profile sets, and any other top-level keys.
- Write the result atomically using a temp file and rename in the same directory.
- Set the resulting file mode to `0o600` and create any missing parent directories with mode `0o755`.

#### Scenario: Existing other agents preserved
- **WHEN** the chosen config file already contains an agent `team_implementor` under `profiles.default.agents` and unrelated profile sets `team` and `prod` and the editor writes the four-agent shape with overwrite accepted for any collisions
- **THEN** the resulting file contains the four editor-written entries and still contains `team_implementor`, `team`, and `prod` unchanged

#### Scenario: Atomic write
- **WHEN** the write operation is interrupted or fails part-way through writing
- **THEN** the original file, if any, remains intact and no partial file is left at the target path

#### Scenario: File mode and parent directory creation
- **WHEN** the write target is `~/.agent-runner/config.yaml` and `~/.agent-runner/` does not exist
- **THEN** the directory is created with mode `0o755` and the file is written with mode `0o600`

#### Scenario: Internal command remains wrapper
- **WHEN** `agent-runner internal write-profile` is invoked with a valid payload
- **THEN** it writes through the same shared Go writer used by native setup

#### Scenario: Shell-side YAML rejected by tests
- **WHEN** the native setup profile editor implementation is examined by automated tests
- **THEN** the tests confirm user-selected values are not rendered into YAML by shell script string construction

### Requirement: User-initiated, never auto-generated

The editor SHALL run only from a user-facing setup action. Native first-run setup MAY offer the editor when setup has not completed or been dismissed, but the runner SHALL NOT silently generate profile configuration without the user continuing through setup and confirming the write. The workflow entry point `onboarding:setup-agent-profile` is no longer required for this capability.

#### Scenario: Editor only runs from setup interaction
- **WHEN** the runner starts on a host with no config file and native setup is not eligible
- **THEN** no profile file is created

#### Scenario: Native setup invokes editor
- **WHEN** the user chooses continue in native setup
- **THEN** the native setup TUI collects profile choices and can invoke the profile write path after confirmation

#### Scenario: Old setup workflow is not required
- **WHEN** the user runs `agent-runner run onboarding:setup-agent-profile`
- **THEN** the agent profile editor capability does not require that workflow to exist

### Requirement: Editor produces a fixed four-agent shape

The editor SHALL write exactly four agent entries under `profiles.default.agents` in the chosen config file: `interactive_base`, `headless_base`, `planner`, `implementor`. `interactive_base` SHALL include `default_mode: interactive` and the user-chosen `cli` and `model`. `headless_base` SHALL include `default_mode: headless` and the user-chosen `cli` and `model`. `planner` SHALL declare `extends: interactive_base` and SHALL NOT include any other field. `implementor` SHALL declare `extends: headless_base` and SHALL NOT include any other field. The editor SHALL NOT write `summarizer` or any other agent.

#### Scenario: Successful first-time write
- **WHEN** the user picks `cli: claude, model: opus` for interactive_base and `cli: codex, model: gpt-5` for headless_base, scope `global`, and confirms the write
- **THEN** `~/.agent-runner/config.yaml` contains exactly the four entries: `interactive_base` with `default_mode: interactive, cli: claude, model: opus`; `headless_base` with `default_mode: headless, cli: codex, model: gpt-5`; `planner` with only `extends: interactive_base`; `implementor` with only `extends: headless_base`

#### Scenario: Editor does not write summarizer
- **WHEN** the editor completes a successful write
- **THEN** the resulting `profiles.default.agents` map contains no `summarizer` entry from the editor; if a `summarizer` entry existed in the file before, it is preserved unchanged

### Requirement: One profile pass per editor session

A single editor session SHALL produce one set of writes. The editor SHALL NOT loop back from the confirmation screen to re-collect choices within the same session in this version. To revise choices, the user re-runs the editor.

#### Scenario: User wants to revise after confirming
- **WHEN** the user has reached the confirmation screen and wants to change a previous choice
- **THEN** the only path is to cancel the current session and re-run the editor; the session does not offer a "back" navigation

### Requirement: Native setup settings

The user settings schema SHALL support native setup tracking under a `setup` mapping. `setup.completed_at` records successful setup completion. When set, it SHALL be an RFC3339 timestamp and SHALL be preserved by settings load and save operations. Native setup SHALL NOT define a setup dismissal setting.

#### Scenario: Completed setup timestamp loads
- **WHEN** `~/.agent-runner/settings.yaml` contains `setup.completed_at: 2026-05-03T00:00:00Z`
- **THEN** settings load exposes that timestamp to native setup dispatch logic

#### Scenario: Setup timestamp is preserved on write
- **WHEN** the runner writes unrelated settings
- **THEN** existing `setup.completed_at` is preserved unless the caller explicitly changes it

#### Scenario: Setup dismissed key is ignored
- **WHEN** `~/.agent-runner/settings.yaml` contains `setup.dismissed: 2026-05-03T00:00:00Z`
- **THEN** native setup dispatch does not treat that value as suppressing mandatory setup

### Requirement: Onboarding demo settings

The user settings schema SHALL continue to support onboarding demo completion under `onboarding.completed_at` and onboarding demo dismissal under `onboarding.dismissed`. `onboarding.completed_at` records successful completion of the onboarding demo workflow. `onboarding.dismissed` records explicit dismissal of the optional onboarding demo. Both settings SHALL be distinct from native setup completion.

#### Scenario: Completed onboarding timestamp loads
- **WHEN** `~/.agent-runner/settings.yaml` contains `onboarding.completed_at: 2026-05-03T00:00:00Z`
- **THEN** settings load exposes that timestamp to onboarding demo dispatch logic

#### Scenario: Dismissed onboarding timestamp loads
- **WHEN** `~/.agent-runner/settings.yaml` contains `onboarding.dismissed: 2026-05-03T00:00:00Z`
- **THEN** settings load exposes that timestamp to onboarding demo dispatch logic

#### Scenario: Setup and onboarding completion are independent
- **WHEN** settings contain both `setup.completed_at` and `onboarding.completed_at`
- **THEN** settings load exposes both timestamps independently

#### Scenario: Onboarding completion preserved on setup write
- **WHEN** the runner writes setup tracking settings
- **THEN** existing `onboarding.completed_at` is preserved unless the caller explicitly changes it

## Done When

Targeted tests cover the native setup scenarios, profile editor scenarios, and settings scenarios above. `cmd/agent-runner` startup orchestration runs native setup before home when setup completion is missing, records setup completion only after a successful profile write, and launches `onboarding:onboarding` only when demo completion/dismissal is unset.
