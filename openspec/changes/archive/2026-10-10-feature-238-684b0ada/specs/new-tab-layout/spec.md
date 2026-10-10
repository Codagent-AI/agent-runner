## ADDED Requirements

### Requirement: Workflow group headers
The new tab SHALL render each workflow group with a header containing the group's display name and its description. The header SHALL appear above the group's workflow rows. The header SHALL NOT be selectable: the cursor SHALL skip over it when the user navigates with the keyboard. The visual arrangement of the display name relative to the description (same line, separate lines, etc.) is an implementation detail and not pinned by this spec.

#### Scenario: Project group renders with header and description
- **WHEN** the new tab renders and the project scope contains at least one visible workflow
- **THEN** a header identifying the group as the project's workflows appears above the project workflows
- **AND** the header includes a non-empty description (exact copy and visual layout are implementation details and not pinned by this spec)

#### Scenario: User group renders with header and description
- **WHEN** the new tab renders and the user scope contains at least one visible workflow
- **THEN** a header identifying the group as the user's workflows appears above the user workflows
- **AND** the header includes a non-empty description (exact copy and visual layout are implementation details and not pinned by this spec)

#### Scenario: Builtin group renders using namespace metadata
- **WHEN** the new tab renders a builtin namespace whose metadata file declares a display name and description
- **THEN** the header shows the declared display name
- **AND** the header shows the declared description

#### Scenario: Header is not focusable when navigating downward
- **WHEN** the cursor is on the row immediately above a header and the user presses `down`
- **THEN** the cursor moves to the first workflow row of the group below the header, skipping the header

#### Scenario: Header is not focusable when navigating upward
- **WHEN** the cursor is on the first workflow row of a non-first group and the user presses `up`
- **THEN** the cursor lands on the last workflow row of the previous group, skipping the current group's header and any separator between the groups

#### Scenario: Upward navigation from the first workflow skips the leading header
- **WHEN** the cursor is on the first workflow row of the first visible group and the user presses `up`
- **THEN** the cursor lands on the "Plan with an agent" entry, skipping the group header

### Requirement: Plan with an agent entry
The new tab SHALL render a selectable "Plan with an agent" entry as the first row of the list, above every workflow group. The entry SHALL be present regardless of the search filter and SHALL NOT count toward the workflow count label. When the New tab opens fresh, the cursor SHALL be on this entry. Pressing `Enter` or `r` on the entry SHALL start the built-in intake workflow directly, without a definition view or parameter form. Pressing `↑` on the entry SHALL move focus to the search box.

#### Scenario: Initial cursor is on the intake entry
- **WHEN** the new tab opens fresh with at least one visible workflow
- **THEN** the cursor is on the "Plan with an agent" entry, not on a group header or workflow row

#### Scenario: Down from the intake entry skips the leading header
- **WHEN** the cursor is on the "Plan with an agent" entry and the user presses `down`
- **THEN** the cursor moves to the first workflow row of the first visible group, skipping its header

#### Scenario: Up from the intake entry focuses the search box
- **WHEN** the cursor is on the "Plan with an agent" entry and the user presses `up`
- **THEN** the search box receives focus and the cursor leaves the workflow list

#### Scenario: Intake entry starts the intake workflow
- **WHEN** the cursor is on the "Plan with an agent" entry and the user presses `Enter` or `r`
- **THEN** the built-in intake workflow starts

#### Scenario: Intake entry survives a filter with no matches
- **WHEN** the search filter matches no workflows
- **THEN** the "Plan with an agent" entry is still shown and the count label reports zero workflows


### Requirement: New tab workflow row content
Each valid workflow row on the New tab SHALL display the workflow's version-free canonical name followed by its description when one is present. A long description SHALL wrap onto continuation lines aligned with the description's start column rather than being truncated. A workflow without a description SHALL render its canonical name only. Names, descriptions, and error text read from workflow files SHALL be sanitized so terminal escape sequences and control characters are not rendered.

#### Scenario: Workflow row shows name and description
- **WHEN** a workflow with canonical name `deploy` and description "Deploy to production" is rendered
- **THEN** the row displays `deploy` followed by "Deploy to production" on the same line

#### Scenario: Workflow with no description
- **WHEN** a workflow has no `description` field
- **THEN** the row displays the canonical name only

#### Scenario: Long description wraps
- **WHEN** a workflow's description is wider than the space remaining after its name
- **THEN** the description continues on following lines aligned with its first character, and no part of it is dropped

### Requirement: New tab keybindings
While the New tab's list has focus, the following keybindings SHALL apply to workflow rows:
- `Enter` on a valid workflow row SHALL open the workflow definition view for that row (see `view-run`).
- `r` on a valid workflow row SHALL start a run of that workflow: the workflow parameter form SHALL be presented when the workflow declares one or more parameters (see `workflow-param-form`), and otherwise the run SHALL launch immediately by replacing the TUI process with `agent-runner <canonical-name>`, which opens the live run view.
- `Enter` and `r` on an invalid workflow row SHALL do nothing (see "Latest logical workflow rows").

The help bar SHALL include `enter view` and `r start run` while the New tab is active. The global list keybindings (`q` quit, `↑`/`↓` navigate, tab switching) SHALL remain available.

#### Scenario: Enter opens workflow definition view
- **WHEN** the user presses Enter on a valid workflow row in the New tab
- **THEN** the view navigates to the workflow definition view for that workflow

#### Scenario: r starts a run with parameters
- **WHEN** the user presses `r` on a valid workflow row whose workflow declares parameters
- **THEN** the param form is presented for that workflow

#### Scenario: r starts a run without parameters
- **WHEN** the user presses `r` on a valid workflow row whose workflow declares no parameters
- **THEN** a new run of that workflow launches and the live run view opens for it

#### Scenario: Help bar on new tab
- **WHEN** the New tab is active
- **THEN** the help bar includes `enter view` and `r start run`

### Requirement: New tab search filter
The New tab SHALL show a search box above the workflow list. When the New tab opens, focus SHALL be on the list, not the search box, so tab-switching and action keys work immediately. Pressing `↑` on the first selectable row of the list (the "Plan with an agent" entry) SHALL move focus to the search box. Pressing `↓` or `Enter` in the search box SHALL move focus to the first selectable row of the list. While the search box has focus, typed letters SHALL extend the filter text; in particular the list hotkey letters `n`, `c`, `w`, `a`, `h`, `r`, and `s` SHALL be appended as ordinary characters rather than acting as commands. `Backspace` SHALL delete the last character, and `Tab`, `Shift+Tab`, `←`, and `→` SHALL still switch tabs.

The filter SHALL keep a workflow row when the filter text is a case-insensitive substring of the row's version-free canonical name or its description. The first occurrence of the filter text in a displayed name SHALL be visually emphasized. A count label showing the number of matching workflow rows, formatted as `(N workflows)` or `(1 workflow)`, SHALL be displayed right-aligned on the search box line.

Pressing Escape while the search box has focus SHALL clear the filter and keep focus in the search box. Pressing Escape while the list has focus and the filter is non-empty SHALL clear the filter and move focus to the search box.

#### Scenario: Filter narrows workflow list
- **WHEN** the user types `impl` in the search box
- **THEN** only workflows whose canonical name or description contains `impl`, compared case-insensitively, are shown

#### Scenario: Filter matches description
- **WHEN** the user types a substring that appears in a workflow's description but not in its name
- **THEN** that workflow remains in the filtered list

#### Scenario: Match substring highlighted in name
- **WHEN** the filter is `impl` and a shown workflow's name is `core:implement-task`
- **THEN** the first occurrence of `impl` in the displayed name is visually emphasized

#### Scenario: Count label updates with filter
- **WHEN** the filter narrows results from 13 to 3 workflows
- **THEN** the count label shows `(3 workflows)`

#### Scenario: Focus moves from list to search box
- **WHEN** the cursor is on the first selectable row of the New tab list and the user presses `↑`
- **THEN** focus moves to the search box and printable keystrokes go to the filter text

#### Scenario: Focus moves from search box to list
- **WHEN** the search box has focus and the user presses `↓`
- **THEN** focus moves to the first selectable row of the filtered list and action keybindings work normally

#### Scenario: Hotkey letters typed into search box
- **WHEN** the search box has focus and the user types `n` and then `r`
- **THEN** both characters are appended to the filter text and no tab switch or run start occurs

#### Scenario: Clear filter shows all workflows
- **WHEN** the user clears all text in the search box
- **THEN** all visible workflows are shown with their original grouping restored

#### Scenario: Escape from search box clears filter and keeps focus
- **WHEN** the search box has focus and the user presses Escape
- **THEN** the filter text is cleared, all visible workflows are shown, and focus remains in the search box

#### Scenario: Escape from list clears filter and focuses search box
- **WHEN** the list has focus, the filter is non-empty, and the user presses Escape
- **THEN** the filter text is cleared and focus moves to the search box

#### Scenario: Search with zero results
- **WHEN** the filter text matches no workflows in any group
- **THEN** no workflow rows or group headers are shown and the count label shows `(0 workflows)`

## REMOVED Requirements

### Requirement: Workflow groups render with header and description
**Reason**: Its scenarios "Initial cursor position skips the leading header" and "Upward navigation from the first workflow focuses the search box" are false since the "Plan with an agent" entry became the first selectable row (`internal/listview/newtab.go:116`; `newtab_test.go` `TestNewTab_InitialCursorSkipsLeadingHeader`, `TestNewTab_NavigationAroundIntakeEntry`), and OpenSpec cannot drop scenario names through a MODIFIED block.
**Migration**: The header behavior is unchanged under "Workflow group headers"; the initial cursor and upward-navigation behavior is specified under "Plan with an agent entry".
