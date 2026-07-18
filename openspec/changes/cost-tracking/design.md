# Design: cost-tracking

## Context

Agent Runner records `duration_ms` on `step_end`/`run_end` audit events but has no visibility into token usage or cost. The proposal commits to extracting per-step usage (and CLI-reported USD cost) from structured stdout on autonomous-headless steps for all five CLIs, aggregating per run, persisting a versioned `run-metrics.json` artifact for external consumers (Agent Evals), and surfacing metrics in the run views plus a new run-summary screen.

Relevant current state:

- Adapters (`internal/cli/adapter.go`) use optional capability interfaces (`OutputFilter`, `HeadlessResultFilter`, `StdoutWrapper`, `StderrWrapper`). The adapter registry holds claude, codex, opencode, copilot, cursor.
- `ExecuteAgentStep` (`internal/exec/agent.go`) runs the CLI via `runAgentProcess` (headless stdout captured; `HeadlessResultFilter` applied inside it), derives display/capture text via `OutputFilter`, then emits `step_end` via `emitAgentEnd`. `emitStepEnd` (`internal/exec/step_audit.go`) computes `duration_ms`.
- The Codex adapter already parses `turn.completed` and **discards** its `usage` object (`codexTurnCompleted` in `internal/cli/codex.go`; visible in the `turn.completed` fixtures in `internal/cli/adapter_test.go`).
- Audit events are `audit.Event{Timestamp, Prefix, Type, Data map[string]any}` emitted through `audit.EventLogger{Emit(Event)}` (`internal/audit/types.go`). The runner emits `run_start`/`run_end` directly against the concrete file logger (`finalizeRun` and `rs.runStartTime` in `internal/runner/runner.go`). `executeSteps` loops top-level steps only; nested steps complete inside `internal/exec`, so the audit stream is the only place all step completions converge.
- Resume (`internal/runner/resume.go`) reads `state.json` only; `audit.log` is never replayed by the runner. `stateio.WriteState` (`internal/stateio/stateio.go`) already implements temp-then-rename.
- The runview TUI builds its tree purely by tailing the audit log (`Tree.ApplyEvent`); `applyStepEnd` reads `duration_ms`, and `Tree.ApplyEvent`'s step re-execution handling currently **replaces** runtime data (both in `internal/runview/audit.go`). View modes are boolean flags, not an enum (`showLegend`/`quitConfirming` in `internal/runview/view.go`). Two separate liverun wirings exist in `cmd/agent-runner/main.go`; `handleExecDoneMsg` (`internal/runview/model.go`) is the single choke point both share.

The specs for this change live in `openspec/changes/cost-tracking/specs/`; this design fixes the details the specs deliberately leave to design — the concrete attribution mechanism behind the cumulative-usage requirements and the exact `run-metrics.json` v1 field names — and carries the authoritative per-CLI extraction table (the specs are intentionally adapter-generic).

## Goals / Non-Goals

**Goals:**

- One collection path for usage, cost, and timing that feeds `audit.log`, the live/inspect TUI, and `run-metrics.json` with identical, already-attributed values.
- Extraction implemented for all five adapters (Cursor best-effort), with recorded fixtures.
- Durable, append-only, atomically-written `run-metrics.json` that survives interrupts and accumulates across resume sessions, with honest coverage/completeness indicators.
- Per-step and rolled-up metrics in the detail pane, the live view, and a new summary screen.

**Non-Goals:**

- No pricing catalog, token-times-rate math, price fetching, or credits-to-USD conversion (capture-only cost).
- No OTLP/OTEL collection.
- No usage collection for PTY-backed contexts (interactive, autonomous-interactive): recorded as unavailable with reason, collection deferred to a later change.
- No eval-side aggregation or dashboards.

## Approach

### 1. Metrics collector as a normalizing processor in the audit pipeline

New package `internal/metrics`. The `Collector` is **not** a passive tee sink: it is a processor stage that normalizes each lifecycle event before fan-out.

```go
// internal/metrics
type Collector struct { ... }

// Process attributes/enriches a lifecycle event and updates the projection.
// The returned event is what every downstream sink must see.
func (c *Collector) Process(e audit.Event) audit.Event

// Errors returns accumulated projection/write errors (Emit cannot return them).
func (c *Collector) Errors() []error

// Totals returns the run-level aggregate including the current session's
// active duration added to prior sessions' durations.
func (c *Collector) Totals() model.RunTotals
```

A pipeline logger implements `audit.EventLogger` and is installed by the runner at run initialization (the `initRunState` neighborhood in `runner.go`):

```
executor emits event (raw extraction + identity block attached, typed)
  → Collector.Process: attributes cumulative usage (baseline delta),
    replaces raw values with the final attributed UsageRecord,
    updates in-memory projection, persists run-metrics.json atomically
  → file audit sink receives the ENRICHED event (audit.log)
  → (runview tails audit.log, so the TUI also only ever sees attributed usage)
```

Rules:

- The pipeline logger is installed **even if opening `audit.log` fails**; metrics must not depend on the file sink. The audit event stream is the shared internal lifecycle stream; `audit.log` is one sink.
- `run_start` and `run_end` are rerouted through the pipeline (today the runner emits them directly against the concrete logger in `finalizeRun` and run start). The collector uses `run_start` to open a session record and `run_end` to finalize.
- The collector projects `step_end` and `iteration_end` as terminal events; each one triggers an atomic rewrite of `run-metrics.json` (write-temp-then-rename, via a generalized `WriteJSONAtomic` extracted from `stateio`'s pattern) and a snapshot of the current session's `last_observed_at`/provisional duration.
- Typed values end-to-end: the executor attaches `model.UsageRecord`, `*float64` cost, and a `model.ExecutionIdentity` to `Event.Data` as typed structs. The JSON encoder serializes them for `audit.log`; `Process` type-asserts them back. No reparsing of serialized audit JSON, and no reliance on mutating a shared `Data` map as an ordering side effect: `Process` returns a new event value.
- Before emitting `run_end`, the runner calls `Collector.Totals()` and embeds the result in the `run_end` event data; the collector finalizes the artifact when it processes that event. Run-level active duration = sum of session durations (current session's duration computed from its `run_start`).
- Ordering/concurrency: `Process` is called on the same goroutine path as today's `Emit` calls; the collector guards internal state with a mutex so it is safe if any emitter runs concurrently.
- Projection or write errors are accumulated internally and surfaced via `Errors()`; the runner prints a stderr warning at run end. Artifact-write failure never fails the run or the step.
- Containers (loop, group, sub-workflow) store only their own duration in their records; usage/cost rollups are always derived from descendant records at aggregation/render time, so nothing is double-counted.
- On resume, the collector rehydrates from `run-metrics.json` (see §6): validates `schema_version`, loads prior step records, session records, and derives cumulative baselines from the last record per `session_id`. It never replays `audit.log`.

### 2. Data model (`internal/model`)

Types live in `internal/model` so `internal/cli` and `internal/metrics` stay decoupled (model types remain independent of engine/executor packages, per project convention).

```go
type TokenCounts map[string]int64 // canonical category → count; only reported categories present

// Canonical category keys (fixed vocabulary used in artifact totals):
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
    Model         string            `json:"model,omitempty"` // actual model reported by the CLI
    Tokens        TokenCounts       `json:"tokens,omitempty"`         // attributed per-step counts
    RawCumulative TokenCounts       `json:"raw_cumulative,omitempty"` // provenance for cumulative CLIs
    Source        string            `json:"source"` // e.g. "claude:result-event", "codex:turn.completed"
    Completeness  Completeness      `json:"completeness,omitempty"` // empty on unavailable records
}

type ExecutionIdentity struct {
    StepID          string `json:"step_id"`
    Prefix          string `json:"prefix"`
    StepType        string `json:"step_type"`
    Kind            string `json:"kind"`    // "step" | "iteration"
    Attempt         int    `json:"attempt"` // 1-based; assigned by the collector (executor leaves it 0)
    Iteration       int    `json:"iteration,omitempty"`
    CLI             string `json:"cli,omitempty"`
    SessionID       string `json:"session_id,omitempty"`
    SessionStrategy string `json:"session_strategy,omitempty"` // "new" | "resume" | "inherit"
    AgentInvoked    bool   `json:"agent_invoked"` // false for skipped / never-launched steps
}
```

Cost is carried separately as `EstimatedAPICostUSD *float64` (`estimated_api_cost_usd`; nil = not reported), per the `cost-capture` spec. Shell and other non-agent steps get a collected record with empty `Tokens` (true zero) and nil cost.

The spec's three-way completeness distinction (full / partial / unavailable measurements) is carried jointly by `Status` and `Completeness`: `Status: "unavailable"` is the third state, and on unavailable records `Completeness` is empty and omitted from JSON.

**Attempt numbering is owned by the collector, not the executor.** The executor cannot know how many attempts a logical step has had across resume sessions, so it attaches the identity with `Attempt` left zero; `Collector.Process` assigns the authoritative 1-based attempt by counting prior records for the same prefix/id (rehydrated records included) and stamps it into both the artifact record and the enriched event's identity block, so `audit.log` and the TUI always see the normalized attempt number.

`ExecutionIdentity` is attached by the executor to every terminal lifecycle event. It exists because the collector cannot reliably derive step type, CLI, session strategy, or whether a CLI was actually launched from today's `step_end` data. `AgentInvoked == false` (skipped via `skip_if`, launch failure before exec) excludes the step from usage- and cost-coverage denominators. `SessionStrategy` distinguishes a genuinely new session from an externally resumed one for baseline handling (§5).

### 3. Adapter seam: optional `UsageExtractor`

New optional capability interface in `internal/cli`, following the existing pattern:

```go
type UsageExtraction struct {
    Usage            model.UsageRecord
    EstimatedCostUSD *float64
}

// UsageExtractor is implemented by adapters whose CLI emits structured usage.
type UsageExtractor interface {
    // ExtractUsage parses the raw headless stdout (pre-OutputFilter).
    // error != nil        → malformed output (executor records unavailable/parse-failure)
    // nil error, Status unavailable with reason "no-usage-event"
    //                     → well-formed output containing no usage event
    ExtractUsage(rawStdout string) (UsageExtraction, error)
}
```

Extractors are **stateless**. For cumulative CLIs they populate `RawCumulative` and leave `Tokens` empty; attribution happens in the collector, which owns baselines. Extraction never fails the step: the executor converts any error into an unavailable record and proceeds.

Hook point: `ExecuteAgentStep` in `internal/exec/agent.go`, after `runAgentProcess` returns and before `emitAgentEnd`, autonomous-headless contexts only. PTY-backed contexts record `unavailable`/`pty-context` without calling the extractor. The record, cost, and identity ride the `step_end` event as typed values (§1).

### 4. Per-CLI extraction (authoritative table)

This table is the enforcement point for the five-CLI commitment; `tasks.md` gets one implementation-plus-test task per adapter, with fixture-recording tasks for Copilot and Cursor **before** their extractor tasks (neither schema is publicly established).

| Adapter | Headless flags added | Usage source | Vendor → canonical mapping | Event semantics | Cost |
|---|---|---|---|---|---|
| claude | `--output-format stream-json --verbose` | final `result` event `usage` | `input_tokens→input`, `cache_read_input_tokens→cached_input`, `cache_creation_input_tokens→cache_write`, `output_tokens→output` | single final snapshot; last `result` wins | `total_cost_usd` (USD) |
| codex | none (already `--json`) | `turn.completed` `usage` | `input_tokens→input`, `cached_input_tokens→cached_input`, `output_tokens→output`, `reasoning_output_tokens→reasoning` | **cumulative** snapshot per event; last wins, then baseline-delta (§5) | none |
| opencode | none (already `--format json`) | `step_finish` `part.tokens` | `input→input`, `output→output`, `reasoning→reasoning`, `cache.read→cached_input`, `cache.write→cache_write` | per-step increments; **summed** across events | `part.cost` (USD) |
| copilot | `--output-format json` | token metric events in its JSONL output (per fixture) | per recorded fixtures | expected increments; summed (confirm via fixtures) | none captured (GitHub AI Credits, not USD, not converted) |
| cursor | none (already `--output-format stream-json`) | `usage` on result events | per recorded fixtures | best-effort; published result schema omits usage | none |

Claude specifics: headless invocations switch from plain text to `--output-format stream-json --verbose`. The adapter implements `StdoutWrapper` to stream assistant text deltas live to the TUI, `OutputFilter` to yield the `result` event's text for capture variables and display, and `UsageExtractor` to read `usage` + `total_cost_usd` from the `result` event. The result text is not displayed a second time if already streamed. Interactive invocation args are unchanged. Copilot likewise adds `--output-format json` to headless invocations only, which puts it under the cli-adapter "structured headless output preserves plain-text capture" requirement: its adapter must extract the plain-text response from the JSONL for capture variables and display, with interactive args untouched. Codex reuses its existing parse (`codexTurnCompleted` in `codex.go`) and stops discarding the `usage` object. OpenCode's known race can drop the final stdout event; a missing `step_finish` yields `no-usage-event` unavailable but any received increments are still summed.

### 5. Cumulative attribution (Codex today; any cumulative CLI tomorrow)

The collector keys baselines by `cli + session ID`, held in memory, rehydrated on resume from the last step record per `session_id` in `run-metrics.json` (step records carry `session_id`; see §6).

1. **New session** (strategy `new`, session created by this step): baseline is zero; the reported cumulative is the step's attributed usage.
2. **Resume/inherit of a session used earlier in this run**: attributed usage = reported cumulative − rehydrated baseline, per category.
3. **Resumed session with no trustworthy baseline** (session predates the run, or the artifact has no record for it): the step's usage is `unavailable`/`no-baseline`. The lifetime cumulative is NOT attributed to the step; the raw value is retained in `RawCumulative` and becomes the baseline for the next invocation of that session.
4. **Counter reset** (reported cumulative below baseline in any category): the step is `unavailable`/`counter-reset`; the baseline rebases to the reported values.
5. **Category disappearance**: a category present in the baseline but absent from the report produces no delta for that category (absent, never negative). **Category addition**: a category absent from the baseline deltas from zero.
6. Raw cumulative values are always preserved in `RawCumulative`; only deltas appear in `Tokens`.

### 6. `run-metrics.json` schema v1

Located at `~/.agent-runner/projects/{encoded-path}/runs/{run-id}/run-metrics.json`, alongside `audit.log` and `state.json`. Written atomically after every terminal event.

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
      "attempt": 1, "iteration": null, "agent_invoked": true,
      "outcome": "completed", "duration_ms": 32000,
      "session_id": "abc-123",
      "usage": {
        "status": "collected",
        "cli": "claude", "provider": "anthropic", "model": "claude-sonnet-5",
        "tokens": { "input": 1200, "cached_input": 90000, "output": 400 },
        "raw_cumulative": null,
        "source": "claude:result-event", "completeness": "complete"
      },
      "estimated_api_cost_usd": 0.42
    }
  ],
  "totals": {
    "active_duration_ms": 480000,
    "tokens": { "input": 1200, "cached_input": 90000, "output": 400 },
    "usage_coverage": "complete",
    "estimated_api_cost_usd": 0.42,
    "cost_coverage": "complete"
  }
}
```

Semantics:

- **Append-only attempts.** Step records are never replaced. Each execution of a logical step appends a record with a 1-based `attempt` and `record_id` = `prefix/id#attempt` (where `prefix` and `id` are percent-encoded — only alphanumeric, `_`, and `-` kept literal; other bytes become `%XX`). Failed attempts stay and their usage counts toward totals.
- **Iterations.** `iteration_end` appends a record with `kind: "iteration"` and an `iteration` index, carrying identity and duration only (`usage: null` semantics); usage lives on the nested step records. Loop/iteration rollups derive from descendants. Iteration `record_id` uses the sentinel prefix `@iteration`: `@iteration[/escaped-prefix]/escaped-id/N#attempt` (where N is the 1-based iteration number).
- **Prefix grammar.** `ExecutionIdentity.Prefix` is slash-separated: step IDs from the nesting path, with `stepID:N` when inside the N-th iteration of a loop, and `sub:name` when inside a named sub-workflow. Example: `outer:2/sub:core/inner` for the `inner` step inside the `outer` loop's 2nd iteration's `core` sub-workflow.
- **Sessions.** One record per execution session, opened on `run_start`, `status: "open"`. Every terminal-event write snapshots `last_observed_at` and provisional `duration_ms`. `run_end` closes the session (`ended_at`, `status: "closed"`). After a hard kill the session stays `open` and its duration is trusted only up to the last persisted event; resume closes it at that observed value. Run-level `active_duration_ms` is the sum of session durations; paused time between sessions never counts. (Note: the top-level `sessions[]` array records **execution sessions** — one per `agent-runner` invocation of the run. The `session_id` field on each step record is a different concept: the **agent CLI session** identifier assigned by the CLI to that particular invocation. The two use "session" in distinct senses.)
- **Coverage.** `usage_coverage` and `cost_coverage` are `complete`/`partial`/`none`, computed over agent step records with `agent_invoked` true (skipped/never-launched steps are excluded from denominators). Token totals sum only reported values in canonical categories; a category no step reported is absent, not zero.
- **`history_complete`** is orthogonal to coverage: `false` means the artifact itself lost history (a corrupt or unreadable predecessor forced a fresh start), so the coverage indicators describe only the steps this artifact knows about.
- **Recovery.** On resume: a corrupt artifact, or one with an **unsupported schema version (including newer than this binary)**, is preserved under a unique backup name (`run-metrics.json.bak-<RFC3339-session-start>`), never overwritten in place; a fresh artifact starts with `history_complete: false` and a warning is surfaced.
- **Write failures** are nonfatal: the run and step proceed; errors accumulate in the collector and are reported at run end.

`WriteJSONAtomic(path string, v any) error` is added to `internal/stateio` (generalizing `WriteState`'s existing temp-then-rename logic); `WriteState` becomes a thin wrapper over it.

### 7. TUI

- `tree.StepNode` (`internal/runview/tree.go`) gains `Attempts []AttemptMetrics` where `AttemptMetrics{Attempt int, Usage *model.UsageRecord, CostUSD *float64, DurationMs *int64, Outcome string, AgentInvoked bool}`. `AgentInvoked` (from `identity.agent_invoked`) gates the mid-run coverage denominators so skipped/never-invoked agent steps are excluded, matching the finished-run coverage computed from authoritative `run_end` totals. `StepNode` also records `StartedAt time.Time` from the `step_start` timestamp; the mid-run summary adds the currently-running step's elapsed wall time (now − `StartedAt`) so active duration is never reported as zero while a step is still in flight (live runs only; excluded for aborted steps and static inspects). `applyStepEnd` (`internal/runview/audit.go`) **appends** an attempt instead of overwriting (adjusting `Tree.ApplyEvent`'s step re-execution replacement for metrics purposes; other runtime fields keep today's latest-wins behavior).
- **Detail pane** (`internal/runview/detail.go`): token/cost lines render adjacent to the duration line for the **latest attempt**, annotated `attempt N` when N > 1. Unavailable usage/cost render explicit markers, never zeros or `$0.00`. Since the enriched events flow through the same audit tail, live mid-run updates need no extra plumbing.
- **Summary screen**: a new `showSummary` boolean view flag (matching the `showLegend` pattern in `view.go`), a `renderSummary` view, `s` toggle in `handleKey` (`model.go`), a help-bar entry (`helpBarParts` in `view.go`), auto-show on successful completion in `handleExecDoneMsg` (`model.go`, the single choke point covering both liverun wirings), and `--inspect`/run-list opens of `completed` runs start with `showSummary = true`. Summary rows aggregate **every attempt** of a step; container rows roll up descendants; the totals line shows active duration, canonical-category token totals, and cost with both coverage indicators. The summary is a modal screen (like the legend): while shown it captures keys, and the step rows scroll within a fixed header and a **pinned** totals footer (`summaryOffset`, adjusted by `j`/`k`/arrows and clamped at render time) so the totals line stays on screen for workflows taller than the terminal.

### 8. Testing

TDD throughout, tests next to source packages, `go-cmp` for structured comparisons:

- Per-adapter extraction tests with recorded JSONL fixtures, extending the existing `adapter_test.go` pattern (Codex `turn.completed` fixtures already exist there; new fixtures recorded for claude stream-json, opencode, copilot, cursor).
- Collector unit tests: delta attribution (new session, in-run resume, no-baseline, counter reset, category disappearance/addition), append-only attempts, iteration records, rehydration (valid, corrupt, newer-version), atomic write (temp dir), paused-time-excluded duration, hard-kill session snapshot, coverage denominators excluding non-invoked steps, error accumulation.
- Runner-level tests: pipeline installed when `audit.log` open fails; `run_start`/`run_end` routed through the pipeline; totals embedded in `run_end`.
- Runview tests: detail lines (collected, unavailable, attempt annotation), summary rendering, rollups, `s` toggle, auto-show on success, inspect-completed default view.

## Decisions

1. **Collector as normalizing processor in the audit pipeline** (vs a passive tee sink, vs a dedicated recorder threaded through `ExecutionContext`). A tee would let raw cumulative values reach `audit.log` and the TUI before attribution; a dedicated recorder duplicates the one data path that already sees every nested completion and adds plumbing to every executor. The processor shape guarantees every sink sees identical attributed values, with ordering explicit in the pipeline.
2. **Typed values in `Event.Data`, never reparsed audit JSON.** The executor attaches `UsageRecord`/`ExecutionIdentity` structs; the collector type-asserts. Keeps one source of truth and avoids fragile round-trips.
3. **Stateless extractors, stateful collector.** Adapters only parse; the collector owns baselines and attribution. Keeps `internal/cli` free of cross-step state and makes attribution testable in one place.
4. **Baseline-delta attribution with unavailable-not-fabricated fallbacks** (vs recording raw cumulatives with a marker). Raw-with-marker violates the per-step attribution spec and double-counts rollups. Deltas satisfy it; every untrustworthy case (no baseline, counter reset) records unavailable and rebases rather than inventing counts.
5. **Claude `stream-json --verbose`** (vs plain `json` envelope). Preserves live TUI streaming through the existing `StdoutWrapper` seam; plain `json` would leave stdout silent for the whole step, a UX regression.
6. **Canonical token vocabulary** (`input`, `cached_input`, `cache_write`, `output`, `reasoning`, plus `other:<vendor-key>`). Stable totals require one vocabulary; adapters own the vendor mapping.
7. **Append-only attempt records with `session_id`** (vs latest-wins). Failed attempts consumed tokens and must count; `session_id` per record also makes cumulative baselines derivable on rehydrate without a separate baselines section in the artifact.
8. **`history_complete` orthogonal to coverage.** Coverage describes known agent steps; it cannot honestly describe steps the collector no longer knows existed. A separate flag says "this artifact's history is incomplete".
9. **Iteration records carry duration only.** Usage lives on nested step records; duplicating it on iteration records would double-count rollups.
10. **Model types in `internal/model`.** Keeps `internal/cli` and `internal/metrics` decoupled and follows the project rule that model types stay independent of engine/executor packages.

## Risks / Trade-offs

- [Copilot/Cursor usage schemas are not publicly established] → Fixture-recording tasks precede extractor tasks; Cursor is committed as best-effort (unavailable when the `usage` object is absent), matching its published result schema which omits usage.
- [Claude headless output format change alters capture/display behavior] → `OutputFilter`/`StdoutWrapper` reproduce today's plain-text capture and live display; adapter tests assert capture text, streamed text, and no double display. Interactive args untouched.
- [OpenCode's final-event race can drop `step_finish`] → Missing event → `no-usage-event` unavailable; received increments still summed; never fabricated.
- [Hard kill loses tail-end duration] → Session duration is only trusted through the last persisted terminal event; artifact spec states this, `status: "open"` makes it visible.
- [Corrupt/newer-version artifact on resume loses history] → Backup under a unique name, fresh artifact with `history_complete: false`, surfaced warning. Never silently overwrite a future schema version.
- [Artifact write failures mid-run] → Nonfatal; collector accumulates errors, runner warns at run end. Worst case the artifact lags but is always well-formed (atomic rename).
- [Baseline heuristics can still misattribute if a CLI changes reporting semantics] → Raw cumulatives are always preserved in provenance, so records are auditable and re-derivable.

## Migration Plan

Schema-first sequencing (per proposal): 1) `internal/model` types + canonical vocabulary; 2) `WriteJSONAtomic` in `stateio`; 3) `internal/metrics` collector + pipeline logger + runner wiring (including `run_start`/`run_end` rerouting and resume rehydration); 4) per-adapter extractors with fixtures (claude, codex, opencode, then copilot/cursor after fixture recording); 5) executor hookup in `internal/exec/agent.go` + identity blocks; 6) TUI (detail lines, summary screen, attempts). No data migration: runs predating the feature simply have no `run-metrics.json`; readers treat absence as no metrics. Rollback = revert; the artifact is additive and nothing else depends on it yet. `run-metrics.json` v1 becomes a stable consumer contract on release; breaking changes require a version bump.

## Open Questions

None blocking. Copilot/Cursor exact vendor field mappings are deliberately deferred to their fixture-recording tasks; the canonical vocabulary and best-effort commitments above bound the outcome.
