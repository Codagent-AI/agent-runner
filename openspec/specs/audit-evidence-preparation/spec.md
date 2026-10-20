# audit-evidence-preparation Specification

## Purpose
TBD - created by archiving change audit-step. Update Purpose after archive.
## Requirements
### Requirement: Evidence preparation is deterministic and session-scoped

Before either model-driven audit stage runs, Agent Runner SHALL copy or export the finalized source execution's durable evidence into an immutable audit-launch snapshot and deterministically prepare an evidence index from that snapshot. The index SHALL organize durable run, step, metric, Git, artifact, validation, and available agent-session evidence by logical workflow step and SHALL distinguish evidence inherited from earlier execution sessions.

The prepared index and package SHALL be persisted locally as part of the audit result.

The same snapshotted `run-metrics.json` that serves external workflow-metrics consumers SHALL be the audit's quantitative authority. Evidence preparation SHALL interpret schema v4 current measurement heads, invocation membership, original attribution, and coverage/gaps, while preserving supported legacy artifact behavior and explicit conversion limitations. Both value and correctness audit evidence SHALL include relevant native, called-agent, and Validator measurements. Preparation SHALL not reconstruct quantitative evidence from private Validator storage, independently export or acknowledge Validator telemetry, or count compatibility fields alongside authoritative heads.

For automatic auditing, source finalization SHALL perform its bounded final Validator delivery pass and publish its resulting durable metrics projection before sealing the audit snapshot. An unresolved delivery SHALL remain explicit in that projection; finalization SHALL NOT wait indefinitely for telemetry, change the source outcome, or suppress an otherwise eligible audit. Later delivery recovery SHALL NOT mutate an existing audit snapshot/report or launch another automatic audit for the same source session. An explicit audit replay MAY use the newly recovered durable evidence under its own audit identity.

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

#### Scenario: Final delivery precedes automatic audit snapshot
- **WHEN** the bounded final delivery pass durably incorporates a Validator completion revision
- **THEN** the subsequent audit snapshot and prepared evidence include that revision with its original leaf and execution session

#### Scenario: Final delivery remains blocked
- **WHEN** final delivery stops with unresolved Validator telemetry
- **THEN** an otherwise eligible audit starts from the finalized snapshot containing the explicit delivery limitation and the source outcome is unchanged

#### Scenario: Both audit stages receive nested evidence
- **WHEN** the selected session's v4 artifact contains native, called-agent, and Validator measurement heads and delivery gaps
- **THEN** deterministic preparation exposes the attributable current heads and gaps to both value and correctness stages without requiring a separate Validator metrics source

#### Scenario: Metrics recovery follows an automatic audit
- **WHEN** a later metrics recovery updates the source artifact after its automatic audit snapshot was sealed
- **THEN** the existing audit remains unchanged and no second automatic audit is launched; the updated evidence is available to an explicit replay

### Requirement: Default model input is bounded and coverage-aware

Evidence preparation SHALL create a bounded default package for each model audit. The package SHALL include the important indexed facts needed for judgment and SHALL explicitly identify omitted, missing, unavailable, unsupported, or truncated evidence. Bounds MUST NOT be presented as complete coverage when relevant evidence was excluded.

Each model package SHALL contain at most 256 KiB of UTF-8 JSON and SHALL allocate at most 32 KiB of detailed default evidence to one leaf step. Each batch's structured output schema SHALL also stay within 96 KiB, with the consultation vocabulary defined once and referenced by every observation, because some model CLIs accept that schema only as a single command-line argument. The compact fact record for every covered leaf SHALL remain available. When one package cannot contain all compact facts and prioritized evidence, preparation SHALL emit deterministic batches and the value stage SHALL process every batch before observations are merged.

Reference metadata SHALL count against each leaf's allocation alongside reference content. When a leaf has more references in a category than fit, the default package SHALL carry a deterministic bounded selection: the final attempt's output first, then contributions in path order. It SHALL add one summary reference for that category stating how many entries were omitted and pointing to the leaf's complete reference list in the local evidence index. The final output excerpt and at least one contribution excerpt, when they exist, SHALL fit within the leaf's allocation at every supported cardinality of attempts and written files.

Within each leaf's allocation, a fixed reserved share SHALL be available to the step's own output excerpt and its contribution excerpts (written-file deltas, working-tree patches, and attributed commit patches) whenever such evidence exists. Other evidence SHALL NOT displace that reserved share. Share that a leaf does not need for output or contributions MAY be used by other evidence. Beyond the reserved share, evidence SHALL be selected in this order: identity and outcome, trustworthy metrics and their coverage, Git attribution and aggregate change facts, commit summaries, downstream validation, produced-artifact identity, and narrative output. Each evidence category SHALL be trimmed separately. When any category is cut, preparation SHALL leave a category-specific truncation marker and a stable reference to the complete local evidence, instead of silently dropping whole evidence references. The complete unbounded evidence index SHALL remain local.

The compact metrics facts supplied to both audit stages SHALL retain attributable nested usage/cost summaries and their coverage, model-identity provenance, and delivery-gap indicators. When detailed producer revisions or allocations exceed the package bounds, the package SHALL identify that truncation and reference the complete snapshotted evidence rather than silently omit the limitation or claim complete coverage. Detailed Validator evidence SHALL follow the same privacy and read-only rules as other audit evidence.

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

#### Scenario: Detailed Validator measurements exceed package bounds
- **WHEN** a leaf's full Validator measurement evidence exceeds its default evidence allocation
- **THEN** the package retains compact current metric facts and coverage/delivery-gap indicators, identifies omitted detail, and references the complete local snapshot within the existing package limits

#### Scenario: Evidence-heavy leaf keeps its output
- **WHEN** a leaf has many attempts, large Git detail, and many written files, and its final output is short
- **THEN** the leaf's default evidence still includes the final output excerpt, and the other categories are trimmed with category-specific truncation markers

#### Scenario: Leaf with thousands of written files and many attempts
- **WHEN** a leaf has dozens of attempts and as many written files as the observation bound allows
- **THEN** the leaf stays within its allocation, carries its final output excerpt and at least one contribution excerpt, and summarizes the omitted references with a count and a pointer to the complete local list

#### Scenario: Large output is excerpted
- **WHEN** a step's final output is larger than the reserved share
- **THEN** the leaf includes a bounded excerpt marked truncated, with a reference to the complete local output

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

### Requirement: Leaf evidence includes the step's own output

Evidence preparation SHALL attach to each value leaf the output that the leaf's own executions recorded on their `step_end` events in the snapshotted `audit.log`:

- for an agent step, the agent's final response;
- for a shell or script step, its recorded output, including validator check output.

Output SHALL be matched to a leaf by the event's logical step path and execution session. It SHALL NOT be matched by filename or substring heuristics. When a leaf has several attempts or iterations, each attempt's output SHALL remain distinguishable and the final attempt's output SHALL be prioritized. Output from another execution session SHALL NOT be presented as the selected session's work.

When a leaf has no recorded output, the leaf SHALL report narrative evidence as unavailable with a reason. Examples are an interactive agent step, a shell step without capture, or a run whose `audit.log` is not in the snapshot. Output that Runner truncated when recording SHALL be marked truncated.

#### Scenario: Headless agent step reports findings
- **WHEN** a headless audit step's `step_end` records a final response listing two findings
- **THEN** that step's leaf includes the response as available narrative evidence attributed to that step

#### Scenario: Validator script output
- **WHEN** a validator check script step records its output on `step_end`
- **THEN** that step's leaf includes the output as available validation evidence

#### Scenario: Similarly named steps
- **WHEN** a run contains steps `audit` and `audit-fix`, each with recorded output
- **THEN** each leaf includes only its own step's output

#### Scenario: Retried step
- **WHEN** a leaf step ran twice in the selected session and each attempt recorded output
- **THEN** the leaf's evidence keeps both attempts' output distinguishable and prioritizes the final attempt's output

#### Scenario: Interactive step has no recorded output
- **WHEN** an interactive agent step's `step_end` carries no response
- **THEN** the leaf reports narrative evidence as unavailable with a reason rather than as complete

### Requirement: Leaf evidence includes the step's written-file contributions

Evidence preparation SHALL attach to each value leaf the files that the leaf's own executions recorded as written, together with their retained evidence: the step's delta for each written workflow output file, and the bounded patches for the step's working-tree changes and attributed commits. The delta or patch SHALL be presented as the step's contribution. A whole-file version SHALL be presented only as context and SHALL be labelled as possibly containing content inherited from earlier steps. When the step's own contribution to a file cannot be determined, that file's content SHALL be marked unresolved and SHALL NOT be presented as the step's contribution.

Written-file evidence SHALL be classified as artifact evidence. A file written by a validator step SHALL be classified as validation evidence.

Artifact evidence SHALL be reported as none produced, which is distinct from unavailable, only when both of these hold:

- every attempt of the leaf has a written-file record with status observed and an empty list;
- the repository evidence positively establishes that the leaf made no change: both Git checkpoints are available, there is no working-tree delta, and no commit is attributed.

When any attempt's written-file record is absent, truncated, or unavailable, or the repository evidence is unknown, the leaf SHALL report artifact evidence as unavailable or partial, with each recorded reason preserved. This applies even when other artifact references for the leaf are available.

Contribution references SHALL follow the final cross-leaf Git attribution, including deferred-commit attribution. When a later step's commit packages changes that an earlier step made in the working tree, the earlier step's working-tree patch remains its contribution. The commit patch MAY accompany the earlier step as confirming context, but SHALL NOT become a contribution of the committing step. When a commit packages both earlier steps' deferred changes and changes first observed in the committing step, only the portion for paths not deferred from an earlier step SHALL be the committing step's contribution.

#### Scenario: Acceptance step appends to the assumptions ledger
- **WHEN** a step's written-file record shows it modified the cumulative assumptions ledger by appending one ruling
- **THEN** the leaf presents that ruling as the step's contribution and does not present earlier steps' rulings as its work

#### Scenario: Fixer step changes code
- **WHEN** a step's working-tree patch shows it changed a source file
- **THEN** the leaf presents that patch as the step's contribution

#### Scenario: Step committed its work
- **WHEN** a step has a directly attributed commit
- **THEN** the leaf presents a bounded patch of that commit as the step's contribution

#### Scenario: Step wrote nothing
- **WHEN** a step's written-file record has status observed with an empty list, and its Git checkpoints show no change and no attributed commit
- **THEN** the leaf reports that no artifact was produced and does not report artifact evidence as unavailable

#### Scenario: Observation failed with an empty list
- **WHEN** a step's written-file record has an empty list but its status is truncated or unavailable
- **THEN** the leaf reports artifact evidence as partial or unavailable with the recorded reason, never as none produced or complete

#### Scenario: Later step commits an earlier step's edit
- **WHEN** an implementation step edits a file without committing, and a later commit-only step commits that edit
- **THEN** the implementation step's contribution is its working-tree patch, and the commit-only step receives no contribution for that edit

#### Scenario: Retained delta was truncated
- **WHEN** the retained delta for a step-written file was truncated at recording time
- **THEN** the leaf marks that artifact evidence as truncated and references the retained local evidence

### Requirement: Leaf evidence attribution is explicit

Evidence preparation SHALL attach detailed evidence to a leaf only through explicit attribution: the leaf's step path and execution session on audit events, its written-file records, and the existing Git attribution rules. Snapshot files that have no such attribution SHALL remain in the local evidence index for read-only drill-in, but SHALL NOT be attached to any leaf.

#### Scenario: Unattributed file name contains a step id
- **WHEN** a snapshot file under the run directory has a name containing a step's id but no written-file record attributes it to that step
- **THEN** the file is not attached to that step's leaf and remains discoverable in the local evidence index

### Requirement: Automatic audit snapshots carry step contribution evidence

The automatic audit-launch snapshot SHALL include the Runner-retained written-file versions, deltas, and working-tree patches referenced by the source session's `step_end` events, and bounded patches for commits attributed to the session's steps. Snapshot content SHALL be immutable after sealing, like the rest of the snapshot.

#### Scenario: Automatic audit of a run with written files
- **WHEN** an eligible run completes after its steps wrote output files and committed changes
- **THEN** the automatic audit snapshot contains the retained deltas and the attributed commit patches, and later changes to the run directory do not alter them

