## MODIFIED Requirements

### Requirement: Artifact content

`run-metrics.json` SHALL contain one execution record per executed step or nested model attempt, with one authoritative current measurement head per measured attempt and a run-level aggregate. Each model record SHALL include stable role/tool identity and distinct requested, launch-resolved, and telemetry-observed identities in addition to the step's identifier and nesting prefix, step type, outcome, duration in milliseconds, usage record, and `estimated_api_cost_usd`. The run-level aggregate SHALL include the run's duration, per-category token totals, canonical input/output/overall token totals, canonical-total coverage, and the cost total with its coverage indicator. Unavailable usage and absent totals/cost SHALL appear as explicit null/unavailable states, never as zeros.

In the compatibility views, per-category token totals SHALL be the sum of the values reported for that category across all executed steps regardless of outcome. Canonical input/output/overall totals SHALL sum only steps for which an adapter produced reliable canonical totals. Steps with unavailable usage, and categories or canonical totals a step did not report, contribute nothing to the corresponding aggregate. The aggregate SHALL include usage-coverage and canonical-total-coverage indicators — `complete` when every agent step that actually invoked its CLI reported the metric, `partial` when some did, and `none` when none did — parallel to the cost coverage indicator. Confirmed nested producer model dispatches SHALL participate exactly once in the compatibility usage, canonical-total, and cost coverage denominators alongside native agent steps and called agents. A dispatched attempt lacking eligible full attempt-level USD evidence SHALL count as missing cost: coverage is partial when some attempts have eligible cost and none when no attempts do. Agent steps that never invoked their CLI (skipped, or failed before launch), prepared-only producer records without launch evidence, and confirmed zero-dispatch invocations SHALL NOT count toward these denominators. Unresolved launch/delivery evidence SHALL retain separate explicit completeness limitations.

Schema v4 SHALL retain each accepted producer invocation and model-attempt record intact, including its selected revision, digest, and measurement version, separately from original Runner run/session/parent attribution. The artifact SHALL expose invocation membership, delivery state and gaps, and independent history, collection, per-model, and field-level completeness. External consumers SHALL require neither private Validator storage nor Runner's recovery journal. Compatibility step fields and scalar totals SHALL be derived views and MUST NOT be counted again alongside authoritative measurement heads.

The authoritative per-field v4 aggregates SHALL retain known subtotals and available/partial/unavailable states, precision, and contributing/partial/missing attempt coverage. A partial value SHALL retain its reason and MUST NOT become a complete value merely because numeric. Categories MAY overlap; a grand total SHALL require established non-overlapping semantics. Per-model allocations, unallocated evidence, allocation/identity joins, and provider-reported cost scope/currency/coverage/overlap SHALL survive unchanged. Scalar USD views SHALL use only unambiguous full attempt-level USD evidence: CLI-reported evidence, or a Runner estimate that meets the full-attempt eligibility rule of `cost-estimation`. They MUST NOT promote partial, allocation-only, or overlapping costs, or a partial estimate subtotal, to full attempt cost. Source versions and aggregate interpretation SHALL be explicit; unsupported or conflicting actual heads SHALL not contribute numerically or be replaced by older compatible heads.

Native measurements SHALL carry attempt-scoped `reported-cost` evidence for captured costs from `claude:result-event`, interactive Claude `claude:session-transcript`, and `opencode:step_finish`. These costs SHALL have full coverage and established overlap; the interactive Claude cost SHALL use the attributable CLI-reported delta defined by `cost-capture`. Native measurements of eligible Codex attempts SHALL carry one attempt-scoped `estimated-cost` instead. It has full coverage and established overlap, currency USD, origin `derived`, precision `approximate`, and a derivation naming the valuation policy and price-table version, and it carries the estimate's limitations (such as `cache_writes_unobserved`). Ineligible Codex attempts SHALL carry no full-coverage cost. They MAY carry a known estimated subtotal only with `partial` coverage and the ineligibility reason. Nested Validator projections SHALL keep deriving their cost only from producer cost evidence.

#### Scenario: Interactive Claude reported cost
- **WHEN** an autonomous-interactive Claude attempt has an attributable reported USD cost
- **THEN** its native measurement contains exactly one `reported-cost` with scope `attempt`, coverage `full`, and overlap `established`
- **AND** it does not gain a `legacy_cost_scope_unavailable` limitation

#### Scenario: Agent step record content
- **WHEN** an autonomous-headless agent step completes with usage and cost collected
- **THEN** its record in `run-metrics.json` carries role/tool and requested/resolved/observed identity, step identifier, prefix, type, outcome, duration, token categories with provenance and availability, and the reported cost evidence

#### Scenario: Crosscheck role remains distinct
- **WHEN** a workflow invokes a configured `crosscheck` agent
- **THEN** its metric record has role `crosscheck` rather than being folded into `lead-agent`

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

#### Scenario: Partial token subtotal remains partial
- **WHEN** an attempt reports a known partial output subtotal and another attempt reports complete output
- **THEN** the aggregate includes the known subtotal, names partial and missing coverage as applicable, preserves precision, and does not claim complete output

#### Scenario: Multi-model measurement remains one attempt
- **WHEN** one dispatch contains multiple observed model allocations and unallocated usage
- **THEN** the artifact retains stable allocation and identity references, scoped cost evidence, and attribution completeness while counting the dispatch once

#### Scenario: Partial or overlapping cost evidence
- **WHEN** an attempt reports allocation costs, an earlier partial subtotal, or costs with unknown overlap
- **THEN** the original cost evidence remains available without promoting it to or summing it as an established complete attempt cost
- **AND** a confirmed dispatched attempt with no eligible full USD cost participates as missing cost, producing partial compatibility cost coverage if another attempt has eligible cost and none if no attempts do

#### Scenario: Measurement version differs from outer artifact version
- **WHEN** v4 contains a measurement v1 record
- **THEN** its original version and digest remain unchanged and aggregate interpretation is declared independently

#### Scenario: Codex estimated cost evidence
- **WHEN** an autonomous-headless Codex attempt has an eligible estimate
- **THEN** its native measurement contains exactly one `estimated-cost` with scope `attempt`, coverage `full`, overlap `established`, origin `derived`, and precision `approximate`
- **AND** the compatibility step's `estimated_api_cost_usd` equals that amount

#### Scenario: Partial estimate subtotal not promoted
- **WHEN** an external-user Codex step has eligible estimates for some turns and an unattributed turn
- **THEN** any retained estimated subtotal has `partial` coverage, the step's `estimated_api_cost_usd` is null, and the step counts as missing cost in compatibility cost coverage

#### Scenario: Nested Validator projection unchanged
- **WHEN** a nested Validator attempt's producer evidence has token usage and no full attempt-level USD cost
- **THEN** its projected step's `estimated_api_cost_usd` is null and no Runner estimate is attached across measurement refreshes

#### Scenario: Costed nested projection keeps its basis across refresh
- **WHEN** a run holds a nested Validator projection with a producer-reported full attempt-level USD cost of $0.40 and a native Codex step with an estimated $0.98, and the artifact's measurements are refreshed and the artifact reloaded
- **THEN** the projection's cost basis is `reported` after every refresh and reload, the run and session totals' cost basis is `mixed`, and `cost_by_basis` is $0.40 reported and $0.98 estimated, summing to the $1.38 total

## ADDED Requirements

### Requirement: Cost basis in step records and totals

Each step record SHALL expose its cost basis (`reported`, `estimated`, or absent when the cost is null). Nested producer projections SHALL derive their basis on every measurement refresh, not only on load: a projected producer full attempt-level USD cost is `reported`. A record with an estimated cost SHALL also expose the price-table version used. A Codex record whose estimate is ineligible SHALL expose the ineligibility reason. Run-level totals and each execution-session rollup SHALL expose a cost-basis indicator (`reported`, `estimated`, `mixed`, or null when cost coverage is `none`) and the cost subtotal per basis, alongside the existing cost total and coverage. These are additive fields: the existing `estimated_api_cost_usd` fields and totals keep their names and include estimates, so existing consumers include Codex estimates without changes. The schema version SHALL remain 4, because the change is backward compatible.

#### Scenario: Estimated step record
- **WHEN** a Codex step receives an eligible estimate
- **THEN** its record shows `estimated_api_cost_usd`, cost basis `estimated`, and the price-table version

#### Scenario: Ineligible Codex record
- **WHEN** a Codex step's estimate is ineligible because its band cannot be decided
- **THEN** its record shows a null `estimated_api_cost_usd`, no cost basis, and reason `context-band-unknown`

#### Scenario: Session rollup basis
- **WHEN** a resumed run's first execution session ran only Claude steps with reported costs and its second ran only eligible Codex steps
- **THEN** the first session rollup's cost basis is `reported`, the second's is `estimated`, and the run totals' is `mixed`, each with matching per-basis subtotals

#### Scenario: Existing consumer reads totals
- **WHEN** a consumer reads only `totals.estimated_api_cost_usd` and `totals.cost_coverage` from a mixed Claude and Codex run
- **THEN** the total includes both the reported and the estimated costs

### Requirement: Artifacts written before cost estimation

When Runner loads a `run-metrics.json` written before cost estimation existed, it SHALL NOT re-price earlier Codex records: their `estimated_api_cost_usd` stays null and they carry no estimate reason. Existing non-null costs SHALL be given cost basis `reported`, since only CLI-reported costs existed before. Totals SHALL be recomputed with the cost-basis indicator from the retained records.

#### Scenario: Earlier Codex records stay unpriced
- **WHEN** a run created by an earlier binary, containing a Codex step with null cost and a Claude step with reported cost, is resumed and runs an eligible Codex step
- **THEN** the earlier Codex step stays null, the Claude step's basis is `reported`, the new Codex step is `estimated`, the run's cost basis is `mixed`, and cost coverage is `partial`
