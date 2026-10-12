# new-tab-layout Specification

## Purpose
Define the workflow browser's New tab grouping, filtering, row rendering, and launch affordances.
## Requirements
### Requirement: Group ordering

Groups SHALL appear in this top-to-bottom order: project group first, user group second, then built-in namespace groups in the sequence `spec-driven`, `openspec`, `onboarding`, `core`. When two built-in namespaces tie on the configured ordering signal (or both lack one), ordering SHALL fall back to alphabetical by namespace name.

#### Scenario: Built-in namespaces render in the declared sequence
- **WHEN** the new tab renders with all four built-in namespaces (`spec-driven`, `openspec`, `onboarding`, `core`) present and at least one visible workflow in each
- **THEN** the groups appear in the order: Project, User, spec-driven, openspec, onboarding, core

#### Scenario: Project and user always render above built-ins
- **WHEN** the new tab renders with at least one visible workflow in the project or user scope
- **THEN** that group appears above every built-in group, regardless of built-in ordering configuration

#### Scenario: Unconfigured namespace falls back to alphabetical
- **WHEN** a built-in namespace is present that is not listed in the hardcoded ordering
- **THEN** the namespace renders after the listed namespaces, ordered alphabetically with respect to other unlisted namespaces

### Requirement: Empty groups omitted

A group SHALL be omitted entirely (header and workflow rows) from the new tab when it has zero visible workflows. A workflow is "not visible" when it is hidden via `hidden: true` and the show-hidden toggle is off, or when the current search filter excludes it.

#### Scenario: Scope with zero workflows omits its group
- **WHEN** the project scope contains zero workflows
- **THEN** no "Project workflows" header or description appears on the new tab

#### Scenario: Search filter excluding all rows in a group hides the group
- **WHEN** the user's search filter matches at least one workflow overall, but no workflow in a particular group
- **THEN** that group's header, description, and rows are omitted; only groups with at least one matching workflow render

#### Scenario: Namespace containing only hidden workflows is omitted when toggle is off
- **WHEN** every workflow in a built-in namespace declares `hidden: true` and the show-hidden toggle is off
- **THEN** the namespace's header, description, and rows are omitted entirely from the new tab

### Requirement: Hidden workflow YAML field

Workflows MAY declare `hidden: true` at the top level of their YAML frontmatter to mark themselves as sub-workflows that the new tab omits by default. The `hidden` field SHALL be optional; absence SHALL be equivalent to `hidden: false`. The `hidden` field SHALL have no effect on workflow loading, name resolution, sub-workflow reference resolution, or execution — it is a display-layer hint only.

#### Scenario: Hidden workflow omitted from new tab by default
- **WHEN** a workflow's YAML frontmatter contains `hidden: true` and the show-hidden toggle is off
- **THEN** the workflow does not appear on the new tab

#### Scenario: Hidden workflow runnable from CLI
- **WHEN** a workflow's YAML frontmatter contains `hidden: true`
- **AND** the user invokes `agent-runner run <name>` for that workflow
- **THEN** the workflow loads and executes exactly as if `hidden` were not set

#### Scenario: Hidden workflow usable as sub-workflow reference
- **WHEN** a parent workflow references a hidden workflow via a `workflow:` step
- **THEN** the reference resolves and the sub-workflow executes normally

#### Scenario: Workflows without `hidden` always visible
- **WHEN** a workflow's YAML frontmatter does not contain a `hidden` field, or sets it to `false`
- **THEN** the workflow appears on the new tab whenever its group is visible

#### Scenario: Search does not surface hidden workflows when toggle is off
- **WHEN** the show-hidden toggle is off and the user types a search filter whose substring matches a hidden workflow's canonical name
- **THEN** the hidden workflow does not appear in the filtered list
- **AND** the hidden filter is applied before the search filter

#### Scenario: Search applies to hidden workflows when toggle is on
- **WHEN** the show-hidden toggle is on and the user types a search filter
- **THEN** hidden workflows matching the search filter appear in the list alongside non-hidden matches

### Requirement: Toggle hidden visibility with `h`

Pressing `h` on the new tab SHALL toggle whether hidden workflows are included in the displayed list. The toggle state SHALL default to "hide" each time the new tab is opened — opening the new tab fresh always starts with hidden workflows hidden. The help bar SHALL include `h hidden` while the new tab is active.

#### Scenario: First press reveals hidden workflows
- **WHEN** the user is on the new tab with the show-hidden toggle in its default "off" state and presses `h`
- **THEN** all workflows including those with `hidden: true` appear in the list

#### Scenario: Second press hides them again
- **WHEN** the show-hidden toggle is on and the user presses `h`
- **THEN** workflows with `hidden: true` are removed from the displayed list

#### Scenario: New tab opens with hidden workflows hidden
- **WHEN** the user opens the new tab fresh (e.g., from another tab or after restarting the TUI)
- **THEN** the show-hidden toggle is in its "off" state regardless of its prior state

#### Scenario: Search text persists across `h` press
- **WHEN** the search box contains a non-empty filter and the user (with the search box not focused) presses `h`
- **THEN** the show-hidden toggle flips and the search filter is unchanged

#### Scenario: `h` while search box has focus is captured as input
- **WHEN** the search box has focus and the user types `h`
- **THEN** the character is appended to the search text and the show-hidden toggle is unchanged

#### Scenario: Help bar advertises the shortcut
- **WHEN** the new tab is active
- **THEN** the help bar includes an `h hidden` entry regardless of the current toggle state

### Requirement: Latest logical workflow rows

The New tab SHALL select one candidate per canonical logical workflow name before applying its existing grouping, hidden-workflow, and search behavior. A valid logical group SHALL be represented by its numerically highest major/minor version. The row label and searchable identity SHALL use the version-free canonical logical name and MUST NOT display or index the physical filename, version suffix, or source path.

Opening a logical workflow's definition or starting its row SHALL use the selected latest version. Older versions MUST NOT appear as separate rows or become substitutes under search or the existing show-hidden toggle. Visibility, description, parameters, and other row behavior SHALL come from the selected latest version.

An invalid logical group SHALL appear as one non-launchable row labeled with its version-free logical name and containing its actionable validation error. A valid versioned sibling MUST NOT make that invalid group launchable. Invalid rows SHALL remain searchable by their version-free logical name.

#### Scenario: Multiple versions render one logical row
- **WHEN** discovery finds `deploy-v1.0.yaml` and `deploy-v2.0.yaml` in the winning source
- **THEN** the New tab renders exactly one row labeled `deploy`
- **AND** the row does not display either physical filename or a version suffix

#### Scenario: Definition view opens latest version
- **WHEN** `deploy-v2.0.yaml` is the selected latest version and the user opens the `deploy` row
- **THEN** the definition view loads `deploy-v2.0.yaml`

#### Scenario: Start action launches latest version
- **WHEN** `deploy-v2.0.yaml` is the selected latest version and the user starts the `deploy` row
- **THEN** Agent Runner starts a run from `deploy-v2.0.yaml`

#### Scenario: Version query does not match physical filename
- **WHEN** the New tab contains logical row `deploy` backed by `deploy-v2.0.yaml` and the user searches for `v2.0`
- **THEN** the `deploy` row is not included solely because its physical filename contains that version

#### Scenario: Logical-name query matches latest row
- **WHEN** the New tab contains logical row `team/deploy` backed by `team/deploy-v2.0.yaml` and the user searches for `deploy`
- **THEN** the latest logical row remains in the filtered results

#### Scenario: Older versions never render separately
- **WHEN** a logical workflow has multiple versions and the user searches or enables show-hidden
- **THEN** no older version appears as a separate row

#### Scenario: Latest hidden metadata controls visibility
- **WHEN** `deploy-v1.0.yaml` has `hidden: false` and selected latest `deploy-v2.0.yaml` has `hidden: true`
- **THEN** the `deploy` row is hidden by default and enabling show-hidden reveals the row backed by `deploy-v2.0.yaml`
- **AND** `deploy-v1.0.yaml` never substitutes for the hidden latest version

#### Scenario: Invalid group renders diagnostic row
- **WHEN** discovery finds invalid unversioned `deploy.yaml` and no other definition in the group
- **THEN** the New tab renders one non-launchable `deploy` row containing the actionable versioned-filename error

#### Scenario: Valid sibling does not make invalid group launchable
- **WHEN** discovery finds invalid `deploy.yaml` and valid `deploy-v2.0.yaml`
- **THEN** the New tab renders one non-launchable `deploy` error row rather than a launchable latest-version row

#### Scenario: Invalid row searchable by logical name
- **WHEN** the New tab contains an invalid `deploy` error row and the user searches for `deploy`
- **THEN** the error row remains in the filtered results so the user can read its migration guidance

#### Scenario: Invalid row actions are disabled
- **WHEN** the cursor is on an invalid logical workflow row and the user attempts to open or start it
- **THEN** no workflow definition view or run is launched

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

