# Decisions

## propose

1. **Verdict: go.** Judge usage is unrecorded yet material (~$30–60/week); adapters already extract usage, so the change is small and additive.
   - Alternatives: no-go (rely on provider billing dashboards, which cannot attribute cost to audits).
   - Decision-bearing: yes.

2. **Scope covers every judge invocation (all value batches + correctness), aggregated per audit.**
   - Alternatives: record only the correctness pass; record only the first invocation.
   - Decision-bearing: yes.

3. **Reuse existing `cli.UsageExtractor` adapters; no new parser, no token-based pricing.** Codex judges get tokens with null cost, per `cost-capture`.
   - Alternatives: bespoke Claude/Codex parsers in devaudit; estimating cost from a price table (prohibited by `cost-capture`).
   - Decision-bearing: no (follows existing specs).

4. **Sheet delivery: append audit-level judge columns (repeated on each step row of that audit) under a new header version.** The issue asks for judge usage in "the Google Sheet row the audit delivers", but rows are per step observation. Repeated per-audit columns, named as per-audit, satisfy the issue without inventing a per-step allocation.
   - Alternatives: split judge cost across step rows (invents an allocation); separate audit-summary tab (second destination and header to validate, set up, and keep retry-safe).
   - Decision-bearing: yes.

5. **Upgrade an exact `step_value_v1` header in place by writing only the new trailing header cells; any other mismatch still blocks without modification.** This avoids a manual, out-of-repo sheet edit before acceptance can pass, and it does not reorder or alter existing data.
   - Alternatives: operator manually edits the header (external step); accept both headers and silently omit judge data on v1 tabs (acceptance could not pass without manual action).
   - Decision-bearing: yes.

6. **Judge usage stays out of the source run's `run-metrics.json` and source-run cost totals.**
   - Alternatives: fold audit cost into source-run metrics (mixes audit overhead with workflow cost).
   - Decision-bearing: no.

7. **Extraction failure never fails the audit; usage is marked unavailable (null, not zero).**
   - Alternatives: fail the stage on extraction error.
   - Decision-bearing: no (matches `agent-usage-collection`).

## proposal-review

- **PR-1 (structural): applied.** Accounting unit changed from successful invocation to launched attempt. Usage is extracted from stdout as soon as the process exits, before exit-status or response checks. Failed attempts and retries are recorded and aggregated, and attempts with unavailable usage or cost count in the coverage denominator. This matches `agent-usage-collection` and `cost-capture` behavior for failed steps.
  - Alternatives: success-only accounting (understates cost and overstates coverage).
  - Decision-bearing: yes.
- **PR-2 (significant): applied.** Verified that `ModelValueBatch.Provenance` and `CorrectnessCandidates.Provenance` are `json:"-"`, and that `value-batch-provenance.json` is written only after the whole batch loop. The proposal now requires each attempt's usage record to be persisted atomically before its output counts as complete. A resumed output without a usage record is recorded as usage-unavailable, not complete. Design chooses between a separate ledger and an atomic combined output record.
  - Alternatives: keep the end-of-loop provenance file (loses usage when a crash or a later batch failure happens before that write).
  - Decision-bearing: yes.

## spec

- **Sheet judge columns fixed as `judge_cli`, `judge_effort`, `audit_judge_attempts`, `audit_judge_total_tokens`, `audit_judge_token_coverage`, `audit_judge_cost_usd`, `audit_judge_cost_coverage`, appended after v1 under header `step_value_v2`. Rows carry `step_value_v2`.** Per-category token detail stays local, because the adapters' categories differ (Codex input includes cached tokens; Claude input does not).
  - Alternatives: per-category token columns in the sheet; a single usage column without coverage.
  - Decision-bearing: yes.
- **Aggregates follow the `cost-capture` run-level pattern:** sum over the reporting attempts, with separate token and cost coverage, and null when coverage is `none`. The alternative, keeping the scalar unknown unless complete as with step-level `total_tokens`, was rejected because explicit coverage columns make partial sums unambiguous.
  - Decision-bearing: yes.
- **Header upgrade only when the header is exactly v1 and every header-row cell to its right is empty.** Otherwise it is a mismatch with no modification. The upgrade and append happen under the existing delivery lock. An upgrade write failure is a retryable, non-blocking reporting failure.
  - Alternatives: overwrite trailing cells; upgrade any prefix-compatible header.
  - Decision-bearing: no.
- **Attempts that fail before process launch produce no record and are excluded from coverage**, mirroring `cost-capture`'s "never invoked" rule.
  - Decision-bearing: no.
- **Pending reports assembled before this change still deliver to v2 tabs, with judge columns empty.**
  - Alternatives: reject old reports (would strand pending deliveries).
  - Decision-bearing: no.
- **Judge usage records persist even when the value stage fails and no local report is assembled.** Surfacing them in `audit status` stays out of scope.
  - Decision-bearing: no.
- **Storage layout (ledger vs combined output record) is deferred to design**, marked in the spec.
  - Decision-bearing: no.

## design

- **Per-attempt ledger at `<auditSessionDir>/judge-usage/<attempt-id>.json`, outside the sandbox-writable `model-output/`**. Each file is written in two phases, an exit record and then a final record, both before the output is written. This resolves the deferred storage choice. The spec gained a requirement that the judge cannot forge usage.
  - Alternatives: usage embedded in each output file (failed attempts have no output, and the files are judge-writable); one shared ledger file (read-modify-write races).
  - Decision-bearing: yes.
- **Legacy outputs without records are synthesized as unavailable attempts at summary time, not written to the ledger.**
  - Decision-bearing: no.
- **Local observation and report schema stays `step_value_v1`; the new constant `step_value_v2` versions only sheet rows.** This keeps `RetryReport` and `MigrateReportDestination` working for pending reports.
  - Alternatives: bumping the local report version (strands pending reports).
  - Decision-bearing: yes.
- **Header upgrade is a single idempotent PUT to AF1:AL1, done only when row 1 equals v1 exactly.** The API trims trailing blanks, so an exact match proves the target cells are empty.
  - Decision-bearing: no.
- **A failure to persist the exit record fails the invocation, so no output can exist without a usage record. Extraction failures only mark usage unavailable.**
  - Decision-bearing: no.
- **Fresh-session normalization, cumulative taken as the attempt's values, lives in a local devaudit helper that mirrors the metrics collector's non-resumed branch.** No dependency on `internal/metrics` is added.
  - Decision-bearing: no.

## test-plan

- **Three INT obligations and one E2E.** The INTs cover the subprocess→ledger ordering, value-stage resume and assembly, and the Sheets HTTP contract. The E2E extends the existing tagged-CLI fixture with a v1→v2 upgrade for Claude and Codex judges. Everything else stays at the unit level.
  - Alternatives: a separate E2E per failure mode (duplicates the INT coverage).
  - Decision-bearing: no.
- **The acceptance envelope authorizes up to two real audit replays (~$0.40–$1.00 each) and the one-time real header upgrade (cells AF1:AL1) plus row delivery to the configured tab.** The issue's acceptance item 2 explicitly requires a real audit Sheet row.
  - Alternatives: fixture-only acceptance (cannot satisfy the issue); an unlimited number of real audits.
  - Decision-bearing: yes.
- **Real `[auto-audit]` issues filed by the correctness stage during acceptance are treated as normal product behavior, not a forbidden effect.**
  - Decision-bearing: no.
- **Human-only testing: none.** All checks are agent-executable with the existing credentials.
  - Decision-bearing: no.

## approach-review

- **AR-1 (high): applied.** Every ledger phase uses `stateio.WriteJSONDurable`, which syncs the file and its directory. The durable exit record precedes output publication. Tests were added to design verification and to test-plan INT-001(h).
  - Alternatives: narrowing the spec to exclude power-loss durability (rejected, because the spec promises recoverable usage).
  - Decision-bearing: yes.
- **AR-2 (high): applied.** The record now goes exited (durable) → validation failure → `failed`, or the caller writes the output → `succeeded`, or `failed/output_write_failed` if the write fails. An `exited` record found beside a completed output after a crash is resolved at summary time as `succeeded (recovered)` and counted once. Tests were added to INT-001(g) and INT-002.
  - Decision-bearing: yes.
- **AR-3 (medium): applied.** Ledger paths encode stage and batch (`judge-usage/value/<batch>/…`, `judge-usage/correctness/…`). A corrupt file counts once, for its path-derived group. Legacy synthesis applies only to output groups that have no ledger file at all, which rules out double counting. The corrupt-record case was added to INT-002.
  - Alternatives: persisting the attempt ID in the output files (the outputs sit in the judge-writable directory, and the change to strict decoding is riskier).
  - Decision-bearing: no.
- **AR-4 (low): applied.** `AuditRunID` was added to `JudgeAttempt`, populated from `Request.AuditRunID`, and is asserted in INT-001(a).
  - Decision-bearing: no.
- AR-2 follow-up: updated the `audit-judge-usage` outcome contract to include `unknown`, for interruptions with no completed output, and to state that an output-write failure is a failure.

## tasks

- **One implementation task covering the whole change**, as instructed. It links every artifact and follows the format of archived factory changes.
  - Decision-bearing: no.
