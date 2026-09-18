## MODIFIED Requirements

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
