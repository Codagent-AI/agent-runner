## MODIFIED Requirements

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

Each model package SHALL contain at most 256 KiB of UTF-8 JSON and SHALL allocate at most 32 KiB of detailed default evidence to one leaf step. The compact fact record for every covered leaf SHALL remain available. When one package cannot contain all compact facts and prioritized evidence, preparation SHALL emit deterministic batches and the value stage SHALL process every batch before observations are merged.

Within those bounds, evidence SHALL be selected in this order: identity and outcome, trustworthy metrics and their coverage, Git attribution and aggregate change facts, commit summaries, downstream validation, produced-artifact identity, and narrative output. The complete unbounded evidence index SHALL remain local.

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
