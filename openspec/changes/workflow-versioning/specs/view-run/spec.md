## MODIFIED Requirements

### Requirement: Drill-in navigation with breadcrumbs

The run view SHALL support drilling into sub-workflows and loops via a drill-in model. Enter on a drillable row SHALL scope both the step list AND the log to that container's subtree: the step list shows that container's children, and the log shows those children's blocks (with descendants inline). A breadcrumb line at the top SHALL show the current depth path (run name, then each entered container in order).

When a saved run is opened for non-live inspection from the run list or through `--inspect`, the top-level breadcrumb SHALL show the recorded workflow version next to the version-free canonical runnable name using a `v<major>.<minor>` label. The version label SHALL remain present at every drill depth and for every saved-run status. A saved run whose recorded workflow file is unversioned SHALL remain inspectable and SHALL display `unversioned` instead of a numeric version.

The live run view and the pre-run definition preview MUST NOT show a workflow version. The exact separator and styling of the non-live version label are design details.

Drillable rows SHALL be: sub-workflow steps and loop steps. Drill-in SHALL be available on `pending` containers (children read from the workflow file or resolved statically) as well as executed ones.

#### Scenario: Top-level breadcrumb rendering
- **WHEN** the run view is at the top level (no drill-in)
- **THEN** the breadcrumb shows the workflow's version-free canonical runnable name, the start time, and the run status (active/failed/completed/inactive)

#### Scenario: Non-live saved run shows recorded version
- **WHEN** a saved run recorded `deploy-v2.0.yaml` and is opened from the run list or through `--inspect`
- **THEN** the top-level breadcrumb shows canonical name `deploy` with version label `v2.0`

#### Scenario: Saved-run status does not suppress version
- **WHEN** a saved run is opened for non-live inspection with status inactive, failed, or completed
- **THEN** the breadcrumb shows its recorded version alongside the status

#### Scenario: Version remains visible after drill-in
- **WHEN** the user drills into a sub-workflow, loop, or iteration while inspecting a saved versioned run
- **THEN** the breadcrumb retains the top-level workflow's recorded version while appending the entered container

#### Scenario: Live run omits version
- **WHEN** a workflow is executing in the live run view
- **THEN** the breadcrumb shows the version-free canonical workflow name without a version label

#### Scenario: Definition preview omits version
- **WHEN** the user opens a workflow's pre-run definition preview
- **THEN** the breadcrumb shows the version-free canonical workflow name without a version label

#### Scenario: Legacy saved run displays unversioned
- **WHEN** a saved run recorded an unversioned workflow file and is opened for non-live inspection
- **THEN** the breadcrumb displays `unversioned` and inspection continues without a filename-version error

#### Scenario: Enter on sub-workflow drills in and scopes log
- **WHEN** the user presses Enter on a sub-workflow step row
- **THEN** the step list is replaced by the sub-workflow's children, the log is scoped to show only that sub-workflow's children's blocks (and their descendants inline), and the breadcrumb appends the sub-workflow entry

#### Scenario: Enter on loop drills into iteration list and scopes log
- **WHEN** the user presses Enter on a loop step row
- **THEN** the step list is replaced by a list of iterations, the log is scoped to show only that loop's iteration blocks (and their descendants inline), and the breadcrumb appends the loop entry

#### Scenario: Enter on iteration drills into iteration children
- **WHEN** the user presses Enter on an iteration row in the iteration list
- **THEN** the step list is replaced by that iteration's child steps, the log is scoped to show only those children's blocks (and their descendants inline), and the breadcrumb appends the iteration identifier

#### Scenario: Drill in to pending sub-workflow
- **WHEN** the user presses Enter on a sub-workflow step that has not yet executed
- **THEN** the sub-workflow file is read and its children are displayed with status `pending`; the log contains no blocks at that level (pending steps are hidden from the log)

#### Scenario: Enter on shell step is a no-op
- **WHEN** the user presses Enter on a shell step row
- **THEN** nothing happens (shell steps are neither drillable nor resumable)

#### Scenario: Enter on agent step without session ID is a no-op
- **WHEN** the user presses Enter on an agent step that has no resolved session ID
- **THEN** nothing happens (the resume action requires a session ID)
