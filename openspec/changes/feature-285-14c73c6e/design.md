## Context

Codex steps record full token usage but a null `estimated_api_cost_usd`. Today's pipeline works like
this:

- **Adapter extraction.** `internal/cli/codex.go` `ExtractUsage` reads the last `turn.completed` event's
  session-cumulative usage into `UsageRecord.RawCumulative`, with `CLI: "codex"`,
  `Provider: "openai"`, the model from the stream, and `Source: "codex:turn.completed"`.
- **Rollout read.** `internal/cli/codex_context.go` `ExtractUsageWithContext` also locates the Codex
  session rollout (`$CODEX_HOME/sessions/**/rollout-*-<thread>.jsonl`) and reads every line timestamped
  at or after `UsageContext.StartedAt` to build the context gauge. It decodes only `input_tokens` from
  each `token_usage_record`.
- **Collector.** `internal/metrics/collector.go` `Collector.Process` receives the audit event in
  process. Before the event is logged it attributes cumulative counters to the invocation (`attribute`,
  or `attributeTurns` for external-user steps) and writes the attributed usage and cost back into
  `event.Data`. It also appends a `StepRecord` to `run-metrics.json`.
  `totalsForRecords` sums non-null costs and computes cost coverage.
- **Native measurements.** `internal/metrics/native_measurements.go` `collectReportedCost` creates
  `reported-cost` evidence only for Claude and OpenCode sources. Any other non-null cost gets
  `legacy_cost_scope_unavailable`.
- **Run views.** `internal/runview` reads `estimated_api_cost_usd` from `step_end` event data
  (`audit.go`). It rolls costs up in `summary.go` (`summaryMetrics`, `formatSummaryCost`,
  `formatCoveredCost`) and shows them in selected detail.

Investigation of 386 recent local rollouts (12,194 `token_usage_record` lines, Codex as of 2026-10)
found the following:

- Each record carries one response's full usage: `input_tokens`, `cached_input_tokens`,
  `cache_write_input_tokens`, `output_tokens`, `reasoning_output_tokens`, and `total_tokens`. It also
  carries `turn_token_usage` and `thread_token_usage` running totals, plus `thread_id`, `turn_id`, and
  `response_id`.
- In every record, `input_tokens ≥ cached_input_tokens + cache_write_input_tokens`.
- The models in use were gpt-6.1-sol, gpt-6-sol, gpt-6-astra, gpt-6-luna, and gpt-5.6-sol.
- `turn_context` carried no service tier.

OpenAI's prompt-caching guide computes ordinary input as `input − cached − cache_write`. A cache write
is billed at its own rate, not as an extra fee. The pricing page gives Standard short/long rates with a
272K-input threshold for all five models.

This resolves both `deferred-to-design` markers. Per-request records carry everything the policy
needs, so every attempt is priced request by request. Cache writes are a subset of uncached input. The
cost-estimation spec was updated to match: one per-request path, the model table extended to the five
models in use, and the deferred markers removed.

## Goals / Non-Goals

**Goals:**
- Give native Codex agent steps, agent calls, and external-user turns an exact-arithmetic estimate
  under the request-level Standard-tier policy, gated by the full-attempt eligibility rule.
- Label every cost with its basis end to end: audit `step_end` and `run_end`, `run-metrics.json` steps,
  totals and session rollups (including producer-reported nested costs), native measurements,
  development-audit observations and sheet rows, and the TUI.
- Keep `run-metrics.json` at schema v4 with additive fields only.

**Non-Goals:**
- Pricing nested Validator attempts, other CLIs, or non-Standard tiers.
- Re-pricing historical artifacts.
- Persisting per-request usage in the audit log or `run-metrics.json`.

## Approach

```text
cli/codex_context.go           metrics/collector.go                       metrics/native_measurements.go
rollout read (one pass) ──►  attribute() / attributeTurns()  ──►  StepRecord + event.Data  ──►  estimated-cost evidence
  ├─ ContextMeasurement           │                                   (cost, basis, version,
  └─ RequestEvidence (transient)  └─ estimateCost() ── pricing.Table   reason)  ──► runview (audit events)
```

### 1. `internal/pricing` (new package)

- `prices.json` is embedded with `//go:embed`. Top-level fields: `version` (for example
  `"2026-10-20.1"`), `policy` (`"openai-standard-request-level"`), and `entries[]`. Each entry has
  `provider`, `model`, `aliases[]`, `effective_date`, `source` (URL), `tier: "standard"`,
  `threshold_input_tokens`, and `short` and `long` rate sets. A rate set has `input`, `cached_input`,
  `cache_write`, and `output`, each in USD per million tokens.
- `Load()` parses the file once (`sync.OnceValues`) and validates it:
  - every entry has a non-empty source and date;
  - every rate is positive, and each long rate is at least the matching short rate;
  - model names and aliases are unique;
  - the threshold is positive.
  Any validation failure is a programming error, caught by a package test that loads the real file.
- `Table.Lookup(provider, model string) (Entry, bool)` does an exact match on `model` or an alias. There
  is no normalization beyond the exact string.
- `Estimate(entry Entry, requests []model.RequestUsage) Result`:
  - For each request, the band is short if `Input ≤ threshold` and long otherwise.
  - Component amounts are computed in integer arithmetic:
    `tokens × rate_micro_usd_per_million`, where rates are converted once to integer micro-USD per
    million tokens ($0.125/M becomes 125,000). The sum is divided by 10¹² only at the end to produce
    the float64 dollar amount. This keeps scenario values such as 0.68 and 0.115 exact, with no
    float-accumulation drift.
  - A request whose `CacheWrite` is nil (field absent) has cache-write input of 0 and sets limitation
    `cache_writes_unobserved`.
  - A request with `Input < Cached + CacheWrite` is invalid. The whole result is reported as
    `request-evidence-mismatch`.
  - `Result` holds `{AmountUSD, Requests, LongContextRequests, Limitations}`.
- The package imports only `internal/model`, so it can be used from the metrics layer without cycles.

### 2. Model: transient request evidence and cost-basis types (`internal/model/usage.go`)

```go
type RequestUsage struct {
    Input, Cached, Output int64
    CacheWrite *int64 // nil when the source omitted the field
}
type RequestEvidence struct {
    Source   string            // "codex:rollout"
    Status   UsageStatus       // collected | unavailable
    Reason   UnavailableReason // e.g. codex-rollout-not-found, codex-rollout-unsupported, parse-failure
    Requests []RequestUsage
}
// On UsageRecord:
RequestEvidence *RequestEvidence `json:"-"`
```

The evidence is tagged `json:"-"`, so it never reaches `audit.log` or `run-metrics.json`. The collector
receives `model.UsageRecord` by value in `Process`, which `cloneUsage` copies, and that in-process
value is the only path the estimate needs. This avoids per-request bloat: a 3.8M-token step can contain
hundreds of requests. `cloneUsage` must deep-copy the slice.

Cost-basis vocabulary:
- `CostBasis` (`reported` | `estimated`);
- new `UnavailableReason` constants for the eight spec reasons (`usage-unavailable`,
  `usage-incomplete`, `required-category-missing`, `turn-unattributed`, `model-unknown`,
  `model-unpriced`, `context-band-unknown`, `request-evidence-mismatch`);
- `RunTotals` gains `` CostBasis *string `json:"cost_basis"` `` (explicit null when coverage is `none`)
  and `` CostByBasis map[string]float64 `json:"cost_by_basis,omitempty"` ``.

### 3. Codex adapter: one rollout pass, two outputs (`internal/cli/codex_context.go`)

- Extend `codexRolloutAccumulator.apply` for `token_usage_record`. For in-window lines on the step's
  thread, it decodes the full `usage` object, keeping `CacheWrite` nil when the key is absent, and
  appends a `RequestUsage`.
- The context gauge keeps its own filtering: it skips compacted responses and unclassified trailing
  lines. Request evidence includes every in-window record, compaction responses included, because
  those requests are billed.
- `codexContext` returns `(ContextMeasurement, RequestEvidence)`, and `ExtractUsageWithContext` sets
  both.
- Failure mapping reuses the context reasons:
  - no thread, or rollout not found: `Status unavailable, Reason codex-rollout-not-found`;
  - parse failure: `parse-failure`;
  - a rollout with no `token_usage_record` lines in the window: `codex-rollout-unsupported`.
  The collector maps all three to the spec reason `context-band-unknown`.
- A malformed `usage` payload marks the evidence `parse-failure`. It never silently drops a request,
  because that would break reconciliation in an unpredictable direction.

### 4. Collector: estimate after attribution (`internal/metrics/collector.go`)

In `Process`, after `attribute` and `attributeCost`, the cost is resolved as follows:

```text
if CLI-reported cost present (event cost non-nil after attributeCost)  → basis=reported
else if usage.CLI == "codex" && event is not EventNestedAgentEnd       → c.estimateCost(identity, usage)
else                                                                    → null, no basis, no reason
```

`estimateCost(identity, attributed UsageRecord) costOutcome` runs the spec's checks in the spec's
order. Each failing check returns a null cost with the reason shown.

**Pricing completeness is not `UsageRecord.Completeness`.** The Codex adapter marks a record `partial`
whenever any of its four known fields is absent, reasoning included (`tokenCountsFromObject` in
`internal/cli/usage.go`). It sets no `Reason` in that case. Attribution-level limitations, by
contrast, always set `Reason`. For example, `attributeTurns` copies a failing turn's reason. The
collector therefore uses a helper, `attributionComplete(u) := u.Completeness != partial || u.Reason == ""`,
and checks category presence separately. An adapter test pins the invariant that Codex's
field-absence `partial` never carries a `Reason`.

1. Status is not `collected`: `usage-unavailable`.
2. External-user steps only, per turn: see below (`turn-unattributed`).
3. `!attributionComplete(usage)`: `usage-incomplete`.
4. `Tokens` lacks `input`, `cached_input`, or `output`: `required-category-missing`. A missing
   `reasoning` is ignored, so a record that is partial only because reasoning is absent still prices.
5. Model resolution uses `usage.Identity.EffectiveModel`, falling back to `usage.Model`, which the
   adapter takes from telemetry and the collector's identity merge fills from the invocation. No model:
   `model-unknown`. Lookup miss: `model-unpriced`.
6. `RequestEvidence` is nil or not collected, or has zero requests: `context-band-unknown`.
7. Per-request sums of `Input`, `Cached`, and `Output` must equal the attributed `Tokens` exactly;
   otherwise `request-evidence-mismatch`. Request records are per invocation, not cumulative, so they
   compare directly with the attributed delta for both fresh and resumed sessions.
8. Otherwise, `pricing.Estimate` produces the amount and limitations.

External-user steps refactor `attributeTurns` to return the per-turn attributed records alongside the
aggregate. `estimateTurns` then runs `estimateCost` on each turn, each with its own model:

- An unattributed turn (status not collected after attribution, from a missing report, broken
  baseline, counter reset, or session switch) yields `turn-unattributed`. This is checked across all
  turns before any per-turn checks 3–7, so it takes precedence.
- Otherwise, each turn runs checks 3–7 on its own attributed record and model. Any ineligible turn
  yields that turn's reason.
- If all turns are eligible, the step's cost is the sum with the union of limitations.
- If some turns are ineligible, the step's cost is null with the first failing reason, in turn order.
  The sum of the eligible turns is kept as `partialSubtotal` for native evidence only.

Outputs are written both to `event.Data`, so `step_end` in `audit.log` and the run views see them, and
to `StepRecord`:

| Field | event.Data key | StepRecord JSON |
|---|---|---|
| cost | `estimated_api_cost_usd` | `estimated_api_cost_usd` |
| basis | `cost_basis` | `cost_basis,omitempty` |
| table version | `price_table_version` | `price_table_version,omitempty` |
| reason | `cost_unavailable_reason` | `cost_unavailable_reason,omitempty` |
| policy, limitations, request counts, partial subtotal | `cost_estimate` | `cost_estimate,omitempty` (`{policy, model, requests, long_context_requests, limitations, partial_subtotal_usd}`) |

`totalsForRecords` keeps its existing sum and coverage logic, so estimates count exactly like reported
costs. It also accumulates `CostByBasis[basis]` and derives `CostBasis`: one key gives that basis, two
keys give `mixed`, and none gives nil. A non-null cost with an empty basis is a bug. `totalsForRecords`
counts it under `reported` defensively, so subtotals always sum to the total, and a collector test
asserts that no record reaches totals without a basis. Session rollups use the same function, so they get the fields
automatically.

**Legacy normalization on load.** When an existing v4 artifact (or a migrated older one) is loaded, a
step with a non-null cost and an empty `CostBasis` is set to `reported`. Nothing else changes, and
Codex records are not re-priced. Totals are recomputed, as they already are on load. `SchemaVersion`
stays 4.

### 5. Native measurements (`internal/metrics/native_measurements.go`)

`collectReportedCost` branches on `step.CostBasis`:

- `reported`: the current behavior, unchanged.
- `estimated`: append `Cost{ID: "estimated-cost", Scope: "attempt", Coverage: "full", Overlap:
  "established", Source: "provider_usage", Currency: USD, Amount: {Availability: "available",
  Origin: "derived", Precision: "approximate", Derivation: "<policy>@<table version>"}}`. Add the
  estimate's limitations to `n.Limitations`.
- Null cost with a `cost_estimate.partial_subtotal_usd`: append the same `estimated-cost` with
  `Coverage: "partial"` and `Amount.Reason` set to the ineligibility reason. `CompleteUSD` already
  rejects non-full coverage, so the partial subtotal can never become a scalar cost.

Nested Validator projections are never estimated. `validatorStepProjection` keeps deriving
`EstimatedAPICostUSD` only from `CompleteUSD(p.Costs)`. When that yields a value, it also sets
`CostBasis = reported`, because the producer reported the cost. `refreshMeasurementsLocked` rebuilds
projections from producer heads on every refresh, so the basis is recomputed with the cost and is
never lost to refresh (load-time normalization does not cover these records). Producer evidence is not
modified.

### 6. Run views (`internal/runview`)

- `audit.go` reads `cost_basis` and `cost_unavailable_reason` into the step metrics struct (`tree.go`
  `CostUSD` gains siblings `CostBasis` and `CostReason`). Events without `cost_basis` but with a cost
  are treated as reported, which covers old audit logs.
- `summary.go`: `summaryMetrics` gains `costEstimated int`, which is summed through `merge`.
  `formatSummaryCost` prefixes `~` when `costEstimated > 0` and keeps the existing partial indicator
  independently. `formatCoveredCost` takes `totals.CostBasis` and prefixes `~` for `estimated` or
  `mixed`.
- Selected detail renders `~$0.98 (API-equivalent estimate)` for estimated cost. For an ineligible
  Codex step it renders `? (<reason>)`.
- The legend overlay adds a `~ estimated API-equivalent cost` entry.

The live view uses the same data path, so it needs no extra work.

### 7. Development-audit export (`internal/devaudit`)

- `CostEvidence` (`value_audit.go`) gains `` CostBasis *string `json:"cost_basis,omitempty"` ``. The leaf
  cost loop (`value_audit.go` around the `record.EstimatedAPICostUSD` sum) and `leafModelScalars`
  (`measurement_evidence.go`) collect the set of contributing records' `CostBasis`. When the scalar
  cost is known, the basis is `reported`, `estimated`, or `mixed`. When it is unknown, it is nil.
  Per-record bases remain in the detailed local evidence, which already carries the records.
- `sheets_delivery.go`: `stepValueHeaderV3 = stepValueHeaderV2 + ["cost_basis"]` and
  `sheetRowSchemaVersion = "step_value_v3"`. `validateHeader` accepts v3. For a v1 or v2 header with
  empty cells to its right, it writes the missing trailing cells (judge columns plus `cost_basis`, or
  just `cost_basis`), generalizing `upgradeHeader` to "write the trailing cells from
  `len(found)+1`". Rows append `cost_basis`, empty when nil. Pending reports assembled before this
  change decode with nil basis and write empty.
- Judge usage (`judge_usage.go`) does not pass through the collector and is never estimated. Its
  columns are unchanged.

### 8. Docs

Add a cost-estimation section to `docs/usage-and-cost-tracking.md`. It should cover the policy, the
basis fields, the reasons, and how to update `prices.json`: bump `version`, update `effective_date` and
`source`, and run the pricing tests.

## Decisions

1. **Price per request from the rollout, not from `turn.completed` plus peak context.** Rollout records
   carry exact per-request usage including cache writes, so a single path covers both bands and drops
   the "peak ≤ threshold" shortcut. Reconciling against the attributed delta catches window or
   attribution drift. The spec was updated to remove the shortcut.
2. **Transient request evidence (`json:"-"`).** The collector runs in process on the typed record.
   Persisting per-request arrays would grow `audit.log` and every atomic `run-metrics.json` rewrite in
   proportion to request count. What gets persisted is the compact derived `cost_estimate` evidence.
3. **Pricing in the collector; arithmetic in `internal/pricing`.** The collector owns attribution,
   precedence, and eligibility. The pricing package is pure and table-driven, so tests can pin the spec
   scenario numbers directly.
4. **Integer micro-USD arithmetic.** Rates as small as $0.01/M and multi-million-token steps make float
   accumulation visible in tests and in the summary's rounding. Integer math is exact and
   order-independent.
5. **Mirror cost fields into `event.Data`.** The run views and dev-audit read `step_end` data, not
   `run-metrics.json`. Writing the basis and reason there keeps a single source for all views.
6. **Map adapter evidence failures to `context-band-unknown`.** The finer rollout reasons
   (`codex-rollout-not-found`, `-unsupported`, `parse-failure`) stay visible on the context
   measurement. The cost reason stays at the spec's level.

## Risks / Trade-offs

- **Rollout window and reconciliation.** The window is `timestamp ≥ StartedAt` on the step's thread. A
  concurrent Runner invocation resuming the same thread could interleave records. Codex threads are not
  shared concurrently in Runner, and if it ever happens, reconciliation fails closed with
  `request-evidence-mismatch`. Clock skew between Runner and Codex timestamps carries the same
  fail-closed result. Mitigation: tests cover pre-window records, other threads, and an extra in-window
  record.
- **Rollout format drift.** If Codex renames fields, decoding falls back to `parse-failure` or missing
  fields, which produce `context-band-unknown` or `cache_writes_unobserved`. Nothing is silently
  mispriced. A fixture captured from the current Codex version pins the format.
- **Price drift and maintenance.** The table is a reviewed data file. Old records keep their
  `price_table_version`. Leaving a model out is safe (`model-unpriced`).
- **Coverage gap.** Codex steps whose rollout can't be located (for example, a custom `CODEX_HOME`
  without sessions) stay unpriced. This is accepted: an unpriced step is better than a wrong price.

## Migration Plan

- The change is additive at v4, with no schema bump. Old readers ignore the new fields. New readers
  treat a missing basis on a non-null cost as `reported`.
- Rollback means reverting the binary. Artifacts written with estimates remain valid v4. An older
  binary still reads the estimates in `estimated_api_cost_usd`, but without the basis label.

## Verification Strategy

- **`internal/pricing` unit tests:**
  - table load and validation of the real file;
  - every spec arithmetic scenario (0.98, 0.115, 0.68, 0.134, reasoning not double-counted, the
    threshold boundary at exactly 272,000);
  - absent versus zero cache writes;
  - invalid requests.
- **`internal/cli` Codex tests:** request-evidence extraction from a rollout fixture (in-window
  filtering, other threads, compaction responses included, absent `cache_write_input_tokens`,
  malformed payload, no records).
- **`internal/metrics` collector tests:**
  - each eligibility reason and its precedence, including adapter-produced records (real-shaped
    `turn.completed` JSON) that lack `reasoning_output_tokens`, so they are marked partial, and still
    price; and a record lacking `cached_input_tokens`, which gives `required-category-missing`;
  - a costed nested projection keeping basis `reported` across refresh and reload;
  - CLI-reported precedence;
  - resumed-session delta reconciliation;
  - external-user turns: all eligible, unattributed turn with partial subtotal, mixed models;
  - nested-agent events not estimated;
  - totals and session rollup basis and `cost_by_basis`;
  - legacy load normalization;
  - native measurement `estimated-cost` (full and partial), plus contract validation.
- **`internal/devaudit` tests:** leaf basis for reported-only, estimated-only, mixed, and unknown
  costs; v1→v3 and v2→v3 header upgrades; a mismatched header; and a row projection with
  `cost_basis`.
- **`internal/runview` tests:** `~` marker on step, container, and total rows; marker combined with
  the partial indicator; detail label and reason; old audit events treated as reported.
