# run-metrics-artifact Specification

## Purpose
Define the durable, versioned run metrics artifact that records per-attempt duration, token usage, and reported cost while providing stable aggregate fields and explicit coverage for external consumers.
## Requirements
### Requirement: Artifact location

Agent Runner SHALL write a machine-readable metrics artifact named `run-metrics.json` in the run session directory, alongside `audit.log` and `state.json`:

```text
~/.agent-runner/projects/{encoded-path}/runs/{run-id}/run-metrics.json
```

This artifact is the supported boundary for external consumers (Agent Evals and others); consumers SHALL NOT need to reconstruct metrics from audit internals or CLI transcripts.

#### Scenario: Artifact created in run directory
- **WHEN** a workflow run completes its first step
- **THEN** `run-metrics.json` exists in that run's session directory

### Requirement: Versioned schema

`run-metrics.json` SHALL carry a top-level schema version field. Backward-incompatible changes to the artifact's structure SHALL increment the version. Schema v2 adds stable role/tool and requested/effective identity plus structured nested model records. Schema v3 adds durable execution-session identities to `sessions[]` and to every metric record so records and rollups can be assigned to the invocation in which their work occurred. Schema v4 adds authoritative versioned measurement heads, lossless accepted producer records, original Runner attribution, and explicit delivery/history/measurement completeness.

Runner SHALL read schema v1 and rewrite it as v2 without discarding its existing attempts, then migrate v2 to v3. The v1 migration SHALL populate derivable legacy identity and mark identity that v1 did not record as `unknown` with `legacy` provenance rather than infer it. The v2-to-v3 migration SHALL assign deterministic legacy execution-session identities to existing `sessions[]` entries and associate metric records with a session only when persisted evidence makes that association unambiguous. Ambiguous legacy record attribution SHALL remain explicitly unknown with limited history coverage rather than being guessed. Runner SHALL migrate v3 to v4 and complete v1/v2 migration through these stages, retaining all attempts, source evidence, roles, hierarchy, durations, and costs. Legacy fields SHALL map only where their persisted meaning supports the common semantics; unavailable identity, token relationships, versions, or original producer identity MUST NOT be invented. Launch-derived legacy identity SHALL remain resolved rather than observed. Outer schema version SHALL NOT imply conversion of nested measurement versions.

#### Scenario: Version field present
- **WHEN** any `run-metrics.json` is written
- **THEN** it contains the schema version identifying its structure

#### Scenario: Schema v2 with unambiguous session evidence is migrated
- **WHEN** Runner reads a schema v2 artifact whose persisted session boundaries unambiguously identify the execution session for a metric record
- **THEN** it migrates through v3 into v4 with deterministic execution-session identities and assigns that record to the supported session

#### Scenario: Legacy record session is ambiguous
- **WHEN** a schema v2 metric record cannot be associated with one execution session without inference
- **THEN** migration preserves the record, marks its execution-session attribution unknown, and reduces the applicable history coverage

#### Scenario: Schema v3 migration preserves uncertainty
- **WHEN** Runner loads a v3 artifact with launch-derived effective identity and missing cache categories
- **THEN** it preserves the legacy evidence in v4, records that identity as resolved, and leaves unobserved identity and unsupported cache measurements unavailable

#### Scenario: Historical nested identity cannot be joined
- **WHEN** a legacy JSONL record lacks evidence connecting its parent-scoped ID to a new producer record
- **THEN** migration preserves its original identity and marks the join limitation without guessing equivalence or presenting potentially overlapping history as fully reconciled

### Requirement: Artifact content

`run-metrics.json` SHALL contain one execution record per executed step or nested model attempt, with one authoritative current measurement head per measured attempt and a run-level aggregate. Each model record SHALL include stable role/tool identity and distinct requested, launch-resolved, and telemetry-observed identities in addition to the step's identifier and nesting prefix, step type, outcome, duration in milliseconds, usage record, and `estimated_api_cost_usd`. The run-level aggregate SHALL include the run's duration, per-category token totals, canonical input/output/overall token totals, canonical-total coverage, and the cost total with its coverage indicator. Unavailable usage and absent totals/cost SHALL appear as explicit null/unavailable states, never as zeros.

In the compatibility views, per-category token totals SHALL be the sum of the values reported for that category across all executed steps regardless of outcome. Canonical input/output/overall totals SHALL sum only steps for which an adapter produced reliable canonical totals. Steps with unavailable usage, and categories or canonical totals a step did not report, contribute nothing to the corresponding aggregate. The aggregate SHALL include usage-coverage and canonical-total-coverage indicators — `complete` when every agent step that actually invoked its CLI reported the metric, `partial` when some did, and `none` when none did — parallel to the cost coverage indicator. Confirmed nested producer model dispatches SHALL participate exactly once in the compatibility usage, canonical-total, and cost coverage denominators alongside native agent steps and called agents. A dispatched attempt lacking eligible full attempt-level USD evidence SHALL count as missing cost: coverage is partial when some attempts have eligible cost and none when no attempts do. Agent steps that never invoked their CLI (skipped, or failed before launch), prepared-only producer records without launch evidence, and confirmed zero-dispatch invocations SHALL NOT count toward these denominators. Unresolved launch/delivery evidence SHALL retain separate explicit completeness limitations.

Schema v4 SHALL retain each accepted producer invocation and model-attempt record intact, including its selected revision, digest, and measurement version, separately from original Runner run/session/parent attribution. The artifact SHALL expose invocation membership, delivery state and gaps, and independent history, collection, per-model, and field-level completeness. External consumers SHALL require neither private Validator storage nor Runner's recovery journal. Compatibility step fields and scalar totals SHALL be derived views and MUST NOT be counted again alongside authoritative measurement heads.

The authoritative per-field v4 aggregates SHALL retain known subtotals and available/partial/unavailable states, precision, and contributing/partial/missing attempt coverage. A partial value SHALL retain its reason and MUST NOT become a complete value merely because numeric. Categories MAY overlap; a grand total SHALL require established non-overlapping semantics. Per-model allocations, unallocated evidence, allocation/identity joins, and provider-reported cost scope/currency/coverage/overlap SHALL survive unchanged. Scalar USD views SHALL use only unambiguous full attempt-level reported USD evidence and MUST NOT promote partial, allocation-only, or overlapping costs to full attempt cost. Source versions and aggregate interpretation SHALL be explicit; unsupported or conflicting actual heads SHALL not contribute numerically or be replaced by older compatible heads.

#### Scenario: Agent step record content
- **WHEN** an autonomous-headless agent step completes with usage and cost collected
- **THEN** its record in `run-metrics.json` carries role/tool and requested/resolved/observed identity, step identifier, prefix, type, outcome, duration, token categories with provenance and availability, and the reported cost evidence

#### Scenario: Crosscheck role remains distinct
- **WHEN** a workflow invokes a configured `crosscheck` agent
- **THEN** its metric record has role `crosscheck` rather than being folded into `lead-agent`

#### Scenario: Run aggregate content
- **WHEN** a run ends
- **THEN** the artifact's run-level aggregate carries the run duration, per-category token totals, canonical input/output/overall totals with coverage, and the cost total with coverage

#### Scenario: Mixed canonical-total availability
- **WHEN** a run contains one invoked agent step with reliable canonical totals and one without
- **THEN** the aggregate sums the known canonical totals and marks canonical-total coverage `partial`

#### Scenario: Unavailable data explicit in artifact
- **WHEN** a step's usage is unavailable
- **THEN** the artifact represents that step's usage as an explicit unavailable state and its cost as null, not as zeros

#### Scenario: Mixed usage availability in aggregate
- **WHEN** a run contains one agent step with a full usage record and one whose usage is unavailable
- **THEN** the aggregate's token totals equal the reporting step's values, the usage-coverage indicator is `partial`, and no zero is substituted for the missing step

#### Scenario: Skipped step excluded from usage coverage
- **WHEN** a run contains one agent step that invoked its CLI with a full usage record and one agent step that was skipped
- **THEN** the aggregate's usage-coverage indicator is `complete`; the skipped step is excluded from the denominator

#### Scenario: Partial token subtotal remains partial
- **WHEN** an attempt reports a known partial output subtotal and another attempt reports complete output
- **THEN** the aggregate includes the known subtotal, names partial and missing coverage as applicable, preserves precision, and does not claim complete output

#### Scenario: Multi-model measurement remains one attempt
- **WHEN** one dispatch contains multiple observed model allocations and unallocated usage
- **THEN** the artifact retains stable allocation and identity references, scoped cost evidence, and attribution completeness while counting the dispatch once

#### Scenario: Partial or overlapping cost evidence
- **WHEN** an attempt reports allocation costs, an earlier partial subtotal, or costs with unknown overlap
- **THEN** the original cost evidence remains available without promoting it to or summing it as an established complete attempt cost
- **AND** a confirmed dispatched attempt with no eligible full USD cost participates as missing cost, producing partial compatibility cost coverage if another attempt has eligible cost and none if no attempts do

#### Scenario: Measurement version differs from outer artifact version
- **WHEN** v4 contains a measurement v1 record
- **THEN** its original version and digest remain unchanged and aggregate interpretation is declared independently

### Requirement: Execution attempts are append-only

Step records SHALL be append-only per execution attempt: when the same logical step executes more than once within a run, each actual execution SHALL append a new record carrying an attempt identifier, and earlier attempts' execution identity and original attribution SHALL be retained. Run-level aggregates SHALL include every attempt's reported usage and cost. Loop iteration completions SHALL likewise append their own records carrying identity and duration only; usage belongs to the step records nested within the iteration, and container/iteration rollups are derived from descendant records so nothing is double-counted.

Producer measurement revisions SHALL be complete replacements for the same original attempt, not additional executions. Runner SHALL select the newest accepted head by producer/store identity, record type, and stable record ID across recovery sessions. Identical revision/digest replay SHALL be idempotent; stale revisions SHALL NOT replace newer heads. Conflicting payloads for the same identity/revision SHALL remain explicit diagnostics and exclude that head from numeric aggregation. A new model dispatch SHALL retain a new attempt identity and contribute separately. Original raw revisions needed to reconcile receipts SHALL remain in durable local evidence even when only the current head is projected.

#### Scenario: Re-executed step appends a new attempt record
- **WHEN** a logical step executes, fails, and is executed again in the same run
- **THEN** the artifact contains one record per attempt, each with its own usage and cost, and both attempts contribute to run-level aggregates

#### Scenario: Iteration record carries duration only
- **WHEN** a loop iteration completes
- **THEN** the artifact contains an iteration record with identity and duration, without usage of its own; the iteration's usage is represented by its nested step records

#### Scenario: Completion replaces prepared measurement
- **WHEN** a completion revision arrives for an already imported prepared or running attempt
- **THEN** the artifact has one current head for that original attempt and recalculates its aggregates rather than adding both revisions

#### Scenario: Replay occurs during later recovery
- **WHEN** an earlier attempt is delivered again in a later Runner invocation
- **THEN** it remains attributed to its original execution session and parent without adding later-session usage

#### Scenario: Actual redispatch remains new work
- **WHEN** Validator dispatches a new model attempt while retrying validation
- **THEN** its new producer attempt ID produces a distinct measurement even if the logical review and parent step match

#### Scenario: Conflicting or unsupported latest head
- **WHEN** the actual current head conflicts or has an unsupported measurement version
- **THEN** its aggregate contribution is unavailable with explicit coverage limitation and no older compatible revision is substituted

### Requirement: Incremental atomic writes

Agent Runner SHALL update `run-metrics.json` after each step completes and finalize it at run end. Each write SHALL be atomic (write-then-rename), so the file is always well-formed JSON. A run that is interrupted or crashes SHALL leave an artifact containing the metrics of every step that completed before the interruption. A failure to write the artifact SHALL NOT fail the step or the run: execution proceeds, and the write failure is retained and surfaced to the user by the end of the run.

For acknowledged nested telemetry, atomic replacement alone SHALL NOT suffice: imported records, original attribution, outstanding receipt metadata, and the artifact projection SHALL cross a file-and-directory-sync durability boundary before acknowledgment. Recovery SHALL rebuild a missing or stale artifact projection from intact durable imported evidence before acknowledging its receipt. Persistence failures SHALL retain pending delivery and existing corrupt/unsupported-artifact preservation behavior.

#### Scenario: Interrupted run leaves valid partial artifact
- **WHEN** a run is killed after two steps completed and a third was in progress
- **THEN** `run-metrics.json` is valid JSON containing the two completed steps' records

#### Scenario: Reader never sees a torn file
- **WHEN** an external consumer reads `run-metrics.json` while a run is writing it
- **THEN** the consumer sees either the previous complete version or the new complete version, never a partial write

#### Scenario: Write failure does not fail the run
- **WHEN** writing `run-metrics.json` fails after a step completes (e.g. disk error)
- **THEN** the step's outcome and the run's execution are unaffected, and the failure is surfaced to the user as a warning by run end

#### Scenario: Projection publication is interrupted
- **WHEN** imported evidence is durable but the corresponding artifact update fails or is interrupted
- **THEN** no acknowledgment is issued until recovery durably publishes the complete projection

### Requirement: Cumulative aggregation across resume sessions

When a run is resumed, `run-metrics.json` SHALL accumulate: step records from earlier execution sessions of the run are retained, new steps are appended, and run-level aggregates cover all execution sessions, so the artifact always describes the whole run. Every new metric record SHALL identify the durable execution session in which its measured work occurred. In addition to cumulative run totals, the artifact SHALL expose per-execution-session rollups so consumers can distinguish work performed before and after resume without subtracting ambiguous cumulative values.

The run-level duration SHALL be the run's total active execution time: the sum of each execution session's duration. Time between an interruption and the subsequent resume SHALL NOT count toward the run's duration.

The artifact SHALL record each execution session with its durable identity and observed progress, updated as terminal events are persisted. When a session ends without a clean shutdown (hard kill, crash), its duration SHALL reflect only the time observed up to the last persisted event, and the session SHALL be distinguishable from a cleanly closed one; a subsequent resume SHALL close it at that observed duration rather than inventing time.

(Terminology note: "execution session" identifies one `agent-runner` invocation of the run. The existing `session_id` field on a step record identifies an agent CLI session assigned by that CLI and remains unrelated.)

#### Scenario: Hard-killed session duration reflects last observed progress
- **WHEN** a run session is hard-killed some time after its last step completed, and the run is later resumed
- **THEN** the killed session's recorded duration extends only to its last persisted event, the session is marked as not cleanly closed until resume finalizes it, and the run's total active duration includes no time after that event

#### Scenario: Resumed run accumulates metrics
- **WHEN** a run executes two steps, is interrupted, and is later resumed to execute two more
- **THEN** the final `run-metrics.json` contains all four step records, distinct execution-session rollups, and run totals spanning both sessions

#### Scenario: Paused time excluded from run duration
- **WHEN** a run executes for 5 minutes, sits interrupted for an hour, and is resumed to execute for 3 more minutes
- **THEN** the artifact's run-level duration is 8 minutes, not 68

#### Scenario: Earlier step is not revisited
- **WHEN** a resumed execution session does not execute a step completed in an earlier session
- **THEN** the later session rollup does not present the earlier step's metrics as newly incurred

### Requirement: Recovery from corrupt or unsupported artifacts

When a resume finds `run-metrics.json` corrupt, unreadable, or carrying a schema version the running binary does not support (including a version newer than it knows), Agent Runner SHALL preserve the existing file under a unique backup name — never overwriting it in place — start a fresh artifact, and surface a warning. The fresh artifact SHALL carry an explicit history-completeness flag, orthogonal to the usage/cost coverage indicators, marking that earlier history was lost: coverage indicators describe only the steps the artifact knows about, and SHALL NOT be forced to `partial` to stand in for lost history.

#### Scenario: Corrupt artifact preserved and rebuilt
- **WHEN** a run is resumed and its `run-metrics.json` cannot be parsed
- **THEN** the corrupt file is preserved under a unique backup name, a fresh artifact is started with its history-completeness flag indicating lost history, and a warning is surfaced

#### Scenario: Newer schema version is not overwritten silently
- **WHEN** a run is resumed and its `run-metrics.json` carries a schema version newer than the running binary supports
- **THEN** the file is preserved under a unique backup name before a fresh artifact is written, and a warning is surfaced

#### Scenario: Intact artifact reports complete history
- **WHEN** a run resumes with a valid, supported `run-metrics.json`
- **THEN** the artifact accumulates normally and its history-completeness flag indicates no loss

### Requirement: Agent-call metric records and aggregation

Each accepted agent call that reaches a terminal outcome SHALL append a distinct `agent-call` record to `run-metrics.json`, including an accepted call whose child CLI fails to launch. The record SHALL include its call ID, parent attempt identity, target kind and name, outcome, duration in milliseconds, usage record, `estimated_api_cost_usd`, provenance, and completeness using ordinary agent-step metric semantics. Parent workflow-step records SHALL contain only the parent's own usage and cost; called-agent records SHALL remain separate so consumers can roll up the parent and its calls without counting any execution more than once.

The record's `parent_attempt_id` SHALL be opaque provenance identifying the originating control attempt. Agent Runner MUST NOT promise that it equals a parent record's `record_id` or another foreign key. The record's structural prefix SHALL remain the authoritative parent/child hierarchy.

Called-agent usage and cost SHALL contribute to run totals regardless of call outcome when the child reports them. Every called child that invokes its CLI SHALL participate in usage, canonical-total, and cost coverage calculations; a call rejected or failed before CLI launch MUST NOT participate in those coverage denominators. A called child's duration SHALL be retained on its record but MUST NOT be added to run elapsed time because that interval overlaps the waiting parent; the existing active execution-session duration remains authoritative.

Agent Runner SHALL update the artifact through its existing atomic-write path after each agent call completes. Separate calls SHALL append separate records, an idempotent retry MUST NOT append a duplicate record, and completed call records SHALL accumulate across workflow resume with existing step records.

#### Scenario: Successful call appends nested record
- **WHEN** a called agent succeeds
- **THEN** `run-metrics.json` contains one `agent-call` record with its call ID, parent attempt identity, target, outcome, duration, usage, and cost data

#### Scenario: Parent attempt identity is not a record foreign key
- **WHEN** Agent Runner appends an `agent-call` record
- **THEN** the record contains an opaque `parent_attempt_id` while its structural prefix identifies where the call belongs beneath its parent

#### Scenario: Failed call retains reported metrics
- **WHEN** a called agent fails after its CLI reports usage or cost
- **THEN** its failed call record retains those metrics and they contribute to run totals

#### Scenario: CLI launch failure appends failed record
- **WHEN** an accepted call fails before its child CLI launches
- **THEN** `run-metrics.json` contains a failed `agent-call` record and excludes that call from CLI usage-coverage denominators

#### Scenario: Separate calls append separate records
- **WHEN** one parent completes multiple separate agent calls
- **THEN** each call appends a distinct metric record

#### Scenario: Idempotent retry does not duplicate record
- **WHEN** an accepted agent-call request is retried with the same request ID
- **THEN** only the original called-agent execution appears in the metrics artifact

#### Scenario: Parent and child metrics counted once
- **WHEN** both a parent agent step and its called child report usage or cost
- **THEN** run totals include each execution's reported metrics exactly once

#### Scenario: Child duration does not inflate run time
- **WHEN** a parent waits synchronously for a child call lasting 30 seconds
- **THEN** the child record reports 30 seconds while run elapsed time continues to use active execution-session wall time without adding another 30 seconds

#### Scenario: Invoked child participates in coverage
- **WHEN** a called child invokes its CLI and then succeeds or fails
- **THEN** that execution participates in usage, canonical-total, and cost coverage calculations according to the metrics it reported

#### Scenario: Canceled invoked child participates in coverage
- **WHEN** a called child invokes its CLI and is then canceled
- **THEN** that execution participates in usage, canonical-total, and cost coverage calculations according to the metrics it reported

#### Scenario: Pre-acceptance rejection creates no record
- **WHEN** an agent-call request is rejected before reaching the acceptance boundary
- **THEN** it contributes no metric record and is excluded from coverage denominators

#### Scenario: Call completion updates artifact atomically
- **WHEN** a called child completes
- **THEN** Agent Runner updates `run-metrics.json` through the existing atomic-write behavior

#### Scenario: Call records survive workflow resume
- **WHEN** completed calls exist before a workflow interruption and the run is later resumed
- **THEN** their records remain in `run-metrics.json` alongside records appended after resume

### Requirement: Declared nested-tool model metrics

A shell or script step that launches model-using Validator tooling SHALL explicitly declare `metrics_source: agent-validator`. Runner SHALL use the correlated, versioned CLI delivery protocol defined by `validator-metrics-delivery`, replacing the JSONL sink. Each accepted producer model attempt SHALL appear as a distinct `nested-agent` execution with role `implementation-validator`, tool `agent-validator`, original parent/session attribution, and lossless current measurement evidence. Invocation IDs and model-attempt IDs SHALL remain distinct; deduplication MUST NOT be scoped to a later recovery parent.

Missing, invalid, unsupported, or unrecoverable declared evidence SHALL create explicit delivery/measurement gaps as applicable. Gaps SHALL reduce applicable coverage without fabricating a confirmed model dispatch or inflating observed attempt count. A finalized invocation that explicitly proves zero dispatch SHALL create no missing-model gap. Valid sibling evidence remains usable, while an invalid batch MUST NOT be acknowledged. Requested-only or partial identity and multi-model usage permitted by the common contract SHALL remain valid partial evidence rather than be rejected for lacking one effective model. Runner MUST NOT ingest a parallel JSONL transport, scrape human-readable telemetry, or read private Validator metrics files.

#### Scenario: Validator child invocation is attributable
- **WHEN** a declared Agent Validator shell step exports a valid versioned model-attempt record through the metrics CLI
- **THEN** `run-metrics.json` contains a separate `nested-agent` record with role `implementation-validator`, tool `agent-validator`, distinct requested/resolved/observed identity, and its usage and cost evidence

#### Scenario: Missing declared child metrics reduce coverage
- **WHEN** a declared Agent Validator metrics source has missing structured evidence rather than a finalized zero-dispatch invocation
- **THEN** Runner records an explicit nested delivery gap and does not report complete applicable coverage or a confirmed dispatch count

#### Scenario: Invalid sibling does not erase valid metrics
- **WHEN** a producer emits one valid child record and one malformed or schema-invalid record
- **THEN** Runner retains independently valid child evidence, records the invalid evidence as a gap, does not acknowledge the batch, and reports applicable partial coverage

#### Scenario: Human-readable telemetry is not ingested
- **WHEN** Validator prints token summaries to stdout or stderr but provides no valid correlated CLI export
- **THEN** Runner records the structured metrics gap rather than scraping the console text

### Requirement: Step audit measurements remain trustworthy and explicit

For each logical step and execution session, the metrics artifact SHALL make available trustworthy duration, monetary cost, total token usage, source model identity, and attempt count where those values can be measured. It SHALL also expose aggregate repository change counts supported by the recorded step checkpoints. Each unavailable or incomplete measurement SHALL preserve its coverage or unknown state and MUST NOT be coerced to zero. In particular, interactive steps whose terminal traffic and native usage are not captured SHALL report usage and cost as unknown unless an existing adapter provides trustworthy metrics.

Detailed changed-file identities MAY remain in local checkpoint evidence; only aggregate counts are required in the metrics projection used for external value reporting.

#### Scenario: All measurements are available
- **WHEN** a step's attempts report complete usage and cost and its Git checkpoints are available
- **THEN** its session metrics expose duration, cost, tokens, model identity, attempt count, and aggregate change counts with complete applicable coverage

#### Scenario: Cost is missing
- **WHEN** a step has known duration and token usage but no trustworthy cost
- **THEN** cost remains unknown while the known metrics remain usable

#### Scenario: Interactive usage is not observable
- **WHEN** an interactive step inherits the terminal and its adapter provides no trustworthy native usage record
- **THEN** its usage and cost remain unknown rather than being reported as zero

#### Scenario: Repository checkpoint is incomplete
- **WHEN** a step's closing Git checkpoint is unavailable
- **THEN** repository change counts remain unknown rather than being reported as zero

### Requirement: Audit overhead is separate from source metrics

The linked audit run SHALL record its own duration, usage, and cost using normal run-metrics behavior. Audit execution and reporting overhead SHALL NOT be added to the source workflow's step records, execution-session rollups, or run totals.

#### Scenario: Source succeeds and audit runs
- **WHEN** a completed source run launches a linked audit that consumes model tokens and time
- **THEN** the source metrics contain only source work and the audit run metrics contain the audit overhead

#### Scenario: Audit reporting is retried without model rerun
- **WHEN** a completed audit retries only its Google Sheets write
- **THEN** any retry duration is associated with the audit and no new source model usage is recorded

