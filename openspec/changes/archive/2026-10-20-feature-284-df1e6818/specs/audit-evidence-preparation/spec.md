## ADDED Requirements

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

## MODIFIED Requirements

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
