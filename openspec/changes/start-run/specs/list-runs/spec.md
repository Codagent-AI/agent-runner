## ADDED Requirements

### Requirement: Tab navigation includes new tab
The list TUI's tab bar SHALL include a fourth tab labeled "new", activated by pressing `n`. The tab order SHALL be: new (`n`), current-dir (`c`), worktrees (`w`), all (`a`). Tabs can also be cycled with `←`/`→` arrow keys and `Tab`/`Shift+Tab`. The new tab's content is a workflow list (from the `workflow-discovery` capability), not a run list. Existing tab keybindings and behavior are unchanged.

#### Scenario: n switches to new tab
- **WHEN** the user presses `n` from any tab
- **THEN** the active tab switches to the "new" tab displaying the workflow list

#### Scenario: Tab order in tab bar
- **WHEN** the list TUI is open
- **THEN** the tab bar displays tabs in order: new, current-dir, worktrees, all

### Requirement: Default tab on entry
The initial focused tab when the list TUI opens SHALL depend on how it was invoked:
- Bare `agent-runner` (no subcommand, no flags) SHALL focus the "new" tab.
- `--resume` (no argument) SHALL focus the current-dir tab.
- `--list` SHALL focus the current-dir tab.
- Return from the run view (Escape at top level) SHALL focus whichever tab was active when the user entered the run view.

#### Scenario: Bare invocation opens new tab
- **WHEN** the user runs `agent-runner` with no subcommand or flags
- **THEN** the list TUI opens with the "new" tab focused

#### Scenario: --resume no arg opens current-dir tab
- **WHEN** the user runs `agent-runner --resume` with no session ID argument
- **THEN** the list TUI opens with the current-dir tab focused

#### Scenario: --list opens current-dir tab
- **WHEN** the user runs `agent-runner --list`
- **THEN** the list TUI opens with the current-dir tab focused

#### Scenario: Return from run view restores previous tab
- **WHEN** the user was on the "new" tab, entered a workflow definition view, and presses Escape at top level
- **THEN** the list TUI is restored with the "new" tab focused and its prior cursor/scroll state

### Requirement: New tab workflow list rendering
The "new" tab SHALL display workflows from the `workflow-discovery` enumeration, grouped by scope (project, user, builtin) and by builtin namespace. Groups SHALL be separated by blank lines (no header rows). Each workflow row SHALL display the workflow's canonical name and description (if present). Malformed workflows SHALL be displayed with an error indicator.

#### Scenario: Workflows grouped by blank lines
- **WHEN** the new tab is displayed with workflows from multiple scopes
- **THEN** scope groups are separated by blank lines in order: project, user, builtin; blank lines are non-selectable (cursor skips them)

#### Scenario: Builtin namespace sub-groups separated by blank lines
- **WHEN** builtins include workflows from `core` and `spec-driven` namespaces
- **THEN** each namespace's workflows are separated from the next by a blank line

#### Scenario: Workflow row shows name and description
- **WHEN** a workflow with name `deploy` and description "Deploy to production" is rendered
- **THEN** the row displays both the canonical name and the description

#### Scenario: Workflow with no description
- **WHEN** a workflow has no `description` field
- **THEN** the row displays the canonical name only

#### Scenario: Malformed workflow shown with error
- **WHEN** a workflow file failed to parse
- **THEN** the row displays the canonical name with an error indicator

#### Scenario: Empty new tab
- **WHEN** no workflows are found in any scope
- **THEN** the new tab displays an empty state message

### Requirement: New tab keybindings
On the "new" tab, the following keybindings SHALL apply:
- `Enter` on a workflow row SHALL navigate to the workflow definition view for that workflow.
- `r` on a workflow row SHALL initiate starting a run — transitioning to the param form if the workflow has parameters, or launching the run directly if it has none.
- `Enter` and `r` SHALL be ignored on malformed workflow rows.
- The help bar SHALL show `enter view` and `r start run` when on the new tab.
- Existing global keybindings (`q` quit, `↑`/`↓` navigate, `←`/`→`/`n`/`c`/`w`/`a` switch tab) remain unchanged.

#### Scenario: Enter opens workflow definition view
- **WHEN** the user presses Enter on a valid workflow row in the new tab
- **THEN** the view navigates to the workflow definition view for that workflow

#### Scenario: r starts a run with parameters
- **WHEN** the user presses `r` on a workflow that declares parameters
- **THEN** the param form is presented for that workflow

#### Scenario: r starts a run without parameters
- **WHEN** the user presses `r` on a workflow with no declared parameters
- **THEN** a new run launches and the view transitions to the live run view

#### Scenario: Enter on malformed workflow is ignored
- **WHEN** the user presses Enter on a malformed workflow row
- **THEN** no action is taken

#### Scenario: r on malformed workflow is ignored
- **WHEN** the user presses `r` on a malformed workflow row
- **THEN** no action is taken

#### Scenario: Help bar on new tab
- **WHEN** the new tab is active
- **THEN** the help bar shows `enter view` and `r start run`

### Requirement: New tab search filter
The "new" tab SHALL include a search box above the workflow list. Focus SHALL default to the first list item (not the search box). Pressing `↑` from the first list item SHALL move focus to the search box. Pressing `↓` or `Enter` from the search box SHALL move focus back to the list. When the search box has focus, printable keystrokes go to filter text; when the list has focus, action keybindings (`r`, `Enter`, etc.) work normally.

The filter SHALL match against the workflow's canonical name or source path (substring match, case-insensitive). The first occurrence of the search substring in the displayed name SHALL be highlighted in the accent color. A count label showing the number of matching workflows SHALL be displayed right-aligned next to the search box.

Groups with no matching workflows SHALL be collapsed entirely (blank-line separator omitted).

#### Scenario: Filter narrows workflow list
- **WHEN** the user types `impl` in the search box
- **THEN** only workflows whose canonical name or source path contains `impl` (case-insensitive) are shown

#### Scenario: Match substring highlighted in name
- **WHEN** the filter is `impl` and the workflow name is `core:implement-task`
- **THEN** the first occurrence of `impl` in the displayed name is rendered in the accent color

#### Scenario: Empty groups collapsed
- **WHEN** the filter matches no workflows in the project scope
- **THEN** the project scope group and its blank-line separator are not rendered

#### Scenario: Count label updates with filter
- **WHEN** the filter narrows results from 13 to 3 workflows
- **THEN** the count label shows `(3 workflows)`

#### Scenario: Focus moves from list to search box
- **WHEN** the cursor is on the first workflow row and the user presses `↑`
- **THEN** focus moves to the search box; printable keystrokes go to filter text

#### Scenario: Focus moves from search box to list
- **WHEN** the search box has focus and the user presses `↓`
- **THEN** focus moves to the first item in the filtered workflow list; action keybindings work normally

#### Scenario: Clear filter shows all workflows
- **WHEN** the user clears all text in the search box
- **THEN** all workflows are shown with original grouping restored

#### Scenario: Escape from search box clears filter and returns focus to list
- **WHEN** the search box has focus and the user presses Escape
- **THEN** the filter text is cleared, all workflows are shown, and focus returns to the first list item

#### Scenario: Search with zero results
- **WHEN** the filter text matches no workflows in any scope
- **THEN** the workflow list is empty, the count label shows `(0 workflows)`, and focus remains in the search box (there is no list item to move to)
