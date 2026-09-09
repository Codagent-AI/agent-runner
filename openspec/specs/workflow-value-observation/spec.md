# workflow-value-observation Specification

## Purpose
TBD - created by archiving change audit-step. Update Purpose after archive.
## Requirements
### Requirement: Value is assessed once per executed leaf step and execution session

The workflow-value stage SHALL emit one structured value observation for each executed leaf in the resolved source workflow tree covered by the selected execution session. Its identity SHALL be the full logical path. Structural containers SHALL NOT receive separate observations when their work is represented by descendant leaves. Multiple attempts or loop iterations of the same leaf within that session SHALL be aggregated into that step observation, with their count and combined trustworthy metrics retained locally. Agent-internal child usage SHALL roll into its owning leaf. Leaves revisited in a later execution session SHALL receive a distinct observation linked to that later session.

The owning leaf's quantitative evidence SHALL include its own native measurements and all durably attributable called-agent and Validator model attempts, selected from the authoritative current measurement heads in the snapshotted Runner artifact. Each child SHALL contribute once to that leaf's metrics without receiving a separate value observation. Ownership SHALL use persisted original attribution and supported structural hierarchy, never assume an opaque parent attempt ID equals a step-record ID. Missing or ambiguous ownership SHALL remain an explicit evidence gap rather than be guessed.

Original execution-session ownership SHALL govern inclusion even when records were recovered later. Measurement revisions, model allocations, and compatibility projections SHALL NOT become additional attempts or contributions. Leaf attempt count SHALL continue to count workflow-leaf executions; confirmed child dispatch count SHALL remain distinct local evidence. Child usage SHALL roll up only in the audit projection, leaving source parent measurements exclusive of children. Leaf duration SHALL use its workflow execution intervals without adding overlapping child durations; audit overhead SHALL remain separate from source costs and elapsed time.

#### Scenario: Step runs once
- **WHEN** an executed leaf step has one attempt in the selected execution session
- **THEN** the value audit emits one observation for that step and session

#### Scenario: Step has multiple attempts
- **WHEN** an executed leaf step is attempted more than once in the selected execution session
- **THEN** the value audit emits one aggregate observation rather than one independent value row per attempt

#### Scenario: Structural container has executed descendants
- **WHEN** a group, loop, dispatch, or sub-workflow container has work represented by descendant leaf steps
- **THEN** the container receives no separate observation and its descendant leaves remain independently attributable

#### Scenario: Leaf owns child agent calls
- **WHEN** an executed workflow leaf invokes agent-internal child sessions
- **THEN** their trustworthy usage contributes to the owning leaf observation rather than creating separate value rows

#### Scenario: Step is revisited after resume
- **WHEN** a later execution session revisits a step observed in an earlier session
- **THEN** the later session receives a distinct observation that identifies overlap with the earlier evidence

#### Scenario: Validator reviews contribute to their workflow leaf
- **WHEN** a validation leaf has two confirmed Validator model dispatches and a later revision of one dispatch in the selected session
- **THEN** its single value observation includes each current dispatch measurement once and preserves their model and cost evidence without adding child value rows or revision attempts

#### Scenario: Parent calls a child that invokes Validator
- **WHEN** persisted attribution connects a called agent and its Validator dispatches to an executed workflow leaf
- **THEN** each native and Validator model attempt contributes once to that owning leaf while overlapping child durations do not increase leaf duration

#### Scenario: Child ownership cannot be established
- **WHEN** a child record lacks unambiguous original leaf or execution-session attribution
- **THEN** the audit retains an attribution gap and excludes that record from guessed leaf totals

#### Scenario: Recovered child retains original session
- **WHEN** a Validator record is recovered during a later Runner session but belongs to an earlier leaf execution
- **THEN** it is not counted as new usage in the later session's value observations

### Requirement: Concrete Git evidence leads value judgment

When a step has attributable working-tree changes or directly attributed commits, the value auditor SHALL inspect those deltas or commit diffs as the primary evidence of what the step delivered. If a later bulk commit contains an earlier step's already-attributed working-tree changes, the auditor SHALL retain `deferred_commit` provenance for the earlier step and SHALL NOT credit or count the same change again for the later commit-only step. The auditor SHALL use the step's stated result, artifacts, tests, validator results, downstream steps, and other source evidence as supporting or contradictory evidence.

When no commit is attributable, the auditor SHALL assess value from the evidence appropriate to that step type and SHALL NOT equate no repository change with no value.

#### Scenario: Implementation step has attributed commit
- **WHEN** a step has one or more directly attributed commits
- **THEN** its value judgment is grounded first in what those commits actually changed

#### Scenario: Definition step leaves working-tree changes
- **WHEN** a step produces an unambiguous working-tree delta but does not create a commit
- **THEN** its value judgment is grounded first in that delta and records `working_tree` Git attribution

#### Scenario: Later step bulk-commits earlier changes
- **WHEN** a commit-only step packages changes already attributed to earlier step boundaries
- **THEN** the earlier observations retain `deferred_commit` provenance and the later step receives no duplicate change statistics or contribution credit for those changes

#### Scenario: Claimed result disagrees with commit
- **WHEN** a step's narrative claims a result that its attributed commit and downstream evidence do not support
- **THEN** the judgment follows the concrete change and records the disagreement locally

#### Scenario: Planning step has no commit
- **WHEN** a planning or review step produces useful local artifacts but no repository commit
- **THEN** the auditor judges those artifacts and their downstream effect rather than assigning no value solely because no commit exists

### Requirement: Value observations use a small fixed rubric

Each observation SHALL contain exactly one categorical judgment for each of the following dimensions:

- overall value: `high`, `medium`, `low`, `none`, `negative`, or `unknown`;
- change effect: `intended`, `partial`, `no_material_change`, `regressive`, `not_applicable`, or `unknown`;
- unique contribution: `unique`, `complementary`, `duplicative`, `not_applicable`, or `unknown`;
- downstream evidence: `confirmed`, `supporting`, `none`, `contradicted`, or `unavailable`;
- confidence: `high`, `medium`, or `low`; and
- evidence coverage: `complete`, `partial`, or `limited`.

The observation MAY include one bounded short note explaining the most important reason for the judgments. It SHALL NOT contain a transcript or transcript summary.

#### Scenario: Evidence strongly confirms unique value
- **WHEN** a step makes a distinct useful change that downstream validation confirms
- **THEN** the observation uses the applicable fixed categories and may briefly state the decisive reason

#### Scenario: Evidence cannot support a judgment
- **WHEN** material evidence is unavailable
- **THEN** the auditor uses `unknown`, `unavailable`, or reduced confidence and coverage as applicable rather than inventing certainty

#### Scenario: Step causes a regression
- **WHEN** concrete evidence shows that a step's change is counterproductive
- **THEN** the observation can record `negative` overall value and `regressive` change effect

### Requirement: Local value records contain approved high-level fields

Each local step observation SHALL contain the following high-level field groups:

- identity: schema version, stable observation identity, observation timestamp, project, workflow, source run, execution session, audit run, automatic or replay trigger, source outcome, full leaf-step identity, lineage, and step outcome;
- cost: duration, cost, total tokens, and source model identity where each is trustworthy;
- change: Git attribution state, attributed commit SHA or SHAs, changed-file count, additions, and deletions;
- judgment: every fixed rubric dimension, judge model identity, rubric version, and the optional short note.

Unknown quantitative values SHALL remain explicitly unknown rather than becoming zero. The complete local report MAY retain additional detailed evidence and diagnostic fields that are prohibited from the external value dataset.

Local metric evidence SHALL preserve v4 field-level known subtotals, availability, precision, original model identity provenance, and independent measurement, attribution, history, and delivery coverage, including Validator evidence. Partial usage or cost SHALL remain usable as explicitly partial evidence. A scalar total-token or USD-cost field that cannot express partial coverage SHALL remain unknown unless the complete leaf value is established; the known subtotal and reasons SHALL still be retained in detailed local evidence and supplied to the auditors. Multiple models and allocation-scoped costs SHALL not be flattened into a fabricated single model or complete attempt cost. Model aliases and launch-resolved identities SHALL remain distinguishable from telemetry-observed identities.

This change SHALL preserve the existing external value dataset's approved columns and privacy boundary. Detailed measurements and delivery diagnostics SHALL remain in the local report and bounded audit evidence; any existing scalar projection SHALL preserve unknown values instead of publishing a partial subtotal as a complete total.

#### Scenario: Cost is unavailable
- **WHEN** a source step reports duration and tokens but no trustworthy monetary cost
- **THEN** the observation retains duration and tokens and marks cost unknown

#### Scenario: Several models contribute to one logical step
- **WHEN** a step and its declared child agents use more than one source model
- **THEN** the local observation preserves their basic model identities without copying prompts or responses

#### Scenario: Observation is ready for reporting
- **WHEN** the value stage completes validation of an observation
- **THEN** the approved high-level fields and stable observation identity can be projected to the external dataset while detailed local fields remain excluded

#### Scenario: Partial Validator usage remains useful
- **WHEN** a leaf's Validator attempt reports partial output usage with a known subtotal
- **THEN** the local report and model evidence retain that subtotal, precision, and partial coverage while the scalar leaf token total remains unknown if completeness cannot be established

#### Scenario: Validator cost lacks a full USD total
- **WHEN** a confirmed nested dispatch reports only allocation-scoped, partial, or overlapping costs
- **THEN** the audit retains the scoped evidence and cost-coverage limitation without treating it as a full leaf USD cost

#### Scenario: Delivery gap is visible to value judgment
- **WHEN** the snapshot records a blocked, missing, or discarded Validator delivery relevant to a leaf
- **THEN** the audit exposes the gap, does not classify affected evidence as complete, and does not substitute zero usage or cost

### Requirement: Value observations are informational only

The value audit SHALL NOT file or comment on GitHub issues, alter code or workflows, recommend automatic policy changes, or initiate implementation. Its primary outputs SHALL be the complete local value report and the approved lightweight dataset observations.

#### Scenario: Step appears duplicative
- **WHEN** a value observation classifies a step as duplicative
- **THEN** the result is recorded for later analysis and no issue or workflow change is created

#### Scenario: Step appears harmful
- **WHEN** a value observation assigns negative value
- **THEN** the auditor records the hypothesis without modifying code or filing a correctness issue from the value stage

