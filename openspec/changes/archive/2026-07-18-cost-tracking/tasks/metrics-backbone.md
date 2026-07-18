# Task: Metrics collection backbone

## Goal

Build the metrics collection backbone: usage/identity model types, an atomic JSON writer, the `internal/metrics` collector, and its wiring into the runner and executors, so that every workflow run durably produces a versioned `run-metrics.json` artifact and enriched audit events. After this task, durations, outcomes, sessions, and run-level aggregates are collected end-to-end; agent-step usage appears as an explicit unavailable state until adapter extraction lands separately.

## Background

You MUST read these files before starting:

- `openspec/changes/cost-tracking/design.md` — the full approved design. Sections 1 (collector pipeline), 2 (data model), 6 (artifact schema v1) and the Decisions list govern this task.
- `openspec/changes/cost-tracking/specs/run-metrics-artifact/spec.md`
- `openspec/changes/cost-tracking/specs/audit-log-entries/spec.md`
- `internal/audit/types.go` — `Event{Timestamp, Prefix, Type, Data map[string]any}` and `EventLogger{Emit(Event)}`.
- `internal/runner/runner.go` — run initialization around `initRunState`, the top-level step loop `executeSteps`, and `finalizeRun` which emits `run_end`.
- `internal/runner/resume.go` — resume reads `state.json`; the runner never replays `audit.log`.
- `internal/stateio/stateio.go` — `WriteState` already implements temp-then-rename.
- `internal/exec/step_audit.go` — `emitStepEnd` computes `duration_ms`.
- `internal/exec/agent.go` — `emitAgentEnd` emits the agent `step_end`.
- The loop executor in `internal/exec/` — emits `iteration_start`/`iteration_end`.
- `docs/run-state-and-audit.md` — documents the run directory contents; add a `run-metrics.json` section.

### Architecture (from the design)

The collector is a **normalizing processor** in the audit pipeline, not a passive tee. A pipeline logger implementing `audit.EventLogger` wraps the file audit sink:

```text
executor emits event (typed usage record + identity block in Data)
  → Collector.Process(e audit.Event) audit.Event: attributes cumulative usage
    (baseline delta), replaces raw values with the final UsageRecord, updates
    the in-memory projection, persists run-metrics.json atomically
  → file audit sink receives the ENRICHED event (audit.log)
```

Hard rules:

- Install the pipeline **even if opening `audit.log` fails**; metrics must not depend on the file sink.
- Reroute `run_start` and `run_end` through the pipeline (today the runner emits them directly against the concrete logger). The collector opens a session record on `run_start` and finalizes on `run_end`.
- `step_end` and `iteration_end` are the terminal events; each triggers an atomic artifact rewrite and a snapshot of the current session's `last_observed_at`/provisional duration.
- Typed values end-to-end: executors put `model.UsageRecord` and `model.ExecutionIdentity` structs into `Event.Data`; `Process` type-asserts them back and returns a **new** event value (never rely on mutating a shared `Data` map as an ordering side effect). The JSON encoder serializes the structs for `audit.log`.
- Before emitting `run_end`, the runner calls `Collector.Totals()` and embeds the result in the event data. Run-level active duration = sum of session durations; the current session's duration comes from its `run_start`. Paused time between sessions never counts.
- `Emit` cannot return errors: the collector accumulates projection/write errors and exposes `Errors() []error`; the runner prints a stderr warning at run end. Artifact-write failure never fails a step or the run.
- Containers (loop, group, sub-workflow) store only their own duration; usage/cost rollups are derived from descendant records at aggregation time (never double-counted).
- On resume, the collector rehydrates from `run-metrics.json`: validate `schema_version`, load step records, session records, and derive cumulative baselines from the last record per `session_id`. Never replay `audit.log`.
- `Process` must be mutex-guarded so it is safe if any emitter runs concurrently.

### Model types (new file in `internal/model/`)

Model types stay independent of engine/executor packages (project convention). From the design:

```go
type TokenCounts map[string]int64 // canonical category → count; only reported categories present

const (
    TokenInput       = "input"
    TokenCachedInput = "cached_input"
    TokenCacheWrite  = "cache_write"
    TokenOutput      = "output"
    TokenReasoning   = "reasoning"
)
// Unknown vendor categories are preserved as "other:<vendor-key>", never dropped.

type UsageStatus string        // "collected" | "unavailable"
type Completeness string       // "complete" | "partial"
type UnavailableReason string  // "pty-context" | "parse-failure" | "no-usage-event" |
                               // "no-baseline" | "counter-reset" | "unsupported-adapter"

type UsageRecord struct {
    Status        UsageStatus       `json:"status"`
    Reason        UnavailableReason `json:"reason,omitempty"`
    CLI           string            `json:"cli"`
    Provider      string            `json:"provider,omitempty"`
    Model         string            `json:"model,omitempty"`
    Tokens        TokenCounts       `json:"tokens,omitempty"`
    RawCumulative TokenCounts       `json:"raw_cumulative,omitempty"`
    Source        string            `json:"source"`
    Completeness  Completeness      `json:"completeness"`
}

type ExecutionIdentity struct {
    StepID          string `json:"step_id"`
    Prefix          string `json:"prefix"`
    StepType        string `json:"step_type"`
    Kind            string `json:"kind"`    // "step" | "iteration"
    Attempt         int    `json:"attempt"` // assigned by the collector; executor leaves it 0
    Iteration       int    `json:"iteration,omitempty"`
    CLI             string `json:"cli,omitempty"`
    SessionID       string `json:"session_id,omitempty"`
    SessionStrategy string `json:"session_strategy,omitempty"` // "new" | "resume" | "inherit"
    AgentInvoked    bool   `json:"agent_invoked"` // false for skipped / never-launched steps
}
```

Cost travels separately as `estimated_api_cost_usd` (`*float64`, nil = not reported).

### Artifact schema v1 (fixed by the design; consumers depend on these names)

```json
{
  "schema_version": 1,
  "run_id": "...",
  "workflow": "...",
  "history_complete": true,
  "sessions": [
    { "started_at": "RFC3339", "last_observed_at": "RFC3339",
      "ended_at": "RFC3339", "duration_ms": 480000, "status": "closed" }
  ],
  "steps": [
    {
      "record_id": "plan#1",
      "prefix": "", "id": "plan", "kind": "step", "type": "agent",
      "attempt": 1, "iteration": null,
      "outcome": "completed", "duration_ms": 32000,
      "session_id": "abc-123",
      "usage": { "status": "...", "cli": "...", "provider": "...", "model": "...",
                 "tokens": {}, "raw_cumulative": null, "source": "...", "completeness": "..." },
      "estimated_api_cost_usd": 0.42
    }
  ],
  "totals": {
    "active_duration_ms": 480000,
    "tokens": { "input": 1200 },
    "usage_coverage": "complete",
    "estimated_api_cost_usd": 0.42,
    "cost_coverage": "complete"
  }
}
```

Semantics the collector must implement: append-only attempt records (`record_id` = `prefix/id#attempt`; the collector assigns the authoritative attempt number by counting prior records for the same prefix/id, rehydrated records included, and stamps it into both the artifact record and the enriched event's identity block, so `audit.log` and the TUI always see the normalized attempt number); iteration records (`kind: "iteration"`) carry identity and duration only; sessions open on `run_start`, snapshot on every terminal write, close on `run_end` (a hard-killed session stays `"open"` and resume closes it at the last observed value); coverage denominators count only agent records with `agent_invoked` true; `history_complete: false` plus a unique backup (`run-metrics.json.bak-<filename-safe-UTC-session-start>`) whenever corrupt data, malformed persisted timestamps, or an unsupported version (including newer) forces a fresh start. Invalid event timestamps surface warnings and never fabricate elapsed time.

### Cumulative attribution (implemented in the collector now, exercised once adapters report `RawCumulative`)

Baselines are keyed by `cli + session ID` and rehydrated from the last artifact record per `session_id`:

1. New session (strategy `new`): baseline zero; reported cumulative is the step's usage.
2. Resume/inherit of a session recorded earlier in the run: usage = reported − baseline, per category.
3. Resumed session with no trustworthy baseline: `unavailable`/`no-baseline`; keep the raw value in `RawCumulative` and use it as the next baseline.
4. Any category below its baseline (counter reset): `unavailable`/`counter-reset`; rebase to the reported values.
5. Category missing from the report → absent (never negative); category new vs the baseline → delta from zero.

### Executor wiring in this task

Attach a `model.ExecutionIdentity` to every terminal lifecycle event (`emitStepEnd`, `emitAgentEnd`, the loop executor's `iteration_end`). Agent steps additionally attach a `UsageRecord`: `unavailable`/`unsupported-adapter` for headless steps (adapter extraction is implemented elsewhere and will replace this default), `unavailable`/`pty-context` for interactive and autonomous-interactive contexts, and non-agent steps attach a collected record with empty `Tokens` (a true zero) and nil cost.

### New/changed files

- `internal/model/` — new types file (e.g. `usage.go`) with the types above.
- `internal/stateio/` — add `WriteJSONAtomic(path string, v any) error` generalizing the temp-then-rename in `WriteState`; make `WriteState` a thin wrapper.
- `internal/metrics/` — new package: collector, pipeline logger, artifact read/write/rehydrate.
- `internal/runner/runner.go`, `internal/runner/resume.go` — pipeline installation, `run_start`/`run_end` rerouting, totals, warning on `Errors()`, resume rehydration.
- `internal/exec/step_audit.go`, `internal/exec/agent.go`, loop executor — identity blocks and default usage records.
- `docs/run-state-and-audit.md` — document `run-metrics.json` (location, schema v1, atomicity, resume accumulation).

### Conventions

TDD (failing test first for each behavior), tests next to the package, `google/go-cmp` for structured comparisons, local stubs instead of mocking frameworks, `make fmt` before finishing, `make test` and `make lint` clean. Audit logging keeps the real `audit.EventLogger` interface — do not introduce empty interfaces. Commit style: `type: lowercase description` (e.g. `feat: add metrics collector and run-metrics artifact`).

## Spec

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

`run-metrics.json` SHALL contain one record per executed step and a run-level aggregate. Each step record SHALL include the step's identifier and nesting prefix, step type, outcome, duration in milliseconds, the usage record (token categories plus provenance and completeness, per `agent-usage-collection`), and `estimated_api_cost_usd` (per `cost-capture`). The run-level aggregate SHALL include the run's duration, per-category token totals, and the cost total with its coverage indicator. Unavailable usage and absent cost SHALL appear as explicit null/unavailable states, never as zeros.

Per-category token totals SHALL be the sum of the values reported for that category across all executed steps regardless of outcome. Steps with unavailable usage, and categories a step did not report, contribute nothing to the totals; a category no step reported SHALL be absent from the totals, not zero. The aggregate SHALL include a usage-coverage indicator — `complete` when every agent step that actually invoked its CLI has an available usage record, `partial` when some do, and `none` when none do (including runs with no agent steps) — parallel to the cost coverage indicator. Agent steps that never invoked their CLI (skipped, or failed before launch) SHALL NOT count toward either coverage denominator.

#### Scenario: Agent step record content
- **WHEN** an autonomous-headless agent step completes with usage and cost collected
- **THEN** its record in `run-metrics.json` carries the step identifier, prefix, type, outcome, duration, token categories with provenance, and the reported cost

#### Scenario: Run aggregate content
- **WHEN** a run ends
- **THEN** the artifact's run-level aggregate carries the run duration, per-category token totals across all steps, and the cost total with coverage

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

When a run is resumed, `run-metrics.json` SHALL accumulate: step records from earlier sessions of the run are retained, new steps are appended, and run-level aggregates cover all sessions, so the artifact always describes the whole run.

The run-level duration SHALL be the run's total active execution time: the sum of each execution session's duration. Time between an interruption and the subsequent resume SHALL NOT count toward the run's duration.

The artifact SHALL record each execution session with its observed progress, updated as terminal events are persisted. When a session ends without a clean shutdown (hard kill, crash), its duration SHALL reflect only the time observed up to the last persisted event, and the session SHALL be distinguishable from a cleanly closed one; a subsequent resume SHALL close it at that observed duration rather than inventing time.

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

### Requirement: Run end usage and cost totals

The `run_end` entry SHALL include the run's aggregated metrics: per-category token totals across all steps of the run (cumulative across resume sessions) with the usage-coverage indicator, and the run-level cost total with its coverage indicator, per the `cost-capture` capability. Token totals SHALL follow the aggregation semantics of the `run-metrics-artifact` capability: only reported values are summed, steps with unavailable usage contribute nothing, and unreported categories are absent rather than zero. When no step reported cost, the cost total SHALL be null with coverage `none`.

#### Scenario: Run end carries aggregated totals
- **WHEN** a run ends after agent steps consumed tokens and some reported cost
- **THEN** the `run_end` entry includes per-category token totals and the cost total with its coverage indicator

#### Scenario: Run end with no cost data
- **WHEN** a run ends and no step reported a USD cost
- **THEN** the `run_end` entry's cost total is null and its coverage is `none`

#### Scenario: Run end with mixed usage availability
- **WHEN** a run ends containing one agent step with a full usage record and one whose usage is unavailable
- **THEN** the `run_end` entry's token totals equal the reporting step's values with usage coverage `partial`; no zeros are substituted

### Requirement: Missing cost is null, never zero

When a step has no CLI-reported USD cost — because the CLI reports none, the step's usage is unavailable, or the step is a non-agent step — the cost SHALL be represented as null (or an equivalent explicit absent state). A missing cost SHALL never be recorded as `0`.

#### Scenario: Unavailable usage yields null cost
- **WHEN** an agent step's usage record is unavailable (e.g. PTY-backed context or parse failure)
- **THEN** the step's `estimated_api_cost_usd` is null, not zero

#### Scenario: Shell step has null cost
- **WHEN** a shell step completes
- **THEN** its `estimated_api_cost_usd` is null (its token usage is a true zero, but no cost was reported)

### Requirement: Run-level cost aggregation with coverage

Run-level cost SHALL be the sum of `estimated_api_cost_usd` over the agent steps that reported a cost, regardless of step outcome, accompanied by an explicit coverage indicator: `complete` when every agent step that actually invoked its CLI reported a cost, `partial` when some did and some did not, and `none` when no step reported a cost (including runs with no agent steps). Agent steps that never invoked their CLI (skipped, or failed before launch) SHALL NOT count toward the coverage denominator. When coverage is `none`, the run-level cost total SHALL be null.

#### Scenario: All agent steps priced
- **WHEN** every completed agent step in a run reported a USD cost
- **THEN** the run-level cost is their sum and coverage is `complete`

#### Scenario: Mixed run is flagged partial
- **WHEN** a run contains one agent step whose CLI reported a cost and one whose CLI did not
- **THEN** the run-level cost is the sum of the reported costs and coverage is `partial`

#### Scenario: No cost reported
- **WHEN** a run's agent steps reported no cost (or the run has no agent steps)
- **THEN** the run-level cost is null and coverage is `none`

#### Scenario: Skipped agent step does not affect coverage
- **WHEN** a run contains one agent step that invoked its CLI and reported a cost, and one agent step that was skipped and never invoked its CLI
- **THEN** the run-level cost coverage is `complete`; the skipped step is excluded from the denominator

### Requirement: Per-step attribution for cumulative usage sources

When a CLI reports cumulative session totals rather than per-invocation usage, the usage recorded for a step that resumes an existing session SHALL reflect only that step's consumption, not the session's lifetime total. Attribution SHALL never produce a negative or fabricated token count: when the reported cumulative total is lower than the session's previously recorded total (e.g. a counter reset), the step's usage SHALL be recorded as unavailable. When a session is resumed but no previously recorded total for it exists within the run, the step's usage SHALL be recorded as unavailable rather than attributing the session's lifetime total to the step; the reported cumulative value SHALL be retained in provenance and SHALL serve as the prior total for subsequent invocations of that session. A token category present in the prior total but absent from the current report SHALL produce no value for that category (absent, never negative); a category absent from the prior total SHALL be attributed from zero.

#### Scenario: Resumed session step records its own usage
- **WHEN** an agent step resumes a session whose earlier step already consumed tokens, and the CLI reports cumulative session totals
- **THEN** the resumed step's usage record reflects only the tokens consumed by that step's invocation

#### Scenario: Resumed session without prior total yields unavailable
- **WHEN** an agent step resumes a session whose earlier consumption was never recorded in this run, and the CLI reports cumulative session totals
- **THEN** the step's usage record is an explicit unavailable state, the reported cumulative value is retained in provenance, and a subsequent step on the same session is attributed relative to that retained value

#### Scenario: Cumulative counter reset yields unavailable
- **WHEN** a step's reported cumulative total is lower than the total previously recorded for that session
- **THEN** the step's usage record is an explicit unavailable state; no negative counts are recorded; subsequent attribution for that session is based on the newly reported total

#### Scenario: Category disappears from cumulative reporting
- **WHEN** a token category present in the session's previously recorded total is absent from the current report
- **THEN** the step's usage record contains no value for that category (absent, never negative)

#### Scenario: Category appears mid-session
- **WHEN** the current report contains a token category absent from the session's previously recorded total
- **THEN** that category's attributed value equals the newly reported value (attributed from zero)

### Requirement: Non-agent step usage

Non-agent steps (shell, UI, and other step types that invoke no agent CLI) SHALL report zero token usage while retaining their measured duration. Zero here is a true measurement — no tokens were consumed — and is distinct from the unavailable state.

#### Scenario: Shell step reports zero usage
- **WHEN** a shell step completes
- **THEN** its metrics carry zero token usage and the step's duration in milliseconds

## Done When

Tests covering every scenario above pass (collector unit tests for attribution, attempts, sessions, rehydration, recovery, atomic writes, coverage; runner-level tests that the pipeline is installed when `audit.log` open fails, `run_start`/`run_end` flow through it, and totals land in `run_end`). Running a workflow end-to-end produces a valid `run-metrics.json`. `docs/run-state-and-audit.md` documents the artifact. `make test` and `make lint` are clean.
