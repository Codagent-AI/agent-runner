## ADDED Requirements

### Requirement: Workflow definition view mode
The system SHALL support a view mode that renders a workflow's definition without an associated run instance. The step list, detail pane, drill-in navigation, breadcrumbs, and keyboard shortcuts SHALL behave identically to the `view-run` capability, with all steps in `pending` status. The breadcrumb SHALL show the workflow's canonical name with no run ID, start time, or run status.

#### Scenario: All steps shown as pending
- **WHEN** the workflow definition view opens for a workflow
- **THEN** the step list is populated from the workflow definition file with every row in `pending` status

#### Scenario: Drill-in works on sub-workflow steps
- **WHEN** the user presses Enter on a sub-workflow step in the definition view
- **THEN** the referenced workflow file is loaded and its children are displayed, matching `view-run` drill-in behavior

#### Scenario: Breadcrumb shows workflow name only
- **WHEN** the workflow definition view is open
- **THEN** the breadcrumb shows the workflow's canonical name (e.g. `core:finalize-pr`) with no run ID, start time, or status

### Requirement: Start run from definition view
The workflow definition view SHALL provide an `r` keybinding at any drill depth that initiates starting a run of the top-level displayed workflow. The help bar SHALL show `r start run`. Pressing `r` SHALL transition to the `workflow-param-form` (if the workflow has parameters) or launch the run directly (if no parameters). After launch, the view transitions to a live run view for the newly created run.

#### Scenario: r on workflow with parameters opens param form
- **WHEN** the user presses `r` on a workflow that declares one or more parameters
- **THEN** the param form is presented for that workflow

#### Scenario: r on workflow with no parameters launches immediately
- **WHEN** the user presses `r` on a workflow with no declared parameters
- **THEN** a new run is launched and the view transitions to the live run view

#### Scenario: r while drilled into a sub-workflow starts the top-level workflow
- **WHEN** the user has drilled into a sub-workflow step and presses `r`
- **THEN** the param form (or direct launch) is triggered for the top-level workflow displayed in the definition view, not the drilled-in sub-workflow

#### Scenario: Help bar shows r binding
- **WHEN** the workflow definition view is open at any drill depth
- **THEN** the help bar includes `r start run`
