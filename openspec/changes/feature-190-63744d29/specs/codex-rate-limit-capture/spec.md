## ADDED Requirements

### Requirement: Rate-limit evidence for every measured Codex attempt

Agent Runner SHALL attempt to capture Codex usage-limit (rate-limit) evidence for every Codex attempt it measures: headless Codex agent steps, interactive and autonomous-interactive Codex agent steps whose Codex session is identified, Codex agent calls, and Codex model attempts reported in declared Agent Validator nested records. Each such attempt SHALL carry rate-limit evidence that is either captured or explicitly unavailable with a reason. Attempts that do not run Codex (other CLIs, shell, UI, and other non-agent steps) SHALL carry no Codex rate-limit evidence and SHALL NOT count toward rate-limit coverage.

#### Scenario: Headless Codex step records evidence
- **WHEN** a headless Codex agent step completes and Codex reported rate-limit state during the attempt
- **THEN** the step's attempt record carries captured Codex rate-limit evidence

#### Scenario: Agent call records its own evidence
- **WHEN** a workflow step whose parent agent is any CLI calls a Codex agent through `call_agent` and the call completes
- **THEN** the `agent-call` record carries the called Codex attempt's rate-limit evidence, and the parent step's record does not include it

#### Scenario: Interactive Codex step with discovered session
- **WHEN** an interactive Codex agent step ends and its Codex session is identified
- **THEN** the step's attempt record carries rate-limit evidence captured from that session

#### Scenario: Interactive Codex step without discovered session
- **WHEN** an interactive Codex agent step ends and its Codex session cannot be identified
- **THEN** the step's rate-limit evidence is unavailable with reason `session-unidentified`

#### Scenario: Nested Validator Codex attempt
- **WHEN** Runner accepts a declared Agent Validator model-attempt record for a Codex model whose provider session identifies a local Codex session
- **THEN** that nested attempt carries rate-limit evidence captured from that session within the attempt's lifecycle bounds

#### Scenario: Non-Codex step carries no evidence
- **WHEN** a Claude agent step or a shell step completes
- **THEN** its record carries no Codex rate-limit evidence and is excluded from rate-limit coverage

### Requirement: Rate-limit evidence comes only from Codex-reported state

Rate-limit snapshots SHALL be taken only from the usage-limit state that Codex itself records for the session (the `rate_limits` block of its token-count events). Agent Runner MUST NOT estimate usage-limit consumption from token counts, cost, or model identity, and MUST NOT query external account services. When the headless structured stdout carries no rate-limit state, that absence alone SHALL NOT make evidence unavailable if the session's own record provides it.

#### Scenario: Fixture event stream provides rate limits
- **WHEN** a Codex attempt's session record contains token-count events with `rate_limits` showing the primary window at 40% before launch and 43% at the attempt's last event, in the same window
- **THEN** the attempt records a start of 40%, an end of 43%, and a primary-window delta of 3 percentage points

#### Scenario: No token counts are converted
- **WHEN** a Codex attempt reports token usage but its session contains no rate-limit state
- **THEN** its rate-limit evidence is unavailable with reason `no-snapshots`, and no percentage is derived from its tokens

### Requirement: Snapshot content

Each recorded rate-limit snapshot SHALL include, as reported by Codex: the observation time, the limit identity and plan type when reported, and for each of the primary and secondary windows the `used_percent`, window length in minutes, and reset time. A window Codex does not report in a snapshot SHALL be recorded as not reported, never as zero usage. Values SHALL be preserved as reported, without rounding. Captured evidence SHALL also carry the opaque, run-local account scope defined by `run-metrics-artifact` (or `unverified` when Codex's session metadata does not identify the account); the raw account identity MUST NOT be recorded.

#### Scenario: Secondary window not reported
- **WHEN** Codex reports a primary window and reports the secondary window as null
- **THEN** the snapshot records the primary window's values and records the secondary window as not reported rather than 0%

#### Scenario: Account scope without raw identity
- **WHEN** a Codex session's metadata identifies its account
- **THEN** the attempt's evidence carries an opaque account scope, and no raw account identifier appears in the evidence

#### Scenario: Fractional percentages preserved
- **WHEN** Codex reports `used_percent` of 52.5
- **THEN** the snapshot records 52.5

### Requirement: Attempt-bounded snapshots

The end snapshot and all in-attempt observations SHALL come only from rate-limit state Codex recorded during the attempt. When an attempt resumes an existing Codex thread, state that thread recorded before the attempt launched MUST NOT be treated as in-attempt evidence. The end snapshot SHALL be the last rate-limit state recorded during the attempt.

#### Scenario: Resumed thread excludes earlier turns
- **WHEN** a Codex agent step resumes a thread that recorded rate-limit state in an earlier step, then records new state during this attempt
- **THEN** the attempt's end snapshot is the last state recorded during this attempt, and the earlier step's state is never used as this attempt's end

#### Scenario: Multiple requests in one attempt
- **WHEN** Codex records rate-limit state after each of three model requests in one attempt
- **THEN** the end snapshot is the state recorded after the third request

#### Scenario: Nested attempt bounded by lifecycle
- **WHEN** a nested Validator Codex session contains rate-limit state from before its accepted record's start time or after its end time
- **THEN** only state recorded at or after the record's start time and no later than 2 seconds after its end time counts as in-attempt evidence

### Requirement: Pre-launch start baseline with provenance

The start snapshot SHALL be the rate-limit state from before the attempt's first model request, and SHALL record which baseline was used. Qualifying baselines, in order of preference:

1. `same-thread`: for a resumed thread, the last rate-limit state that thread recorded before the attempt launched.
2. `cross-thread`: the most recent rate-limit state recorded before launch by another local Codex session of the same Codex account (as identified by Codex's session metadata), with the same limit identity and plan type as the attempt's end snapshot, and no older than 10 minutes before launch. It serves as the baseline for a window only when its window length and reset time for that window match the end snapshot's. A candidate session whose account cannot be established SHALL NOT be used. Account identity SHALL be used only for matching and MUST NOT be recorded.

The first rate-limit state recorded inside the attempt already includes that attempt's first request and MUST NOT be used as a start baseline. When no baseline qualifies, the in-attempt state SHALL still be recorded as end-only evidence, and the delta SHALL be unavailable with reason `no-baseline`.

#### Scenario: Resumed thread uses same-thread baseline
- **WHEN** a Codex step resumes a thread whose last pre-launch rate-limit state showed 40% in the current window
- **THEN** the start snapshot is that state with baseline provenance `same-thread`

#### Scenario: Fresh thread uses a fresh matching cross-thread baseline
- **WHEN** a fresh Codex thread's end snapshot is in a window whose limit, plan, window length, and reset time match another local session's recent pre-launch state within the freshness bound
- **THEN** the start snapshot is that state with baseline provenance `cross-thread`, and the unobserved interval between that state and launch is recorded

#### Scenario: Cross-thread candidate from another account
- **WHEN** the only recent pre-launch state from another local session belongs to a different Codex account, or its account cannot be established
- **THEN** it is not used as a baseline, and the delta is unavailable with reason `no-baseline`

#### Scenario: Cross-thread candidate in a different window
- **WHEN** the only pre-launch state from another session has a different reset time than the attempt's end snapshot
- **THEN** it is not used as a baseline, and the delta is unavailable with reason `no-baseline`

#### Scenario: Cross-thread candidate too old
- **WHEN** the only matching pre-launch state from another session was recorded more than 10 minutes before launch
- **THEN** it is not used as a baseline, and the delta is unavailable with reason `no-baseline`

#### Scenario: Single-request fresh attempt without baseline
- **WHEN** a fresh Codex thread records rate-limit state once during the attempt and no baseline qualifies
- **THEN** the attempt records that state as an end-only observation, and the delta is unavailable with reason `no-baseline`, not 0

### Requirement: Per-window delta

For each window, the delta SHALL be the end snapshot's `used_percent` minus the start snapshot's `used_percent`, in percentage points, computed only when both snapshots report that window with the same limit identity, window length, and reset time. Otherwise the delta for that window SHALL be unavailable with a reason: `no-baseline` (no qualifying start), `window-reset` (the reset time or window length differs), `window-not-reported` (either endpoint does not report the window), or `inconsistent` (usage decreased within the same window). An unavailable delta MUST NOT be reported as zero. An observed zero delta within the same window SHALL be reported as an available 0.

#### Scenario: Delta within the same window
- **WHEN** start and end snapshots report the primary window with the same reset time at 40% and 43%
- **THEN** the primary delta is 3 percentage points and available

#### Scenario: Window reset during the attempt
- **WHEN** the start snapshot's primary window reset time differs from the end snapshot's
- **THEN** both snapshots are recorded, and the primary delta is unavailable with reason `window-reset`

#### Scenario: Secondary window missing at one endpoint
- **WHEN** the start snapshot reports a secondary window and the end snapshot does not
- **THEN** the secondary delta is unavailable with reason `window-not-reported`, and the primary delta is unaffected

#### Scenario: Usage decreased in the same window
- **WHEN** start and end report the same window and reset time but the end `used_percent` is lower
- **THEN** the delta is unavailable with reason `inconsistent`

#### Scenario: Observed zero delta
- **WHEN** start and end report the same window at 52%
- **THEN** the delta is an available 0

### Requirement: Stated precision and limitations

Every available delta SHALL be marked approximate and SHALL list its applicable limitations: `account-wide` (always: usage from any other Codex session on the same account during the interval, including parallel steps in the same run, is included), `coarse-precision` (always: Codex's reported percentage granularity can hide small consumption), and `unobserved-gap` for a `cross-thread` baseline or a `same-thread` baseline older than 10 minutes. The latter two cases SHALL include the interval between the baseline observation and the attempt's launch.

#### Scenario: Same-thread delta limitations
- **WHEN** an attempt's delta uses a `same-thread` baseline
- **THEN** the delta is marked approximate with limitations `account-wide` and `coarse-precision`

#### Scenario: Old same-thread baseline
- **WHEN** the last pre-launch rate-limit state in a resumed thread was recorded more than 10 minutes before launch
- **THEN** each available delta also carries limitation `unobserved-gap` and the evidence records the gap in milliseconds

#### Scenario: Cross-thread delta limitations
- **WHEN** an attempt's delta uses a `cross-thread` baseline observed 90 seconds before launch
- **THEN** the delta is marked approximate with limitations `account-wide`, `coarse-precision`, and `unobserved-gap` with a 90-second interval

### Requirement: Capture failures are isolated

Failure to locate, read, or parse a Codex session's rate-limit state SHALL produce explicit unavailable evidence with a reason (`session-log-unavailable`, `no-snapshots`, or `unparseable`) and MUST NOT change the attempt's outcome, exit handling, usage record, cost, or the run's progress. Unrecognized fields in Codex's rate-limit state SHALL be ignored. A malformed individual event SHALL be skipped without discarding valid snapshots from the same session.

#### Scenario: Session log missing
- **WHEN** a headless Codex step completes successfully but its session log cannot be found
- **THEN** the step still succeeds with its usage and cost unchanged, and its rate-limit evidence is unavailable with reason `session-log-unavailable`

#### Scenario: One malformed event among valid ones
- **WHEN** an attempt's session contains one malformed token-count event and later valid rate-limit state
- **THEN** the valid state is used and the attempt's evidence is captured

#### Scenario: Format drift
- **WHEN** Codex records rate-limit state in a shape Runner cannot interpret
- **THEN** the evidence is unavailable with reason `unparseable` and the step outcome is unaffected
