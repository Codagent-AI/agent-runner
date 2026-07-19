# view-run Specification (delta)

## MODIFIED Requirements

### Requirement: Detail pane per step type
The run view SHALL render a single continuous, scrollable log pane that stacks a detail block for every started step at the current drill-in level, in execution order. Sub-workflow and loop blocks SHALL contain their started children's blocks inline beneath the parent header, recursively at arbitrary depth. Selection SHALL NOT swap the pane's content; it scrolls the pane so the selected step's block is visible.

Each block SHALL open with a header containing the step name and its type glyph, and SHALL contain the same content contract previously rendered on selection:

- **Shell**: interpolated command, exit code, duration, captured-variable name if `capture:` is set, full stdout and stderr (distinguishable).
- **Headless agent**: agent profile, CLI, model, resolved session ID, interpolated prompt, exit code, duration, token usage, reported cost, full stdout and stderr, resume action. The header lines SHALL render in the order: profile, CLI, model, session strategy, session ID. The `model:` line SHALL always be present on a started agent step; when no model can be resolved (no step-level override and no profile default available), the value SHALL render as `(unknown)`.
- **Interactive agent**: agent profile, CLI, model, session ID, interpolated prompt, outcome, duration, token usage, reported cost, resume action. The header-line ordering and always-shown `model:` rule (including the `(unknown)` fallback) match the headless-agent block.
- **Sub-workflow**: resolved workflow path, interpolated params, outcome, duration. Children's blocks render inline beneath this header; no "drill in" hint appears because the content is already inline.
- **Loop**: loop type (counted or for-each), iteration counter `(N/M)`, iterations completed, break_triggered, outcome, duration. Each started iteration renders as a block inline beneath this header, with that iteration's children inline beneath the iteration block; no "drill in" hint appears.

Token-usage and cost lines on agent blocks SHALL render adjacent to the duration line on completed steps. When usage is unavailable (PTY-backed step, parse failure) the usage line SHALL render an explicit unavailable marker; when no cost was reported the cost line SHALL render an unavailable marker, never `$0.00` (per `cost-capture`). When a logical step executed more than once, the detail block SHALL reflect the latest attempt's metrics, annotated with the attempt number; earlier attempts remain part of run-level aggregates (per `run-metrics-artifact`).

#### Scenario: Shell step block
- **WHEN** a shell step has started and is rendered in the log
- **THEN** the log contains a block with the shell step's header (name, `$` glyph), interpolated command, exit code, duration, captured-variable name if any, and stdout/stderr

#### Scenario: Headless agent block
- **WHEN** a headless agent step has started
- **THEN** the log contains a block with profile, CLI, model, session ID, interpolated prompt, exit code, duration, token usage, reported cost, stdout/stderr, and a resume action

#### Scenario: Interactive agent block
- **WHEN** an interactive agent step has started
- **THEN** the log contains a block with profile, CLI, model, session ID, interpolated prompt, outcome, duration, token usage, reported cost, and a resume action

#### Scenario: Agent block shows collected usage and cost
- **WHEN** a completed autonomous-headless agent step's block is rendered and usage plus cost were collected
- **THEN** the block shows the token usage and the reported cost adjacent to the duration line

#### Scenario: Agent block shows unavailable usage marker
- **WHEN** a completed agent step's block is rendered and its usage record is unavailable
- **THEN** the usage and cost lines render explicit unavailable markers; no zero token counts and no `$0.00` are shown

#### Scenario: Re-executed step block shows latest attempt
- **WHEN** a logical step executed twice and its block is rendered
- **THEN** the block shows the latest attempt's usage, cost, and duration with an attempt annotation; earlier attempts are not shown in the block but still count in run aggregates

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
