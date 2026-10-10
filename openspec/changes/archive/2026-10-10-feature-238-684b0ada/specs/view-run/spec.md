## MODIFIED Requirements

### Requirement: Run-view entry points
The CLI SHALL provide three entry points to the run view: a `--inspect <run-id>` flag for direct entry, an Enter action on a run in the list TUI (covered by the `list-runs` capability), and an Enter action on a workflow row in the list TUI's New tab, which opens the workflow definition view (see "Workflow definition view"). Direct entry SHALL require a full run ID (no prefix matching). When the target run's run-lock is held by another live process, `--inspect` SHALL reject the entry with an error and not launch the TUI.

#### Scenario: --inspect launches run view
- **WHEN** `agent-runner --inspect <run-id>` is invoked and the run exists and is not locked by another live process
- **THEN** the run-view TUI launches for that run

#### Scenario: --inspect with unknown run ID
- **WHEN** `agent-runner --inspect <run-id>` is invoked and the run does not exist
- **THEN** agent-runner prints an error message naming the missing run ID and exits with a non-zero status

#### Scenario: --inspect requires full run ID
- **WHEN** `agent-runner --inspect <prefix>` is invoked with a prefix that is not a complete run ID
- **THEN** agent-runner treats it as "not found" and exits non-zero

#### Scenario: --inspect is mutually exclusive with --list and --resume
- **WHEN** `agent-runner --inspect <run-id>` is invoked together with `--list` or `--resume`
- **THEN** agent-runner prints an error indicating the flags are mutually exclusive and exits non-zero

#### Scenario: --inspect rejects a run locked by another process
- **WHEN** `agent-runner --inspect <run-id>` is invoked and the target run's run-lock belongs to another live process
- **THEN** agent-runner prints an error to stderr identifying the run as active in another process and exits non-zero; no TUI is launched

#### Scenario: --inspect proceeds past a stale lock
- **WHEN** `agent-runner --inspect <run-id>` is invoked and the target run's run-lock PID is dead
- **THEN** the lock is treated as stale and the run-view TUI launches normally

#### Scenario: Enter on a New tab workflow opens the definition view
- **WHEN** the user presses Enter on a valid workflow row in the list TUI's New tab
- **THEN** the run view opens in workflow definition mode for that row's selected workflow file, with no run instance attached

### Requirement: Exit behavior
The run view SHALL support two exit mechanisms. Escape SHALL navigate up one breadcrumb level. At the top level, Escape SHALL:
- return to the list TUI, restoring its prior state, when the view was entered from the list TUI for a run that is neither completed nor failed, or when the view is a workflow definition view;
- follow "Post-run Escape navigates to list" when the run has completed or failed and the view was entered from the list TUI or is a live run view whose workflow has finished;
- exit the program when the view was entered via `--inspect`.

The `q` key SHALL unconditionally exit the program regardless of depth.

#### Scenario: Escape drills out one level
- **WHEN** the user presses Escape while drilled inside a sub-workflow, loop, or iteration
- **THEN** the view returns to the parent level and the breadcrumb drops its last entry

#### Scenario: Escape at top level returns to list
- **WHEN** the user presses Escape at the top level of a run view entered from the list TUI for a run that is neither completed nor failed, or of a workflow definition view
- **THEN** the run view exits and the list TUI is shown with its prior tab, cursor, and scroll state

#### Scenario: Escape at top level exits program when launched via --inspect
- **WHEN** the user presses Escape at the top level of a run view launched via `--inspect`
- **THEN** the program exits

#### Scenario: q or Ctrl+C exits program
- **WHEN** the user presses `q` or `Ctrl+C` at any depth
- **THEN** the program exits immediately

## ADDED Requirements

### Requirement: Workflow definition view
The run view SHALL support a workflow definition mode that renders a workflow's definition without an associated run instance. The view SHALL load the exact workflow file selected for the New tab row and render every step in `pending` status. Step list rendering, detail pane, drill-in navigation, keyboard focus, scrolling, and the legend overlay SHALL behave as they do for a pending run. The top-level breadcrumb SHALL show the workflow's version-free canonical name with no run ID, start time, or workflow version, followed by an `inactive` status token and a `(r to start run)` affordance.

The definition view SHALL NOT poll for state, follow an active step, resume a run or agent session, or offer the debug action, because no run exists.

#### Scenario: All steps shown as pending
- **WHEN** the workflow definition view opens for a workflow
- **THEN** the step list is populated from the workflow definition file with every row in `pending` status and no audit log is read

#### Scenario: Drill-in works on sub-workflow steps
- **WHEN** the user presses Enter on a statically resolvable sub-workflow step in the definition view
- **THEN** the referenced workflow file is loaded and its direct children are displayed in `pending` status, matching run-view drill-in behavior

#### Scenario: Breadcrumb shows workflow name without run details
- **WHEN** the workflow definition view for `core:finalize-pr` is open
- **THEN** the breadcrumb shows `core:finalize-pr` with no run ID, start time, or version label, followed by `inactive (r to start run)`

#### Scenario: Definition view does not poll
- **WHEN** the run view is in workflow definition mode
- **THEN** no refresh polling occurs and the view changes only in response to user input

### Requirement: Start run from definition view
The workflow definition view SHALL bind `r` at any drill depth to start a run of the top-level workflow it displays. Pressing `r` SHALL present the workflow parameter form (see the `workflow-param-form` capability) when the workflow declares one or more parameters, and SHALL otherwise launch the run immediately. A launched run SHALL replace the TUI process with `agent-runner <canonical-name>` plus any submitted parameters, which opens the live run view for the new run. The help bar SHALL include `r start run` in the definition view, and SHALL NOT offer `r resume` there.

#### Scenario: r on workflow with parameters opens param form
- **WHEN** the user presses `r` in the definition view of a workflow that declares one or more parameters
- **THEN** the param form is presented for that workflow

#### Scenario: r on workflow with no parameters launches immediately
- **WHEN** the user presses `r` in the definition view of a workflow with no declared parameters
- **THEN** a new run of that workflow is launched and the live run view opens for it

#### Scenario: r while drilled into a sub-workflow starts the top-level workflow
- **WHEN** the user has drilled into a sub-workflow step in the definition view and presses `r`
- **THEN** the param form or direct launch is triggered for the top-level workflow, not the drilled-in sub-workflow

#### Scenario: Help bar shows r binding
- **WHEN** the workflow definition view is open at any drill depth
- **THEN** the help bar includes `r start run`

### Requirement: Post-run Escape navigates to list
When the viewed run has completed or failed and the user presses Escape at the top level, the run view SHALL exit and the process SHALL exec `agent-runner --resume` with no run ID, which opens the list TUI on the current-dir tab. This SHALL apply to a live run view whose workflow has finished, including a run resumed with `r` from the run list, and to a completed or failed run opened from the list TUI. Runs opened via `--inspect`, including a completed run opened through `--resume <run-id>`, are unaffected: Escape at the top level still exits the program.

#### Scenario: Escape after live run completion opens list
- **WHEN** a workflow executing in the live run view has completed and the user presses Escape at the top level
- **THEN** the process execs `agent-runner --resume` with no run ID, opening the list TUI on the current-dir tab

#### Scenario: Escape after live run failure opens list
- **WHEN** a workflow executing in the live run view has failed and the user presses Escape at the top level
- **THEN** the process execs `agent-runner --resume` with no run ID, opening the list TUI on the current-dir tab

#### Scenario: Escape after resumed run completion opens list
- **WHEN** a run resumed via `r` from the run list has completed in the live run view and the user presses Escape at the top level
- **THEN** the process execs `agent-runner --resume` with no run ID, opening the list TUI on the current-dir tab

#### Scenario: Escape on completed run opened from list opens list
- **WHEN** the user opened a completed run from the list TUI and presses Escape at the top level
- **THEN** the process execs `agent-runner --resume` with no run ID instead of restoring the previous list state

#### Scenario: Escape after --inspect still exits
- **WHEN** a run opened via `--inspect` is at the top level and the user presses Escape
- **THEN** the program exits
