# user-settings-editor Specification

## Purpose
TBD - created by archiving change settings. Update Purpose after archive.
## Requirements
### Requirement: Editor exposes editable user settings

The user-settings editor SHALL present every user-editable setting from `~/.agent-runner/settings.yaml` (as defined by the `user-settings-file` capability) as one labeled row per setting, displaying the setting's currently persisted value next to its label. The editor SHALL NOT present lifecycle keys that the app manages on the user's behalf. The set of user-editable settings SHALL be `theme`, `autonomous_backend`, and `autonomous_permission_mode`, presented in that order. Lifecycle keys under `setup` and `onboarding` SHALL NOT be presented.

#### Scenario: Theme row is presented with its current value

- **WHEN** the editor is open with `theme: dark` persisted
- **THEN** the editor renders a `Theme` row with `Dark` shown as the current value

#### Scenario: Autonomous Backend row is presented with its current value

- **WHEN** the editor is open with `autonomous_backend: interactive-claude` persisted
- **THEN** the editor renders an `Autonomous Backend` row with `Interactive for Claude` shown as the current value, appearing below the Theme row

#### Scenario: Autonomous Permission Mode row is presented with its current value

- **WHEN** the editor is open with `autonomous_permission_mode: yolo` persisted
- **THEN** the editor renders an `Autonomous Permission Mode` row with `YOLO` shown as the current value, appearing below the Autonomous Backend row

#### Scenario: YOLO row carries risk copy when its current value is YOLO

- **WHEN** the editor is open with the row cursor on the `Autonomous Permission Mode` row and its current value is `YOLO`
- **THEN** the editor displays risk copy describing the broader authority granted by YOLO and the recommendation to use an external sandbox

#### Scenario: Lifecycle keys are not presented

- **WHEN** the editor is open and `~/.agent-runner/settings.yaml` contains `setup.completed_at`, `onboarding.completed_at`, or `onboarding.dismissed`
- **THEN** none of those values are shown in the editor and the editor does not provide any control to modify them

### Requirement: Cycle-in-place row cursor model

The editor SHALL operate as a single-cursor row list:

- One row at a time is highlighted by the cursor. The cursor SHALL be drawn as a visible marker (e.g., `▶`) next to the highlighted row's label.
- Each row's current value SHALL be shown directly next to the row's label, reflecting what is currently persisted in `~/.agent-runner/settings.yaml`.
- Moving the cursor between rows SHALL NOT change any persisted value.
- "Cycling" the cursor row's value SHALL advance the row's value to the next (or previous) option in the row's option list, wrapping past the ends, and SHALL persist immediately (see "Cycle commits and persists in place").

The editor SHALL open with the cursor on the first row (`Theme`). The editor SHALL NOT use `lipgloss.HasDarkBackground()` for theme display (unlike the first-launch theme modal defined by `tui-theme`); the displayed value SHALL always reflect the persisted value.

#### Scenario: Editor opens with the cursor on the first row

- **WHEN** the editor is freshly opened
- **THEN** the cursor highlights the `Theme` row

#### Scenario: Cursor movement does not change values

- **WHEN** the editor is open and the user presses Down to move the cursor between rows
- **THEN** no row's current value changes and no write to `~/.agent-runner/settings.yaml` occurs

### Requirement: Editor keyboard model

The editor SHALL accept the following keys while it is open:

- Up / k SHALL move the cursor to the previous row, wrapping from the first row to the last.
- Down / j SHALL move the cursor to the next row, wrapping from the last row to the first.
- Tab / Space / Enter / Right / l SHALL cycle the cursor row's value forward to the next option (wrapping from the last option to the first), and SHALL persist the new value to `~/.agent-runner/settings.yaml` immediately.
- Shift+Tab / Left / h SHALL cycle the cursor row's value backward to the previous option (wrapping from the first option to the last), and SHALL persist the new value to `~/.agent-runner/settings.yaml` immediately.
- Esc SHALL close the editor without invoking any additional save.
- Ctrl+C SHALL behave as it does globally elsewhere in the TUI: quit the program. The editor SHALL NOT intercept Ctrl+C.

All other keys SHALL be ignored by the editor and SHALL NOT be forwarded to the underlying run list.

#### Scenario: Down moves the cursor to the next row

- **WHEN** the editor is open with the cursor on the `Theme` row and the user presses Down
- **THEN** the cursor moves to the `Autonomous Backend` row

#### Scenario: Down from the last row wraps to the first row

- **WHEN** the editor is open with the cursor on the `Autonomous Permission Mode` row and the user presses Down
- **THEN** the cursor wraps to the `Theme` row

#### Scenario: Up from the first row wraps to the last row

- **WHEN** the editor is open with the cursor on the `Theme` row and the user presses Up
- **THEN** the cursor wraps to the `Autonomous Permission Mode` row

#### Scenario: Tab cycles the cursor row's value forward

- **WHEN** the editor is open with the cursor on the `Theme` row, the current value is `Light`, and the user presses Tab
- **THEN** the row's value becomes `Dark` and is persisted to `~/.agent-runner/settings.yaml`

#### Scenario: Space cycles the cursor row's value forward

- **WHEN** the editor is open with the cursor on the `Theme` row, the current value is `Light`, and the user presses Space
- **THEN** the row's value becomes `Dark` and is persisted

#### Scenario: Enter cycles the cursor row's value forward

- **WHEN** the editor is open with the cursor on the `Theme` row, the current value is `Light`, and the user presses Enter
- **THEN** the row's value becomes `Dark` and is persisted

#### Scenario: Shift+Tab cycles the cursor row's value backward

- **WHEN** the editor is open with the cursor on the `Autonomous Backend` row, the current value is `Interactive`, and the user presses Shift+Tab
- **THEN** the row's value becomes `Headless` and is persisted

#### Scenario: Cycle past the last option wraps to the first

- **WHEN** the editor is open with the cursor on the `Autonomous Backend` row, the current value is `Interactive for Claude` (the last option), and the user presses Tab
- **THEN** the row's value wraps to `Headless` (the first option) and is persisted

#### Scenario: Cycle past the first option backward wraps to the last

- **WHEN** the editor is open with the cursor on the `Autonomous Backend` row, the current value is `Headless` (the first option), and the user presses Shift+Tab
- **THEN** the row's value wraps to `Interactive for Claude` (the last option) and is persisted

#### Scenario: Cycle only affects the cursor row

- **WHEN** the editor is open with the cursor on the `Theme` row and the user cycles the value
- **THEN** the `Autonomous Backend` and `Autonomous Permission Mode` rows' values are unchanged

#### Scenario: Unrelated keys are swallowed

- **WHEN** the editor is open and the user presses `r`, `n`, `c`, `?`, or another list-level shortcut
- **THEN** the key has no effect; neither the cursor moves nor any value changes, and the run list does not act on it

#### Scenario: Ctrl+C still quits

- **WHEN** the editor is open and the user presses Ctrl+C
- **THEN** the program exits as it would from any other TUI screen

### Requirement: Cycle commits and persists in place

When the user cycles the cursor row's value (forward or backward), the editor SHALL persist all editor-visible settings to `~/.agent-runner/settings.yaml` via the write path defined by the `user-settings-file` capability (with the cursor row's value replaced by the new option), SHALL apply any changed runtime-affecting setting (theme, autonomous backend, autonomous permission mode) so it takes effect without restart, and SHALL leave the editor open with the cursor on the same row so the user can continue editing. The save SHALL preserve unrelated keys in the file (e.g., `setup.*`, `onboarding.*`) untouched.

If the underlying save fails, the editor SHALL leave the persisted value unchanged for that row, display an inline error message identifying the file path and the underlying error, SHALL NOT apply any runtime change, and SHALL leave the editor open with the cursor on the same row. The next cycle attempt SHALL be independent of the previous failure.

#### Scenario: Cycling theme persists and applies live

- **WHEN** the user cycles the `Theme` row from `Light` to `Dark`
- **THEN** `~/.agent-runner/settings.yaml` is written with `theme: dark`, `lipgloss.SetHasDarkBackground(true)` is invoked, the run list immediately re-renders with the Dark variant of every adaptive color token, and the editor remains open

#### Scenario: Cycling autonomous backend persists

- **WHEN** the user cycles the `Autonomous Backend` row from `Headless` to `Interactive`
- **THEN** `~/.agent-runner/settings.yaml` is written with `autonomous_backend: interactive` and the editor remains open

#### Scenario: Cycling autonomous permission mode persists

- **WHEN** the user cycles the `Autonomous Permission Mode` row from `Conservative` to `YOLO`
- **THEN** `~/.agent-runner/settings.yaml` is written with `autonomous_permission_mode: yolo`, subsequent autonomous agent steps started after the cycle use the new mode, and the editor remains open

#### Scenario: Cycle preserves unrelated settings

- **WHEN** the file contains `setup.completed_at`, `onboarding.completed_at`, and `onboarding.dismissed` before a cycle, and the user cycles any visible row
- **THEN** all three lifecycle keys are still present with their original values after the write

#### Scenario: Cycle save failure surfaces inline and keeps the editor open

- **WHEN** the user cycles a row and writing `~/.agent-runner/settings.yaml` fails (e.g., permission denied)
- **THEN** the row's persisted value is unchanged, the editor displays an inline error message identifying the file path and the underlying error, no runtime change is applied, and the editor remains open

#### Scenario: Multiple rows can be cycled in one editor session

- **WHEN** the user opens the editor, cycles the `Theme` row, navigates to the `Autonomous Permission Mode` row, and cycles it
- **THEN** both changes are present in `~/.agent-runner/settings.yaml` after the second cycle, and each cycle was its own atomic write

### Requirement: Closing the editor preserves all cycled values

When the user presses Esc, the editor SHALL close without invoking any additional save. Any values cycled during the session SHALL remain persisted (close does not undo any cycle). The run list SHALL be re-displayed without modification.

#### Scenario: Esc closes the editor

- **WHEN** the editor is open and the user presses Esc
- **THEN** the editor closes and the run list is re-displayed

#### Scenario: Esc after cycles preserves the cycled values

- **WHEN** the user opens the editor, cycles the `Theme` row from `Light` to `Dark`, then presses Esc
- **THEN** the editor closes and `~/.agent-runner/settings.yaml` still contains `theme: dark`

### Requirement: Editor renders as a bordered overlay over the run list

The editor SHALL be rendered as a bordered box drawn on top of the run list. The run list's visible content underneath SHALL remain on screen behind the overlay. The run list's own state — cursor position, selected tab, scroll offset, and any drill-in sub-view — SHALL be preserved while the editor is open and SHALL be restored without modification when the editor closes via Esc.

#### Scenario: Run list content remains visible behind the editor
- **WHEN** the editor is open
- **THEN** the run list's tab bar and rows are still visible around / behind the editor's bordered box (not blanked out)

#### Scenario: List state is preserved across the editor lifecycle
- **WHEN** the user is on tab "Worktrees" with the cursor on the third row, opens the editor, and then closes it
- **THEN** the run list is still on tab "Worktrees" with the cursor on the third row and the same scroll offset

#### Scenario: List does not process keys while the editor is open
- **WHEN** the editor is open and the user presses a key that the run list would otherwise handle (such as `j`, `k`, `n`, `r`, or Enter)
- **THEN** the run list does not act on the key; the editor consumes the input according to its own keyboard model
