# audit-judge-usage Specification

## Purpose
TBD - created by archiving change feature-189-41a4e7f1. Update Purpose after archive.
## Requirements
### Requirement: Every launched judge attempt records usage

In a development-audit build, the audit SHALL produce exactly one judge usage record for each launched judge CLI attempt. This covers every value-batch invocation and every correctness invocation, including relaunches of a stage after an earlier failure or resume. A judge attempt is launched when the judge CLI process has been started. An invocation that fails before its process starts, such as an argument-building or workspace-preparation error, SHALL NOT produce a judge usage record.

Each record SHALL identify the audit run, the stage (`value` with its batch identity, or `correctness`), the judge CLI, the resolved judge model and reasoning effort frozen in the audit request, the judge CLI session identity when known, and the attempt's outcome: succeeded; failed with a non-secret reason category; or unknown when an interruption prevented the final outcome from being recorded and no completed output establishes success. An attempt whose valid response could not be written as the judge output SHALL be recorded as failed.

#### Scenario: Value batches and correctness each record usage
- **WHEN** an audit launches two value-batch judge attempts and one correctness judge attempt, and all succeed
- **THEN** the audit has three judge usage records, one per attempt, each with its stage, batch identity where applicable, judge CLI, resolved model, and effort

#### Scenario: Invocation fails before launch
- **WHEN** a judge invocation fails while building its CLI arguments, before any process starts
- **THEN** no judge usage record is produced for that invocation

#### Scenario: Stage is relaunched after failure
- **WHEN** a value-batch judge attempt fails and a later resume of the audit launches that batch again
- **THEN** the audit has one judge usage record for the failed attempt and one for the relaunched attempt

### Requirement: Usage is extracted from judge output regardless of outcome

After a launched judge CLI process exits, the audit SHALL extract usage from that process's captured output using the judge CLI's existing usage extraction. It SHALL do this before judging whether the exit status, structured response, or decoded judgment is valid. Usage and cost extracted from a failed attempt SHALL be retained and included in audit-level aggregates. The usage record SHALL use the canonical token categories and availability semantics of `agent-usage-collection`, and the USD cost semantics of `cost-capture`. Cost is recorded only when the judge CLI reports it in USD, and is never computed from tokens. Each judge attempt starts a fresh CLI session, so a session-cumulative reported cost SHALL be attributed to that attempt in full.

When usage cannot be extracted, because the adapter does not support extraction, the output contains no usage, or the output cannot be parsed, the record SHALL mark usage unavailable with a reason, and cost SHALL be null. Extraction failure SHALL NOT fail the judge attempt, the audit stage, or the audit.

#### Scenario: Claude judge reports usage and cost
- **WHEN** a Claude judge attempt's JSON result reports token usage and `total_cost_usd`
- **THEN** its judge usage record contains the reported token categories and a cost equal to the reported `total_cost_usd`

#### Scenario: Codex judge reports usage without cost
- **WHEN** a Codex judge attempt's output contains `turn.completed` events with usage
- **THEN** its judge usage record contains the reported token categories, and its cost is null rather than zero or an estimate

#### Scenario: Failed attempt still reports usage
- **WHEN** a judge attempt exits nonzero, or returns a missing, invalid, or undecodable structured response, and its output contains valid usage
- **THEN** its judge usage record contains that usage and any reported USD cost, marks the attempt failed, and counts toward audit-level aggregates

#### Scenario: Usage cannot be extracted
- **WHEN** a judge attempt's output contains no parseable usage
- **THEN** its judge usage record marks usage unavailable with a reason, its cost is null, and the audit continues as it would have without usage recording

### Requirement: Judge usage is durable before judge output completes

Each judge usage record SHALL be persisted durably in the audit run's artifacts before the corresponding judge output is treated as complete. A value-batch or correctness output that an audit resume skips SHALL therefore always have its usage recoverable. A failed attempt's record SHALL persist even though that attempt produces no judge output. Records SHALL have stable attempt identity, so re-reading them on resume or report assembly counts each attempt exactly once.

When a resumed audit finds a completed judge output with no associated usage record, as with an audit started before this capability existed, it SHALL count that output as one launched attempt with usage unavailable. It SHALL NOT treat the output's usage as complete, and SHALL NOT omit it from coverage.

Judge usage records SHALL be written only by Agent Runner, and stored outside any location the sandboxed judge can write. Content the judge writes SHALL NOT be read as usage.

#### Scenario: Audit resumes after a completed batch
- **WHEN** an audit completes value batch A, is interrupted before finishing batch B, and is resumed
- **THEN** batch A is not rerun, and the final judge usage includes batch A's original attempt record exactly once

#### Scenario: Output exists without a usage record
- **WHEN** a resumed audit skips a value-batch output that has no associated judge usage record
- **THEN** the audit-level summary counts that output as a launched attempt with usage and cost unavailable

#### Scenario: Judge writes a forged usage file
- **WHEN** a judge attempt writes a file resembling a usage record into its writable output location
- **THEN** the audit's judge usage records and summary are unaffected by that file

#### Scenario: Later batch fails
- **WHEN** value batch A succeeds and value batch B's attempt fails, ending the value stage
- **THEN** judge usage records for batch A's attempt and batch B's failed attempt both remain in the audit run's artifacts

### Requirement: Audit-level judge usage summary

The audit's local report SHALL include an audit-level judge usage summary containing:
- the judge CLI, resolved model, and reasoning effort
- every judge usage record for the audit, failed attempts included
- the count of launched attempts
- aggregate token counts per canonical category
- an aggregate total token count
- an aggregate USD cost

Each aggregate SHALL carry an explicit coverage indicator over the launched attempts: `complete` when every launched attempt reported the metric, `partial` when some did, and `none` when none did. Aggregate token counts and cost SHALL be the sum over the attempts that reported them. They SHALL be null, never zero, when coverage is `none`, including when the audit launched no judge attempt.

When the value stage fails and no local report is assembled, the judge usage records SHALL still remain in the audit run's artifacts.

#### Scenario: All attempts reported cost
- **WHEN** every judge attempt in an audit reported token usage and a USD cost
- **THEN** the summary's total tokens and cost are their sums and both coverages are `complete`

#### Scenario: One attempt lacks usage
- **WHEN** an audit launched three judge attempts and one of them has usage unavailable
- **THEN** the summary's aggregates sum the two reporting attempts and their coverage is `partial`

#### Scenario: Codex judge audit
- **WHEN** every judge attempt in an audit ran on Codex and reported tokens but no USD cost
- **THEN** token coverage is `complete`, cost coverage is `none`, and the aggregate cost is null

#### Scenario: Correctness attempt failed but report is assembled
- **WHEN** the correctness judge attempt fails with extractable usage, and the audit records a correctness diagnostic and assembles its local report
- **THEN** the summary includes the failed correctness attempt's usage and cost

### Requirement: Judge usage stays separate from source-run metrics

Judge usage SHALL be recorded only in the audit run's artifacts and in the value dataset's audit-level judge columns. It SHALL NOT be added to the source run's `run-metrics.json`, the source run's cost or token totals, or any step observation's source `duration_ms`, `cost_usd`, or `total_tokens`.

#### Scenario: Source metrics unchanged by audit
- **WHEN** an audit of a source run completes with judge usage and cost recorded
- **THEN** the source run's metrics artifact and each step observation's source cost and token fields are unchanged by the judge's usage

