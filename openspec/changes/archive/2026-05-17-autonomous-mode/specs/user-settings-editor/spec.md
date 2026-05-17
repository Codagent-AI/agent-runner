## MODIFIED Requirements

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

#### Scenario: Save with a theme change
- **WHEN** the user opens the editor with `Light` selected, moves to `Dark`, and presses Enter
- **THEN** `~/.agent-runner/settings.yaml` is written with `theme: dark`, `lipgloss.SetHasDarkBackground(true)` is invoked, the run list immediately re-renders with the Dark variant of every adaptive color token, and the editor closes

#### Scenario: Save with an autonomous backend change
- **WHEN** the user opens the editor with `Headless` selected for Autonomous Backend, moves to `Interactive for Claude`, and presses Enter
- **THEN** `~/.agent-runner/settings.yaml` is written with `autonomous_backend: interactive-claude` and the editor closes

#### Scenario: Save preserves unrelated settings
- **WHEN** the file contains `setup.completed_at`, `onboarding.completed_at`, and `onboarding.dismissed` before save, and the user saves a change
- **THEN** all three lifecycle keys are still present with their original values after save

#### Scenario: Save failure surfaces inline and keeps the editor open
- **WHEN** the user presses Enter and writing `~/.agent-runner/settings.yaml` fails (e.g., permission denied)
- **THEN** the editor remains open, displays an inline error message identifying the file path and the underlying error, and does NOT apply any changes to the running TUI

## ADDED Requirements

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
