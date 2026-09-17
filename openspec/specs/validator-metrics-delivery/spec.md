# validator-metrics-delivery Specification

## Purpose
TBD - created by archiving change agent-validator-metrics-instrumentation. Update Purpose after archive.
## Requirements
### Requirement: Correlated declared Validator launch

For each launched shell or script step with `metrics_source: agent-validator`, Runner SHALL durably associate a fresh opaque context with its original run, execution session, parent attempt, structural prefix, effective project/configuration selection, and producer executable before enabling telemetry correlation. Runner SHALL expose consumer/context to the process; bundled Validator scripts SHALL explicitly pass `--metrics-consumer agent-runner --metrics-context <id>` to validation commands. Custom integrations MUST forward the correlation flags and honor the recorded launch scope. Runner MUST NOT infer flags from arbitrary shell text or ingest console token summaries.

#### Scenario: Bundled retry and final verification
- **WHEN** a bundled Validator retry or final-verification step launches
- **THEN** it passes its own durably mapped context, including when task-compliance arguments are present

#### Scenario: Multiple validation commands in one parent
- **WHEN** a declared script launches multiple validation commands with its assigned context
- **THEN** their distinct invocation and model-attempt IDs remain separate beneath the same original parent

#### Scenario: Actual working directory determines scope
- **WHEN** a declared step has a working directory different from the workflow project
- **THEN** launch and delivery use that actual directory and its selected Validator configuration

#### Scenario: Attribution cannot be persisted
- **WHEN** Runner cannot durably save the launch mapping
- **THEN** validation still runs without newly enabled correlation, Runner warns of incomplete instrumentation, and no evidence is acknowledged without durable attribution

### Requirement: Versioned bounded CLI ingestion

Runner SHALL use Validator's capabilities/export/acknowledge CLI protocol, initially supporting capabilities v1, protocol v1, and measurement v1 only. It SHALL select the original project/configuration, consumer, and context explicitly, honor advertised count/byte limits, validate the response scope/store and complete closed record schemas, reject duplicate keys and unsupported fields/versions, and verify SHA-256 over RFC 8785 canonical bytes excluding only top-level record `digest`. All accepted fields SHALL survive incorporation unchanged in the raw record. An unacknowledgeable batch SHALL stop draining its context for that pass and leave later pending revisions explicitly undelivered. Runner SHALL retain blocked delivery and report safe actionable diagnostics, including the blocking record identity when safely established, without skipping or automatically discarding evidence.

#### Scenario: Supported bounded export
- **WHEN** compatible pending revisions exceed one batch
- **THEN** Runner durably saves and acknowledges each valid batch and repeats export until drained, an unacknowledgeable batch blocks the context, or its bounded delivery budget expires

#### Scenario: Unsupported pending measurement version
- **WHEN** the producer reports an unsupported version anywhere in the pending scope
- **THEN** Runner records incomplete delivery and required versions without acknowledging or assuming the compatible subset is complete

#### Scenario: Invalid record or scope
- **WHEN** an export has a wrong scope/store, malformed payload, unknown field, duplicate key, invalid number, or digest mismatch
- **THEN** Runner rejects the invalid evidence and does not acknowledge the batch receipt; independently verified siblings may remain usable with explicit incomplete delivery

#### Scenario: Invalid batch prevents later delivery
- **WHEN** a permanently invalid record prevents acknowledgment and later revisions remain pending in the same context
- **THEN** Runner stops that context for the pass, retains blocked delivery and the later undelivered revisions, and explicit recovery reports non-success with safe diagnostics identifying the correction or restoration needed
- **AND** Runner neither acknowledges a valid subset of the receipt nor automatically discards the batch

#### Scenario: Repeated export before acknowledgment
- **WHEN** Runner exports again before disposing of an earlier batch
- **THEN** repeated revisions do not create duplicate measurements and export is not treated as consuming or advancing a cursor

### Requirement: Durable incorporation before acknowledgment

Runner SHALL commit original attribution, verified immutable record revisions/digests, exact batch metadata, and outstanding opaque receipts to durable local recovery state and publish the corresponding durable workflow artifact before acknowledging. File/directory sync and atomic replacement SHALL establish the persistence boundary. Failed persistence SHALL leave the receipt unacknowledged. Acknowledgment SHALL apply to the whole receipt, with dispositions recorded per revision in its manifest, and SHALL be idempotently retryable with no automatic discard. Runner MUST NOT acknowledge only a valid subset of the receipt.

#### Scenario: Crash before durable incorporation
- **WHEN** Runner crashes before the batch and artifact are durably committed
- **THEN** the producer's revisions remain pending and recovery reimports them without losing evidence

#### Scenario: Crash after save before acknowledgment
- **WHEN** Runner crashes after durable incorporation but before successful acknowledgment
- **THEN** recovery republishes the artifact if necessary, retries acknowledgment, and counts the original work once

#### Scenario: Acknowledgment response is lost
- **WHEN** Validator commits acknowledgment but Runner does not receive or persist its response
- **THEN** retrying the receipt is harmless and creates no new work

#### Scenario: Overlapping and stale receipts
- **WHEN** R1 covers revision 1 and R2 covers revisions 1 and 2 and R2 is acknowledged
- **THEN** recovery recognizes the confirmed dispositions without requiring a separate acknowledgment of abandoned R1; R1 alone cannot acknowledge revision 2

#### Scenario: Durability operation fails
- **WHEN** writing, syncing, renaming, or syncing the containing directory fails for required delivery state or artifact publication
- **THEN** Runner does not acknowledge the batch and preserves retryable delivery evidence without changing validation outcome

### Requirement: Delivery completeness follows evidence

Runner SHALL reconcile invocation attempt membership and lifecycle with current imported attempts, batch-generation coverage, prior incorporated records, and delivery gaps before declaring complete delivery. A terminal invocation, an empty export, or `scope_complete` alone MUST NOT imply zero dispatch or complete history. A finalized invocation with confirmed zero dispatch and an empty expected attempt set SHALL contribute no invented model attempt. Prepared-only records SHALL retain uncertainty about launch. Discard gaps SHALL remain visible and MUST NOT be converted to successful receipt.

#### Scenario: Finalization arrives before attempts
- **WHEN** a batch contains a finalized invocation whose expected attempts arrive in later batches
- **THEN** Runner retains the membership and reports delivery incomplete until those attempts and other delivery obligations are reconciled

#### Scenario: Confirmed zero dispatch
- **WHEN** a finalized invocation explicitly confirms zero dispatch with no expected attempts
- **THEN** Runner records that invocation without creating an unavailable model-attempt gap for it

#### Scenario: Empty or missing evidence
- **WHEN** export reports missing evidence or previously acknowledged evidence absent from Runner's durable history
- **THEN** Runner records an explicit history/delivery gap rather than zero usage or a fabricated parent

#### Scenario: Explicit discard
- **WHEN** Validator reports a user-discarded revision
- **THEN** Runner retains a delivery gap, including after an acknowledgment response reports that same discard

### Requirement: Recover original delivery without executing work

Runner SHALL attempt bounded delivery after any declared process outcome, on resume before new measured work, and at final shutdown. It SHALL provide `agent-runner metrics recover <run-id>` to replay a completed or interrupted run's durable contexts without running workflow steps, validation, cleanup, or models. Recovery SHALL use original attribution and producer/project/configuration selection, serialize with other run writers, retry only transient failures within finite limits, and leave unresolved evidence available for later recovery. Unknown contexts or replaced stores MUST NOT acquire new attribution.

#### Scenario: Failed or canceled validation
- **WHEN** a declared validation command fails or is canceled
- **THEN** Runner attempts bounded delivery independently of the canceled child context, retaining reported usage without changing the validation outcome

#### Scenario: Completed run recovery
- **WHEN** an operator recovers a completed run with pending receipts
- **THEN** only delivery and artifact projection are retried, with no new execution session or measured attempt and no workflow-state transition

#### Scenario: Original location is unavailable
- **WHEN** original configuration/storage is inaccessible, moved, or replaced
- **THEN** recovery preserves pending state and reports the required intervention without switching stores, guessing zero, or acknowledging unknown evidence

#### Scenario: Concurrent writer or unfinished producer closure
- **WHEN** another Runner process owns the run
- **THEN** recovery refuses concurrent mutation
- **AND WHEN** only Validator archive closure is unfinished but committed delivery evidence is intact
- **THEN** delivery can proceed through the metrics CLI without running validation or clean

#### Scenario: Retry budget expires
- **WHEN** transient delivery failures exhaust the bounded retry budget
- **THEN** normal workflow execution keeps its original outcome, pending delivery remains explicit, and an explicit recovery command reports non-success

### Requirement: Evidence safety and local integration acceptance

Runner SHALL export only allowlisted measurement evidence and safe diagnostics, excluding prompts, responses, secrets, environment values, unrestricted events, and account/user/organization/email/host identifiers. Operational paths SHALL stay in local recovery metadata. Shared schema/semantic/delivery fixtures SHALL be pinned and exercised. Integration acceptance MUST explicitly select a feature-bearing Validator build and verify its resolved entry point, SHA-256, and supported capabilities for launch and all metrics operations. Ambient global command resolution MUST NOT substitute a different build. Change-specific local checkout paths and inspected build hashes belong in the change design and acceptance evidence.

#### Scenario: Disallowed payload content
- **WHEN** incoming telemetry includes a forbidden field or evidence identifier outside the allowlisted contract
- **THEN** it is not published as accepted measurement evidence and the associated receipt is not acknowledged

#### Scenario: Local CLI acceptance
- **WHEN** Runner's delivery integration is exercised for acceptance
- **THEN** validation and metrics operations use the explicitly selected feature-bearing Validator build directly or through an isolated executable wrapper, and acceptance records and verifies its resolved entry point, SHA-256, and capabilities

#### Scenario: Local feature is unavailable
- **WHEN** the selected local CLI cannot provide the supported protocol or any producer operation resolves to an unexpected build
- **THEN** integration acceptance reports that limitation as a failure instead of silently skipping or selecting the global installation

