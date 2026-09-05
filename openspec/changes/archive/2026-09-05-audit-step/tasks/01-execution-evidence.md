# Task: Persist execution-session metrics and conservative Git checkpoints

## Goal

Create the shared, production-safe evidence foundation for development audits: every new or resumed Runner invocation has a durable execution-session identity, lifecycle and metrics records remain attributable to that invocation, `run-metrics.json` migrates safely to schema v3 with per-session rollups, and executable leaf attempts record trustworthy start/end Git checkpoints without changing step outcomes when Git evidence is unavailable.

## Background

You MUST read:

- `openspec/changes/audit-step/proposal.md` for the commit-first measurement intent, explicit unknowns, resume lineage, and separation of source and audit overhead
- `openspec/changes/audit-step/design.md` sections 2–4 and the migration plan for finalization ordering, execution-session UUIDs, the audit-logger checkpoint wrapper, leaf aggregation, conservative v2-to-v3 migration, and Git attribution inputs
- `openspec/changes/audit-step/specs/audit-log-lifecycle/spec.md` for execution-session and Git-checkpoint behavior
- `openspec/changes/audit-step/specs/run-metrics-artifact/spec.md` for schema v3, resume rollups, trustworthy measurements, and audit-overhead separation
- `openspec/changes/audit-step/test-plan.md` for the shared-instrumentation portion of `INT-003`
- `docs/run-state-and-audit.md` and `docs/usage-and-cost-tracking.md` for the public schema-v2 and audit-event documentation that schema v3 replaces

Relevant existing code is in `internal/audit/` (event types and the real `audit.EventLogger` interface), `internal/metrics/collector.go`, `internal/metrics/pipeline.go`, `internal/model/state.go`, `internal/model/context.go`, `internal/runner/runner.go`, `internal/runner/resume.go`, and step-event emission under `internal/exec/`. Keep core model types independent of engine and executor packages. Implement Git observation as a thin logger/pipeline wrapper or similarly centralized boundary; do not add repository probing independently to every executor.

Preserve the stable run ID and the existing agent CLI `session_id`: the new execution-session identity names one `agent-runner` invocation and is unrelated to an agent adapter's session ID. New data must be usable by untagged builds because the metrics and audit artifacts are shared runtime formats. Missing, interrupted, interactive, non-Git, or ambiguous evidence must remain unknown/unavailable rather than becoming zero or guessed attribution.

Use TDD for behavior. Keep tests beside the affected packages, prefer `google/go-cmp`, and use temporary repositories and local fakes rather than a mocking framework.

## Spec

### Requirement: Execution sessions have durable identities

Every run invocation that starts or resumes workflow execution SHALL have a durable execution-session identity. Run-level and step-level lifecycle events SHALL identify the execution session that emitted them so evidence from separate resume sessions can be distinguished without changing the stable source-run identity.

#### Scenario: New run starts
- **WHEN** Agent Runner starts a new source run
- **THEN** its lifecycle events identify both the run and its first execution session

#### Scenario: Run resumes
- **WHEN** Agent Runner resumes an existing source run
- **THEN** newly emitted lifecycle events retain the run identity and use a new execution-session identity

### Requirement: Step Git checkpoints are recorded durably

For each executable source step attempt, Agent Runner SHALL record a local repository checkpoint at step start and step end. When Git evidence is available, the checkpoint SHALL include the current revision plus index, working-tree, and untracked-file state sufficient to derive aggregate file/addition/deletion deltas and identify commits appearing across the boundary. When unavailable, unsupported, or interrupted before the closing checkpoint, the event SHALL state that limitation rather than inventing a revision or zero change.

#### Scenario: Step starts and completes in Git repository
- **WHEN** a step executes normally in a supported Git worktree
- **THEN** its lifecycle evidence contains starting and ending Git revisions and the local checkpoint data needed for conservative change attribution

#### Scenario: Step does not change HEAD
- **WHEN** a step completes without creating a commit
- **THEN** both checkpoints retain enough repository state to distinguish no change from uncommitted work

#### Scenario: Step is interrupted before end checkpoint
- **WHEN** the process is terminated before Agent Runner can capture the step's ending repository checkpoint
- **THEN** the lifecycle evidence marks the ending checkpoint unavailable

#### Scenario: Run is outside a Git repository
- **WHEN** a step executes where supported Git evidence is unavailable
- **THEN** the checkpoint records Git evidence as unavailable and the step continues normally

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

## Test Plan

- `INT-003: Session metrics, Git checkpoints, and leaf attribution` (shared-instrumentation portion): use temporary Git repositories and v2 metrics fixtures to prove lossless v3 migration, deterministic legacy execution-session identities, explicit ambiguous legacy attribution, distinct resume rollups, active-time duration, unknown interactive usage/cost, durable start/end checkpoint payloads, and schema-v3 aggregate `files_changed`, `lines_added`, and `lines_deleted` values or explicit unknown coverage when a checkpoint is incomplete. Do not implement full-logical-leaf aggregation or the `attributed`/`working_tree`/`deferred_commit`/`ambiguous`/`no_change` commit-attribution classification in the shared metrics/audit packages.

## Done When

- A new and resumed invocation each creates a durable execution-session identity that is present on all new run/step lifecycle and metric records while the stable run ID and agent CLI `session_id` retain their meanings.
- `internal/metrics/` reads v1/v2 artifacts, writes schema v3, preserves legacy attempts and totals, assigns only defensible legacy session attribution, exposes per-session rollups, and excludes paused wall time.
- A centralized `audit.EventLogger`-compatible boundary records start/end HEAD, index, worktree, and untracked state for executable step attempts, including explicit unavailable or missing-end states, without changing step results.
- Each schema-v3 step record and applicable per-execution-session rollup exposes aggregate `files_changed`, `lines_added`, and `lines_deleted` supported by its recorded checkpoints, or an explicit unknown/coverage state when derivation is incomplete; unavailable repository counts and all other unavailable duration/cost/usage measurements are never coerced to zero.
- Metrics collection remains run-local: source step records, execution-session rollups, and run totals never absorb work recorded in a distinct audit run, and report-only retries cannot create new source model usage.
- `docs/run-state-and-audit.md` documents schema v3, execution-session identity, per-session rollups, conservative v2-to-v3 migration, and the new step Git-checkpoint audit evidence/unknown states; `docs/usage-and-cost-tracking.md` describes the same consumer-visible schema without documenting the private development-audit capability.
- Focused unit tests and the shared-instrumentation portion of `INT-003` pass in normal CI; tagged evidence-projection assertions are executable from the persisted artifacts without duplicating attribution logic in `internal/audit/` or `internal/metrics/`.
- `make fmt`, targeted package tests, and `go test ./internal/audit ./internal/metrics ./internal/runner ./internal/exec/...` pass.
