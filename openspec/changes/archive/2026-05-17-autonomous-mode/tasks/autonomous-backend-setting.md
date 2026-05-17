# Task: Autonomous backend setting + editor + routing + setup

## Goal

Add the `autonomous_backend` user setting that controls how autonomous steps are invoked (headless vs interactive), expose it in the settings editor and native setup wizard, and implement the routing logic that selects the invocation context based on the setting, adapter, and TTY availability.

## Background

The previous task introduced the `InvocationContext` type with three values (interactive, autonomous-headless, autonomous-interactive) and updated all adapters to handle them. Currently, all autonomous steps produce `ContextAutonomousHeadless`. This task adds the mechanism for users to choose the interactive backend and implements the routing that produces `ContextAutonomousInteractive` when appropriate.

**Why this matters:** Starting 2026-06-15, Anthropic bills programmatic/headless Claude usage at API rates. Running autonomous Claude steps via an interactive session with autonomy instructions in the system prompt avoids this billing entirely.

**Key files and what to change:**

### Settings (load/save/validation)

- `internal/usersettings/settings.go` — Add a named string type `AutonomousBackend` with three constants:
  ```go
  BackendHeadless          AutonomousBackend = "headless"
  BackendInteractive       AutonomousBackend = "interactive"
  BackendInteractiveClaude AutonomousBackend = "interactive-claude"
  ```
  Add `AutonomousBackend AutonomousBackend` field to the `Settings` struct. Follow the existing `Theme` pattern: the field is loaded from the YAML node tree and saved back via the same node-based marshal/unmarshal that preserves unknown keys. When absent from the file, `Load()` returns `BackendHeadless` as the default. When present with an invalid value, `Load()` returns a validation error.

- `internal/usersettings/settings_test.go` — Add tests for: valid value loads, absent key defaults to headless, invalid value produces error, value preserved on unrelated write.

### Settings editor (multi-field generalization)

- `internal/settingseditor/editor.go` — The editor is currently hardcoded to a single `Theme` field. Generalize it to support multiple fields with flat-list navigation:
  - Replace the `selected usersettings.Theme` field with an integer cursor index over all options across both fields. The flat list is: 0=Light, 1=Dark, 2=Headless, 3=Interactive, 4=Interactive for Claude.
  - Define fields as an ordered slice of structs, each containing a label and its options. Field 0 is "Theme" with options [Light, Dark]. Field 1 is "Autonomous Backend" with options [Headless, Interactive, Interactive for Claude].
  - The `move(delta int)` function uses `(cursor + delta + total) % total` to wrap at boundaries.
  - Rendering iterates the field slice, inserting a label before each option group. The cursor position maps to concrete field values via index arithmetic.
  - The `New()` constructor computes the initial cursor position from both the persisted theme and persisted autonomous backend.
  - `saveSelected()` writes both theme and autonomous backend to the Settings struct.

- `internal/settingseditor/editor_test.go` — Add tests for: multi-field navigation (down from Dark moves to Headless), wrap-around (down from Interactive for Claude wraps to Light), save persists both fields, pre-selection from persisted values.

### Routing (executor)

- `internal/model/context.go` — Add `AutonomousBackend string` field to `ExecutionContext`.

- `internal/runner/runner.go` — At run start, load user settings via `usersettings.Load()` and set `ctx.AutonomousBackend` to the loaded value. The runner already loads settings for other purposes; find where `ExecutionContext` is initialized and add the field there.

- `internal/exec/agent.go` — After resolving the step mode to `ModeAutonomous`, compute the `InvocationContext` using the routing logic:
  1. If mode is `interactive` → `ContextInteractive`
  2. If mode is `autonomous`:
     a. Check `ctx.AutonomousBackend`:
        - `"interactive"` → wants interactive for all adapters
        - `"interactive-claude"` → wants interactive only if the adapter is Claude (check the adapter's CLI name)
        - `"headless"` or empty → wants headless
     b. If wants interactive AND `term.IsTerminal(os.Stdin.Fd())` → `ContextAutonomousInteractive`
     c. If wants interactive but no TTY → `ContextAutonomousHeadless` + log a warning
     d. Otherwise → `ContextAutonomousHeadless`

  `golang.org/x/term` is already a dependency. The TTY check is per-step (checked at dispatch time for each step).

  For system prompt enrichment: rename `headlessPreamble` to `autonomyPreamble` (if not already done by the previous task). For `ContextAutonomousInteractive`, prepend the preamble plus continuation signal instructions (the `<<DONE>>` marker used by the existing interactive step continuation mechanism). For `ContextAutonomousHeadless`, prepend the preamble as today. For `ContextInteractive`, no preamble.

### Native setup

- The native setup TUI code is in `internal/onboarding/native/` (imported as `nativesetup`), with orchestration in `cmd/agent-runner/main.go` (the `firstRunDeps` struct and `ensureFirstRunForTUI` function). After the implementor CLI selection step (where the programmatic billing disclosure is shown), add an "Autonomous Backend" selection screen. The screen presents the three options — Headless, Interactive, Interactive for Claude — each with a one-sentence explanation:
  - **Headless**: Runs the agent in non-interactive print mode (default, uses API billing for Claude)
  - **Interactive**: Runs the agent in an interactive session with autonomy instructions
  - **Interactive for Claude**: Uses interactive mode for Claude only; other CLIs use headless (recommended, avoids API billing)

  `Interactive for Claude` is pre-selected. The selected value is written to `settings.yaml` alongside `setup.completed_at` when setup completes. Cancelled setup does not persist the value.

**Conventions:**
- This project uses `google/go-cmp` for structured comparisons in tests.
- Run `make fmt` (goimports) after changes.
- Tests live next to source packages.

## Spec

### Requirement: Autonomous backend setting
The user settings schema SHALL support an `autonomous_backend` top-level key that controls how autonomous agent steps are invoked. Valid values are `headless`, `interactive`, and `interactive-claude`. When the key is absent from the file, the loader SHALL expose a default value of `headless`. When the key is present with a value not in the valid set, settings load SHALL fail with a validation error identifying the invalid value and the valid options.

#### Scenario: Valid autonomous_backend loads
- **WHEN** `~/.agent-runner/settings.yaml` contains `autonomous_backend: interactive-claude`
- **THEN** settings load exposes `interactive-claude` as the autonomous backend value

#### Scenario: Absent autonomous_backend defaults to headless
- **WHEN** `~/.agent-runner/settings.yaml` exists but does not contain an `autonomous_backend` key
- **THEN** settings load exposes `headless` as the autonomous backend value

#### Scenario: Invalid autonomous_backend rejected
- **WHEN** `~/.agent-runner/settings.yaml` contains `autonomous_backend: magic`
- **THEN** settings load fails with a validation error identifying `magic` as invalid and listing the valid values

#### Scenario: Autonomous backend preserved on unrelated write
- **WHEN** the runner writes unrelated settings (e.g., theme change) and `autonomous_backend: interactive` is already in the file
- **THEN** the existing `autonomous_backend` value is preserved

### Requirement: Editor exposes editable user settings

The user-settings editor SHALL present every user-editable setting from `~/.agent-runner/settings.yaml` (as defined by the `user-settings-file` capability) and SHALL NOT present lifecycle keys that the app manages on the user's behalf. The set of user-editable settings SHALL be `theme` and `autonomous_backend`, presented in that order. Lifecycle keys under `setup` and `onboarding` SHALL NOT be presented.

#### Scenario: Theme is presented
- **WHEN** the editor is open
- **THEN** the editor renders a `Theme` field with the two choices `Light` and `Dark`

#### Scenario: Autonomous Backend is presented
- **WHEN** the editor is open
- **THEN** the editor renders an `Autonomous Backend` field with the three choices `Headless`, `Interactive`, and `Interactive for Claude`, appearing below the Theme field

#### Scenario: Lifecycle keys are not presented
- **WHEN** the editor is open and `~/.agent-runner/settings.yaml` contains `setup.completed_at`, `onboarding.completed_at`, or `onboarding.dismissed`
- **THEN** none of those values are shown in the editor and the editor does not provide any control to modify them

### Requirement: Editor keyboard model

The editor SHALL accept the following keys while it is open:

- Up / Down SHALL move through all options across all fields in a flat list (Light, Dark, Headless, Interactive, Interactive for Claude). Up from the first option wraps to the last; Down from the last wraps to the first.
- Left / Right SHALL behave as Up and Down respectively.
- Tab and Shift+Tab SHALL behave as Down and Up respectively.
- Enter SHALL trigger save.
- Esc SHALL trigger cancel.
- Ctrl+C SHALL behave as it does globally elsewhere in the TUI: quit the program. The editor SHALL NOT intercept Ctrl+C.

All other keys SHALL be ignored by the editor and SHALL NOT be forwarded to the underlying run list.

#### Scenario: Arrow keys move across fields
- **WHEN** the editor is open with `Dark` selected and the user presses Down
- **THEN** the option cursor moves to `Headless` (the first option of the next field)

#### Scenario: Arrow keys wrap around
- **WHEN** the editor is open with `Interactive for Claude` selected (last option) and the user presses Down
- **THEN** the option cursor wraps to `Light` (the first option of the first field)

#### Scenario: Tab acts like Down
- **WHEN** the editor is open with `Dark` selected and the user presses Tab
- **THEN** the option cursor moves to `Headless`

#### Scenario: Unrelated keys are swallowed
- **WHEN** the editor is open and the user presses `r`, `n`, `c`, `?`, or another list-level shortcut
- **THEN** the key has no effect; the run list does not act on it

#### Scenario: Ctrl+C still quits
- **WHEN** the editor is open and the user presses Ctrl+C
- **THEN** the program exits as it would from any other TUI screen

### Requirement: Save persists settings, applies theme, and closes the editor

When the user presses Enter, the editor SHALL persist all editor-visible settings to `~/.agent-runner/settings.yaml` via the write path defined by the `user-settings-file` capability, SHALL apply any changed runtime-affecting setting (theme and autonomous backend) so they take effect without restart, and SHALL close itself so the underlying run list is re-displayed. The save SHALL preserve unrelated keys in the file (e.g., `setup.*`, `onboarding.*`) untouched.

#### Scenario: Save with no change
- **WHEN** the user opens the editor and presses Enter without moving the cursor
- **THEN** the file is written with the same values, no visible change occurs, and the editor closes

#### Scenario: Save with an autonomous backend change
- **WHEN** the user opens the editor with `Headless` selected for Autonomous Backend, moves to `Interactive for Claude`, and presses Enter
- **THEN** `~/.agent-runner/settings.yaml` is written with `autonomous_backend: interactive-claude` and the editor closes

#### Scenario: Save preserves unrelated settings
- **WHEN** the file contains `setup.completed_at`, `onboarding.completed_at`, and `onboarding.dismissed` before save, and the user saves a change
- **THEN** all three lifecycle keys are still present with their original values after save

#### Scenario: Save failure surfaces inline and keeps the editor open
- **WHEN** the user presses Enter and writing `~/.agent-runner/settings.yaml` fails (e.g., permission denied)
- **THEN** the editor remains open, displays an inline error message identifying the file path and the underlying error, and does NOT apply any changes to the running TUI

### Requirement: Autonomous Backend field pre-selection reflects the currently persisted value

When the editor opens, the cursor on the `Autonomous Backend` field SHALL be pre-selected to the value currently persisted in `~/.agent-runner/settings.yaml`. When the key is absent (defaulting to `headless`), the `Headless` option SHALL be pre-selected.

#### Scenario: Persisted backend is interactive-claude
- **WHEN** the editor opens and `~/.agent-runner/settings.yaml` contains `autonomous_backend: interactive-claude`
- **THEN** the `Interactive for Claude` option is pre-selected

#### Scenario: Persisted backend is absent
- **WHEN** the editor opens and `~/.agent-runner/settings.yaml` does not contain an `autonomous_backend` key
- **THEN** the `Headless` option is pre-selected

#### Scenario: Re-opening after a mid-session change reflects the new value
- **WHEN** the user opens the editor, changes autonomous backend from `Headless` to `Interactive`, saves, and then re-opens the editor
- **THEN** the `Interactive` option is pre-selected on the second open

### Requirement: Autonomy system prompt enrichment for interactive backend

When the invocation context is autonomous-interactive, the runner SHALL prepend autonomy instructions to the step's system prompt before passing it to the adapter. The instructions SHALL direct the agent to work autonomously without asking for human input and to signal continuation when done using the same continuation mechanism that interactive steps use. The autonomy instructions SHALL be prepended before any engine enrichment or step-level system prompt content.

#### Scenario: Autonomy instructions prepended in autonomous-interactive context
- **WHEN** the runner prepares system prompt content for an autonomous-interactive step
- **THEN** the system prompt begins with autonomy instructions followed by any agent-level, step-level, and engine-provided content

#### Scenario: No continuation-signal instructions in autonomous-headless context
- **WHEN** the runner prepares system prompt content for an autonomous-headless step
- **THEN** the existing headless preamble is prepended as before, but no continuation-signal autonomy instructions are added (the headless backend exits on completion rather than signalling)

#### Scenario: No autonomy instructions in interactive context
- **WHEN** the runner prepares system prompt content for an interactive step
- **THEN** no autonomy instructions are prepended (the human supervises directly)

### Requirement: TTY fallback for autonomous-interactive

When the runner determines that the invocation context should be autonomous-interactive (based on the step mode and the `autonomous_backend` setting) but no TTY is available, the runner SHALL fall back to autonomous-headless for that step and SHALL log a warning indicating the fallback occurred and the reason. The fallback SHALL be per-step, not global.

#### Scenario: No TTY triggers fallback to headless
- **WHEN** the `autonomous_backend` setting is `interactive` and the runner is executing an autonomous step without a TTY (e.g., in CI or Docker)
- **THEN** the runner invokes the step as autonomous-headless and logs a warning

#### Scenario: TTY available uses interactive backend as configured
- **WHEN** the `autonomous_backend` setting is `interactive` and the runner is executing an autonomous step with a TTY available
- **THEN** the runner invokes the step as autonomous-interactive

#### Scenario: Fallback is per-step
- **WHEN** a run contains two autonomous steps, one with TTY available and one without
- **THEN** the step without TTY falls back to autonomous-headless while the step with TTY uses autonomous-interactive

#### Scenario: Interactive-claude backend with non-Claude adapter
- **WHEN** the `autonomous_backend` setting is `interactive-claude` and the adapter is Codex (not Claude)
- **THEN** the runner invokes the step as autonomous-headless regardless of TTY availability

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

## Done When

- `AutonomousBackend` type and field exist on `Settings` with load/save/validation working
- Settings editor supports two fields with flat-list navigation, pre-selection, and save
- `ExecutionContext.AutonomousBackend` is populated from settings at run start
- Routing logic in the executor correctly produces the three invocation contexts based on setting, adapter, and TTY
- System prompt enrichment includes continuation instructions for autonomous-interactive
- TTY fallback logs a warning when falling back to headless
- Native setup presents the autonomous backend selection screen after implementor CLI
- Tests covering all above scenarios pass
- `make test` and `make lint` pass
