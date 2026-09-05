## ADDED Requirements

### Requirement: Value is assessed once per executed leaf step and execution session

The workflow-value stage SHALL emit one structured value observation for each executed leaf in the resolved source workflow tree covered by the selected execution session. Its identity SHALL be the full logical path. Structural containers SHALL NOT receive separate observations when their work is represented by descendant leaves. Multiple attempts or loop iterations of the same leaf within that session SHALL be aggregated into that step observation, with their count and combined trustworthy metrics retained locally. Agent-internal child usage SHALL roll into its owning leaf. Leaves revisited in a later execution session SHALL receive a distinct observation linked to that later session.

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

#### Scenario: Cost is unavailable
- **WHEN** a source step reports duration and tokens but no trustworthy monetary cost
- **THEN** the observation retains duration and tokens and marks cost unknown

#### Scenario: Several models contribute to one logical step
- **WHEN** a step and its declared child agents use more than one source model
- **THEN** the local observation preserves their basic model identities without copying prompts or responses

#### Scenario: Observation is ready for reporting
- **WHEN** the value stage completes validation of an observation
- **THEN** the approved high-level fields and stable observation identity can be projected to the external dataset while detailed local fields remain excluded

### Requirement: Value observations are informational only

The value audit SHALL NOT file or comment on GitHub issues, alter code or workflows, recommend automatic policy changes, or initiate implementation. Its primary outputs SHALL be the complete local value report and the approved lightweight dataset observations.

#### Scenario: Step appears duplicative
- **WHEN** a value observation classifies a step as duplicative
- **THEN** the result is recorded for later analysis and no issue or workflow change is created

#### Scenario: Step appears harmful
- **WHEN** a value observation assigns negative value
- **THEN** the auditor records the hypothesis without modifying code or filing a correctness issue from the value stage
