# run-metrics-artifact Specification

## Purpose
TBD - created by archiving change cost-tracking. Update Purpose after archive.
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

`run-metrics.json` SHALL carry a top-level schema version field. Backward-incompatible changes to the artifact's structure SHALL increment the version. Consumers can rely on a given version's structure remaining stable across Agent Runner releases. (The v1 field names are fixed in this change's design document.)

#### Scenario: Version field present
- **WHEN** any `run-metrics.json` is written
- **THEN** it contains the schema version identifying its structure

### Requirement: Artifact content

`run-metrics.json` SHALL contain one record per executed step and a run-level aggregate. Each step record SHALL include the step's identifier and nesting prefix, step type, outcome, duration in milliseconds, the usage record (token categories, optional canonical processed-token totals, provenance, and completeness, per `agent-usage-collection`), and `estimated_api_cost_usd` (per `cost-capture`). The run-level aggregate SHALL include the run's duration, per-category token totals, canonical input/output/overall token totals, canonical-total coverage, and the cost total with its coverage indicator. Unavailable usage and absent totals/cost SHALL appear as explicit null/unavailable states, never as zeros.

Per-category token totals SHALL be the sum of the values reported for that category across all executed steps regardless of outcome. Canonical input/output/overall totals SHALL sum only steps for which an adapter produced reliable canonical totals. Steps with unavailable usage, and categories or canonical totals a step did not report, contribute nothing to the corresponding aggregate. The aggregate SHALL include usage-coverage and canonical-total-coverage indicators — `complete` when every agent step that actually invoked its CLI reported the metric, `partial` when some did, and `none` when none did — parallel to the cost coverage indicator. Agent steps that never invoked their CLI (skipped, or failed before launch) SHALL NOT count toward these coverage denominators.

#### Scenario: Agent step record content
- **WHEN** an autonomous-headless agent step completes with usage and cost collected
- **THEN** its record in `run-metrics.json` carries the step identifier, prefix, type, outcome, duration, token categories with provenance, and the reported cost

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

### Requirement: Execution attempts are append-only

Step records SHALL be append-only per execution attempt: when the same logical step executes more than once within a run, each execution SHALL append a new record carrying an attempt identifier, and earlier attempts' records SHALL be retained unchanged. Run-level aggregates SHALL include every attempt's reported usage and cost. Loop iteration completions SHALL likewise append their own records carrying identity and duration only; usage belongs to the step records nested within the iteration, and container/iteration rollups are derived from descendant records so nothing is double-counted.

#### Scenario: Re-executed step appends a new attempt record
- **WHEN** a logical step executes, fails, and is executed again in the same run
- **THEN** the artifact contains one record per attempt, each with its own usage and cost, and both attempts contribute to run-level aggregates

#### Scenario: Iteration record carries duration only
- **WHEN** a loop iteration completes
- **THEN** the artifact contains an iteration record with identity and duration, without usage of its own; the iteration's usage is represented by its nested step records

### Requirement: Incremental atomic writes

Agent Runner SHALL update `run-metrics.json` after each step completes and finalize it at run end. Each write SHALL be atomic (write-then-rename), so the file is always well-formed JSON. A run that is interrupted or crashes SHALL leave an artifact containing the metrics of every step that completed before the interruption. A failure to write the artifact SHALL NOT fail the step or the run: execution proceeds, and the write failure is retained and surfaced to the user by the end of the run.

#### Scenario: Interrupted run leaves valid partial artifact
- **WHEN** a run is killed after two steps completed and a third was in progress
- **THEN** `run-metrics.json` is valid JSON containing the two completed steps' records

#### Scenario: Reader never sees a torn file
- **WHEN** an external consumer reads `run-metrics.json` while a run is writing it
- **THEN** the consumer sees either the previous complete version or the new complete version, never a partial write

#### Scenario: Write failure does not fail the run
- **WHEN** writing `run-metrics.json` fails after a step completes (e.g. disk error)
- **THEN** the step's outcome and the run's execution are unaffected, and the failure is surfaced to the user as a warning by run end

### Requirement: Cumulative aggregation across resume sessions

When a run is resumed, `run-metrics.json` SHALL accumulate: step records from earlier execution sessions of the run are retained, new steps are appended, and run-level aggregates cover all execution sessions, so the artifact always describes the whole run.

The run-level duration SHALL be the run's total active execution time: the sum of each execution session's duration. Time between an interruption and the subsequent resume SHALL NOT count toward the run's duration.

The artifact SHALL record each execution session with its observed progress, updated as terminal events are persisted. When a session ends without a clean shutdown (hard kill, crash), its duration SHALL reflect only the time observed up to the last persisted event, and the session SHALL be distinguishable from a cleanly closed one; a subsequent resume SHALL close it at that observed duration rather than inventing time.

(Terminology note: "session" is used in two distinct senses in this change. The top-level `sessions[]` array records *execution sessions* — one per `agent-runner` invocation of the run. The `session_id` field on each step record is the *agent CLI session* identifier assigned by the CLI to that invocation. These are unrelated concepts.)

#### Scenario: Hard-killed session duration reflects last observed progress
- **WHEN** a run session is hard-killed some time after its last step completed, and the run is later resumed
- **THEN** the killed session's recorded duration extends only to its last persisted event, the session is marked as not cleanly closed until resume finalizes it, and the run's total active duration includes no time after that event

#### Scenario: Resumed run accumulates metrics
- **WHEN** a run executes two steps, is interrupted, and is later resumed to execute two more
- **THEN** the final `run-metrics.json` contains all four step records and run totals spanning both sessions

#### Scenario: Paused time excluded from run duration
- **WHEN** a run executes for 5 minutes, sits interrupted for an hour, and is resumed to execute for 3 more minutes
- **THEN** the artifact's run-level duration is 8 minutes, not 68

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
