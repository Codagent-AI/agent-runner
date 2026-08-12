## ADDED Requirements

### Requirement: Plan with an agent entry

The new tab SHALL render a selectable "Plan with an agent" entry above every workflow group. Selecting it SHALL start the built-in intake workflow. The entry SHALL always be present: it SHALL NOT be removed by the search filter, and the show-hidden toggle SHALL NOT affect it. Exact copy and visual treatment are implementation details and are not pinned by this spec.

#### Scenario: Entry renders above every group
- **WHEN** the new tab renders with at least one visible workflow
- **THEN** the intake entry appears above the first group's header

#### Scenario: Entry renders when no workflow is visible
- **WHEN** the new tab renders and no workflow group is visible
- **THEN** the intake entry still appears and remains selectable

#### Scenario: Selecting the entry starts intake
- **WHEN** the cursor is on the intake entry and the user activates it
- **THEN** Agent Runner starts a run of the built-in intake workflow

#### Scenario: Search filter does not remove the entry
- **WHEN** the user types a search filter that excludes some or all workflow rows
- **THEN** the intake entry remains visible regardless of the filter text

#### Scenario: Hidden toggle does not affect the entry
- **WHEN** the user presses `h` to toggle hidden workflow visibility
- **THEN** the intake entry's presence is unchanged

#### Scenario: Run shortcut activates the entry
- **WHEN** the cursor is on the intake entry and the user presses the run shortcut `r`
- **THEN** Agent Runner starts a run of the built-in intake workflow, exactly as activating it with Enter does

#### Scenario: Downward navigation leaves the entry
- **WHEN** the cursor is on the intake entry and the user presses `down`
- **THEN** the cursor moves to the first workflow row of the first visible group, skipping that group's header

## MODIFIED Requirements

### Requirement: Workflow groups render with header and description

The new tab SHALL render each workflow group with a header containing the group's display name and its description. The header SHALL appear above the group's workflow rows. The header SHALL NOT be selectable — the cursor SHALL skip over it when the user navigates with the keyboard. The visual arrangement of the display name relative to the description (same line, separate lines, etc.) is an implementation detail and not pinned by this spec.

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

#### Scenario: Initial cursor position is the intake entry
- **WHEN** the new tab opens fresh
- **THEN** the initial cursor position is on the "Plan with an agent" entry, above the first group's header

#### Scenario: Upward navigation from the first workflow reaches the intake entry
- **WHEN** the cursor is on the first workflow row of the first visible group and the user presses `up`
- **THEN** the cursor lands on the intake entry, skipping that group's header

#### Scenario: Upward navigation from the intake entry focuses the search box
- **WHEN** the cursor is on the intake entry and the user presses `up`
- **THEN** the search box receives focus and the cursor leaves the list
