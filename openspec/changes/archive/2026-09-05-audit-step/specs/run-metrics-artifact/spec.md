## MODIFIED Requirements

### Requirement: Versioned schema

`run-metrics.json` SHALL carry a top-level schema version field. Backward-incompatible changes to the artifact's structure SHALL increment the version. Schema v2 adds stable role/tool and requested/effective identity plus structured nested model records. Schema v3 adds durable execution-session identities to `sessions[]` and to every metric record so records and rollups can be assigned to the invocation in which their work occurred.

Runner SHALL read schema v1 and rewrite it as v2 without discarding its existing attempts, then migrate v2 to v3. The v1 migration SHALL populate derivable legacy identity and mark identity that v1 did not record as `unknown` with `legacy` provenance rather than infer it. The v2-to-v3 migration SHALL assign deterministic legacy execution-session identities to existing `sessions[]` entries and associate metric records with a session only when persisted evidence makes that association unambiguous. Ambiguous legacy record attribution SHALL remain explicitly unknown with limited history coverage rather than being guessed.

#### Scenario: Version field present
- **WHEN** any `run-metrics.json` is written
- **THEN** it contains the schema version identifying its structure

#### Scenario: Schema v2 with unambiguous session evidence is migrated
- **WHEN** Runner reads a schema v2 artifact whose persisted session boundaries unambiguously identify the execution session for a metric record
- **THEN** it rewrites the artifact as v3 with deterministic execution-session identities and assigns that record to the supported session

#### Scenario: Legacy record session is ambiguous
- **WHEN** a schema v2 metric record cannot be associated with one execution session without inference
- **THEN** migration preserves the record, marks its execution-session attribution unknown, and reduces the applicable history coverage

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

## ADDED Requirements

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
