## Context

- `codex exec --json` stdout (`thread.started`, `item.*`, `turn.completed{usage}`) carries no
  rate-limit state (checked against codex-cli 0.157.1). Codex writes it to the thread's session log,
  `<codex-home>/sessions/YYYY/MM/DD/rollout-<local-timestamp>-<thread_id>.jsonl`. That file is JSONL:
  - line 1 is a `session_meta` event whose payload has `id`, `cwd` and `creator_account_id`;
  - each model request appends an `event_msg` whose payload type is `token_count`, with an optional
    `rate_limits` block:

  ```json
  {"timestamp":"2026-09-29T02:07:37.116Z","type":"event_msg","payload":{"type":"token_count",
   "info":{...},"rate_limits":{"limit_id":"codex","primary":{"used_percent":52.0,
   "window_minutes":10080,"resets_at":1791047425},"secondary":null,"plan_type":"prolite", ...}}}
  ```
  A one-request `codex exec` writes exactly one `token_count`.
- **Codex home.** `internal/cli/completion_plugin.go` resolves the user's Codex home as `$CODEX_HOME`,
  falling back to `~/.codex`. When Runner injects a private home, it symlinks every source entry,
  `sessions/` included, so logs always land in the source home. The existing interactive discovery
  (`discoverCodexInteractiveSession`) hard-codes `~/.codex`.
- **Native attempt flow.** Agent steps (`internal/exec/agent.go`) and agent calls
  (`internal/exec/agent_call.go`) both run through `InvokeAgent`
  (`internal/exec/invocation.go`). `InvokeAgent` returns an `AgentInvocationResult` with `StartedAt`
  (taken before spawn), `FinishedAt` (after exit) and `DiscoveredSessionID` (the headless
  `thread.started` ID, or the interactive log scan). Each caller puts usage into the terminal audit
  event's data (`step_end` / `agent_call_end`). `metrics.Collector.processTerminal` turns that event
  into a `StepRecord`. The artifact is persisted atomically and rehydrated on resume.
- **Nested Validator attempts.** `Collector.IncorporateValidator` stores immutable
  `MeasurementHead`s. `refreshMeasurementsLocked` drops every step with a `MeasurementKey` and
  rebuilds it with `validatorStepProjection` on each import or refresh, so fields added to those
  projections do not survive. Validator records carry lifecycle `started_at`/`ended_at`, `adapter`,
  and allowlisted `provider_native_usage` rows. `provider_session_id` is allowlisted by both sides.
  **However, Agent Validator's Codex adapter does not currently emit it** (`codex-usage.ts` emits only
  token rows), so today no nested Codex attempt can be tied to its thread log.
- `internal/metrics` imports only `audit`, `measurements`, `model` and `stateio`, never `internal/cli`.

## Goals / Non-Goals

**Goals:**
- Capture start, end and per-window delta for every measured native Codex attempt (headless,
  interactive, agent call), following `codex-rate-limit-capture`.
- Persist the evidence on attempt records and produce a run-level rollup, following
  `run-metrics-artifact`.
- Provide a durable enrichment path for nested Validator Codex attempts. It lights up as soon as
  Validator exports `provider_session_id`, and until then it reports `session-unidentified`.
- Never affect step outcome, usage, cost or run progress.

**Non-Goals:**
- Changing Agent Validator or the measurement contract.
- UI, audit-report or `list`/`view` presentation.
- Any heuristic matching of Validator attempts to logs by time or cwd.
- Persisting account identifiers.

## Approach

```
InvokeAgent ──(StartedAt, FinishedAt, thread ID)──► cli.CodexRateLimitReader.Read ──► model.CodexRateLimitEvidence
     │                                                                        │
     ▼                                                                        ▼
AgentInvocationResult.RateLimits ──► step_end / agent_call_end data["codex_rate_limits"]
                                                                              │
Collector.processTerminal ──► StepRecord.CodexRateLimits ──► refreshAggregatesLocked ──► Artifact.CodexRateLimits (rollup)
                                                                              ▲
IncorporateValidator ──► reader (injected) ──► Artifact.RateLimitEnrichments ─┘ (joined in validatorStepProjection)
```

### 1. Model types (`internal/model/ratelimits.go`)

These are pure data types with JSON tags. They are shared by `cli`, `exec` and `metrics`.

```go
type CodexRateLimitEvidence struct {
    Status           string              `json:"status"`            // "captured" | "unavailable"
    Reason           string              `json:"reason,omitempty"`  // when unavailable
    Source           string              `json:"source"`            // "codex:session-log"
    AttemptStartedAt string              `json:"attempt_started_at"`// RFC3339Nano, Runner/producer clock
    AttemptEndedAt   string              `json:"attempt_ended_at"`
    Start            *RateLimitSnapshot  `json:"start"`             // nil = no qualifying baseline
    StartProvenance  string              `json:"start_provenance,omitempty"` // "same-thread" | "cross-thread"
    BaselineGapMS    *int64              `json:"baseline_gap_ms,omitempty"`  // cross-thread or stale same-thread
    AccountScope     string              `json:"account_scope,omitempty"`    // opaque run-local scope or "unverified"
    End              *RateLimitSnapshot  `json:"end"`
    Deltas           []RateLimitDelta    `json:"deltas"`            // one per role: primary, secondary
}
type RateLimitSnapshot struct {
    ObservedAt string           `json:"observed_at"`
    LimitID    *string          `json:"limit_id"`
    PlanType   *string          `json:"plan_type"`
    Primary    RateLimitWindow  `json:"primary"`
    Secondary  RateLimitWindow  `json:"secondary"`
}
type RateLimitWindow struct {
    Reported      bool     `json:"reported"`
    UsedPercent   *float64 `json:"used_percent"`
    WindowMinutes *int64   `json:"window_minutes"`
    ResetsAt      *int64   `json:"resets_at"` // unix seconds, as reported
}
type RateLimitDelta struct {
    Window       string   `json:"window"`        // "primary" | "secondary"
    Availability string   `json:"availability"`  // "available" | "unavailable"
    PercentagePoints *float64 `json:"percentage_points"`
    Reason       string   `json:"reason,omitempty"`
    Precision    string   `json:"precision,omitempty"`   // "approximate" when available
    Limitations  []string `json:"limitations,omitempty"` // account-wide, coarse-precision, unobserved-gap
}
```

Evidence is `captured` whenever an end snapshot exists, even if every delta is unavailable. Otherwise
it is `unavailable` with one of these reasons: `session-unidentified`, `session-log-unavailable`,
`no-snapshots`, `unparseable`, `stale-enrichment` or `excluded-measurement`. Rollup-only reason: `account-unverified`.
Reason and limitation names are exported constants.

### 2. Reader (`internal/cli/codex_ratelimits.go`)

```go
type CodexRateLimitRequest struct {
    ThreadID            string
    RunID               string        // salt for the opaque account scope
    StartedAt, EndedAt  time.Time
    EndTolerance        time.Duration // 0 native, 2s nested
    Now                 func() time.Time
}
type CodexRateLimitReader struct{ Home string } // resolved source Codex home
func NewCodexRateLimitReader() CodexRateLimitReader // $CODEX_HOME or ~/.codex (shared helper with completion_plugin.go)
func (r CodexRateLimitReader) Read(req CodexRateLimitRequest) model.CodexRateLimitEvidence
```

It works in this order:

1. **Locate.** An empty `ThreadID` gives `session-unidentified`. Otherwise glob
   `sessions/*/*/*/rollout-*-<ThreadID>.jsonl`. The thread ID is checked against a UUID/safe-ID
   pattern before it is used in the glob. No match or an open error gives
   `session-log-unavailable`.
2. **Scan incrementally, streaming.** Read at most 1 MiB per JSONL line and discard the rest of an
   oversized record. Cache parsed snapshots and the last byte offset for up to 128 session logs in
   the Runner process, reading only appended records on later attempts. Any line that does not contain
   `"token_count"` or `"session_meta"` is skipped before JSON decoding.
   - Remember `session_meta.payload.creator_account_id`, in memory only.
   - For each `token_count` with a non-null `rate_limits`, parse a snapshot. A malformed line or
     block is skipped and counted.
   - Snapshots with `timestamp < StartedAt` update `lastPreLaunch` (the same-thread baseline).
     Snapshots with `StartedAt <= timestamp <= EndedAt+EndTolerance` update `end`.
3. **Outcome of the scan.**
   - No in-attempt snapshot: if any line was malformed and no snapshot parsed, the reason is
     `unparseable`; otherwise it is `no-snapshots`.
   - The end snapshot is the last in-attempt snapshot.
4. **Choose a baseline.**
   - `lastPreLaunch` found gives `same-thread`. If it is more than 10 minutes old, record the gap in
     `baseline_gap_ms` and add `unobserved-gap` to available deltas.
   - Otherwise search for a cross-thread baseline, described below.
   - With neither, `Start` is nil.
5. **Per-window deltas** (`computeDeltas(start, end, provenance)`), evaluated in this order:
   1. Either endpoint does not report the window: `window-not-reported`.
   2. No start: `no-baseline`.
   3. `limit_id`, `window_minutes` or `resets_at` differ: `window-reset` for a same-thread baseline,
      `no-baseline` for a cross-thread baseline (the spec's "candidate in a different window" rule).
   4. `end - start < 0`: `inconsistent`.
   5. Otherwise the delta is available and `approximate`, with limitations `account-wide` and
      `coarse-precision`, plus `unobserved-gap` when the baseline is cross-thread or a same-thread
      snapshot more than 10 minutes old.

**Cross-thread baseline search.**
- **Freshness bound:** a constant `crossThreadBaselineFreshness = 10 * time.Minute`.
- **Candidate files:** only `sessions/YYYY/MM/DD` directories for the local dates spanning
  `[StartedAt-10m, StartedAt]` (Codex names directories and files in local time). Within those, only
  files whose mtime is at or after `StartedAt-10m`, excluding the attempt's own thread.
- **Account match:** a candidate qualifies only if its `session_meta.creator_account_id` is non-empty
  and equals the attempt log's. If either side lacks the ID, the candidate is rejected.
- **Choice:** from each qualifying file, take the latest `rate_limits` snapshot with timestamp in
  `[StartedAt-10m, StartedAt)` whose `limit_id` and `plan_type` equal the end snapshot's. Across
  files, pick the most recent. Window equality is not a selection filter. It is checked per window
  in step 5, so a candidate from an earlier window yields `no-baseline` rather than being skipped in
  favour of an even older snapshot, which could only belong to an equal or earlier window anyway.
- **Gap:** `BaselineGapMS = StartedAt - candidate.ObservedAt`.

**Account scope.** When the attempt log's `session_meta.creator_account_id` is present, the reader
sets `AccountScope = "acct-" + hex(sha256(RunID + "\x00" + creator_account_id))[:16]`; when it is
absent, `AccountScope = "unverified"`. The run ID salt keeps the scope stable across resume within one
run, which always has the same run ID. It also makes the scope uncorrelatable across runs and
impractical to reverse. Raw account IDs are compared and then discarded. They never reach evidence,
audit or artifact. All filesystem and parse errors are absorbed into reasons and never returned as Go
errors.

### 3. Native capture (`internal/exec`)

- `AgentInvocation` gets `RateLimitReader func(cli.CodexRateLimitRequest) model.CodexRateLimitEvidence`.
  When it is nil and the CLI is `codex`, the default reader is used.
- `AgentInvocationResult` gets `RateLimits *model.CodexRateLimitEvidence`.
- In `InvokeAgent`, after `DiscoveredSessionID`, when `input.CLI == "codex"` and `launched`, call the
  reader with:
  - thread ID = `DiscoveredSessionID`, falling back to `input.SessionID` for resumed or preset threads;
  - `RunID` = the run ID (the base of `ctx.SessionDir`, as already passed to `BuildArgsInput.RunID`);
    it is threaded into `AgentInvocation` alongside the reader;
  - `StartedAt` = `result.StartedAt`, `EndedAt` = `now()` after the process exits;
  - tolerance 0.
- The result records `attempt_started_at` and `attempt_ended_at`. An unlaunched attempt gets no
  evidence, because it is not a measured CLI attempt.
- `emitAgentEnd` (`agent.go`) and the agent-call terminal data (`agent_call.go`) set
  `data["codex_rate_limits"]` when the evidence is non-nil. Interactive and autonomous-interactive
  steps use the same path. Their thread ID comes from the existing interactive discovery, which
  gives `session-unidentified` when it is empty.
- `metrics.DataCodexRateLimits = "codex_rate_limits"`. `processTerminal` copies it into
  `StepRecord.CodexRateLimits *model.CodexRateLimitEvidence` (`json:"codex_rate_limits,omitempty"`).
  Because it lives on the persisted record, it survives resume unchanged.
- Fix along the way: `discoverCodexInteractiveSession` uses the shared Codex-home resolver instead of
  hard-coded `~/.codex`, so discovery and reading look in the same place.

### 4. Nested Validator enrichment (`internal/metrics`)

- Artifact field: `RateLimitEnrichments []RateLimitEnrichment` (`json:"rate_limit_enrichments,omitempty"`):

  ```go
  type RateLimitEnrichment struct {
      HeadKey   string `json:"head_key"`   // store + "/model_attempt/" + record ID (== MeasurementHead.Key)
      AttemptID string `json:"attempt_id"`
      StartedAt string `json:"started_at"` // lifecycle bounds captured against
      EndedAt   string `json:"ended_at"`
      Evidence  model.CodexRateLimitEvidence `json:"evidence"`
  }
  ```
- `Collector.SetCodexRateLimitReader(fn)` injects the reader at construction in the runner wiring,
  which avoids a `metrics → cli` import. The collector supplies its own `RunID` in each request.
- In `IncorporateValidator`, after heads are upserted and before `refreshAggregatesLocked`, each
  `model_attempt` head that is Codex (`payload.adapter == "codex"`, or requested/resolved CLI `codex`)
  and has both lifecycle timestamps is checked. When its enrichment is missing, or its `AttemptID`,
  `StartedAt` or `EndedAt` differ from the head, and a reader is installed, the enrichment is
  recaptured eagerly:
  - thread ID = the `provider_native_usage` row named `provider_session_id`, or `session-unidentified`
    when absent;
  - bounds = lifecycle, end tolerance 2s.

  Recapture always replaces the enrichment with the reader's result bound to the new head's
  identity and bounds, even when that result is unavailable (`session-log-unavailable`,
  `unparseable`, `no-snapshots`), so old values are never shown for a revised head. It never edits
  `MeasurementHead.Record`. When no reader is installed, the enrichment is left untouched.
- `validatorStepProjection` joins by `head.Key`:
  - enrichment bounds do not match the head (possible only when no reader was installed at import):
    a copy with `stale-enrichment`;
  - head `conflicting`: a copy with `excluded-measurement`. The projection still exists today, so it
    counts in coverage;
  - otherwise the enrichment's evidence.

  Heads with status `unsupported_current_scope` are skipped by `refreshMeasurementsLocked` before
  projection, as they are today. They therefore carry no evidence and are not counted, and that
  existing exclusion is left unchanged.

  Codex heads with no enrichment at all get `session-unidentified`.
- Rehydrate keeps `RateLimitEnrichments` unchanged, so evidence survives resume.

### 5. Run rollup (`refreshAggregatesLocked`)

`Artifact.CodexRateLimits *CodexRateLimitRollup` (`json:"codex_rate_limits,omitempty"`) is recomputed
from `Steps` on every refresh and is nil when no step carries evidence.

```go
type CodexRateLimitRollup struct {
    MeasuredAttempts int                     `json:"measured_attempts"`
    Coverage         []RateLimitRoleCoverage `json:"coverage"` // one per role: primary, secondary
    Windows          []RateLimitWindowGroup  `json:"windows"`
}
type RateLimitRoleCoverage struct {
    Window    string `json:"window"`            // primary | secondary
    Measured  int    `json:"measured_attempts"`
    WithDelta int    `json:"attempts_with_delta"`
    Coverage  string `json:"coverage"`          // complete | partial | none
}
type RateLimitWindowGroup struct {
    AccountScope  string   `json:"account_scope"`   // opaque scope or "unverified"
    LimitID       *string  `json:"limit_id"`
    Window        string   `json:"window"`          // primary | secondary
    WindowMinutes int64    `json:"window_minutes"`
    ResetsAt      int64    `json:"resets_at"`
    DeltaSum      RateLimitDelta `json:"delta_sum"` // unavailable account-unverified for "unverified"
    Contributing  int      `json:"contributing_attempts"`
    Span          RateLimitDelta `json:"run_span"`  // no-baseline | account-unverified when unavailable
    Limitations   []string `json:"limitations,omitempty"` // + overlapping-attempts
}
```

- **Measured attempts:** every step with non-nil evidence, counted once. This includes
  `excluded-measurement` projections and excludes unprojected unsupported heads.
- **Group key:** `(account_scope, limit_id, role, window_minutes, resets_at)`, taken from each
  captured attempt's end snapshot. A per-attempt delta is only ever available when start and end share
  that window, so each delta belongs to exactly one group. No total is computed across groups, which
  means none across accounts or resets.
- **Sum:** adds only the available deltas in the group. For the `unverified` scope, the sum and span
  are unavailable with reason `account-unverified`, and only `Contributing` is reported. When measured
  attempts overlap, the sum is unavailable with reason `overlapping-attempts`; the contributing count
  remains visible.
- **Run span:**
  - start = the earliest-observed start snapshot for this window among the group's attempts;
  - end = the latest-observed end snapshot;
  - unavailable with `no-baseline` when no attempt in the group has a start.

  Because start and end share the group's reset window by construction, a span never crosses a reset
  or an account.
- **Coverage:** computed per role across the whole run, as attempts with an available delta for the
  role over measured attempts.
- **Overlap:** computed within each group from `attempt_started_at`/`attempt_ended_at` intervals, by
  sorting and checking whether any interval starts before the previous maximum end. An overlapping
  group's sum is unavailable because adding its account-wide deltas would count shared usage twice.
- The schema version stays 4. Every added field is optional and `omitempty`.

## Decisions

1. **Bound the attempt by timestamps, not a pre-launch byte offset.** `StartedAt` is taken before
   spawn and `FinishedAt` after exit on the same clock that Codex stamps its events with. This is one
   rule for native and nested attempts, and it needs no pre-launch file lookup. The alternative, a
   byte-offset marker, needs the log path before launch, which does not exist yet for fresh threads,
   and it cannot serve nested attempts. This supersedes proposal decision 3.
2. **The cross-thread baseline requires the same `creator_account_id` in `session_meta`.** This is the
   account evidence proposal review PR-2 asked for. It is used only for comparison and is never
   stored, which is consistent with the measurement contract's prohibition on account evidence.
3. **Freshness bound: 10 minutes.** In sequential workflows the previous Codex attempt normally ended
   seconds earlier, so 10 minutes accepts ordinary step gaps while capping how much unobserved
   consumption can enter a delta. The actual gap is always recorded.
4. **Inject the reader into the collector.** This keeps the `metrics` package free of `cli`. For native
   attempts, `exec` calls the reader directly.
5. **Recapture nested enrichment eagerly at revision import.** Import already holds the collector lock
   and has the new bounds. `stale-enrichment` appears only when recapture is impossible (no reader,
   for example in offline tools that rehydrate).
6. **Add the reason `excluded-measurement`** for conflicting heads (see decision 10 for unsupported heads), so their exclusion
   from coverage is visible per attempt.
7. **Accept that nested Validator attempts are `session-unidentified` until Validator emits
   `provider_session_id` for Codex.** Using the allowlisted field needs no contract change. Heuristic
   log matching by cwd and time would misattribute parallel reviews. The issue explicitly scopes
   Validator reviews as "once those are measured".
8. **Partition the rollup by an opaque account scope and reset window, and report no cross-group total.**
   The scope is a run-salted SHA-256 prefix of `creator_account_id`. It survives resume and never
   exposes the raw ID. Otherwise, observed logs share the generic `limit_id` `codex`, so an account
   switch or a reset would be summed or subtracted as if it were one quota. Alternatives: a run-local
   ordinal mapping, which would have to persist the raw ID to stay stable across resume; or marking
   every cross-account run unavailable, which loses valid per-account data.
9. **Recapture failure stores the reader's own reason. `stale-enrichment` means only that no reader
   was available.** The underlying reason is more actionable, and old values are never shown either
   way.
10. **Unsupported-scope heads stay unprojected and uncounted. Conflicting heads count as
    `excluded-measurement`.** This preserves the existing `refreshMeasurementsLocked` exclusion
    rather than adding projections for heads Runner already treats as out of scope.

## Risks / Trade-offs

- **Undocumented log format.** Codex's session-log format is not a published contract. The reader is
  tolerant: it ignores unknown fields, skips malformed lines, and turns an unrecognised shape into
  `unparseable`. Fixture tests pin the current shape.
- **Fresh threads often have no baseline.** Their deltas can be unavailable, for example for the first
  Codex step in a quiet period. This is intended: no false zeros.
- **Cost of scanning logs.** The cross-thread search reads a handful of recent files. The date-directory
  and mtime prefilter bounds this; each line gets a substring check before JSON decoding.
- **Clock skew between Validator and Runner.** Both run on the same host. The 2s end tolerance covers
  flush ordering.
- **Parallel steps.** Account-wide deltas can cover the same usage. Every delta carries the
  `account-wide` limitation; the rollup leaves an overlapping group's sum unavailable with reason
  `overlapping-attempts`.

## Migration Plan

No migration is needed. Existing v4 artifacts rehydrate with no evidence: pre-feature records carry
none and are excluded from the rollup. Rollback means ignoring the unknown optional fields.

## Testing

- **Reader unit tests** (fixtures under `internal/cli/testdata/ratelimits/`):
  - the issue's fixture: a resumed thread with pre-launch 40% and in-attempt 42% then 43% gives start
    40, end 43, delta 3, `same-thread`;
  - a fresh single-snapshot thread with no candidates gives `no-baseline`;
  - a cross-thread match, account mismatch, stale candidate, and candidate in a different window;
  - window reset, secondary null, decrease (`inconsistent`), observed zero, a malformed line,
    `unparseable` and missing log.

  Home is a temp dir.
- **`exec` tests:** a stub reader shows `InvokeAgent`/`emitAgentEnd` and the agent-call data carry
  the evidence for Codex, omit it for Claude and unlaunched attempts, and leave outcome, usage and cost
  unchanged when evidence is unavailable.
- **Metrics tests:**
  - `processTerminal` persists the evidence, and it survives rehydrate;
  - rollup cases: sum, partial per-role coverage, separate groups across a reset, separate groups
    across an account switch, `unverified` scope giving `account-unverified`, overlap, none;
  - the account scope is stable for the same run ID, differs across run IDs, and never equals or
    contains the raw ID;
  - enrichment cases:
    - survives refresh;
    - producer bytes unchanged;
    - eager recapture on revision;
    - a recapture with a missing log gives `session-log-unavailable` and does not show old values;
    - no reader plus a changed-bounds revision gives `stale-enrichment`;
    - `excluded-measurement` on conflict;
    - an unsupported-scope head is neither projected nor counted.
- **Real-run acceptance:** a workflow with at least two sequential Codex steps sharing one thread, on
  an account that reports rate limits, shows per-step `codex_rate_limits`. The resumed step has an
  available `same-thread` delta with end ≥ start. See test-plan E2E-001.

## Open Questions

None blocking. Follow-up outside this repo: Agent Validator should export `provider_session_id` (the
Codex thread ID) for Codex attempts.
