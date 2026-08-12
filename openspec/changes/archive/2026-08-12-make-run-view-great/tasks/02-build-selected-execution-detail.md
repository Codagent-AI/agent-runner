# Task: Build the selected-execution detail document

## Goal

Replace the recursive continuous log with a semantic, width-aware document for exactly one selected real execution. Deliver compact per-type metadata, labeled rails, bounded expandable input, selected output and lazy loading, copy semantics, progress animation, pending detail, and ordinary agent-call filtering/resume behavior without changing persisted evidence.

This is a production replacement, not a second rendering path. Once all step types are covered, retire the cross-step ranges, recursive detail blocks, selection-from-log coupling, and cross-step auto-scroll that exist only for the old continuous log.

## Background

You MUST read:

- `openspec/changes/make-run-view-great/proposal.md`, especially the selected-step presentation, metadata, copy, large-output, and removed-narrative decisions.
- `openspec/changes/make-run-view-great/design.md`, especially sections 4 and 6 (“Build a semantic document for one selected node” and “Render labeled rails instead of blocks and separators”) plus the selected-output portions of section 9.
- `openspec/changes/make-run-view-great/specs/view-run/spec.md`, especially `Detail pane per step type`, `Copy selected step detail`, `Temporary detail for selected pending step`, `In-progress agent progress indicator in the log`, `Agent-call detail and resume`, `Current input preview and expansion`, and the three removed requirements.
- `openspec/changes/make-run-view-great/test-plan.md` for per-type detail, metrics, copy, lazy-output, and deterministic fake-agent strategy. Do not execute or claim the `AT-*` flows in this implementation task.

Relevant production paths:

- `internal/runview/detail.go`: existing per-type metadata, output renderers, wrapping, metrics/model rules, failure context, and progress glyph.
- `internal/runview/logview.go`: recursive continuous-log construction and ranges to replace after characterization coverage exists.
- `internal/runview/model.go`: selected detail, scroll state, copy, full-output loading, pulse scheduling, call-output loading/filtering, resume eligibility, and current cross-step synchronization.
- `internal/runview/output.go`, `clipboard.go`, `view.go`, `summary.go`, and `tree.go`.
- Existing tests in `logview_test.go`, `model_test.go`, `view_test.go`, `clipboard_test.go`, `tui_metrics_test.go`, and `agent_call_view_test.go`.

Implementation constraints:

- Use TDD. Characterize persisted output, metrics/model resolution, direct call resume, clipboard notices, and large-output behavior before deleting the old renderer.
- Introduce a semantic document/section representation equivalent to the approved `detailDocument` and `detailSection` design. The same semantic source must render styled screen text and unstyled copy text.
- Build detail for only the selected `StepNode`. Changing selection replaces the document and starts manual detail scrolling at the top.
- Render one compact primary header. Preserve status, type, outcome/duration, relevant triggered conditions, `capture`, exit status, agent profile/CLI/model, call target, metrics, and error/resume context. Do not display ordinary session IDs, workdirs, request IDs, session strategy, or inactive modifiers.
- Keep current model-resolution semantics: model under CLI; profile default when no inline override; session-originating model for resumed/inherited sessions; `(unknown)` when unresolved.
- Keep metrics semantics: collected usage and reported cost when present; `?` and reason for explicit unavailability; absent cost is not `$0.00`; legacy entries without structured metrics omit the fields.
- Use labeled left rails for semantic sections. Exact glyphs/colors/padding are design choices, but labels and information ownership are normative.
- Preserve raw stdout/stderr separately and unchanged. Agent-call display must use the resolved adapter’s ordinary filtering and bounded persisted files, never reconstruct a full response from `audit.log`.
- A current prompt, command, or script is collapsed by visual rows, not source lines. Store expansion by `NodeKey()` so selection changes and resize do not accidentally discard it. `i` is inert when the wrapped input fits.
- Copy must use the same semantic document, strip styling, prepend directory and breadcrumb, include complete input even when collapsed, and reflect the selected output’s current lazy-loading state.
- For `Copy includes selected context`, this task owns serialization of an already-built optional `Previous` section and must unit-test that contract with a semantic-document fixture. Audit-order derivation and production population of that section are outside this task.
- Keep `g` bound to the selected execution’s full-output loader. Loading output or resizing cannot move selection to another execution.
- Pending detail is statically knowable only and must never invent runtime data or a previous execution that recorded order cannot establish.
- Progress animation belongs inside the selected headless-agent/call `Current response` rail and must stop for terminal or inactive interrupted executions.
- Containers show compact aggregate direct-child counts/status/duration; they do not recursively render child detail.
- UI detail must expose a stable `Current form`/`Current outcome` section boundary for live UI integration, without embedding modal ownership into the generic detail builder.

## Spec

The following source requirements and scenarios are controlling excerpts from `specs/view-run/spec.md`.

### Requirement: Detail pane per step type

The right pane SHALL render detail for exactly one selected real tree row. Changing selection SHALL replace the pane's content; the pane SHALL NOT stack blocks for other steps or recursively embed descendant detail.

The selected detail SHALL begin with compact primary metadata. Identity, type, status, outcome, and duration SHALL appear when available. Agent steps SHALL show profile, CLI, and model; agent calls SHALL additionally show their target. Completed agent and call executions SHALL show concise usage and reported cost when available. Shell and script steps SHALL show exit status. `capture` SHALL appear when configured, and a `skip_if` or `break_if` expression SHALL appear when it explains the recorded outcome.

Session IDs, workdirs, request IDs, session strategy, and inactive modifiers SHALL be omitted from the primary presentation unless a value is needed for an available resume action or to explain an error. Omitting primary metadata MUST NOT remove it from persisted run evidence.

Current-step content SHALL be grouped into visually distinct, labeled rail sections:

- **Headless agent and agent call**: `Current prompt` and `Current response`. The response SHALL use the resolved adapter's ordinary filtered output.
- **Interactive agent**: `Current prompt`; no response transcript SHALL be fabricated when terminal output was not captured.
- **Shell**: `Current command` and `Current output`, with stdout and stderr distinguishable.
- **Script**: `Current script` and `Current output`, with stdout and stderr distinguishable.
- **UI**: `Current form` and `Current outcome`, integrated with the existing live UI behavior.
- **Sub-workflow, loop, iteration, and group**: `Current status`, containing identity, workflow params or loop counters, outcome, duration, and aggregate direct-child counts by status. Container detail SHALL NOT list child rows or render descendant detail.

The exact rail glyph, rail color, padding, and label styling are design decisions, but every section SHALL have its semantic label.

The latest attempt of a re-executed logical step SHALL provide its selected-detail outcome, duration, usage, and cost, with the attempt number shown when greater than one. Earlier attempts SHALL remain in run-level aggregates.

Agent metadata SHALL retain the existing model rules: the model line follows the CLI line, uses the profile default when there is no inline override, uses the session-originating step's effective model for resumed or inherited sessions, and displays `(unknown)` when no model can be resolved.

Completed agent and call detail SHALL retain explicit metrics semantics. Collected usage and reported cost appear beside duration. Unavailable usage displays `?` and its reason when known; absent cost displays `?`, never `$0.00`. Legacy audit entries with no structured metric fields omit usage and cost lines instead of inventing unavailable values.

#### Scenario: Selection replaces detail
- **WHEN** the user moves from one real tree row to another
- **THEN** the right pane replaces the prior content with detail for the newly selected row

#### Scenario: Interactive agent detail has no fabricated transcript
- **WHEN** a completed interactive agent row is selected and no terminal transcript was captured
- **THEN** the pane shows known agent metadata, prompt, outcome, and duration without a `Current response` transcript

#### Scenario: Container detail is aggregate only
- **WHEN** a sub-workflow, loop, iteration, or group row is selected
- **THEN** `Current status` shows the container's own metadata and aggregate direct-child status counts without listing child detail rows

#### Scenario: Triggered condition is explanatory metadata
- **WHEN** `skip_if` caused a selected step to skip or `break_if` caused a selected loop to stop
- **THEN** the expression responsible for that recorded outcome appears in the primary detail

#### Scenario: Agent block shows unavailable usage marker
- **WHEN** a completed selected agent or call has an unavailable usage record
- **THEN** usage and cost show `?`, the usage reason appears when available, and neither zero token counts nor `$0.00` is fabricated

#### Scenario: Agent block shows model for a resumed or inherited session
- **WHEN** a selected agent uses `session: resume` or `session: inherit`
- **THEN** the model line shows the effective model used to launch the originating CLI session

### Requirement: Copy selected step detail

The run view SHALL provide a `c` keybinding that copies the current directory, current breadcrumb, and the selected row's detail to the system clipboard as plain text with terminal styling removed. The copied content SHALL include the complete untruncated current prompt, command, or script even when the on-screen input preview is collapsed.

Copied response/output SHALL reflect the selected output's current large-output loading state. A previous-execution rail that is visible for the selected row SHALL be included. Existing success notice, two-second clearing behavior, and clipboard failure notice SHALL remain.

#### Scenario: Copy includes full collapsed input
- **WHEN** the selected input exceeds three visual rows, its preview is collapsed, and the user presses `c`
- **THEN** the copied text contains the complete input rather than only the three-row preview

#### Scenario: Copy includes selected context
- **WHEN** a previous-execution rail is visible and the user presses `c`
- **THEN** the copied text includes directory, breadcrumb, previous execution, primary metadata, current input, and current response/output

This task's portion of the scenario is the semantic copy contract when a `Previous` section is present; it does not derive which execution supplies that section.

#### Scenario: Copy preserves lazy-output state
- **WHEN** exceptionally large output has not been fully loaded and the user copies the selected detail
- **THEN** the copied response/output matches the currently loaded output state

### Requirement: Temporary detail for selected pending step

Selecting a pending row SHALL replace the right pane with visually pending detail containing only statically knowable fields. The pane SHALL show identity, type, and the configured command, script, prompt, form, workflow path, or raw template params as applicable. It SHALL NOT show runtime outcome, duration, metrics, response, output, or a previous execution that cannot be established from recorded execution order.

Pending input SHALL use the same labeled rail and three-row preview/expansion behavior as executed input.

#### Scenario: Pending leaf shows static input
- **WHEN** the user selects a pending shell, script, agent, or UI row
- **THEN** the pane shows its statically configured input and no runtime result fields

#### Scenario: Pending container shows raw params
- **WHEN** the user selects a pending sub-workflow
- **THEN** the pane shows its resolved workflow path and raw template params

### Requirement: Current input preview and expansion

A current prompt, command, or script that wraps beyond three visual rows SHALL initially show only its first three rows in the appropriate labeled rail, with an ellipsis indicating omitted content and an `i expand` hint. Pressing `i` SHALL expand the complete input inline and change the hint to `i collapse`. Pressing `i` again SHALL restore the three-row preview.

When the complete input fits within three visual rows, no expansion hint SHALL appear and `i` SHALL have no effect. A terminal resize SHALL recompute whether the input exceeds three rows and SHALL preserve the expanded/collapsed choice whenever expansion remains applicable.

#### Scenario: Long prompt starts collapsed
- **WHEN** a selected agent prompt wraps to more than three visual rows
- **THEN** `Current prompt` shows the first three rows, an ellipsis, and `i expand`

#### Scenario: i expands complete input
- **WHEN** a long current input is collapsed and the user presses `i`
- **THEN** the complete input appears inline and the hint changes to `i collapse`

#### Scenario: Resize recomputes visual rows
- **WHEN** a terminal resize changes the wrapped input height
- **THEN** the preview and toggle availability update for the new detail width

### Requirement: In-progress agent progress indicator in the log

A selected in-progress headless agent or agent call SHALL display an animated progress indicator inside its `Current response` rail. Before response text exists, the indicator SHALL occupy the response body. After response text begins streaming, a compact animated indicator SHALL appear below the visible response.

The indicator SHALL disappear when the execution reaches a terminal status or when an interrupted in-progress execution is viewed without an active run. Exact animation frames and colors are design decisions.

#### Scenario: Spinner shown before response
- **WHEN** a selected headless agent or call is running and has no response text
- **THEN** `Current response` contains an animated progress indicator

#### Scenario: Spinner absent for inactive interrupted run
- **WHEN** an interrupted step retains in-progress status but the run is inactive
- **THEN** selected detail shows no animation

### Requirement: Agent-call detail and resume

Selecting an agent-call row SHALL show target kind and name; resolved profile, CLI, and model; prompt, outcome, duration, metrics, error context, and retained stdout and stderr through the ordinary selected-step rails. Session metadata and working directory SHALL remain in persisted evidence but SHALL appear in primary detail only when needed for an available resume action or to explain an error.

Called-agent stdout and stderr SHALL use the resolved CLI adapter's ordinary headless output, result, and diagnostic filtering for successful and failed calls. Filtering display output SHALL NOT alter raw persisted evidence, and full output SHALL NOT be reconstructed from `audit.log`.

When the run is inactive, a completed called-agent execution with a known CLI session ID SHALL offer the existing direct session-resume action. The action SHALL be unavailable while the run is active or when no session ID is known.

#### Scenario: Persisted call output is displayed
- **WHEN** ordinary headless-agent output persistence created stdout or stderr files for the selected call
- **THEN** `Current response` displays the output after applying the resolved CLI adapter's ordinary filtering

#### Scenario: Raw persisted evidence remains unchanged
- **WHEN** the detail pane filters called-agent output for display
- **THEN** the raw persisted stdout and stderr files remain unchanged for evidence and debugging

#### Scenario: Audit metadata is not treated as full output
- **WHEN** no persisted output exists for a selected call
- **THEN** the run view does not reconstruct or display a full child response from `audit.log`

#### Scenario: Resume unavailable during active run
- **WHEN** the run is active or the selected call has no known CLI session ID
- **THEN** the direct resume action is unavailable for that call

### Removed requirements

`Pending steps hidden from log`, `Cross-step auto-scroll while run is active`, and `Recursive log nesting with progressive separators` are removed. Their approved migrations are:

- Pending rows remain in the tree and show statically knowable selected detail when chosen.
- Active-step selection and selected-response tail-follow are defined by `live-run-view`.
- Inline ancestry expansion provides bounded context, and manual drill-in exposes all direct children.

Do not preserve the removed continuous-log behaviors through hidden compatibility code.

## Test Plan

No named `INT-*` or `E2E-*` obligation is assigned to this renderer replacement. Add focused unit/model tests for every selected type and for:

- semantic screen/copy variants;
- collapsed and expanded visual-row input across resize;
- selected-output lazy loading;
- stdout/stderr distinction and error rails;
- call filtering, missing output, raw-evidence preservation, and direct resume eligibility;
- latest-attempt metrics/model rules and unavailable/legacy metrics;
- pending-only data;
- container aggregation;
- progress before output, after output, on completion, and for inactive interrupted runs; and
- removal of cross-step log ranges, recursive nesting, and selection/scroll coupling.

## Done When

- The right pane is built from one selected-node semantic document and changing selection replaces it.
- Every step type satisfies all scenarios in the assigned `view-run` requirements, including scenarios not repeated above.
- Styled screen rendering and plain-text copying share semantic content while allowing full copied input behind a collapsed preview.
- An already-populated `Previous` section is included in plain-text copy; production audit-order selection of that section is not a completion condition here.
- Large selected output remains bounded until explicitly loaded and never comes from unbounded or synthetic audit reconstruction.
- Agent/call metadata, filtering, metrics, model resolution, resume context, and persisted evidence retain their approved semantics.
- Pending and container detail contain no fabricated runtime or descendant content.
- Progress animation appears only for the selected active headless agent/call.
- Recursive continuous-log rendering, cross-step scroll synchronization, and obsolete range state/tests are removed after replacement coverage passes.
- Focused `internal/runview` tests pass.
