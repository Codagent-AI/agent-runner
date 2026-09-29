## Why

On a ChatGPT subscription, Codex is limited by a rolling usage limit, not by dollars. `run-metrics.json`
already records Codex token categories and an API-equivalent cost for each attempt. It does not record
how much of the usage limit an attempt used. A user who hits the weekly limit therefore can't tell which
workflow steps used it up. Token counts are only a rough proxy: the limit depends on the model, the
cache mix and the plan, and Codex does not publish the conversion.

Codex already reports the account's limit state after each model request. That state is the only direct
signal of limit consumption available to the Runner, and it is currently thrown away. Codex is the
default review and implementation agent in several built-in workflows, so this gap affects most real
runs.

**Verdict: go, with caveats.** The change is small, it only adds data, and it reuses the existing
metrics pipeline. The caveats set how much the data can be trusted, so the artifact must record them
and not hide them:

- `used_percent` is an account-wide value. Any other Codex session on the same account during an
  attempt, including parallel steps in the same run, is included in that attempt's delta.
- Codex reports the value as a coarse percentage. A short attempt can show a delta of 0 even though it
  used some of the limit.
- A window can reset during an attempt. The start and end values then belong to different windows and
  cannot be subtracted.
- A fresh thread has no earlier snapshot of its own. Its start value must come from another local
  session, which leaves an unobserved gap before launch; when no trustworthy baseline exists, the delta
  is unavailable.

The issue asks us to record these limitations and not drop the data. We will do that.

## What Changes

- For each measured Codex attempt, record the rate-limit state Codex reported at the start and at the
  end of the attempt, plus the delta. Each state holds the primary and secondary windows with
  `used_percent`, window length (`window_minutes`) and reset time (`resets_at`), and the limit/plan
  identity when Codex reports it.
- Compute a delta for each window only when the start and end values belong to the same window. After a
  reset, or when an endpoint is missing, the delta is explicitly unavailable and carries a reason. It is
  never shown as zero. Every delta is marked approximate and states the concurrency limitation.
- Roll the values up per run in `run-metrics.json`, grouped by an opaque, run-local account scope and
  by quota window (limit, window role, length and reset time). For each group, report the sum of
  per-attempt deltas and the run-span change. No total ever combines different accounts or reset
  windows. For each window role, report coverage: how many Codex attempts had a usable delta out of
  how many were measured.
- Apply this to every Codex attempt the Runner measures:
  - headless Codex agent steps;
  - interactive Codex agent steps whose session is discovered;
  - Codex agent calls (`call_agent`), recorded on the called agent's own call record, kept separate from
    the parent step's evidence;
  - Codex model attempts in declared Agent Validator nested records, when the accepted record identifies
    a Codex provider session that resolves to a local Codex session log.
  Any other attempt records explicit unavailable rate-limit evidence.
- Add a test fixture: a Codex session log that contains `token_count` events with `rate_limits`. It is
  used to verify the recorded start, end and delta.

Nothing here is a breaking change. The new fields are optional additions to schema v4 records and the
run aggregate, and existing fields keep their meaning.

## Capabilities

### New Capabilities
- `codex-rate-limit-capture`: how the Runner reads Codex rate-limit evidence for an attempt. Covers the
  source, how the attempt's events are bounded, the start/end selection and its provenance
  (same-thread or bounded cross-thread baseline, end-only observations), per-window delta rules (same
  window, reset, missing endpoint or baseline), precision, and the concurrency and unobserved-gap
  limitations. Covers agent steps, agent calls and nested Validator attempts.

### Modified Capabilities
- `run-metrics-artifact`: per-attempt records (including agent-call records) and the run-level
  aggregate gain optional Codex rate-limit start/end/delta evidence and a run rollup with coverage.
  Nested Validator evidence lives in a durable Runner-owned enrichment, kept apart from accepted
  producer records, that survives projection refresh. Unavailable states are explicit and never zero.

## Technical Approach

**Source of evidence.** `codex exec --json` stdout, which the Runner parses today, does not include
`rate_limits`. This was checked against codex-cli 0.157.1: `turn.completed` carries only `usage`.
Codex writes the data to the thread's session log, `$CODEX_HOME/sessions/YYYY/MM/DD/rollout-*-<thread_id>.jsonl`.
There, each `event_msg` with payload type `token_count` carries a `rate_limits` block:
`primary`/`secondary` with `used_percent`, `window_minutes` and `resets_at`, plus `limit_id` and
`plan_type`. The Runner already scans this directory to discover interactive sessions and already knows
the headless thread ID from `thread.started`. The Codex adapter will get a rollout reader that finds the
thread's log after the attempt and extracts the rate-limit snapshots that belong to that attempt. The
reader must use the effective `CODEX_HOME`. When the adapter has swapped in a private integration home,
session state stays linked to the real home, and the design must confirm which directory holds the log.

**Bounding the attempt.** A resumed thread appends to the same log. So before launch the Runner records
the log's size, or an equivalent marker, and after exit it attributes only the events appended after
that point. This is the same "attempt-only" principle that cumulative usage attribution already follows.

**Start and end values.** The end value is the last snapshot the attempt produced. The start value must
be a snapshot taken before launch. The first `token_count` inside an attempt already includes the
attempt's first model request, so it is never used as a start. Only two kinds of snapshot qualify as a
start baseline, and the record stores which one was used:

- **Same thread:** for resumed threads, the thread's last snapshot before launch.
- **Cross-thread:** the most recent pre-launch snapshot from another local session log. Observed logs
  use the generic `limit_id` `codex` and carry no account ID, so this baseline is accepted only when
  all of these hold:
  - `limit_id`, `plan_type`, `window_minutes` and `resets_at` match the attempt's end value (an
    identical `resets_at` is the strongest available evidence of the same account window);
  - the snapshot is newer than a freshness bound, which the design sets.

  The time between that snapshot and launch is unobserved: consumption in that gap is counted in the
  delta. The record therefore stores the size of the gap as part of the delta's stated limitation.

When neither baseline qualifies, the in-attempt snapshots are still recorded as end-only observations
and the delta is unavailable with a reason (`no-baseline`), rather than a misleading zero.

**Placement.** For native agent steps and agent calls, the evidence goes on the existing Runner-owned
attempt record as an optional, additive block next to usage and cost. Nested Validator step records are
different: they are disposable projections that the collector rebuilds from the measurement heads on
every import, revision, resume or refresh. Evidence written onto them would be lost. So Validator
rate-limit evidence goes in a separate, durable, Runner-owned enrichment. It is keyed by the producer
store and the model-attempt identity, and stored apart from the accepted producer record, which is never
rewritten. The projection step and the run rollup are then rebuilt from the enrichment together with the
current authoritative head. The design specifies what happens to an enrichment when its head is
revised or conflicts; by default, an enrichment whose attempt identity or lifecycle bounds no longer
match the head is marked stale and shown as unavailable, never silently reapplied. The enrichment reads
the `provider_session_id` the producer already exports and bounds the events with the record's
lifecycle timestamps, so the Validator does not change. Because the change only adds data, the design
keeps the current schema version rather than bumping it; the spec requires a bump only for
backward-incompatible changes.

**Failure isolation.** A missing log, an unreadable log, or no snapshots produces an unavailable state
with a reason. It never changes the step outcome, the usage record or the cost.

## Out of Scope

- Converting tokens or cost into usage-limit percentage, or estimating consumption when Codex reports no
  snapshot.
- Separating this run's consumption from concurrent Codex sessions on the same account. This is recorded
  as a limitation only.
- Querying Codex's account endpoints or app-server for live limit state.
- Changes to Agent Validator or its metrics contract.
- Showing rate-limit data in the TUI, the run-complete screen, `list`/`view` output or the audit report.
  This change only makes the data available in `run-metrics.json`.
- Budgeting, throttling, or pausing runs based on limit state.
- Rate-limit evidence for non-Codex CLIs.

## Impact

- **Code:** `internal/cli/codex.go` (rollout location, pre-launch marker, snapshot extraction);
  `internal/cli/usage.go` or a small sibling type for the extracted evidence; `internal/metrics/`
  (attempt record field, run rollup, durable Validator enrichment rebuilt into projections in
  `refreshMeasurementsLocked`); the executor paths that record the pre-launch marker for Codex agent
  steps and agent calls in `internal/exec/` (including `agent_call.go`); new fixtures under
  `internal/cli/testdata/`.
- **Artifact:** `run-metrics.json` gains optional fields. Existing consumers such as Agent Evals are
  unaffected, and new consumers can opt in.
- **Specs:** new `codex-rate-limit-capture`; modified `run-metrics-artifact`.
- **Dependencies:** relies on the on-disk format of Codex's session log, which is not a published
  contract. If the format drifts, parsing must degrade to an explicit unavailable state rather than an
  error.
- **Users:** Codex subscription users can see how much of the usage limit each step used, with its
  caveats.
