# audit-evidence-preparation Specification

## Purpose
TBD - created by archiving change audit-step. Update Purpose after archive.
## Requirements
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

