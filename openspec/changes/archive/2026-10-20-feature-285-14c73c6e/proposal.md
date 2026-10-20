## Why

Claude steps record `estimated_api_cost_usd` because the Claude CLI reports a USD cost. Codex reports
only token usage (`turn.completed` carries input, cached input, output, and reasoning counts), and the
`cost-capture` spec forbids Agent Runner from pricing tokens. Codex steps therefore record full token
usage with a null cost.

That gap skews every cost comparison that mixes CLIs:

- In and-scene eval `3ea9d7f8` rep 1, the audit step (gpt-6.1-sol, about 3.8M tokens) and an
  acceptance-test round (gpt-6-luna, about 5.0M tokens) have no cost. Stage and total costs count
  Claude only, so stages that use Codex look cheaper than they are.
- In the first pass over development audit data, only 1,431 of 6,690 non-skipped step rows had a cost.

Codex runs on a subscription, so no CLI will ever report a spend figure for it. The only way to compare
cost across Claude and Codex steps is for Runner to estimate an API-equivalent price from the tokens it
already records. This change reverses the original `cost-tracking` decision "Agent Runner does no
pricing" for Codex only, because that decision now blocks the main use of the cost data.

## What Changes

- Add a versioned price table to Agent Runner, embedded in the binary. It holds OpenAI Standard-tier USD
  rates per model across every dimension OpenAI publishes: input, cached input, cache writes, and output
  (reasoning is billed as output). Each dimension has a short-context and a long-context band with an
  explicit threshold; for gpt-6.1-sol and gpt-6-luna the threshold is 272K input tokens per request.
  Each entry records its effective date and source.
- Define one valuation policy, **request-level Standard-tier API equivalent**, in `cost-estimation`.
  Each model request is priced in the band set by its own input tokens. The policy includes a stated
  convention for evidence Codex does not report (see Technical Approach).
- Produce an estimate only when the evidence covers the whole attempt (see Technical Approach). Record it
  in the existing `estimated_api_cost_usd` field so native step, container, and run totals and downstream
  consumers include it without changes.
- Mark every cost with its basis: `reported` (the CLI's own figure) or `estimated` (Runner's price-table
  figure). Step records expose the basis and the price-table version used. Run and session totals show
  whether the cost is all reported, all estimated, or mixed, alongside the existing coverage indicator.
- In the native measurement, record the estimate as attempt-scoped cost evidence that is `derived` and
  `approximate`, with a derivation that names the price table. Reported costs stay `observed` and `exact`.
- When an estimate isn't eligible, the cost is null with a reason, never zero, and it doesn't count
  toward cost coverage. That covers an unknown model, missing model identity, unavailable or partially
  attributed usage, a missing turn, a missing required category, or an undeterminable context band.
- Run views and the run-complete screen show estimated costs with a distinct marker (for example `~$1.23`)
  so a reader never takes an estimate for real spend.
- No **BREAKING** changes. The artifact changes are additive, and existing fields keep their meaning:
  `estimated_api_cost_usd` was always an API-price estimate, not a bill.

## Capabilities

### New Capabilities
- `cost-estimation`: The valuation policy (request-level Standard-tier API equivalent, context bands,
  the convention for unobserved cache writes), the embedded price table (rate dimensions, bands,
  versioning, effective dates, sources), the full-attempt eligibility rule, which invocations it covers
  (native Runner Codex invocations only), how tokens map to rates without double counting, and how the
  estimate and its limitations are labeled.

### Modified Capabilities
- `cost-capture`: "No pricing" becomes "no pricing except the Codex estimate defined by
  `cost-estimation`". Costs carry a `reported`/`estimated` basis. Run-level aggregation reports the mix
  of bases next to coverage. The Claude rules are unchanged: Claude cost stays CLI-reported only.
- `run-metrics-artifact`: Step records gain the cost basis and price-table version. Run and session
  totals gain the cost-basis mix. Native measurements carry derived, approximate estimate evidence with
  its limitations. Scalar USD views accept that evidence as full-attempt cost only when it meets the
  eligibility rule; otherwise it is neither summed nor counted as covered. Nested Validator projections
  are never estimated, but producer-reported costs on them carry basis `reported`.
- `audit-log-entries`: Agent `step_end` carries the cost basis, price-table version, and ineligibility
  reason. `run_end` totals carry the basis mix.
- `lightweight-audit-reporting` / `workflow-value-observation`: Development-audit observations record
  the basis of their cost, and the sheet export gains a `cost_basis` column (`step_value_v3`, upgraded
  in place from v1 and v2), so exported estimates are never indistinguishable from reported costs.
- `run-complete-screen`: Estimated costs and rolled-up totals that include estimates get an estimate
  marker.
- `view-run` / `live-run-view`: Per-step and per-run cost display distinguishes estimated from reported
  cost.

## Technical Approach

Pricing happens in the metrics collector, after usage attribution, not in the Codex adapter. Codex's
`turn.completed` usage is session-cumulative. The collector already turns it into a per-invocation delta
(`RawCumulative` → `Tokens`), and the estimate must price that attributed delta, not the raw cumulative
counts. Pricing at that point also means one code path covers native headless agent steps, called
agents, and external-user turns, since all of them already flow through the collector. Nested Validator
model attempts take a separate path (they are projected from immutable producer measurement heads) and
are out of scope; see decision 7.

```text
codex adapter → UsageRecord (cumulative tokens, model) + rollout context evidence
   → collector attributes per-invocation tokens (per turn for external-user steps)
      → eligibility check (complete attribution, required categories, model, band)
         → eligible:   price by policy → estimated_api_cost_usd + cost_basis=estimated + table version
         → ineligible: estimated_api_cost_usd = null + reason
            → totals (sum + coverage + basis mix), native measurement (derived/approximate + limitations)
```

Key decisions:

1. **Embedded, versioned table; no network.** Rates live in a data file in the repository, embedded at
   build time. Updating a price is a reviewed change that bumps the table version. Runs never fetch
   prices, so they stay deterministic and work offline.
2. **Reuse `estimated_api_cost_usd`.** The field name and the spec already define the value as an
   API-price estimate. Reusing it lets eval comparisons and dev-audit sheets pick up Codex cost with no
   change outside this repository. The new `cost_basis` field carries the reported-versus-estimated
   distinction.
3. **Codex only.** Claude keeps its CLI-reported cost. Steps where Claude reports none (no baseline,
   stale report) stay null: estimating those would mix two valuation methods for one provider. Copilot
   credits and Cursor stay out of scope.
4. **Valuation policy: request-level Standard-tier API equivalent.** OpenAI prices four dimensions
   (input, cached input, cache writes, output), each with short- and long-context bands chosen per
   request by that request's input tokens. Cache writes replace the ordinary input rate for those tokens
   rather than adding a fee. The policy prices against that structure, using the evidence Runner has:
   - **Context band.** Codex's `turn.completed` reports only invocation totals, which can't show which
     requests crossed the threshold. The band is decided per request from the per-request usage
     records (`token_usage_record`) in the Codex session rollout. These records must reconcile with the
     attributed delta. When they are missing, the cost is null with reason `context-band-unknown`.
   - **Cache writes.** Rollout records report `cache_write_input_tokens`, which is priced at the
     cache-write rate. A record that lacks the field has its uncached input valued at the ordinary
     input rate, and the evidence carries the limitation `cache_writes_unobserved`.
   - **No double counting.** Cached input and cache writes are subsets of input, and reasoning is a
     subset of output. Each token is priced once.
   - **Tier.** Standard tier. Batch, Flex, and priority tiers are not modeled.
   (Revised after design confirmed the rollout fields; see `design.md`.)
5. **Exact model match.** Rates resolve by the effective model identity Codex reports, falling back to
   the launch-resolved model the collector already records. An unmatched model gives a null cost with a
   reason. The table may list explicit aliases, but no fuzzy or family matching.
6. **Full-attempt eligibility.** The scalar `estimated_api_cost_usd` is set only when the estimate
   covers the whole attempt. That requires usage `collected` with `complete` completeness; for
   external-user steps, every turn attributed (no missing report, broken baseline, or session switch);
   every category the policy needs (input, cached input, output) present; a resolvable model; and a
   decidable band. An optional detail the policy does not need, such as the reasoning breakdown, does
   not block eligibility. A missing required category never defaults to zero. External-user turns are
   checked and priced per turn, before aggregation, with each turn's own model identity. The step's
   estimate is the sum of its turn estimates and is eligible only if every turn is. When the attempt is
   ineligible, the scalar is null with a reason and the step counts as missing cost for coverage. Native
   evidence may keep a known subtotal with `partial` coverage, which is never promoted to a full
   attempt cost.
7. **Native Runner invocations only.** Nested Validator model attempts (`kind: nested-agent`, CLI
   `agent-validator`) are rebuilt from immutable producer measurement heads on every refresh, and
   their cost comes only from the producer's own cost evidence. This change does not price them and
   does not infer Codex identity from the `agent-validator` compatibility CLI. Runs with Codex-backed
   validation will therefore still show `partial` cost coverage for those attempts. Pricing them
   belongs in the producer (agent-validator), or in a follow-up that attaches Runner-owned derived
   evidence alongside producer heads.

Main risks:

- **Price drift.** Published rates change, and subscription-only Codex models may have no published API
  price. Effective dates and the table version on each record keep old estimates interpretable, and
  models without a credible published price stay unpriced rather than guessed.
- **Thin request-level evidence.** When Codex's rollout can't be located (the `codex-rollout-not-found`
  context state), the band is unknown, so the step stays unpriced. This trades coverage for not
  embedding systematic long-context errors. The design should measure how often this happens on real
  runs.

## Out of Scope

- Codex steps that record no usage at all (autonomous-interactive Codex, #248).
- Pricing nested Validator model attempts, even when the Validator ran on Codex. Their cost stays
  whatever the producer reports.
- Non-Standard OpenAI service tiers (Batch, Flex, priority).
- Estimating cost for Claude, Cursor, OpenCode, or Copilot, including converting Copilot credits.
- Fetching live prices, user-configurable price overrides, or per-account discounts.
- Re-pricing existing `run-metrics.json` files from earlier runs during migration. Old records keep a
  null Codex cost, and their basis is unknown or treated as reported.
- Reporting actual subscription spend or reconciling estimates against bills.
- Eval-side reporting changes. Consumers get native Codex estimates through the existing field. Mixed
  runs with Codex-backed validation stay `partial`.

## Impact

- **Code:** new pricing package or file plus the embedded table (likely under `internal/metrics/`), the
  `internal/metrics/collector.go` cost path and totals, `internal/metrics/native_measurements.go` cost
  evidence, `internal/model/usage.go` (cost-basis types and `RunTotals` additions), and cost rendering
  in `internal/runview/` (summary, tree, selected detail) and the run-complete screen. `internal/devaudit`
  reads `estimated_api_cost_usd` and gains coverage for native Codex steps. It also carries the cost
  basis into observations and the `step_value_v3` sheet column.
- **Artifact:** additive fields in `run-metrics.json`, which stays at schema v4. The development-audit
  worksheet moves to `step_value_v3` (one appended column, with an in-place header upgrade).
- **Specs:** new `cost-estimation`; deltas to `cost-capture`, `run-metrics-artifact`,
  `audit-log-entries`, `lightweight-audit-reporting`, `workflow-value-observation`,
  `run-complete-screen`, `view-run`, and `live-run-view`.
- **Maintenance:** someone must keep the price table current as OpenAI changes rates or ships models.
- **Dependencies:** none new; no network access during runs.
