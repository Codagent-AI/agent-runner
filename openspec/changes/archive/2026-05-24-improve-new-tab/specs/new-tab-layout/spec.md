## ADDED Requirements

### Requirement: Workflow groups render with header and description

The new tab SHALL render each workflow group with a header row showing the group's display name and a description row below it. The header and description SHALL appear above the group's workflow rows. Header and description rows SHALL NOT be selectable — the cursor SHALL skip over them when the user navigates with the keyboard.

#### Scenario: Project group renders with header and description
- **WHEN** the new tab renders and the project scope contains at least one visible workflow
- **THEN** a header row identifying the group as the project's workflows appears above the project workflows
- **AND** a non-empty description row appears below the header (exact copy is an implementation detail and not pinned by this spec)

#### Scenario: User group renders with header and description
- **WHEN** the new tab renders and the user scope contains at least one visible workflow
- **THEN** a header row identifying the group as the user's workflows appears above the user workflows
- **AND** a non-empty description row appears below the header (exact copy is an implementation detail and not pinned by this spec)

#### Scenario: Builtin group renders using namespace metadata
- **WHEN** the new tab renders a builtin namespace whose metadata file declares a display name and description
- **THEN** the header row shows the declared display name
- **AND** the description row shows the declared description

#### Scenario: Header rows are not focusable when navigating downward
- **WHEN** the cursor is on the row immediately above a header row and the user presses `down`
- **THEN** the cursor moves to the first workflow row below the header, skipping both the header and description rows

#### Scenario: Header rows are not focusable when navigating upward
- **WHEN** the cursor is on the first workflow row of a non-first group and the user presses `up`
- **THEN** the cursor lands on the last workflow row of the previous group, skipping the current group's header, description, and the separator between the groups

#### Scenario: Initial cursor position skips the leading header
- **WHEN** the new tab opens fresh with at least one visible workflow
- **THEN** the initial cursor position is on the first workflow row of the first non-empty group, not on the header or description row above it

#### Scenario: Upward navigation from the first workflow focuses the search box
- **WHEN** the cursor is on the first workflow row of the first visible group and the user presses `up`
- **THEN** the search box receives focus and the cursor leaves the workflow list

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

A group SHALL be omitted entirely (header row, description row, and workflow rows) from the new tab when it has zero visible workflows. A workflow is "not visible" when it is hidden via `hidden: true` and the show-hidden toggle is off, or when the current search filter excludes it.

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
