- [ ] Implement the change described by these files using TDD, as `CLAUDE.md` requires: write each failing test before the code that makes it pass. Satisfy every spec scenario in `specs/` and every `INT-*` / `E2E-*` obligation in `test-plan.md`. `design.md` is the technical source of truth; the bullets below summarize it.
  - [proposal.md](proposal.md), [design.md](design.md), [test-plan.md](test-plan.md), [decisions.md](decisions.md)
  - [specs/cost-estimation/spec.md](specs/cost-estimation/spec.md), [specs/cost-capture/spec.md](specs/cost-capture/spec.md), [specs/run-metrics-artifact/spec.md](specs/run-metrics-artifact/spec.md), [specs/audit-log-entries/spec.md](specs/audit-log-entries/spec.md), [specs/lightweight-audit-reporting/spec.md](specs/lightweight-audit-reporting/spec.md), [specs/workflow-value-observation/spec.md](specs/workflow-value-observation/spec.md), [specs/run-complete-screen/spec.md](specs/run-complete-screen/spec.md), [specs/view-run/spec.md](specs/view-run/spec.md), [specs/live-run-view/spec.md](specs/live-run-view/spec.md)

  **Price table** (`internal/pricing`, new; imports only `internal/model`)
  - Add `prices.json`, loaded with `//go:embed`. It has `version`, `policy: "openai-standard-request-level"`, and `entries[]`. Each entry has `provider`, `model`, `aliases`, `effective_date`, `source`, `tier: "standard"`, `threshold_input_tokens`, and `short` and `long` rate sets (`input`, `cached_input`, `cache_write`, `output`, all USD per million tokens).
  - Seed it with gpt-6.1-sol, gpt-6-luna, gpt-6-sol, gpt-6-astra, and gpt-5.6-sol, using the cost-estimation spec's table (effective 2026-10-20, threshold 272,000).
  - `Load()` parses once and validates: a non-empty source and date, positive rates, long ≥ short, unique names and aliases, and a positive threshold. A package test loads the real file.
  - `Table.Lookup(provider, model)`: exact match on the model name or an alias only.
  - `Estimate(entry, requests)`:
    - Each request is short when its input ≤ threshold, otherwise long.
    - Remaining input = input − cached − cache_write.
    - Use integer micro-USD-per-million arithmetic, converting to dollars only at the end.
    - A nil `CacheWrite` counts as 0 and sets `cache_writes_unobserved`.
    - `Input < Cached + CacheWrite` gives `request-evidence-mismatch`.
    - Pin every spec arithmetic scenario (0.98, 0.115, 0.68, 0.134, reasoning not double-counted, the 272,000 boundary).

  **Model** (`internal/model/usage.go`)
  - Add `RequestUsage{Input, Cached, Output int64; CacheWrite *int64}` and `RequestEvidence{Source, Status, Reason, Requests}`.
  - Add `UsageRecord.RequestEvidence` with tag `json:"-"`. `cloneUsage` deep-copies it.
  - Add the `CostBasis` values `reported` and `estimated`.
  - Add the reasons `usage-unavailable`, `usage-incomplete`, `required-category-missing`, `turn-unattributed`, `model-unknown`, `model-unpriced`, `context-band-unknown`, and `request-evidence-mismatch`.
  - `RunTotals` gains `CostBasis *string` (`json:"cost_basis"`, explicit null when coverage is `none`) and `CostByBasis map[string]float64` (`json:"cost_by_basis,omitempty"`).

  **Codex adapter** (`internal/cli/codex_context.go`)
  - In the same rollout pass, the accumulator decodes the full `usage` object of every in-window `token_usage_record` on the step's thread, compaction responses included. Leave `CacheWrite` nil when `cache_write_input_tokens` is absent.
  - `codexContext` returns both the context measurement and the request evidence, and `ExtractUsageWithContext` sets both. The context gauge rules are unchanged.
  - Map failures to evidence reasons: missing thread or rollout gives `codex-rollout-not-found`, a malformed payload gives `parse-failure`, and no records in the window gives `codex-rollout-unsupported`. Never drop a request silently.
  - Add an adapter test pinning that Codex's field-absence `partial` completeness never sets `Reason`.

  **Collector** (`internal/metrics/collector.go`)
  - After `attribute` and `attributeCost`:
    - a CLI-reported cost gives basis `reported`;
    - otherwise, a `codex` usage on an event that is not `EventNestedAgentEnd` goes to `estimateCost`;
    - otherwise the cost is null with no basis.
  - `estimateCost` runs the spec's ordered checks:
    1. status `collected`, else `usage-unavailable`;
    2. external-user steps only: every turn attributed, else `turn-unattributed`;
    3. `attributionComplete(u) := u.Completeness != partial || u.Reason == ""`, else `usage-incomplete`;
    4. input, cached input, and output present, else `required-category-missing` (missing reasoning is ignored);
    5. the model from `Identity.EffectiveModel`, falling back to `usage.Model`, else `model-unknown`, and a table entry, else `model-unpriced`;
    6. request evidence present and non-empty, else `context-band-unknown`;
    7. per-request input, cached, and output sums exactly equal to the attributed `Tokens`, else `request-evidence-mismatch`.
  - Refactor `attributeTurns` to also return the per-turn attributed records. `estimateTurns` checks `turn-unattributed` across all turns first, then runs checks 3–7 per turn, each with its own model. The step's estimate is the sum of its turns. If any turn is ineligible, the cost is null with the first failing reason, and the eligible turns' sum is kept as `partial_subtotal_usd`.
  - Write the following to both `event.Data` and `StepRecord`: `estimated_api_cost_usd`, `cost_basis`, `price_table_version`, `cost_unavailable_reason`, and `cost_estimate` (`{policy, model, requests, long_context_requests, limitations, partial_subtotal_usd}`). Use `omitempty` on the new `StepRecord` fields.
  - `totalsForRecords` keeps its sum and coverage logic and adds `CostByBasis` and `CostBasis` (one basis, `mixed`, or nil). A non-null cost with an empty basis is counted as `reported` defensively, and a test asserts that this never happens.
  - On load, set `reported` on any non-null cost with an empty basis. Never re-price. `SchemaVersion` stays 4.
  - `run_end` carries the basis and `cost_by_basis` with the totals.

  **Native measurements and nested projections** (`internal/metrics/native_measurements.go`, `measurements.go`)
  - `collectReportedCost` branches on `CostBasis`:
    - `reported`: unchanged.
    - `estimated`: add an `estimated-cost` entry (scope `attempt`, coverage `full`, overlap `established`, source `provider_usage`, currency USD, origin `derived`, precision `approximate`, derivation `<policy>@<version>`) and the estimate's limitations.
    - null with a partial subtotal: the same entry with coverage `partial` and the reason.
  - All of these must pass measurement contract validation.
  - `validatorStepProjection` sets `CostBasis = reported` whenever `CompleteUSD` yields a cost, on every refresh. It never estimates and never modifies producer evidence.

  **Development audit** (`internal/devaudit`)
  - `CostEvidence` gains `cost_basis` (`reported`, `estimated`, or `mixed`; nil when the cost is unknown). Derive it from the contributing records in the `value_audit.go` leaf cost loop and in `leafModelScalars`.
  - In `sheets_delivery.go`:
    - Add `stepValueHeaderV3 = V2 + ["cost_basis"]` and set `sheetRowSchemaVersion = "step_value_v3"`.
    - `validateHeader` accepts v3.
    - A v1 or v2 header with empty cells to its right gets only its missing trailing cells.
    - Rows append `cost_basis`, written empty when nil.
  - Judge columns are unchanged.

  **Run views** (`internal/runview`)
  - `audit.go` reads `cost_basis` and `cost_unavailable_reason`. A cost with no basis is treated as reported.
  - In the summary:
    - add `costEstimated` to `summaryMetrics`, carried through `merge`;
    - `~` prefixes any cell that includes an estimate, independently of the partial indicator;
    - the Total row uses `totals.CostBasis`.
  - Detail shows `~$X (API-equivalent estimate)`, or `? (<reason>)` for an ineligible Codex step.
  - Add the `~` entry to the legend.

  **Docs**
  - Add a cost-estimation section to `docs/usage-and-cost-tracking.md` covering:
    - the policy;
    - the basis fields and reasons;
    - the `step_value_v3` `cost_basis` column;
    - how to update `prices.json` (bump `version`, update `effective_date` and `source`, run the pricing tests).

  **Tests from `test-plan.md`**
  - INT-001 (`internal/cli`): request evidence read from rollout fixtures through the private Codex home.
  - INT-002 (`internal/metrics`): persist and reload across a resume from a pre-change v4 artifact, with a costed nested head, a refresh, and a reload.
  - INT-003 (`internal/runview`): render from a recorded `audit.log`.
  - INT-004 (`internal/metrics`): real adapter output fed into collector eligibility, covering missing reasoning, missing cached input, and resumed and external-user turns.
  - INT-005 (`internal/devaudit`): observation basis and v1/v2→v3 sheet delivery via the local fake Sheets server.
  - E2E-001 (`cmd/agent-runner`): built binary with fake `claude` and `codex` scripts and a mixed workflow. Existing context fixtures keep their assertions.
  - E2E-002: add reconciliation and pricing assertions to the opt-in `TestCodexHeadlessRealAgentE2E` and `TestCodexChildFollowUpHeadlessRealAgentE2E`, using isolated storage; skip with a reason if Codex can't authenticate. Do not add CI jobs.

  **Done when**
  - `make fmt`, `make lint`, and `make test` pass.
  - `openspec validate feature-285-14c73c6e --strict` passes.
