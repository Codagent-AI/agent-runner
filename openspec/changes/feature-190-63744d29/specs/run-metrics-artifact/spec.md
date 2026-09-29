## ADDED Requirements

### Requirement: Codex rate-limit evidence on attempt records

`run-metrics.json` SHALL include each measured Codex attempt's rate-limit evidence, as defined by `codex-rate-limit-capture`, on that attempt's record: the start snapshot with its baseline provenance (or its absence), the end snapshot, all per-window deltas with availability, reasons, precision, and limitations, and an overall captured/unavailable state with reason. This applies to native agent-step attempt records and to `agent-call` records; nested Validator attempts follow the enrichment requirement below. The evidence SHALL be an optional addition that does not change the meaning of existing fields, and SHALL NOT by itself increment the artifact schema version. Non-Codex records SHALL omit it. Each re-executed Codex attempt SHALL carry its own evidence.

#### Scenario: Fixture attempt in the artifact
- **WHEN** a Codex agent step runs against a fixture session whose rate-limit state shows the primary window moving from 40% to 43% within the same window with a `same-thread` baseline
- **THEN** that step's record in `run-metrics.json` shows the start at 40%, the end at 43%, and an available, approximate primary delta of 3 percentage points

#### Scenario: Unavailable evidence is explicit
- **WHEN** a Codex agent step's rate-limit evidence cannot be captured
- **THEN** its record shows the evidence as unavailable with a reason, and no zero delta

#### Scenario: Existing consumers unaffected
- **WHEN** a consumer that ignores unknown fields reads an artifact containing rate-limit evidence
- **THEN** every pre-existing field has the same value and meaning it would have without the evidence, and the schema version is unchanged

#### Scenario: Retried step keeps separate evidence
- **WHEN** a Codex step fails and is executed again in the same run
- **THEN** each attempt record carries its own rate-limit evidence

### Requirement: Run-level Codex rate-limit rollup

The run-level aggregate SHALL include a Codex rate-limit rollup covering all execution sessions of the run. Rollup arithmetic SHALL only combine values that belong to one account's one quota window. Each captured attempt SHALL carry an opaque account scope: a run-local identifier derived from the Codex account identity that is stable across resume within the run, and from which the account identity cannot be recovered. When Codex's session metadata does not establish the account, the attempt's account scope SHALL be `unverified`. Raw account identifiers MUST NOT appear in the artifact.

The rollup SHALL contain one window group per distinct combination of account scope, limit identity, window role (primary or secondary), window length, and reset time, taken from each attempt's end snapshot. Each group SHALL report:

- the sum of available per-attempt deltas for that window, with the number of contributing attempts;
- the run-span change from the group's earliest start snapshot to its latest end snapshot (both in that same window by construction), unavailable with reason `no-baseline` when no attempt in the group has a start snapshot for that window;
- for the `unverified` account scope, the sum and run span SHALL both be unavailable with reason `account-unverified`, and only the contributing-attempt count is reported, because those attempts cannot be shown to share one account.

The rollup SHALL NOT report any total that combines different account scopes or different reset windows. Coverage SHALL be reported per window role across the whole run: the number of measured Codex attempts, how many have an available delta for that role, and a `complete`/`partial`/`none` indicator. When measured Codex attempts in a group overlapped in time, that group's sum SHALL carry limitation `overlapping-attempts`, because account-wide deltas of concurrent attempts count shared consumption more than once. The rollup SHALL be absent when the run has no measured Codex attempts. Unavailable per-attempt deltas SHALL contribute nothing to sums and SHALL reduce coverage, never add zero.

#### Scenario: Two sequential Codex steps
- **WHEN** a run has two sequential Codex steps on the same account with available primary deltas of 3 and 2 percentage points in the same window
- **THEN** a single primary window group reports a sum of 5 from 2 contributing attempts and an available run-span change, and primary coverage is complete

#### Scenario: One attempt without a delta
- **WHEN** a run has three measured Codex attempts and one has an unavailable primary delta
- **THEN** the primary group's sum includes only the two available deltas, and primary coverage is partial with 2 of 3

#### Scenario: Window reset during the run
- **WHEN** the primary window resets between the run's first and last Codex attempts
- **THEN** the rollup has separate primary groups for the two reset windows, each with its own sum and run span, and no total combines them

#### Scenario: Account switch during a resumed run
- **WHEN** a run executes a Codex step on one account, is interrupted, and is resumed on a different Codex account with the same limit identity and reset schedule
- **THEN** the two attempts fall into different account-scope groups, no sum or run span combines them, and neither group exposes a raw account identifier

#### Scenario: Account not established
- **WHEN** a captured attempt's Codex session metadata does not identify the account
- **THEN** it falls into the `unverified` account scope group, whose sum and run span are unavailable with reason `account-unverified`

#### Scenario: Parallel Codex attempts
- **WHEN** two Codex attempts in the same window group overlap in time
- **THEN** that group's sum carries limitation `overlapping-attempts`

#### Scenario: Resumed run rollup
- **WHEN** a run executes a Codex step, is interrupted, and is resumed to execute another Codex step on the same account and window
- **THEN** both attempts contribute to the same window group

#### Scenario: Run without Codex
- **WHEN** a run uses no Codex attempts
- **THEN** the run-level aggregate contains no Codex rate-limit rollup

### Requirement: Durable rate-limit enrichment for nested Validator attempts

Rate-limit evidence for nested Validator Codex attempts SHALL be stored as Runner-owned enrichment that is separate from the accepted producer record, keyed by the producer store and the model-attempt identity. Accepted producer records MUST NOT be modified to carry it. Each time Runner refreshes, re-imports, revises, or resumes, it SHALL rebuild the nested attempt's compatibility view and the run rollup from the current authoritative head combined with the enrichment, so the evidence survives. When the current head's attempt identity or lifecycle bounds no longer match those the enrichment was captured against, Runner SHALL recapture the enrichment against the current head when the revision is imported, and the old evidence MUST NOT be applied to the revised head. A recapture whose read fails SHALL replace the enrichment with the read's own unavailable reason (for example `session-log-unavailable` or `unparseable`), bound to the revised head. When Runner has no means to read Codex session state at import time, the enrichment is left unchanged and, because its bounds no longer match the current head, the attempt's evidence SHALL be shown as unavailable with reason `stale-enrichment`. When the current head is conflicting, its rate-limit evidence SHALL be shown as unavailable with reason `excluded-measurement` and excluded from the rollup's sums while still counting as a measured attempt in coverage. A head that Runner does not project at all (an unsupported measurement version in the current scope) SHALL carry no rate-limit evidence and SHALL NOT count toward rate-limit coverage, consistent with its existing exclusion from metrics. A nested Codex attempt whose accepted record does not identify its Codex provider session SHALL show evidence unavailable with reason `session-unidentified`.

#### Scenario: Evidence survives projection refresh
- **WHEN** a nested Validator Codex attempt has captured rate-limit evidence and Runner later imports another record, triggering a refresh of nested projections
- **THEN** the attempt's rate-limit evidence is still present in `run-metrics.json` and still counts in the rollup

#### Scenario: Producer record untouched
- **WHEN** Runner captures rate-limit evidence for a nested Validator attempt
- **THEN** the retained accepted producer record is byte-for-byte unchanged

#### Scenario: Completion revision with changed bounds
- **WHEN** evidence was captured against a running revision and a completion revision for the same attempt arrives with a later end time
- **THEN** the evidence is recaptured against the new bounds when the revision is imported, and the old evidence is never applied to the new head

#### Scenario: Recapture read fails
- **WHEN** a revised head's bounds differ from its enrichment and the recapture finds the Codex session log missing
- **THEN** the attempt's evidence is unavailable with reason `session-log-unavailable`, and the earlier captured values are not shown

#### Scenario: No reader available at revision import
- **WHEN** a run's artifact is loaded by a Runner process that cannot read Codex session state, and a revision with changed bounds is then imported
- **THEN** the attempt's evidence is unavailable with reason `stale-enrichment`

#### Scenario: Unsupported head not projected
- **WHEN** a nested attempt's current head has an unsupported measurement version in the current scope
- **THEN** the attempt has no rate-limit evidence in `run-metrics.json` and is not counted in rate-limit coverage

#### Scenario: Validator record without provider session
- **WHEN** an accepted nested Codex attempt record carries no Codex provider session identity
- **THEN** its evidence is unavailable with reason `session-unidentified`, and it counts in rollup coverage as a measured attempt without a delta

#### Scenario: Evidence survives resume
- **WHEN** a run with enriched nested Validator attempts is interrupted and resumed
- **THEN** the enrichment is still present and applied after resume

#### Scenario: Conflicting head excluded
- **WHEN** a nested attempt's current head is conflicting and is excluded from numeric aggregation
- **THEN** its rate-limit evidence is unavailable with reason `excluded-measurement`, its delta does not contribute to the rollup, and the rollup's coverage reflects the exclusion
