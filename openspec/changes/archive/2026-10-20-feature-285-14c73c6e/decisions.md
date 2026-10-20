# Decisions

## propose: verdict
- **Decision**: Go with caveats. Codex token usage is already collected and attributed per invocation, so an estimate fits the existing collector. The caveat is that this reverses the `cost-tracking` "no pricing" decision for Codex and adds a price table someone has to maintain.
- **Alternatives considered**: No-go and price in eval tooling instead. Rejected: every consumer (and-scene evals, dev-audit sheets, run views) would need its own table, and the issue asks for the cost in run metrics.
- **Decision-bearing**: yes

## propose: not a direction-level stop
- **Decision**: Proceed without stopping. The issue explicitly asks for the estimate, and the spec it contradicts lives in this repository. The artifact changes are additive. Consumers get the value through the existing `estimated_api_cost_usd` field, so no change outside the repository is needed.
- **Alternatives considered**: Stop because the change contradicts the `cost-capture` spec. Rejected: the stop rules cover contradicting the issue, not contradicting earlier in-repo specs the issue is asking to change.
- **Decision-bearing**: yes

## propose: where the estimate is stored
- **Decision**: Reuse `estimated_api_cost_usd` and add a `cost_basis` (`reported` | `estimated`) plus the price-table version. Totals report the basis mix.
- **Alternatives considered**: A separate `estimated_codex_cost_usd` field. Rejected: consumers would have to sum two fields, so mixed-run comparisons would still need changes outside this repository.
- **Decision-bearing**: yes

## propose: scope of pricing
- **Decision**: Codex only. Claude stays CLI-reported only, even when its report is missing.
- **Alternatives considered**: Generic pricing for all CLIs lacking cost. Rejected: beyond the issue, and it mixes valuation methods for one provider.
- **Decision-bearing**: yes

## propose: price source
- **Decision**: An embedded, versioned table in the repository, with effective dates and sources. No live fetching and no user overrides. Unknown models get a null cost with a reason.
- **Alternatives considered**: models.dev or OpenAI price fetch at runtime (non-deterministic, needs network); user-configurable overrides (more surface than the issue needs).
- **Decision-bearing**: no

## propose: pricing point
- **Decision**: Price in the metrics collector, after cumulative-to-delta token attribution.
- **Alternatives considered**: Price in the Codex adapter. Rejected: the adapter sees session-cumulative counts, so it would overprice resumed sessions.
- **Decision-bearing**: no

## propose: historical artifacts
- **Decision**: Do not re-price existing run-metrics files during migration.
- **Alternatives considered**: Back-fill estimates on migration. Rejected: migration would silently change historical numbers.
- **Decision-bearing**: no

## proposal-review: PR-001 (pricing inputs can't represent published prices)
- **Decision**: Applied. I checked OpenAI's pricing page: gpt-6.1-sol and gpt-6-luna publish input, cached input, cache-write, and output rates, each in short and long (>272K input per request) bands. The proposal now defines a request-level Standard-tier valuation policy. The band is decided per request from rollout evidence (peak per-request input, or per-request records); an undecidable band gives a null cost with reason `context-band-unknown`. Unobserved cache writes are valued at the ordinary input rate under an explicit `cache_writes_unobserved` limitation that the derivation names.
- **Alternatives considered**: A normalized short-context benchmark using only the turn.completed counters (simpler and higher coverage, but it systematically underprices long-context requests, which are common in large Codex steps such as the 3.8M-token audit); refusing to price whenever cache writes are unobserved (Codex never reports them, so no Codex step would ever get a cost).
- **Decision-bearing**: yes

## proposal-review: PR-002 (partial attribution promoted to full cost)
- **Decision**: Applied. Added a full-attempt eligibility rule: usage collected and complete, every external-user turn attributed, required categories present (never defaulted to zero), model and band resolvable. External-user turns are priced per turn before aggregation. An ineligible attempt gets a null scalar with a reason and counts as missing for coverage. Native evidence may keep a partial-coverage subtotal, which is never promoted.
- **Alternatives considered**: Pricing the known subtotal as the scalar cost with a partial flag. Rejected: existing aggregation counts any non-null step cost as covered, so the subtotal would read as a complete cost.
- **Decision-bearing**: yes

## proposal-review: PR-003 (nested Validator attempts)
- **Decision**: Applied by narrowing scope. Only native Runner Codex invocations (agent steps, called agents, external-user turns) are priced. Nested Validator attempts are out of scope, Codex identity is not inferred from the `agent-validator` CLI, and the downstream-coverage claim now says runs with Codex-backed validation stay `partial`.
- **Alternatives considered**: Attaching Runner-owned derived evidence to producer measurement heads and keeping it across refreshes. Deferred: it adds a second evidence channel over immutable producer records for attempts the issue doesn't name. The issue's examples are native steps, and the producer can report its own cost.
- **Decision-bearing**: yes

## spec: price table contents
- **Decision**: The table starts with gpt-6.1-sol and gpt-6-luna at OpenAI's published Standard rates effective 2026-10-20 (four dimensions, short and long bands, threshold 272K input tokens per request; a request exactly at the threshold is short). Models without a published API price are left out of the table.
- **Alternatives considered**: Seeding every OpenAI model (more upkeep, prices nobody uses yet); pricing unknown models by family (rejected by the exact-match rule).
- **Decision-bearing**: no

## spec: ineligibility reasons
- **Decision**: A null Codex cost carries one of `usage-unavailable`, `usage-incomplete`, `required-category-missing`, `turn-unattributed`, `model-unknown`, `model-unpriced`, `context-band-unknown`, or `request-evidence-mismatch`. A missing reasoning breakdown does not block an estimate.
- **Alternatives considered**: A single generic `cost-unavailable` reason. Rejected: consumers couldn't tell fixable gaps (an unpriced model) from structural ones (no rollout).
- **Decision-bearing**: no

## spec: cost basis presentation
- **Decision**: Steps carry `cost_basis` (`reported` | `estimated`, absent when the cost is null) plus the price-table version. Totals and session rollups carry a basis indicator (`reported` | `estimated` | `mixed`) and per-basis subtotals. The TUI marks any cell that is, or includes, an estimate with `~`, independently of the partial indicator.
- **Alternatives considered**: Marking only pure-estimate cells; a separate estimated-cost column. Rejected: a mixed total would look like reported spend, and an extra column widens the summary table.
- **Decision-bearing**: no

## spec: schema version
- **Decision**: Keep `run-metrics.json` at schema v4. The new fields are additive, and the Versioned schema requirement bumps the version only for backward-incompatible changes. On load, earlier non-null costs get basis `reported`, and earlier Codex records are not re-priced.
- **Alternatives considered**: Bump to v5. Rejected: no incompatible change, and a bump forces a consumer update for nothing.
- **Decision-bearing**: no

## spec: CLI-reported cost precedence
- **Decision**: A CLI-reported cost always wins. An estimate never replaces or adjusts a reported value, even for a CLI that might start reporting cost later.
- **Alternatives considered**: Preferring the estimate for consistency across CLIs. Rejected: it would override the provider's own figure.
- **Decision-bearing**: no

## spec: deferred to design
- **Decision**: Two points are left to design and marked `deferred-to-design` in the specs: which per-request fields Codex rollouts expose (this decides whether long-context attempts can be priced or come out `context-band-unknown`), and the subset semantics of cache writes for any future source that reports them.
- **Alternatives considered**: Settling these during specification. Rejected: both depend on source-format investigation, not on observable behavior choices.
- **Decision-bearing**: no

## design: per-request pricing source
- **Decision**: Price every Codex attempt request by request from the rollout's `token_usage_record` lines, which carry full per-request usage including `cache_write_input_tokens` (checked against 12,194 local records). The records must reconcile exactly with the attributed delta. The spec's peak-context shortcut is removed, and both deferred-to-design markers are resolved in the cost-estimation spec.
- **Alternatives considered**: Price the `turn.completed` delta at short rates when the peak context is ≤272K, and use per-request records only for long context. Rejected: two paths for the same result, and it can't price cache writes.
- **Decision-bearing**: no

## design: price table models
- **Decision**: Add gpt-6-sol, gpt-6-astra, and gpt-5.6-sol (all seen in local rollouts) at their published 2026-10-20 Standard rates, alongside gpt-6.1-sol and gpt-6-luna.
- **Alternatives considered**: Only the two models the issue names. Rejected: the other three are in active use and would otherwise come out `model-unpriced`.
- **Decision-bearing**: no

## design: transient request evidence
- **Decision**: Per-request usage travels on `UsageRecord.RequestEvidence` tagged `json:"-"` (in-process only). The persisted record holds the compact `cost_estimate` evidence (policy, model, request counts, limitations, partial subtotal).
- **Alternatives considered**: Persisting per-request arrays in audit.log and run-metrics.json (bloats both in proportion to request count); having the adapter compute band summaries (couples the adapter to the price table).
- **Decision-bearing**: no

## design: arithmetic and package boundary
- **Decision**: New `internal/pricing` package with an embedded `prices.json` and integer micro-USD arithmetic. The collector owns precedence and eligibility.
- **Alternatives considered**: float64 math (accumulation drift); pricing inside the collector file (harder to pin to the spec numbers).
- **Decision-bearing**: no

## design: view data path
- **Decision**: Mirror `cost_basis`, `price_table_version`, `cost_unavailable_reason`, and `cost_estimate` into `step_end` event data so the run views and dev-audit read them from audit events. Old events with a cost and no basis are treated as reported.
- **Alternatives considered**: Have the run views read run-metrics.json. Rejected: the views are built on audit events today.
- **Decision-bearing**: no

## test-plan: obligations
- **Decision**: Three integration obligations (Codex rollout request evidence through the private CODEX_HOME; collector persist/reload across resume and a legacy v4 artifact; run views rendering from a recorded audit.log) and two E2E obligations (a fake-CLI mixed Claude/Codex workflow through the built binary in CI; the opt-in real Codex test extended to assert reconciliation and pricing). No new suites.
- **Alternatives considered**: A TUI E2E under PTY for the `~` marker. Rejected: INT-003 renders from the real audit file at lower cost, and the exploratory pass covers the interactive view. A dedicated price-table check against OpenAI's live page in CI. Rejected: it needs network and the page changes; the acceptance pass may cross-check it read-only.
- **Decision-bearing**: no

## test-plan: live-run-view coverage
- **Decision**: No extra INT/E2E for `live-run-view` Estimated costs during an active run. It shares the audit-event data path that INT-003 and E2E-001 cover; unit tests and the exploratory pass cover live refresh.
- **Alternatives considered**: A live-run PTY test. Rejected: a run can't be launched under a synthetic PTY (CLAUDE.md).
- **Decision-bearing**: no

## test-plan: envelope
- **Decision**: Acceptance may use existing Claude/Codex CLI auth with isolated session storage, a few small real runs under $1, and read-only pricing-page fetches. Real `~/.codex/sessions` is read-only. Human-only testing: none.
- **Alternatives considered**: Requiring an OpenAI API key to compare against a real billed request. Rejected: Codex runs on a subscription, and the estimate is a comparison figure by definition.
- **Decision-bearing**: no

## approach-review: AR-001 (eligibility rejects reasoning-less Codex usage)
- **Decision**: Applied. Confirmed that `tokenCountsFromObject` marks Codex usage `partial` when `reasoning_output_tokens` is absent, with no reason. Pricing completeness is now judged separately: the ordered checks are status, then turn attribution, then attribution-level limitation (partial *with* a reason → `usage-incomplete`), then required-category presence, then model, then request evidence. So a reasoning-less record still prices, and `required-category-missing` is reachable. Updated the cost-estimation spec (ordered checks plus scenarios for adapter-shaped records, resumed turns, and missing cached input), design §4 (the `attributionComplete` helper, with an adapter invariant test), and the test plan (new INT-004, which feeds real adapter output into the collector).
- **Alternatives considered**: Changing Codex adapter completeness to ignore reasoning. Rejected: it would also change the usage completeness and canonical-total contracts that other consumers read.
- **Decision-bearing**: no

## approach-review: AR-002 (nested producer costs lack a basis)
- **Decision**: Applied. Confirmed that `validatorStepProjection` sets a `CompleteUSD` producer cost on every refresh. It now also sets basis `reported` there, so the basis survives refreshes. Totals never aggregate a cost under an empty basis (a defensive `reported` fallback, plus a test), and per-basis subtotals always sum to the total. Updated the cost-capture and run-metrics-artifact specs (a producer-reported basis scenario and a refresh/reload scenario), design §4 and §5, and the test plan (INT-002 extended with a costed nested head, a refresh, and a reload).
- **Alternatives considered**: Leaving producer costs unlabelled. Rejected: it breaks the "every non-null cost carries a basis" guarantee and the subtotal sums.
- **Decision-bearing**: no

## approach-review: AR-003 (dev-audit export unlabelled)
- **Decision**: Applied by propagating the basis. Observations carry `cost_basis` (`reported` | `estimated` | `mixed`, absent when cost is unknown), and the worksheet moves to `step_value_v3`, which appends a `cost_basis` column. It reuses the existing additive in-place header-upgrade pattern from v1 and v2; existing cells and rows are untouched, and older rows read as empty. `cost_usd` stays the numeric total. Judge costs are never estimated, so they are unchanged. Also added the missing `audit-log-entries` delta (`step_end` and `run_end` cost basis). Updated the proposal, specs (`lightweight-audit-reporting`, `workflow-value-observation`, `audit-log-entries`), design §7, and the test plan (new INT-005 using the local fake Sheets server). Not a direction-level stop: the change is additive, follows a header-upgrade mechanism the spec already defines, and needs no manual change outside the repository.
- **Alternatives considered**: Narrowing the labelling guarantee to metrics, audit, and TUI and documenting the export limitation. Rejected: the dev-audit dataset is one of the issue's two motivating consumers, and unlabelled estimates there would defeat "marked as an estimate".
- **Decision-bearing**: yes

## tasks: single implementation task
- **Decision**: One task covers the whole change, with per-area bullets summarizing design.md and the full INT/E2E obligation list, following the feature-280 tasks.md shape.
- **Alternatives considered**: Per-package tasks. Rejected: the instruction requires exactly one task.
- **Decision-bearing**: no
