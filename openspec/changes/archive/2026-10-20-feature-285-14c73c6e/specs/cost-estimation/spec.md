## ADDED Requirements

### Requirement: Codex invocations receive an API-equivalent cost estimate

Agent Runner SHALL estimate a USD API-equivalent cost for each native Runner invocation of the Codex CLI whose token usage it collected: agent steps, agent calls, and external-user turns. It SHALL record the estimate as the attempt's `estimated_api_cost_usd` when the attempt meets the full-attempt eligibility rule. The estimate SHALL be computed from the attempt's attributed token counts (the per-invocation delta, never the session-cumulative counters), its effective model, and the embedded price table, using the valuation policy defined in this capability. The estimate is a comparison figure: it SHALL NOT be presented as actual spend.

Agent Runner SHALL NOT estimate cost for any other CLI, and SHALL NOT estimate cost for nested Validator model attempts (records projected from producer measurement heads, including those whose underlying model ran on Codex). Agent Runner SHALL NOT infer Codex identity from the `agent-validator` compatibility CLI.

#### Scenario: Headless Codex step is priced
- **WHEN** an autonomous-headless Codex step on `gpt-6.1-sol` completes with complete attributed usage of 1,000,000 input tokens (800,000 of them cached), 50,000 output tokens, and per-request rollout records that reconcile with that usage, report no cache writes, and have no request above 150,000 input tokens
- **THEN** the step's `estimated_api_cost_usd` is 0.98 (200,000 uncached input × $2.00/M + 800,000 cached input × $0.10/M + 50,000 output × $10.00/M) and its cost basis is `estimated`

#### Scenario: Resumed Codex session is priced from its delta
- **WHEN** a Codex step resumes a session whose cumulative counters already include an earlier step's tokens
- **THEN** the estimate prices only the tokens attributed to this step, and the earlier step's tokens are not priced again

#### Scenario: Codex agent call is priced
- **WHEN** an agent call runs on the Codex CLI and its usage is collected completely on a priced model with a decidable band
- **THEN** the call's record carries an estimated cost under the same policy as an agent step

#### Scenario: Claude step is not estimated
- **WHEN** a Claude step completes with token usage but no Claude-reported cost
- **THEN** its `estimated_api_cost_usd` is null and no estimate is computed from its tokens

#### Scenario: Nested Validator attempt is not estimated
- **WHEN** a run contains a nested Validator model attempt whose producer recorded token usage but no full attempt-level USD cost, including one that ran on a Codex model
- **THEN** that attempt's `estimated_api_cost_usd` remains whatever the producer evidence supports (null here), and Runner adds no estimate

### Requirement: Embedded versioned price table

Agent Runner SHALL ship a price table embedded in its binary and SHALL NOT fetch prices or accept user price overrides at run time. The table SHALL carry a version identifier. Each entry SHALL identify the provider, the exact model identifier, any explicit aliases, the effective date of its rates, and the published source of its rates. Each entry SHALL give OpenAI Standard-tier USD rates per million tokens for every published dimension (input, cached input, cache writes, and output) in both a short-context and a long-context band, with the band threshold expressed as input tokens per request. A request whose input tokens equal the threshold SHALL be short-context.

The table SHALL include at least the following models at their published Standard rates effective 2026-10-20 (threshold 272,000 input tokens per request):

| Model | Band | Input | Cached input | Cache writes | Output |
|---|---|---|---|---|---|
| gpt-6.1-sol | short | $2.00 | $0.10 | $2.50 | $10.00 |
| gpt-6.1-sol | long | $4.00 | $0.20 | $5.00 | $15.00 |
| gpt-6-luna | short | $0.10 | $0.01 | $0.125 | $0.50 |
| gpt-6-luna | long | $0.20 | $0.02 | $0.25 | $0.75 |
| gpt-6-sol | short | $2.00 | $0.20 | $2.50 | $10.00 |
| gpt-6-sol | long | $4.00 | $0.40 | $5.00 | $15.00 |
| gpt-6-astra | short | $10.00 | $1.00 | $12.50 | $50.00 |
| gpt-6-astra | long | $20.00 | $2.00 | $25.00 | $75.00 |
| gpt-5.6-sol | short | $4.00 | $0.40 | $5.00 | $20.00 |
| gpt-5.6-sol | long | $8.00 | $0.80 | $10.00 | $30.00 |

Models without a credible published API price SHALL be absent from the table rather than estimated.

#### Scenario: Run works offline
- **WHEN** a Codex step is priced on a machine with no network access
- **THEN** the estimate is produced from the embedded table and no network request is made

#### Scenario: Record carries the table version
- **WHEN** a step receives an estimated cost
- **THEN** its record identifies the price-table version used

#### Scenario: Resumed run priced by a newer binary
- **WHEN** a run's earlier steps were priced with one table version and the run is resumed by a binary carrying a newer table version
- **THEN** earlier records keep their original estimates and version, and new records carry the newer version

#### Scenario: Request at the threshold is short-context
- **WHEN** every request of a `gpt-6-luna` attempt has at most 272,000 input tokens, with the largest exactly 272,000
- **THEN** the attempt is priced entirely at short-context rates

### Requirement: Model resolution for pricing

The estimate SHALL resolve rates by the attempt's effective model identity, using the existing identity precedence: the telemetry-observed model, otherwise the launch-resolved model. Resolution SHALL be an exact match against a table model identifier or one of its explicit aliases. It SHALL NOT use prefix, family, or fuzzy matching. When no model identity is available, the cost SHALL be null with reason `model-unknown`. When the model has no table entry, the cost SHALL be null with reason `model-unpriced`.

#### Scenario: Launch-resolved model used when telemetry omits it
- **WHEN** Codex's output names no model but the step launched with `-m gpt-6-luna`
- **THEN** the estimate uses the `gpt-6-luna` rates

#### Scenario: Unpriced model
- **WHEN** a Codex step's effective model is `gpt-6.1-sol-preview` and the table lists only `gpt-6.1-sol` with no such alias
- **THEN** its `estimated_api_cost_usd` is null with reason `model-unpriced`

#### Scenario: Unknown model
- **WHEN** a Codex step has collected usage but no telemetry-observed or launch-resolved model
- **THEN** its `estimated_api_cost_usd` is null with reason `model-unknown`

### Requirement: Request-level Standard-tier valuation policy

The estimate SHALL follow one named valuation policy, request-level Standard-tier API equivalent:

- Each model request SHALL be priced in the band set by that request's own input tokens.
- Cached input and cache-write input are both subsets of input, and reasoning output is a subset of output, so each token SHALL be priced exactly once: cached input at the cached-input rate, cache-write input at the cache-write rate, the remaining input (input minus cached input minus cache-write input) at the input rate, and output (including reasoning) at the output rate.
- When a request's evidence does not report a cache-write count, its cache-write input SHALL be taken as zero for pricing (all uncached input at the ordinary input rate), and the estimate SHALL carry the limitation `cache_writes_unobserved`. A reported cache-write count of zero is observed, not unobserved.
- Rates SHALL be Standard tier. Batch, Flex, and priority tiers are not modeled.

The band SHALL be decided from request-level evidence: the per-request usage records Codex writes to the session rollout for the attempt's own requests. Each record gives that request's input, cached input, cache-write input, and output. The estimate SHALL price every request in its own band from these records. The records SHALL reconcile with the attempt's attributed delta: the per-request sums of input, cached input, and output SHALL equal the delta exactly. When no usable request-level evidence exists (the rollout was not found, is unreadable, or predates per-request usage records), the cost SHALL be null with reason `context-band-unknown`. When the records do not reconcile, the cost SHALL be null with reason `request-evidence-mismatch`.

#### Scenario: Reasoning is not priced twice
- **WHEN** a `gpt-6.1-sol` attempt in the short band reports 50,000 output tokens, 20,000 of them reasoning
- **THEN** the output component of the estimate is 50,000 × $10.00/M = $0.50, with no separate reasoning charge

#### Scenario: Short-context attempt priced
- **WHEN** a `gpt-6-luna` attempt reports 2,000,000 input tokens (1,500,000 cached), 100,000 output tokens, and reconciling per-request records with zero cache writes and no request above 90,000 input tokens
- **THEN** its estimate is 0.115 (500,000 × $0.10/M + 1,500,000 × $0.01/M + 100,000 × $0.50/M)

#### Scenario: Long-context requests priced in their own band
- **WHEN** a `gpt-6.1-sol` attempt made two requests, one with 100,000 uncached input and 1,000 output tokens, and one with 300,000 input (200,000 cached) and 2,000 output tokens, and per-request evidence accounts for the whole attempt
- **THEN** its estimate is 0.68: the first request at short rates ($0.20 + $0.01) and the second at long rates ($0.40 + $0.04 + $0.03)

#### Scenario: Rollout predates per-request usage records
- **WHEN** a Codex step's rollout is found but contains no per-request usage records for the attempt
- **THEN** its `estimated_api_cost_usd` is null with reason `context-band-unknown`

#### Scenario: Rollout not found
- **WHEN** a Codex step's usage is collected but its rollout could not be located, so no per-request input is known
- **THEN** its `estimated_api_cost_usd` is null with reason `context-band-unknown`

#### Scenario: Per-request evidence does not reconcile
- **WHEN** the per-request records of an attempt sum to fewer input tokens than the attempt's attributed delta
- **THEN** its `estimated_api_cost_usd` is null with reason `request-evidence-mismatch`

#### Scenario: Unobserved cache writes are labeled
- **WHEN** a Codex attempt is priced and one of its per-request records has no cache-write field
- **THEN** that request's uncached input is priced at the input rate and the estimate's evidence carries the limitation `cache_writes_unobserved`

#### Scenario: Reported cache writes use the cache-write rate
- **WHEN** a short-context `gpt-6.1-sol` request reports 100,000 input tokens, 40,000 cached and 20,000 cache-write
- **THEN** its input is priced as 40,000 × $2.00/M + 40,000 × $0.10/M + 20,000 × $2.50/M = $0.134, and the estimate carries no `cache_writes_unobserved` limitation

#### Scenario: Zero cache writes are observed
- **WHEN** every per-request record reports `cache_write_input_tokens` of 0
- **THEN** the estimate carries no `cache_writes_unobserved` limitation

### Requirement: Full-attempt eligibility

The scalar `estimated_api_cost_usd` SHALL carry an estimate only when the estimate covers the whole attempt. Pricing completeness SHALL be judged separately from optional category availability: a usage record marked partial only because an optional breakdown (such as reasoning output) is absent SHALL remain eligible. Eligibility SHALL be decided by these checks, in this order; the first failing check sets the reason:

1. usage status SHALL be `collected` (otherwise `usage-unavailable`);
2. for external-user steps, every turn SHALL be attributed: no missing report, broken baseline, counter reset, or session switch (otherwise `turn-unattributed`);
3. attribution SHALL be complete: no attribution-level limitation (a reason recorded on the usage, such as an unreconstructable span) may apply to the attempt (otherwise `usage-incomplete`);
4. every category the policy needs (input, cached input, output) SHALL be present (otherwise `required-category-missing`);
5. a model SHALL be resolved (otherwise `model-unknown`) and SHALL have a table entry (otherwise `model-unpriced`);
6. request-level evidence SHALL exist (otherwise `context-band-unknown`) and SHALL reconcile with the attributed delta (otherwise `request-evidence-mismatch`).

An absent required category SHALL NOT be treated as zero. When the attempt is ineligible, `estimated_api_cost_usd` SHALL be null with that reason, and the attempt SHALL count as missing cost for coverage.

External-user steps SHALL be checked and priced per turn, before aggregation, each turn with its own model identity and its own checks 3–6. The step's estimate SHALL be the sum of its turn estimates and SHALL be eligible only when every turn is attributed and eligible; otherwise the step's reason is the first failing turn's reason. Native measurement evidence MAY retain a known subtotal of eligible turns, but only with `partial` coverage. Such a subtotal SHALL NOT become the scalar cost or count toward cost coverage.

#### Scenario: Attribution-limited usage is not priced
- **WHEN** a Codex step's usage is collected but marked partial with an attribution-level reason
- **THEN** its `estimated_api_cost_usd` is null with reason `usage-incomplete`, and the step counts as missing cost

#### Scenario: Usage partial only for a missing reasoning field is priced
- **WHEN** Codex's `turn.completed` usage reports `input_tokens`, `cached_input_tokens`, and `output_tokens` but no `reasoning_output_tokens`, so the adapter marks the record partial with no attribution reason, and the step is otherwise eligible
- **THEN** the step receives an estimate with basis `estimated`

#### Scenario: Missing required category is not zero
- **WHEN** a Codex step's `turn.completed` usage omits `cached_input_tokens` and the step is otherwise attributed completely
- **THEN** its `estimated_api_cost_usd` is null with reason `required-category-missing`, and the missing category is not priced as zero

#### Scenario: Resumed external-user turn without reasoning is priced
- **WHEN** an external-user Codex step's second turn resumes the session and its `turn.completed` usage lacks `reasoning_output_tokens`, and both turns are otherwise eligible
- **THEN** both turns are priced and the step's estimate is their sum

#### Scenario: Unavailable usage
- **WHEN** a Codex step's usage is unavailable (for example no `turn.completed` event)
- **THEN** its `estimated_api_cost_usd` is null with reason `usage-unavailable`

#### Scenario: External-user turns all eligible
- **WHEN** an external-user step on Codex has three attributed turns, each eligible, with estimates of $0.10, $0.20, and $0.30
- **THEN** the step's `estimated_api_cost_usd` is 0.60 with basis `estimated`

#### Scenario: External-user step with an unattributed turn
- **WHEN** one of four Codex turns in an external-user step has no usage report, breaking attribution for the turns after it
- **THEN** the step's `estimated_api_cost_usd` is null with reason `turn-unattributed`, the step counts as missing cost, and any subtotal kept in native evidence has `partial` coverage

#### Scenario: External-user turns on different models
- **WHEN** an external-user step's turns ran on `gpt-6.1-sol` and `gpt-6-luna`
- **THEN** each turn is priced at its own model's rates before the step's estimate is summed

### Requirement: Estimates are labeled as estimates

Every estimated cost SHALL be distinguishable from a CLI-reported cost wherever cost is recorded or shown. Recorded estimates SHALL carry cost basis `estimated`, the price-table version, the valuation policy name, and its limitations. Native measurement evidence for an estimate SHALL use origin `derived` and precision `approximate`, with a derivation naming the policy and the table version. CLI-reported costs SHALL keep origin `observed` and precision `exact`.

#### Scenario: Estimate evidence in the native measurement
- **WHEN** a Codex attempt receives an eligible estimate
- **THEN** its native measurement contains exactly one attempt-scoped cost with full coverage, established overlap, currency USD, origin `derived`, precision `approximate`, and a derivation naming the request-level Standard-tier policy and the table version

#### Scenario: Reported cost evidence unchanged
- **WHEN** a Claude step records a CLI-reported cost
- **THEN** its cost evidence keeps origin `observed`, precision `exact`, and basis `reported`
