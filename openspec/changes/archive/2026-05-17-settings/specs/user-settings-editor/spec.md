## ADDED Requirements

### Requirement: Editor exposes editable user settings

The user-settings editor SHALL present every user-editable setting from `~/.agent-runner/settings.yaml` (as defined by the `user-settings-file` capability) and SHALL NOT present lifecycle keys that the app manages on the user's behalf. The set of user-editable settings SHALL initially be exactly `theme`. Lifecycle keys under `setup` and `onboarding` SHALL NOT be presented.

#### Scenario: Theme is presented
- **WHEN** the editor is open
- **THEN** the editor renders a `Theme` field with the two choices `Light` and `Dark`

#### Scenario: Lifecycle keys are not presented
- **WHEN** the editor is open and `~/.agent-runner/settings.yaml` contains `setup.completed_at`, `onboarding.completed_at`, or `onboarding.dismissed`
- **THEN** none of those values are shown in the editor and the editor does not provide any control to modify them

### Requirement: Theme field pre-selection reflects the currently persisted theme

When the editor opens, the cursor on the `Theme` field SHALL be pre-selected to the value currently persisted in `~/.agent-runner/settings.yaml`. The editor SHALL NOT use `lipgloss.HasDarkBackground()` for pre-selection (unlike the first-launch theme modal defined by `tui-theme`), because by the time the editor opens an authoritative persisted theme always exists.

#### Scenario: Persisted theme is dark
- **WHEN** the editor opens and `~/.agent-runner/settings.yaml` contains `theme: dark`
- **THEN** the `Dark` option is pre-selected

#### Scenario: Persisted theme is light
- **WHEN** the editor opens and `~/.agent-runner/settings.yaml` contains `theme: light`
- **THEN** the `Light` option is pre-selected

#### Scenario: Re-opening after a mid-session change reflects the new value
- **WHEN** the user opens the editor, changes the theme from `dark` to `light`, saves, and then re-opens the editor
- **THEN** the `Light` option is pre-selected on the second open

### Requirement: Editor keyboard model

The editor SHALL accept the following keys while it is open:

- Up / Down / Left / Right SHALL move between the two theme options.
- Tab and Shift+Tab SHALL behave as Down and Up respectively (forward / backward movement between fields and options), to keep the model extensible as more fields are added.
- Enter SHALL trigger save.
- Esc SHALL trigger cancel.
- Ctrl+C SHALL behave as it does globally elsewhere in the TUI: quit the program. The editor SHALL NOT intercept Ctrl+C.

All other keys SHALL be ignored by the editor and SHALL NOT be forwarded to the underlying run list.

#### Scenario: Arrow keys move the option cursor
- **WHEN** the editor is open with `Dark` selected and the user presses Up (or Left)
- **THEN** the option cursor moves to `Light`

#### Scenario: Tab acts like Down
- **WHEN** the editor is open with `Dark` selected and the user presses Tab
- **THEN** the option cursor moves to `Light`

#### Scenario: Unrelated keys are swallowed
- **WHEN** the editor is open and the user presses `r`, `n`, `c`, `?`, or another list-level shortcut
- **THEN** the key has no effect; the run list does not act on it

#### Scenario: Ctrl+C still quits
- **WHEN** the editor is open and the user presses Ctrl+C
- **THEN** the program exits as it would from any other TUI screen

### Requirement: Save persists settings, applies theme, and closes the editor

When the user presses Enter, the editor SHALL persist all editor-visible settings to `~/.agent-runner/settings.yaml` via the write path defined by the `user-settings-file` capability, SHALL apply any changed runtime-affecting setting (today: theme) so it takes effect without restart, and SHALL close itself so the underlying run list is re-displayed. The save SHALL preserve unrelated keys in the file (e.g., `setup.*`, `onboarding.*`) untouched.

#### Scenario: Save with no change
- **WHEN** the user opens the editor and presses Enter without moving the cursor
- **THEN** the file is written with the same theme value, no visible theme change occurs, and the editor closes

#### Scenario: Save with a theme change
- **WHEN** the user opens the editor with `Light` selected, moves to `Dark`, and presses Enter
- **THEN** `~/.agent-runner/settings.yaml` is written with `theme: dark`, `lipgloss.SetHasDarkBackground(true)` is invoked, the run list immediately re-renders with the Dark variant of every adaptive color token, and the editor closes

#### Scenario: Save preserves unrelated settings
- **WHEN** the file contains `setup.completed_at`, `onboarding.completed_at`, and `onboarding.dismissed` before save, and the user saves a theme change
- **THEN** all three lifecycle keys are still present with their original values after save

#### Scenario: Save failure surfaces inline and keeps the editor open
- **WHEN** the user presses Enter and writing `~/.agent-runner/settings.yaml` fails (e.g., permission denied)
- **THEN** the editor remains open, displays an inline error message identifying the file path and the underlying error, and does NOT apply the theme change to the running TUI

### Requirement: Cancel discards changes and closes the editor

When the user presses Esc, the editor SHALL close without writing to `~/.agent-runner/settings.yaml` and without applying any runtime change. The persisted theme and the running TUI's theme SHALL be unchanged.

#### Scenario: Esc after moving the cursor
- **WHEN** the user opens the editor with `Light` selected, moves to `Dark`, and presses Esc
- **THEN** the editor closes, `~/.agent-runner/settings.yaml` is unchanged, and the run list continues to render with the Light variant

#### Scenario: Esc without moving the cursor
- **WHEN** the user opens the editor and presses Esc without moving the cursor
- **THEN** the editor closes and no write occurs

### Requirement: Editor renders as a bordered overlay over the run list

The editor SHALL be rendered as a bordered box drawn on top of the run list. The run list's visible content underneath SHALL remain on screen behind the overlay. The run list's own state — cursor position, selected tab, scroll offset, and any drill-in sub-view — SHALL be preserved while the editor is open and SHALL be restored without modification when the editor closes via either save or cancel.

#### Scenario: Run list content remains visible behind the editor
- **WHEN** the editor is open
- **THEN** the run list's tab bar and rows are still visible around / behind the editor's bordered box (not blanked out)

#### Scenario: List state is preserved across the editor lifecycle
- **WHEN** the user is on tab "Worktrees" with the cursor on the third row, opens the editor, and then closes it (via save or cancel)
- **THEN** the run list is still on tab "Worktrees" with the cursor on the third row and the same scroll offset

#### Scenario: List does not process keys while the editor is open
- **WHEN** the editor is open and the user presses a key that the run list would otherwise handle (such as `j`, `k`, `n`, `r`, or Enter)
- **THEN** the run list does not act on the key; the editor consumes the input according to its own keyboard model
