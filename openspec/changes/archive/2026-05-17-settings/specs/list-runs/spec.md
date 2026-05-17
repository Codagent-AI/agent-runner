## ADDED Requirements

### Requirement: Open user-settings editor from list with `s`

Pressing `s` on a run list SHALL open the in-session user-settings editor (defined by the `user-settings-editor` capability) as an overlay on top of the run list. The list view's state (cursor, tab, scroll offsets, drill-in) SHALL be preserved while the editor is open and SHALL be restored when the editor closes (either by save or by cancel).

`s` SHALL be handled identically regardless of which row the cursor is on or whether the row represents an active, inactive, or completed run — the editor is not row-scoped.

`s` SHALL be ignored on picker sub-views (worktree/all drill-in pickers), consistent with how other list-level shortcuts behave on those views.

The list's help bar SHALL include `s settings` whenever `s` would currently open the editor.

#### Scenario: s on the run list opens the editor
- **WHEN** the user is on any tab's run list and presses `s`
- **THEN** the user-settings editor opens overlaid on the run list

#### Scenario: List state preserved across editor lifecycle
- **WHEN** the user is on tab "Worktrees" with the cursor on the third row, presses `s` to open the editor, and then closes it via either save or cancel
- **THEN** the run list is still on tab "Worktrees" with the cursor on the third row and the same scroll offset

#### Scenario: s is independent of cursor row
- **WHEN** the user presses `s` while the cursor is on an active run, an inactive run, or a completed run
- **THEN** the editor opens in every case; the action does not depend on the row's status

#### Scenario: s on picker sub-view is ignored
- **WHEN** the user presses `s` while on a worktree or all-tab picker sub-view (not a run list)
- **THEN** no action is taken; the editor does not open

#### Scenario: Help bar advertises the shortcut
- **WHEN** the run list is visible and `s` would open the editor
- **THEN** the help bar includes a `s settings` entry
