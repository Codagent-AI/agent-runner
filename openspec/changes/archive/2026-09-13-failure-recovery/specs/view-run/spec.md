# Capability: view-run

## Purpose

Defines the run view: the workflow tree, selected-step detail, navigation, and actions for inspecting a run.

## MODIFIED Requirements

### Requirement: Step list rendering

The run view SHALL render the current manual drill scope as a selectable workflow tree on the left and the selected row's detail on the right. The panes SHALL be separated by whitespace; the run view SHALL NOT render a full-height vertical separator between them.

Each real tree row SHALL display, in order: indentation for its tree depth, a status indicator, the step name, and the type glyph. Loop rows SHALL additionally display an iteration counter in the form `(N/M)`. A check row with `repair` SHALL display a repair suffix once any attempt has run: `(repairing N/M)` while an attempt is active, `(repaired N/M)` after recovery, `(blocked)` after a blocked declaration, and `(N/M)` after exhaustion, where N is attempts used and M is `max`. Each repair attempt SHALL be a child row of the check: a failed check run labeled `attempt N`, followed by either the inline repair agent row or a `rerun N` container whose children are the replayed steps. Attempt children SHALL be shown through the existing inline expansion of direct children when the check is selected or active. Iteration rows SHALL NOT display binding values, per-iteration parameters, or arguments.

Step statuses SHALL be `pending`, `in-progress`, `success`, `warning`, `failed`, and `skipped`. The deepest visible active row's `in-progress` indicator SHALL blink only while the run is active. Visible in-progress ancestor containers SHALL retain a static in-progress indicator while the active leaf blinks. An interrupted step in an inactive run SHALL render a static in-progress indicator. Loop exhaustion SHALL render as success unless that exhaustion was explicitly classified as a non-blocking warning. An otherwise successful container with warning descendants SHALL visibly indicate warning ancestry without becoming an additional warning origin.

The sidebar SHALL measure complete visible rows, including indentation, before truncating names. A name SHALL remain untruncated whenever the preferred sidebar width and a usable detail pane fit within the terminal. When they do not fit, the sidebar SHALL truncate names with an ellipsis while preserving indentation, status, suffixes, and type glyphs. The detail pane SHALL retain at least 20 visual columns whenever the terminal can accommodate the minimum tree chrome, whitespace gap, and that width. The sidebar SHALL NOT have a proportional-width ceiling.

When the terminal is too narrow to fit minimum tree chrome, the whitespace gap, and 20 detail columns, layout SHALL first reduce the name to an ellipsis, then preserve fixed tree chrome for as long as it physically fits, and only then allow the detail pane to fall below 20 columns. If the terminal cannot fit even those fixed elements, rendering SHALL clip safely at the terminal boundary without negative dimensions or a panic.

Within one run-view entry, sidebar width MAY grow when a newly visible row needs more space but SHALL NOT shrink until the terminal is resized or the user exits and re-enters the run view. A terminal resize SHALL recompute the pane widths from the new available width.

Status glyphs SHALL remain `●` running, `○` pending, `✓` success, `✗` failed, and `⇥` skipped. Warning status SHALL use a distinct amber warning indicator. Every supported row type, including shell, script, UI, headless agent, interactive agent, agent call, sub-workflow, loop, iteration, group, and repair attempt, SHALL have a type glyph. Exact type glyphs, warning glyph shape, rail glyphs, colors other than the warning's amber distinction, padding, and whitespace are design decisions.

#### Scenario: Shell step row

- **WHEN** a shell step is rendered in the workflow tree
- **THEN** its row shows indentation, a status indicator, the step name, and the shell type glyph

#### Scenario: Loop step row shows iteration counter and type glyph

- **WHEN** a counted or for-each loop has completed 3 of 5 iterations
- **THEN** its row shows the loop type glyph and `(3/5)` after the name

#### Scenario: For-each loop row shows iteration counter and type glyph

- **WHEN** a for-each loop has resolved five matches and completed three iterations
- **THEN** its row shows the loop type glyph and `(3/5)` after the name

#### Scenario: Active step blinks

- **WHEN** an expanded active ancestry contains an in-progress leaf
- **THEN** the leaf displays the blinking running indicator and every visible in-progress ancestor displays a static running indicator

#### Scenario: Selected container with active child suppresses its own indicator

- **WHEN** a selected in-progress container has a visible in-progress child while the run is active
- **THEN** the child owns the blinking indicator while the container retains a static running indicator

#### Scenario: Selected container with no active child keeps its indicator

- **WHEN** a selected in-progress container has no visible in-progress child
- **THEN** its running indicator remains visible and blinks only while it is the deepest active row of an active run

#### Scenario: Stopped active ancestry retains status indicators

- **WHEN** a run stops while an expanded container and its visible descendant retain `in-progress` status
- **THEN** both rows display static running indicators

#### Scenario: Aborted step does not blink when no run is active

- **WHEN** a step retained `in-progress` status from an interrupted run and the run is no longer active
- **THEN** its indicator is static

#### Scenario: Pending steps from workflow file before execution

- **WHEN** the workflow definition is known but the run has no audit entries
- **THEN** the tree shows every defined top-level row with pending status

#### Scenario: Executed steps recovered when the workflow file is gone

- **WHEN** a saved run's workflow file is unavailable but audit history identifies executed top-level steps
- **THEN** the tree reconstructs those executed rows and does not report a missing-workflow error

#### Scenario: Missing workflow and audit history still reports an error

- **WHEN** neither the workflow definition nor recoverable audit history is available
- **THEN** the run view reports the missing-workflow error

#### Scenario: Long step name not truncated in log separator

- **WHEN** a row name exceeds 20 visual characters and the terminal can fit the complete measured tree row plus the minimum detail width
- **THEN** the complete name is shown without truncation

#### Scenario: Long step name truncated in sidebar

- **WHEN** the preferred sidebar and minimum detail pane do not fit together
- **THEN** the sidebar truncates the name with an ellipsis while retaining the row's indentation, status, and type glyph

#### Scenario: Panes have no full-height separator

- **WHEN** the two-pane run view is rendered
- **THEN** whitespace separates the tree and detail pane and no full-height vertical rule appears between them

#### Scenario: Sidebar grows for wider visible row

- **WHEN** a newly visible row needs more width and the terminal can provide it without violating the detail minimum
- **THEN** the sidebar grows to fit the row

#### Scenario: Sidebar does not shrink during same entry

- **WHEN** a wider row disappears after the sidebar has grown and the terminal has not been resized
- **THEN** the sidebar retains its established width

#### Scenario: Resize recomputes pane widths

- **WHEN** the terminal width changes
- **THEN** the tree and detail widths are recomputed from the new terminal width

#### Scenario: Sidebar is not capped at half the terminal

- **WHEN** a complete visible tree row needs more than half the terminal and it still fits beside the whitespace gap and a 20-column detail pane
- **THEN** the sidebar may grow beyond half the terminal so the complete row remains visible

#### Scenario: Extremely narrow terminal degrades deterministically

- **WHEN** the terminal cannot fit minimum tree chrome, the whitespace gap, and a 20-column detail pane
- **THEN** the name is already reduced to an ellipsis, fixed tree chrome is preserved while it fits, the detail width takes the unavoidable remaining reduction, and layout remains valid

#### Scenario: Warning origin uses warning indicator

- **WHEN** a step has terminal warning status
- **THEN** its row displays the distinct amber warning indicator while its detail retains the underlying outcome

#### Scenario: Collapsed ancestor reveals contained warning

- **WHEN** a warning origin is nested beneath a container whose children are not visible
- **THEN** the container visibly indicates that its ancestry contains a warning without increasing the run's warning count

#### Scenario: Repaired check row
- **WHEN** a check with `max: 1` failed once and passed after an inline repair
- **THEN** its row shows `✓`, the name, `(repaired 1/1)`, and its type glyph, and when selected expands to show `✗ attempt 1` with the repair-attempt glyph and `✓ repair 1` with the headless agent glyph

#### Scenario: Repaired check output
- **WHEN** a check failed once, was repaired, and passed on its rerun
- **THEN** the check's detail pane shows the passing rerun's output under `Current output` while `attempt 1` shows the failing run's output

#### Scenario: Blocked check row
- **WHEN** a check failed because the guarded agent declared `REPAIR_BLOCKED`
- **THEN** its row shows `✗`, the name, and `(blocked)`, with no attempt children

#### Scenario: Rerun attempt children
- **WHEN** a check with `repair: {rerun: open-draft-pr}` is on its first rerun
- **THEN** the check row expands to `✗ attempt 1` and a `● rerun 1` container whose children are `open-draft-pr` and the replayed check

### Requirement: Detail pane per step type

The right pane SHALL render detail for exactly one selected real tree row. Changing selection SHALL replace the pane's content; the pane SHALL NOT stack blocks for other steps or recursively embed descendant detail.

The selected detail SHALL begin with compact primary metadata. Identity, type, status, outcome, and duration SHALL appear when available. Agent steps SHALL show profile, CLI, and model; agent calls SHALL additionally show their target. Completed agent and call executions SHALL show concise usage and reported cost when available. Shell and script steps SHALL show exit status. `capture` SHALL appear when configured, and a `skip_if` or `break_if` expression SHALL appear when it explains the recorded outcome.

Session IDs, workdirs, request IDs, session strategy, and inactive modifiers SHALL be omitted from the primary presentation unless a value is needed for an available resume action or to explain an error. Omitting primary metadata MUST NOT remove it from persisted run evidence.

Current-step content SHALL be grouped into visually distinct, labeled rail sections:

- **Headless agent and agent call**: `Current prompt` and `Current response`. The response SHALL use the resolved adapter's ordinary filtered output.
- **Interactive agent**: `Current prompt`; no response transcript SHALL be fabricated when terminal output was not captured.
- **Shell**: `Current command` and `Current output`, with stdout and stderr distinguishable. A failed check with a failure record SHALL additionally show a `Failure evidence` section before `Current command`, containing the classified failure reason, the blocked declaration's explanation when present, and the guarded agent execution's final response. A check with `repair` SHALL show its repair form, target, and attempts used in its primary metadata, and its `Current output` SHALL be the output of the check run that decided its terminal outcome; earlier failing runs remain on their `attempt N` rows.
- **Script**: `Current script` and `Current output`, with stdout and stderr distinguishable, with the same `Failure evidence` section and repair metadata as shell steps.
- **UI**: `Current form` and `Current outcome`, integrated with the existing live UI behavior. When durable audit evidence identifies a historical UI execution but its workflow definition is unavailable, `Current form` SHALL explicitly state `definition unavailable` while `Current outcome` continues to show the recorded outcome.
- **Sub-workflow, loop, iteration, and group**: `Current status`, containing identity, workflow params or loop counters, outcome, duration, and aggregate direct-child counts by status. Container detail SHALL NOT list child rows or render descendant detail.

The exact rail glyph, rail color, padding, and label styling are design decisions, but every section SHALL have its semantic label.

The latest attempt of a re-executed logical step SHALL provide its selected-detail outcome, duration, usage, and cost, with the attempt number shown when greater than one. Earlier attempts SHALL remain in run-level aggregates.

Agent metadata SHALL retain the existing model rules: the model line follows the CLI line, uses the profile default when there is no inline override, uses the session-originating step's effective model for resumed or inherited sessions, and displays `(unknown)` when no model can be resolved.

Completed agent and call detail SHALL retain explicit metrics semantics. Collected usage and reported cost appear beside duration. Unavailable usage displays `?` and its reason when known; absent cost displays `?`, never `$0.00`. Legacy audit entries with no structured metric fields omit usage and cost lines instead of inventing unavailable values.

#### Scenario: Selecting a step scrolls log to its block
- **WHEN** the user moves from one real tree row to another
- **THEN** the right pane replaces the prior content with detail for the newly selected row

#### Scenario: Headless agent block
- **WHEN** a headless agent row is selected
- **THEN** the pane shows profile, CLI, model, status and timing, metrics when available, `Current prompt`, and its full recorded filtered response

#### Scenario: Interactive agent block
- **WHEN** a completed interactive agent row is selected and no terminal transcript was captured
- **THEN** the pane shows known agent metadata, prompt, outcome, and duration without a `Current response` transcript

#### Scenario: Agent-call detail
- **WHEN** an agent-call row is selected
- **THEN** the pane shows target, profile, CLI, model, status and timing, metrics when available, prompt, error context when present, and its separately recorded filtered response

#### Scenario: Agent-call resume exposes required session
- **WHEN** an inactive selected call has a known resumable CLI session
- **THEN** the resume action is available and the session information needed for that action may appear

#### Scenario: Shell step block
- **WHEN** a shell row is selected
- **THEN** the pane shows `Current command`, exit and duration when available, capture metadata when present, and full recorded stdout and stderr under `Current output`

#### Scenario: Script detail
- **WHEN** a script row is selected
- **THEN** the pane shows `Current script`, exit and duration when available, and full recorded stdout and stderr under `Current output`

#### Scenario: UI detail
- **WHEN** a UI row is selected
- **THEN** the pane groups its form and recorded outcome under `Current form` and `Current outcome`

#### Scenario: Historical UI definition is unavailable
- **WHEN** audit-only recovery identifies a historical UI execution but the workflow definition containing its form is unavailable
- **THEN** `Current form` states `definition unavailable` and `Current outcome` shows the recorded outcome without inventing form content

#### Scenario: Container detail is aggregate only
- **WHEN** a sub-workflow, loop, iteration, or group row is selected
- **THEN** `Current status` shows the container's own metadata and aggregate direct-child status counts without listing child detail rows

#### Scenario: Sub-workflow block contains children inline
- **WHEN** a sub-workflow row is selected
- **THEN** its selected detail shows aggregate status and does not embed its children's detail blocks

#### Scenario: Loop block contains iterations inline
- **WHEN** a loop row is selected
- **THEN** its selected detail shows aggregate status and does not embed iteration detail blocks

#### Scenario: Pending step detail is suppressed unless selected
- **WHEN** a pending step exists but another real tree row is selected
- **THEN** the right pane contains only the selected row's detail and no block for the unselected pending step

#### Scenario: Triggered condition is explanatory metadata
- **WHEN** `skip_if` caused a selected step to skip or `break_if` caused a selected loop to stop
- **THEN** the expression responsible for that recorded outcome appears in the primary detail

#### Scenario: Inactive diagnostic metadata stays hidden
- **WHEN** session strategy, workdir, request ID, or an untriggered modifier is recorded but is not needed for resume or an error
- **THEN** it is absent from the primary detail presentation

#### Scenario: Re-executed step block shows latest attempt
- **WHEN** a logical step has multiple attempts
- **THEN** selected detail shows the latest attempt and its attempt number while earlier attempts remain in run aggregates

#### Scenario: Agent block shows collected usage and cost
- **WHEN** a completed selected agent or call has collected usage and reported cost
- **THEN** the primary detail shows both values adjacent to duration

#### Scenario: Agent block shows unavailable usage marker
- **WHEN** a completed selected agent or call has an unavailable usage record
- **THEN** usage and cost show `?`, the usage reason appears when available, and neither zero token counts nor `$0.00` is fabricated

#### Scenario: Legacy agent block omits metrics lines
- **WHEN** selected detail is reconstructed from an audit event with no structured metrics fields
- **THEN** usage and cost lines are omitted

#### Scenario: Agent block header order places model under CLI
- **WHEN** a headless agent, interactive agent, or agent-call detail header is rendered
- **THEN** the model line appears immediately below the CLI line

#### Scenario: Agent block shows model for steps without an inline override
- **WHEN** a selected agent relies on its profile's default model
- **THEN** the model line shows that profile default

#### Scenario: Agent block shows model for a resumed or inherited session
- **WHEN** a selected agent uses `session: resume` or `session: inherit`
- **THEN** the model line shows the effective model used to launch the originating CLI session

#### Scenario: Agent block shows unknown model as explicit fallback
- **WHEN** no model can be resolved for a selected agent or call
- **THEN** the model value displays `(unknown)`

#### Scenario: Failed check detail shows evidence
- **WHEN** a failed `verify-draft-pr` row is selected after `open-draft-pr` declared blocked
- **THEN** the pane shows exit status, `repair: rerun open-draft-pr · 0 of 1 used · blocked`, a `Failure evidence` section with the reason and the `open-draft-pr` response, then `Current command` and `Current output`

#### Scenario: Repair attempt detail
- **WHEN** an `attempt 1` child row is selected
- **THEN** the pane shows that run's exit status and output as a shell or script detail

#### Scenario: Repair agent detail
- **WHEN** a `repair 1` child row is selected
- **THEN** the pane shows the repair agent's profile, CLI, model, prompt including the evidence block, and response, as for any headless agent

### Requirement: Legend overlay
The run view SHALL provide a `?` key that toggles a modal legend overlay showing status glyph meanings and type glyph meanings. The overlay SHALL be dismissible with `?` or Escape.

#### Scenario: Toggle legend overlay on
- **WHEN** the user presses `?` and the legend is not visible
- **THEN** a modal overlay appears showing status glyphs (`●` running, `○` pending, `✓` success, `✗` failed, `⇥` skipped) and type glyphs (`$` shell, ⚙️ headless agent, 💬 interactive agent, ↳ sub-workflow, the loop glyph, the iteration glyph, and the repair-attempt glyph)

#### Scenario: Toggle legend overlay off
- **WHEN** the user presses `?` or Escape while the legend overlay is visible
- **THEN** the overlay is dismissed and the normal view is restored
