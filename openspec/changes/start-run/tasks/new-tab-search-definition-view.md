# Task: New Tab, Search, and Definition View

## Goal

Add a "new" tab to the list TUI that displays available workflows with a search filter, and implement the `FromDefinition` entry mode in the run view for inspecting a workflow's steps without a run instance. After this task, a user can browse, search, and inspect workflow definitions directly from the TUI.

## Background

### List view tab system

The list view lives in `internal/listview/`. Tabs are defined as an iota enum in `model.go`. Currently there are three tabs: `tabCurrentDir`, `tabWorktrees`, `tabAll`. The tab bar renders in `view.go`'s `renderTabs()`. Tab switching is handled in `Update()` and `nextTab()`/`prevTab()` helpers.

**Add a new first tab:**
```go
const (
    tabNew tab = iota  // must be first
    tabCurrentDir
    tabWorktrees
    tabAll
)
```

The tab bar keybinding for the new tab is `n`. Tab order in the bar: New, Current Dir, Worktrees, All.

**New tab state** to add to the `Model` struct:
```go
type newTabState struct {
    workflows     []discovery.WorkflowEntry
    filtered      []int    // indices into workflows after applying search filter
    cursor        int      // position in filtered list (skipping blank-line separators)
    offset        int      // scroll offset
    searchText    string
    searchFocused bool     // true when search box has focus, false when list has focus
}
```

**Workflow enumeration** is eager — call `discovery.Enumerate(builtinworkflows.FS, projectDir, userHomeWorkflowsDir)` during `listview.New()` alongside existing run loading. The `discovery` package is at `internal/discovery/` (created in a prior task). `builtinworkflows.FS` is the embed.FS from `workflows/embed.go`.

**Default tab on entry**: `listview.New()` needs a way to know which tab to start on. Add an initial tab parameter or option. Current callers pass nothing — add a `WithInitialTab(tab)` option or similar that defaults to `tabNew` for bare invocation and `tabCurrentDir` for `--list`/`--resume`. Look at `cmd/agent-runner/main.go`'s `handleList()` (around line 326) for where `listview.New()` is called.

### New tab rendering

The body for the new tab is a scrollable workflow list. Groups (project scope, user scope, and builtin namespaces) are separated by blank lines. No header rows — the namespace prefix in canonical names (`core:finalize-pr`) makes grouping self-evident. Blank lines are non-selectable; the cursor skips them.

**Row layout:**
- Cursor row: `›` in `AccentCyan`, name in `SelectedText` bold, description in `BodyText`
- Non-cursor rows: name in `BodyText`, description in `DimText`
- Malformed rows: name in `FailedRed`, error message in `FailedRed` replacing description
- Descriptions truncated with `…` to fit terminal width

When a search filter is active, the first occurrence of the search substring in the displayed name is highlighted in `AccentCyan` instead of the row's normal name color.

**Search box** renders above the list:
- Placeholder `Search...` in `DimText`, typed text in `BodyText`
- `🔍` prefix
- Count label `(N workflows)` right-aligned in `DimText` showing filtered count

**Search focus model:**
- Default focus: first list item (not search box)
- `↑` from the first list item moves focus to search box
- `↓` or `Enter` from search box moves focus to first filtered list item
- When search box has focus: printable keystrokes go to filter text; `Backspace` removes last character; `Escape` clears the filter and returns focus to list
- When list has focus: `r`, `Enter`, `↑`/`↓` are action/navigation keybindings

**Filter logic:** Case-insensitive substring match against the entry's canonical name or source path. Groups with no matching entries collapse entirely (blank-line separator omitted). Count label updates to show filtered count.

**Keybindings when list has focus:**
- `Enter` on a valid workflow row: emit a message to navigate to the workflow definition view (similar to how `ViewRunMsg` works for runs — add a new `ViewDefinitionMsg{WorkflowEntry}`)
- `r` on a valid workflow row: emit a message to start a run (add `StartRunMsg{WorkflowEntry}`) — this message is handled in a subsequent task; for now the message type just needs to exist and be emitted correctly
- `Enter` and `r` ignored on malformed rows and blank-line rows
- Help bar shows `enter view  r start run` when new tab is active (in addition to global bindings)

### Run view: FromDefinition entry mode

Add a fourth entry mode to `internal/runview/model.go`:
```go
const (
    FromList Entered = iota
    FromInspect
    FromLiveRun
    FromDefinition  // new: viewing a workflow definition, no run instance
)
```

The list view's `ViewDefinitionMsg` handler (in the top-level switcher in `cmd/agent-runner/main.go` or wherever `ViewRunMsg` is handled) should call `runview.New` with `FromDefinition`, passing the workflow file path as the session directory substitute and the project directory. The key behavioral differences in `FromDefinition` mode:

- No session directory, no audit log, no run-lock check — the workflow definition file is loaded directly
- All steps render as `pending` (the existing code path already handles this when no audit events exist)
- No live refresh (no polling)
- No auto-follow, no auto-scroll
- The `r` keybinding for "resume run" (which requires an inactive run) does NOT apply — in `FromDefinition` mode, `r` MUST emit `StartRunMsg{WorkflowEntry}`. The `StartRunMsg` type is defined in this task; the handler that acts on it (param form / exec-self) is wired in the subsequent task. Emitting an unhandled message is safe — bubbletea discards it until a handler is registered.
- Breadcrumb shows the workflow's canonical name only — no run ID, start time, or status
- Escape at top level returns to the list TUI (same as `FromList`)

**New() initialization for FromDefinition**: The function signature is `func New(sessionDir, projectDir string, entered Entered) (*Model, error)`. For `FromDefinition`, `sessionDir` carries the workflow file path (not a real session directory). The model skips audit log loading, run-lock checking, and polling setup when `entered == FromDefinition`.

### Default tab logic

In `cmd/agent-runner/main.go`:
- `handleList()` → pass `tabCurrentDir` as initial tab
- `--resume` (no arg) → same as `handleList()`, pass `tabCurrentDir`
- Bare invocation (no args, no flags, line ~219) → pass `tabNew` as initial tab

The list view already tracks which tab is active; when returning from the run view via `BackMsg{}`, the active tab is already preserved since the list model is not re-initialized.

**Key files to read before starting:**
- `internal/listview/model.go` — full Model struct, tab enum, New(), Update()
- `internal/listview/view.go` — renderTabs(), renderBody(), renderRunList() for style patterns
- `internal/tuistyle/styles.go` — all color tokens and style vars
- `internal/runview/model.go` — Entered enum, New(), handleEsc(), how FromList differs from FromInspect
- `cmd/agent-runner/main.go` — handleList(), bare invocation path (~line 219), ViewRunMsg handling
- `internal/discovery/` — WorkflowEntry type (created in prior task)
- `workflows/embed.go` — builtinworkflows.FS

## Spec

### Requirement: Tab navigation includes new tab
The list TUI's tab bar SHALL include a fourth tab labeled "new", activated by pressing `n`. The tab order SHALL be: new (`n`), current-dir (`c`), worktrees (`w`), all (`a`).

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
On the "new" tab, the following keybindings SHALL apply when the list has focus:
- `Enter` on a workflow row SHALL navigate to the workflow definition view.
- `r` on a workflow row SHALL emit the start-run signal (handled in subsequent task).
- `Enter` and `r` SHALL be ignored on malformed workflow rows.
- The help bar SHALL show `enter view` and `r start run` when on the new tab.

#### Scenario: Enter opens workflow definition view
- **WHEN** the user presses Enter on a valid workflow row in the new tab
- **THEN** the view navigates to the workflow definition view for that workflow

#### Scenario: Enter on malformed workflow is ignored
- **WHEN** the user presses Enter on a malformed workflow row
- **THEN** no action is taken

#### Scenario: Help bar on new tab
- **WHEN** the new tab is active
- **THEN** the help bar shows `enter view` and `r start run`

### Requirement: New tab search filter
The "new" tab SHALL include a search box above the workflow list. Focus SHALL default to the first list item. Pressing `↑` from the first list item SHALL move focus to the search box. Pressing `↓` or `Enter` from the search box SHALL move focus back to the list. The filter SHALL match against canonical name or source path (case-insensitive substring). The first occurrence of the search substring in the displayed name SHALL be highlighted in the accent color. Groups with no matching workflows SHALL collapse entirely.

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

### Requirement: Workflow definition view mode
The system SHALL support a view mode that renders a workflow's definition without an associated run instance. All steps are in `pending` status. Breadcrumb shows the workflow's canonical name only. No live refresh. Drill-in, step list, detail pane, and keyboard shortcuts behave identically to `view-run`.

#### Scenario: All steps shown as pending
- **WHEN** the workflow definition view opens for a workflow
- **THEN** the step list is populated from the workflow definition file with every row in `pending` status

#### Scenario: Drill-in works on sub-workflow steps
- **WHEN** the user presses Enter on a sub-workflow step in the definition view
- **THEN** the referenced workflow file is loaded and its children are displayed, matching view-run drill-in behavior

#### Scenario: Breadcrumb shows workflow name only
- **WHEN** the workflow definition view is open
- **THEN** the breadcrumb shows the workflow's canonical name (e.g. `core:finalize-pr`) with no run ID, start time, or status

#### Scenario: FromDefinition mode has no live refresh
- **WHEN** the run view is in `FromDefinition` mode
- **THEN** no polling occurs; the view is static

#### Scenario: Escape from FromDefinition returns to list
- **WHEN** the user presses Escape at the top level in `FromDefinition` mode
- **THEN** the view returns to the list TUI

## Done When

Tests cover the above scenarios and pass. The new tab is visible in the TUI, workflows enumerate and display correctly with search/filter working. Entering a workflow opens the definition view with all steps pending. Escape from definition view returns to the list on the new tab. Pressing `r` on a workflow row (in the list or definition view) emits `StartRunMsg` — the actual launch behavior is wired in the subsequent param-form-run-launch task and is NOT required to be end-to-end functional here.
