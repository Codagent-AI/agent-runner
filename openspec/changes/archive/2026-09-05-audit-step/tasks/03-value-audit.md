# Task: Prepare bounded evidence and produce validated value observations

## Goal

Implement the local, deterministic value core of the hidden audit workflow: normalize an immutable selected execution-session snapshot into conservative per-leaf evidence, produce bounded deterministic batches, run fresh `crosscheck` model sessions for value judgment, validate that models supplied only the fixed qualitative fields, and persist validated value artifacts with consultations, provenance, fingerprints, lineage, and stable observation identities.

## Background

You MUST read:

- `openspec/changes/audit-step/proposal.md` for commit-first value observation, local-detail retention, informational-only results, and separate audit cost
- `openspec/changes/audit-step/design.md` sections 3–6 and 9 for leaf identity, Git attribution, the immutable workspace, package limits and priority, hidden workflow stages, `crosscheck` provenance, observation IDs, local report, replay identities, and snapshot/output-boundary fingerprints
- `openspec/changes/audit-step/specs/audit-evidence-preparation/spec.md` for deterministic indexing, bounds, attribution, and selective local consultation
- `openspec/changes/audit-step/specs/workflow-value-observation/spec.md` for leaf aggregation, evidence precedence, the fixed rubric, local record fields, and informational-only behavior
- `openspec/changes/audit-step/test-plan.md` for `INT-004`

Place deterministic preparation, value schemas/validators, and workflow-facing command handlers in a cohesive tagged implementation under `internal/devaudit/` (split into focused subpackages only where package responsibilities remain clear). Extend the single existing `internal/devaudit/workflows/audit/run-audit-v1.0.yaml` asset in place; do not create or embed a second audit workflow. Preserve its `audit:run-audit` identity, hidden flag, seven-stage order, and stage IDs while completing the prepare, per-batch value loop, and validate/merge-value behavior. This task owns the prepare/value contracts and must provide typed, validated artifacts that the correctness, report-assembly, and Sheets stage contracts can consume without trusting model text.

Consume durable `audit.log`, `run-metrics.json`, state, outputs, artifacts, validation evidence, Git checkpoints/exports, and any already discoverable adapter session references from the immutable launch snapshot only. Do not add terminal capture or a cross-adapter transcript API. Work in an audit-owned workspace with read-only evidence and writable model-output directories; audit agents must not run with the source repository as their working directory. Fingerprint the read-only snapshot and permitted output boundary before/after model work. A changed live source run is legitimate and irrelevant; a changed frozen snapshot blocks publication.

The default model package is UTF-8 JSON capped at 256 KiB, with no more than 32 KiB of detailed default evidence for any leaf. Preserve a compact record for every covered leaf and deterministically batch if those records plus prioritized evidence do not fit. Priority is identity/outcome, trustworthy metrics/coverage, Git attribution/change facts, commit summaries, downstream validation, artifact identity, then narrative output. Every omission, truncation, unavailable, or unsupported category must be explicit.

Models may populate only `overall_value`, `change_effect`, `unique_contribution`, `downstream_evidence`, `confidence`, `evidence_coverage`, an optional safe note, and consultation references for the supplied skeletons. Deterministic code owns identity, cost, Git, metrics, lineage, provenance, and observation IDs. Preserve partial evidence and diagnostics on model failure. Never file or comment on issues, change code/workflows, or initiate implementation from a value judgment.

`AT-001` is exercised during change acceptance, not by this implementation task. Unit, integration, and tagged CI tests use deterministic fake `crosscheck` agents only; do not spend real model cost as an implementation completion condition.

## Spec

### Requirement: Evidence preparation is deterministic and session-scoped

Before either model-driven audit stage runs, Agent Runner SHALL copy or export the finalized source execution's durable evidence into an immutable audit-launch snapshot and deterministically prepare an evidence index from that snapshot. The index SHALL organize durable run, step, metric, Git, artifact, validation, and available agent-session evidence by logical workflow step and SHALL distinguish evidence inherited from earlier execution sessions.

The prepared index and package SHALL be persisted locally as part of the audit result.

#### Scenario: Source session has complete evidence
- **WHEN** evidence preparation runs for a finalized execution session
- **THEN** it creates a per-step index whose entries identify the available evidence and the session that produced it

#### Scenario: Resumed session contains earlier evidence
- **WHEN** a resumed execution session includes durable evidence originating in an earlier session
- **THEN** the index marks that evidence as inherited or overlapping rather than newly produced by the selected session

#### Scenario: Deterministic preparation is repeated
- **WHEN** preparation is repeated against unchanged source evidence
- **THEN** it produces equivalent indexed facts and coverage classifications

#### Scenario: Source run changes after launch
- **WHEN** a source run is resumed or otherwise gains new durable evidence after its audit-launch snapshot is complete
- **THEN** the active audit continues against its immutable snapshot and does not invalidate or silently incorporate the later changes

### Requirement: Default model input is bounded and coverage-aware

Evidence preparation SHALL create a bounded default package for each model audit. The package SHALL include the important indexed facts needed for judgment and SHALL explicitly identify omitted, missing, unavailable, unsupported, or truncated evidence. Bounds MUST NOT be presented as complete coverage when relevant evidence was excluded.

Each model package SHALL contain at most 256 KiB of UTF-8 JSON and SHALL allocate at most 32 KiB of detailed default evidence to one leaf step. The compact fact record for every covered leaf SHALL remain available. When one package cannot contain all compact facts and prioritized evidence, preparation SHALL emit deterministic batches and the value stage SHALL process every batch before observations are merged.

Within those bounds, evidence SHALL be selected in this order: identity and outcome, trustworthy metrics and their coverage, Git attribution and aggregate change facts, commit summaries, downstream validation, produced-artifact identity, and narrative output. The complete unbounded evidence index SHALL remain local.

#### Scenario: Evidence exceeds the default bound
- **WHEN** the available evidence is larger than the configured default package bound
- **THEN** preparation selects a bounded subset and records what categories were omitted or truncated

#### Scenario: Compact facts require multiple packages
- **WHEN** compact facts and prioritized evidence for all covered leaves cannot fit within one package
- **THEN** preparation creates deterministic bounded batches and no covered leaf is silently omitted from value processing

#### Scenario: Expected metric is unavailable
- **WHEN** cost, usage, Git, artifact, or validation evidence cannot be measured reliably
- **THEN** the package records that evidence as unavailable rather than inferring a value

#### Scenario: Package contains only partial evidence
- **WHEN** one or more material evidence categories are missing or omitted
- **THEN** the package does not classify its evidence coverage as complete

### Requirement: Git evidence is captured and attributed conservatively

For each source step, evidence preparation SHALL compare its recorded starting and ending revision, index, working-tree, and untracked-file state. A concrete change first observed across that boundary SHALL be associated with that step as `working_tree` evidence even when the step creates no commit. A commit SHALL be directly attributed to a step only when it belongs to that step's revision interval and its subject begins with the workflow commit prefix `[<step_id>]`. Unprefixed, mismatched, shared, or otherwise ambiguous commits SHALL remain unattributed.

When a later commit contains changes already associated with an earlier step's working-tree delta, preparation SHALL mark that commit evidence as `deferred_commit` for the earlier step and MUST NOT count its change statistics again for the later commit-only step. Attributable working-tree changes and attributed commits and diffs SHALL be the primary evidence of code or document changes delivered by a step. A step with no attributable repository change SHALL have an explicit no-change or unknown attribution state, as supported by the evidence. This rule MUST NOT treat the absence of a commit as proof that a non-implementation step delivered no value.

#### Scenario: Prefixed commit falls within step boundary
- **WHEN** a commit reachable between a step's starting and ending revisions has a subject beginning with that step's exact `[<step_id>]` prefix
- **THEN** the commit is directly attributed to that step

#### Scenario: Commit is unprefixed
- **WHEN** a commit falls within the step boundary but lacks the exact workflow step prefix
- **THEN** the commit remains unattributed and the audit does not guess its producer

#### Scenario: Step leaves attributable uncommitted changes
- **WHEN** a step changes the index, working tree, or untracked-file set without creating a commit and the boundary delta is unambiguous
- **THEN** those changes are associated with that step as `working_tree` evidence

#### Scenario: Later bulk commit contains an earlier step's changes
- **WHEN** a later commit packages changes already associated with an earlier step boundary
- **THEN** the earlier step records `deferred_commit` provenance and the later commit-only step does not receive duplicate change counts for those changes

#### Scenario: Prefix and boundary disagree
- **WHEN** a commit has a step prefix but does not belong to that step's recorded revision interval
- **THEN** the commit is not directly attributed to that step and the disagreement is retained locally

#### Scenario: Step creates no commit
- **WHEN** the start and end revisions and repository checkpoints show no attributable commit for a step
- **THEN** the step receives an explicit no-change state without automatically receiving a no-value judgment

#### Scenario: Git evidence cannot be established
- **WHEN** the source is not in a supported Git repository or a boundary revision is unavailable
- **THEN** Git attribution is unknown and no commit is attributed by inference

### Requirement: Auditors may inspect detailed local evidence selectively

Both model-driven audit stages SHALL be allowed read-only access to the complete snapshotted source evidence when the bounded package is insufficient. This MAY include Runner-owned outputs, diffs, artifact contents, repository files, and native agent-session material when it is locally discoverable through an existing adapter or recorded reference. Agent Runner SHALL record unsupported or unavailable evidence categories explicitly and SHALL NOT add a cross-adapter transcript-capture interface in this change. The audit SHALL record locally which evidence categories or artifacts were consulted beyond the default package.

Detailed evidence accessed during review SHALL remain local except for the separately governed, redacted GitHub issue content produced by the correctness audit.

#### Scenario: Value judgment needs the actual diff
- **WHEN** the compact package identifies an attributed commit but does not contain enough detail to judge its effect
- **THEN** the value auditor may inspect the local diff and records that it did so

#### Scenario: Correctness suspicion needs raw run evidence
- **WHEN** the correctness auditor cannot verify a suspected orchestration defect from the compact package
- **THEN** it may inspect the relevant available local output, artifacts, native session material, or source code read-only

#### Scenario: Native session material is unavailable
- **WHEN** an agent adapter exposes no durable native session material that the audit can resolve
- **THEN** the evidence index marks that category unavailable and the auditor proceeds with reduced coverage rather than assuming a transcript exists

#### Scenario: Detailed evidence is consulted
- **WHEN** either auditor drills into raw local evidence
- **THEN** the local audit report records the evidence category or artifact consulted without exporting its contents to the value dataset

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

## Test Plan

- `INT-003: Session metrics, Git checkpoints, and leaf attribution` (evidence-projection portion): author a fixture under `testdata/` containing nested groups, a repeated-attempt loop, a sub-workflow, an autonomous agent leaf with child-call metrics, and an interactive leaf with no native usage record. Execute the fixture through the real runner/event pipeline to produce schema-v3 metrics and Git checkpoints—do not substitute hand-written projection JSON—then prove full logical leaf paths; aggregation of attempts, loop iterations, and agent-internal child usage; `working_tree`, `deferred_commit`, exact prefix-plus-boundary, ambiguous, no-change, and unavailable attribution; single-count change statistics; unknown interactive usage/cost; and strict separation between source-run metrics and audit-run overhead.
- `INT-004: Evidence preparation and model-output containment` (evidence/value portion): use realistic multi-session fixtures with oversized output/diffs, discoverable and unavailable native sessions, inherited artifacts, missing metrics, validation evidence, sensitive-looking content, and enough leaves for multiple packages. Prove deterministic preparation, immutable launch scope, exact byte limits and priority, one fresh fake model session per batch, complete value merge, fixed value enums and completeness, deterministic measured fields, safe-note and evidence-reference rejection, and consultation ledgers without copied contents.

## Done When

- The persisted `evidence-index.json` and source-provenance record normalize every executed logical leaf for one selected execution session and explicitly classify inherited, overlapping, unavailable, unsupported, omitted, and truncated evidence.
- Working-tree, attributed-commit, deferred-commit, no-change, ambiguous, and unavailable states follow the exact boundary-plus-prefix rules and never double-count a later bulk commit.
- Every encoded package is at most 256 KiB, per-leaf detailed default evidence is at most 32 KiB, compact facts are never silently dropped, deterministic batches cover all leaves, and each batch gets a fresh model session.
- Both model stages have read-only selective access to indexed local evidence and return validated consultation identifiers/categories; no transcript subsystem or detailed external projection is introduced.
- The value stage emits one validated observation per full logical leaf path and selected execution session, aggregates attempts/iterations/child usage, preserves overlap lineage, grounds change judgments in attributable Git evidence, and treats useful no-commit artifacts fairly.
- Model validation accepts only the fixed rubric, bounded safe note, coverage, and consultation references; it rejects fabricated measured fields, invalid enums, incomplete batches, unsafe notes, unknown references, and changed frozen evidence.
- Validated value artifacts preserve partial diagnostics on model failure and persist the separately addressable value observations, consultation ledger, fingerprints, actual judge provenance, and automatic/replay observation identities enumerated by `design.md` sections 5 and 9, without inventing a named approved schema or committing the stage-6 report early.
- The evidence-projection portion of `INT-003`, the evidence/value portion of `INT-004`, focused package tests, `make fmt`, and tagged tests for the hidden workflow's prepare/value stages pass.
