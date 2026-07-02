# user-settings-editor Specification

## Purpose
Define the TUI editor for user-editable settings and its navigation, persistence, and cancellation behavior.
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
- Each row's value SHALL be shown directly next to the row's label. The displayed value SHALL reflect the editor's pending state for that row (initialized from the persisted setting on open and updated locally as the user cycles), not necessarily what is currently on disk.
- Moving the cursor between rows SHALL NOT change any pending value.
- "Cycling" the cursor row's value SHALL advance the row's value to the next (or previous) option in the row's option list, wrapping past the ends. Cycling SHALL update only the editor's pending state and SHALL NOT write to `~/.agent-runner/settings.yaml`; persistence is deferred to the explicit commit action (Enter) defined in "Commit on Enter persists pending changes".

The editor SHALL open with the cursor on the first row (`Theme`). The editor SHALL NOT use `lipgloss.HasDarkBackground()` for theme display (unlike the first-launch theme modal defined by `tui-theme`); the displayed value SHALL always reflect the persisted (or pending) value.

#### Scenario: Editor opens with the cursor on the first row

- **WHEN** the editor is freshly opened
- **THEN** the cursor highlights the `Theme` row

#### Scenario: Cursor movement does not change values

- **WHEN** the editor is open and the user presses Down to move the cursor between rows
- **THEN** no row's pending value changes and no write to `~/.agent-runner/settings.yaml` occurs

### Requirement: Editor keyboard model

The editor SHALL accept the following keys while it is open:

- Up / k SHALL move the cursor to the previous row, wrapping from the first row to the last.
- Down / j SHALL move the cursor to the next row, wrapping from the last row to the first.
- Tab / Space / Right / l SHALL cycle the cursor row's value forward to the next option (wrapping from the last option to the first). Cycling SHALL update only the editor's pending state; it SHALL NOT write to `~/.agent-runner/settings.yaml`.
- Shift+Tab / Left / h SHALL cycle the cursor row's value backward to the previous option (wrapping from the first option to the last). Cycling SHALL update only the editor's pending state; it SHALL NOT write to `~/.agent-runner/settings.yaml`.
- Enter SHALL commit every pending change: it SHALL write `~/.agent-runner/settings.yaml` (see "Commit on Enter persists pending changes") and on success SHALL close the editor.
- Esc SHALL close the editor and discard every pending change (see "Esc discards pending changes and closes the editor"); it SHALL NOT invoke save.
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

#### Scenario: Tab cycles the cursor row's value forward without saving

- **WHEN** the editor is open with the cursor on the `Theme` row, the displayed value is `Light`, and the user presses Tab
- **THEN** the row's displayed value becomes `Dark` and `~/.agent-runner/settings.yaml` is NOT written

#### Scenario: Space cycles the cursor row's value forward without saving

- **WHEN** the editor is open with the cursor on the `Theme` row, the displayed value is `Light`, and the user presses Space
- **THEN** the row's displayed value becomes `Dark` and `~/.agent-runner/settings.yaml` is NOT written

#### Scenario: Shift+Tab cycles the cursor row's value backward without saving

- **WHEN** the editor is open with the cursor on the `Autonomous Backend` row, the displayed value is `Interactive`, and the user presses Shift+Tab
- **THEN** the row's displayed value becomes `Headless` and `~/.agent-runner/settings.yaml` is NOT written

#### Scenario: Cycle past the last option wraps to the first

- **WHEN** the editor is open with the cursor on the `Autonomous Backend` row, the displayed value is `Interactive for Claude` (the last option), and the user presses Tab
- **THEN** the row's displayed value wraps to `Headless` (the first option) without saving

#### Scenario: Cycle past the first option backward wraps to the last

- **WHEN** the editor is open with the cursor on the `Autonomous Backend` row, the displayed value is `Headless` (the first option), and the user presses Shift+Tab
- **THEN** the row's displayed value wraps to `Interactive for Claude` (the last option) without saving

#### Scenario: Cycle only affects the cursor row

- **WHEN** the editor is open with the cursor on the `Theme` row and the user cycles the value
- **THEN** the `Autonomous Backend` and `Autonomous Permission Mode` rows' values are unchanged

#### Scenario: Unrelated keys are swallowed

- **WHEN** the editor is open and the user presses `r`, `n`, `c`, `?`, or another list-level shortcut
- **THEN** the key has no effect; neither the cursor moves nor any value changes, and the run list does not act on it

#### Scenario: Ctrl+C still quits

- **WHEN** the editor is open and the user presses Ctrl+C
- **THEN** the program exits as it would from any other TUI screen

### Requirement: Commit on Enter persists pending changes

When the user presses Enter, the editor SHALL write all editor-visible settings to `~/.agent-runner/settings.yaml` via the write path defined by the `user-settings-file` capability — combining every pending change made during the session into a single atomic write — and on success SHALL apply any changed runtime-affecting setting (theme, autonomous backend, autonomous permission mode) so it takes effect without restart, and SHALL close the editor. The save SHALL preserve unrelated keys in the file (e.g., `setup.*`, `onboarding.*`) untouched.

If the underlying save fails, the editor SHALL NOT close, SHALL NOT apply any runtime change, SHALL retain the user's pending values so the user can retry or Esc out, and SHALL display an inline error message identifying the file path and the underlying error.

#### Scenario: Enter persists theme and applies live before closing

- **WHEN** the user cycles the `Theme` row from `Light` to `Dark` and presses Enter
- **THEN** `~/.agent-runner/settings.yaml` is written with `theme: dark`, `lipgloss.SetHasDarkBackground(true)` is invoked, the run list immediately re-renders with the Dark variant of every adaptive color token, and the editor closes

#### Scenario: Enter persists autonomous backend before closing

- **WHEN** the user cycles the `Autonomous Backend` row from `Headless` to `Interactive` and presses Enter
- **THEN** `~/.agent-runner/settings.yaml` is written with `autonomous_backend: interactive` and the editor closes

#### Scenario: Enter persists autonomous permission mode before closing

- **WHEN** the user cycles the `Autonomous Permission Mode` row from `Conservative` to `YOLO` and presses Enter
- **THEN** `~/.agent-runner/settings.yaml` is written with `autonomous_permission_mode: yolo`, subsequent autonomous agent steps started after the commit use the new mode, and the editor closes

#### Scenario: Enter preserves unrelated settings

- **WHEN** the file contains `setup.completed_at`, `onboarding.completed_at`, and `onboarding.dismissed` before a commit, and the user cycles any visible row and presses Enter
- **THEN** all three lifecycle keys are still present with their original values after the write

#### Scenario: Enter save failure surfaces inline and keeps the editor open

- **WHEN** the user cycles a row, presses Enter, and writing `~/.agent-runner/settings.yaml` fails (e.g., permission denied)
- **THEN** no value is persisted, no runtime change is applied, the editor remains open with the user's pending value intact, and the editor displays an inline error message identifying the file path and the underlying error

#### Scenario: Multiple rows can be cycled and committed in one Enter

- **WHEN** the user opens the editor, cycles the `Theme` row, navigates to the `Autonomous Permission Mode` row, cycles it, and presses Enter
- **THEN** both changes are present in `~/.agent-runner/settings.yaml` after a single atomic write and the editor closes

### Requirement: Esc discards pending changes and closes the editor

When the user presses Esc, the editor SHALL close without writing to `~/.agent-runner/settings.yaml`. Any cycles made during the session SHALL be discarded — the persisted file SHALL remain whatever it was when the editor opened, and no runtime-affecting setting SHALL be applied. The run list SHALL be re-displayed without modification.

#### Scenario: Esc with no pending changes closes the editor

- **WHEN** the editor is open and the user presses Esc without cycling any value
- **THEN** the editor closes, the run list is re-displayed, and `~/.agent-runner/settings.yaml` is not written

#### Scenario: Esc after cycles discards the pending changes

- **WHEN** the user opens the editor (theme persisted as `Light`), cycles the `Theme` row to `Dark`, then presses Esc
- **THEN** the editor closes, `~/.agent-runner/settings.yaml` still contains `theme: light`, and no runtime theme change is applied

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
