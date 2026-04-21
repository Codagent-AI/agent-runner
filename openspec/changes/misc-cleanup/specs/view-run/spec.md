## MODIFIED Requirements

### Requirement: Detail pane per step type
The run view SHALL render a single continuous, scrollable log pane that stacks a detail block for every started step at the current drill-in level, in execution order. Sub-workflow and loop blocks SHALL contain their started children's blocks inline beneath the parent header, recursively at arbitrary depth. Selection SHALL NOT swap the pane's content; it scrolls the pane so the selected step's block is visible.

Each block SHALL open with a header containing the step name and its type glyph, and SHALL contain the same content contract previously rendered on selection:

- **Shell**: interpolated command, exit code, duration, captured-variable name if `capture:` is set, full stdout and stderr (distinguishable).
- **Headless agent**: agent profile, CLI, model, resolved session ID, interpolated prompt, exit code, duration, full stdout and stderr, resume action. The header lines SHALL render in the order: profile, CLI, model, session strategy, session ID. The `model:` line SHALL always be present on a started agent step; when no model can be resolved (no step-level override and no profile default available), the value SHALL render as `(unknown)`.
- **Interactive agent**: agent profile, CLI, model, session ID, interpolated prompt, outcome, duration, resume action. The header-line ordering and always-shown `model:` rule (including the `(unknown)` fallback) match the headless-agent block.
- **Sub-workflow**: resolved workflow path, interpolated params, outcome, duration. Children's blocks render inline beneath this header; no "drill in" hint appears because the content is already inline.
- **Loop**: loop type (counted or for-each), iteration counter `(N/M)`, iterations completed, break_triggered, outcome, duration. Each started iteration renders as a block inline beneath this header, with that iteration's children inline beneath the iteration block; no "drill in" hint appears.

#### Scenario: Shell step block
- **WHEN** a shell step has started and is rendered in the log
- **THEN** the log contains a block with the shell step's header (name, `$` glyph), interpolated command, exit code, duration, captured-variable name if any, and stdout/stderr

#### Scenario: Headless agent block
- **WHEN** a headless agent step has started
- **THEN** the log contains a block with profile, CLI, model, session ID, interpolated prompt, exit code, duration, stdout/stderr, and a resume action

#### Scenario: Interactive agent block
- **WHEN** an interactive agent step has started
- **THEN** the log contains a block with profile, CLI, model, session ID, interpolated prompt, outcome, duration, and a resume action

#### Scenario: Agent block header order places model under CLI
- **WHEN** a headless or interactive agent block is rendered
- **THEN** the `model:` line appears immediately below the `cli:` line in the block header (and the `cli:` line itself appears immediately below the `agent:` profile line when a profile is present)

#### Scenario: Agent block shows model for steps without an inline override
- **WHEN** an agent step relies on its profile's default model (no `model:` set on the step)
- **THEN** the block's `model:` line shows the profile's default model value, not an empty or missing line

#### Scenario: Agent block shows model for a resumed or inherited session
- **WHEN** an agent step uses `session: resume` or `session: inherit`, reusing the CLI session of an earlier step
- **THEN** the block's `model:` line shows the model that was used to launch the CLI (sourced from the profile of the session-originating step, with any step-level override applied)

#### Scenario: Agent block shows unknown model as explicit fallback
- **WHEN** an agent step has started but no model can be resolved (no step-level override and no profile default available)
- **THEN** the block's `model:` line renders with the value `(unknown)` rather than being omitted

#### Scenario: Sub-workflow block contains children inline
- **WHEN** a sub-workflow step has started
- **THEN** the log contains a sub-workflow header (resolved path, params, outcome, duration) and the sub-workflow's started child steps are rendered as blocks inline beneath the header

#### Scenario: Loop block contains iterations inline
- **WHEN** a loop step has started
- **THEN** the log contains a loop header (type, counter, completed, break_triggered, outcome, duration) and each started iteration is rendered as a block inline beneath the header, with that iteration's children inline beneath the iteration block

#### Scenario: Pending step detail is suppressed unless selected
- **WHEN** a step with status `pending` exists and is not selected by the cursor
- **THEN** the log does NOT contain a block for it (pending blocks are covered by the separate "Temporary detail for selected pending step" requirement)

#### Scenario: Selecting a step scrolls log to its block
- **WHEN** the user selects a step in the step list whose block is not currently in the viewport
- **THEN** the log scrolls so that step's block is in view; the log's content is not replaced

## ADDED Requirements

### Requirement: In-progress agent progress indicator in the log
A headless or interactive agent block SHALL display a visible progress indicator while its step status is `in-progress`, regardless of whether the step has produced output yet. The indicator SHALL appear in the block's body (below any already-rendered output), so that a user viewing the block always has a motion cue that the agent is still working.

When no output has been produced, the indicator SHALL occupy its own multi-line region at the position the `agent:` body would otherwise begin. Once output has started streaming, the indicator SHALL render as a single-character animated glyph on a line positioned below the streamed output. The exact glyph set and color token are design decisions, but the indicator MUST be visually distinct from static output text.

When the step transitions out of `in-progress` (to `success`, `failed`, `skipped`, or becomes aborted with no active run), the indicator SHALL be removed from the block.

#### Scenario: Spinner shown while agent has not produced output
- **WHEN** an agent step is in progress and has produced no stdout or stderr yet
- **THEN** the log block shows an animated progress indicator in place of the `agent:` output region

#### Scenario: Spinner shown below streaming output
- **WHEN** an agent step is in progress and has produced at least one line of output
- **THEN** the log block shows the streamed output followed by a single-character animated progress indicator on a line below it

#### Scenario: Spinner removed on step completion
- **WHEN** an in-progress agent step transitions to `success`, `failed`, or `skipped`
- **THEN** the log block no longer shows a progress indicator

#### Scenario: Spinner absent for aborted step without active run
- **WHEN** an agent step was interrupted by an earlier run and no run is currently active
- **THEN** the log block shows no animated progress indicator (matching the step-list rule that `in-progress` does not blink outside an active run)
