## Context

The development audit (`internal/devaudit`, built only with the `dev_audit` tag) runs the private
`run-audit` workflow. Its stages are shell steps that call `audit-stage <name>` in-process
(`runAuditStage` in `provider_enabled.go`). Two stages launch the judge CLI. The judge is the source
run's lead agent, frozen in `request.json` as `Request.Auditor` (CLI, model, effort):

- `value-audit` → `ensureValueOutputs` → `invokeCrosscheckValueBatch` runs once per
  `ValuePackage`. It writes `model-output/<batch>.json`, then writes `value-batch-provenance.json`
  once, after the whole loop finishes. On resume, a batch whose output file exists is skipped.
- `correctness-audit` → `ensureCorrectnessOutput` → `invokeCrosscheckCorrectness` writes
  `model-output/correctness.json`. On error it writes `correctness-model-diagnostics.json` and
  **returns nil**, so the audit continues to `assemble-local-report`.

Both invokers build argv with `cli.BuildInvocationArgs` in autonomous-headless context. They wrap it
in an OS sandbox (`crosscheckCommand`, which tests can override), and run it through
`runCrosscheckOutput` → `runBoundedOutput`. That returns the raw stdout on success and on a nonzero
exit. It returns `nil` data when `Start` fails, when output exceeds 256 KiB, or when a read error
occurs. The raw stdout is used only for `auditSessionID` and the response. Usage is discarded.
`BatchProvenance` is tagged `json:"-"` on both output types.

The sandbox makes `model-output/` **writable by the judge**. Anything the judge can write there must
not be trusted as usage.

Usage extraction already exists per adapter (`cli.UsageExtractor`):
- Claude's `ExtractUsage` scans lines for a `type: "result"` object. The audit runs Claude with
  `--output-format json`, which emits that single result object. It returns `Tokens`, `TokenTotals`,
  and a session-cumulative `RawCumulativeCostUSD`.
- Codex's `ExtractUsage` returns the session-cumulative `RawCumulative` and
  `RawCumulativeTokenTotals` from the last `turn.completed` event, and no cost.

In `internal/metrics`, the run-metrics collector turns cumulative values into per-invocation values.
For a session that was not resumed, it takes the cumulative values as-is. Every judge attempt is a
fresh session.

Sheets delivery (`sheets_delivery.go`):
1. Under a per-destination local file lock, it reads `'Tab'!1:1`.
2. It requires the exact 31-column `stepValueHeader`. The API trims trailing empty cells.
3. It de-duplicates on column B, and appends rows to `A:AE` with `RAW` input.

Observation rows and `LocalReport` carry `SchemaVersion = valueSchemaVersion = "step_value_v1"`.
`RetryReport` and `MigrateReportDestination` reject any other version.

## Goals / Non-Goals

**Goals:**
- One durable usage record per launched judge attempt that reaches its exit record, written before
  any exit or response validation and before the output is treated as complete. A crash after launch
  but before the exit record is written leaves that attempt out of the summary (see Risks).
- An audit-level summary in `local-report.json`, and seven trailing judge columns in a
  `step_value_v2` worksheet, including a safe in-place upgrade from `step_value_v1`.
- Reuse the existing adapters' extraction. No new parsers and no pricing.

**Non-Goals:**
- Surfacing judge usage in `audit status`, the TUI, or source `run-metrics.json`.
- Backfilling completed audits or rewriting rows that were already delivered.
- Allocating judge cost to individual steps.

## Approach

### 1. Judge attempt ledger (`judge_usage.go`, new)

The ledger is a directory tree under `<auditSessionDir>/judge-usage/`. It sits beside
`model-output/`, outside the sandbox's writable boundary. It holds one JSON file per launched
attempt. The file's path alone identifies the attempt's stage and batch, even when the file cannot be
decoded:

```text
judge-usage/value/<batch-id>/<utc-nanos>-<rand8>.json    # batch IDs are already path-safe output filenames
judge-usage/correctness/<utc-nanos>-<rand8>.json
```

Every ledger write uses `stateio.WriteJSONDurable`. That call syncs the file and its directory, and
creates and syncs any new parent directories.

```go
type JudgeAttempt struct {
    AttemptID       string            `json:"attempt_id"`       // "<stage>/<batch>/<file stem>", derived from the path
    AuditRunID      string            `json:"audit_run_id"`     // Request.AuditRunID
    Stage           string            `json:"stage"`            // "value" | "correctness"
    BatchID         string            `json:"batch_id,omitempty"`
    CLI             string            `json:"cli"`
    Model           string            `json:"model"`            // frozen Request.Auditor.Model
    Effort          string            `json:"reasoning_effort"`
    SessionID       string            `json:"session_id"`       // auditSessionID, "unknown" if absent
    LaunchedAt      string            `json:"launched_at"`
    Outcome         string            `json:"outcome"`          // "exited" → "succeeded" | "failed"
    FailureCategory string            `json:"failure_category,omitempty"`
    Usage           model.UsageRecord `json:"usage"`
    CostUSD         *float64          `json:"estimated_api_cost_usd"`
    Legacy          bool              `json:"legacy,omitempty"` // synthesized at summary time only
}
```

The failure categories are `process_exit`, `output_unavailable` (oversize or read error),
`trusted_inputs_changed`, `response_invalid`, and `output_write_failed`.

Each attempt's ledger file is written in phases, and the same file is replaced durably each time.
The ordering relative to the output file is the durability contract:

1. **Exit record, before any validation.** It is written as soon as `runCrosscheckOutput` returns,
   provided the process started. Its outcome is `exited`, and it carries usage extracted from the
   raw stdout and the session ID. This happens before the trusted-input fingerprint comparison, the
   exit-error check, response decoding, and output writing.
2. **Failure record, when validation fails.** If any post-launch check fails inside the invoker, the
   invoker durably rewrites the record as `failed` with the matching category, then returns its
   error.
3. **Output, then success record.** On a valid response, the invoker returns the decoded output
   together with an attempt handle. The caller (`ensureValueOutputs`, `ensureCorrectnessOutput`)
   writes `model-output/<batch>.json` or `correctness.json` exactly as today. It then finalizes the
   record:
   - If the output write succeeded, the record becomes `succeeded`.
   - If the output write failed, the record becomes `failed/output_write_failed`, and the caller
     returns the write error as today.

An output is never published until its exit record is durable. So a crash or power loss at any
point leaves one of three states, all recoverable:
- There is no output. Resume reruns the batch, and the orphaned record still counts as a launched
  attempt.
- The output exists and the record is `succeeded`.
- The output exists and the record is still `exited`, because the crash hit between the output write
  and finalization.

For the third state, the summary resolves the record as described in §3. Resume does not rewrite it.

If the exit record cannot be persisted, the invoker records nothing further, returns an error, and
the caller does not write an output. This keeps the invariant "no completed output without a usage
record". Extraction failures, by contrast, only yield an unavailable `UsageRecord`, and the audit
goes on.

### 2. Launch detection and extraction

- `runBoundedOutput` / `runCrosscheckOutput` return a small result that also reports `started`,
  true once `command.Start()` succeeds. If `started` is false, no ledger record is written.
- `judgeAttemptUsage(adapter cli.Adapter, raw []byte) (model.UsageRecord, *float64)`:
  - If the adapter does not implement `cli.UsageExtractor`, the result is unavailable with reason
    `unsupported-adapter`.
  - If `raw` is nil (oversize or read error), the result is unavailable with reason `no-usage-event`.
  - If `ExtractUsage` returns an error, the result is unavailable with reason `parse-failure`.
  - For a fresh session, cumulative values map to attempt values. When `Tokens` is empty and
    `RawCumulative` is present, the helper copies `RawCumulative`→`Tokens` and
    `RawCumulativeTokenTotals`→`TokenTotals`.
  - Cost is `EstimatedCostUSD`, or else `RawCumulativeCostUSD`. The `metrics` collector's
    non-resumed branch applies the same rule. It stays a small local helper, so `devaudit` gains no
    dependency on the collector.
- Both invokers share one helper, `recordJudgeExit(request, adapter, stage, batchID, run)
  (*judgeAttemptHandle, error)`, and one helper, `(*judgeAttemptHandle).finish(outcome, category)`.
  Every early return after launch inside an invoker calls `finish(failed, <category>)`. On success,
  the invoker returns the handle, and the caller calls `finish` after its output write. Invoker
  signatures gain the handle return value. Their existing validation flow is unchanged.

### 3. Summary (`summarizeJudgeUsage`)

`assembleLocalReportStage` calls `summarizeJudgeUsage(request, prepared)`, and the report's new
field `JudgeUsage *JudgeUsageSummary \`json:"judge_usage,omitempty"\`` holds the result.
`summarizeJudgeUsage` works as follows:

1. It walks `judge-usage/`, taking stage, batch, and attempt ID from each file's path, and sorts by
   `LaunchedAt`, then `AttemptID`. A file that cannot be decoded still counts as exactly one attempt
   for its path-derived stage and batch. That attempt has usage unavailable, cost null, and outcome
   `unknown`, and the summary does not fail.
2. It groups records by (stage, batch) and resolves each group against output existence:
   - **Output exists and no `succeeded` record.** The latest record in the group that is `exited`
     or undecodable is reported as the attempt that produced the output, with outcome
     `succeeded` and `recovered: true`. Its usage is kept.
   - **Output exists and the group has no record at all.** This happens for an audit started
     before this change. The summary adds exactly one synthesized `Legacy` attempt, with usage
     unavailable and cost null.
   - **Any other record.** It is counted exactly once. An orphaned `exited` record is reported as
     `unknown`. Every group contributes each of its files exactly once, and legacy synthesis applies
     only to groups with no files.
     So a single invocation can never be counted twice.
   Nothing is written back. Re-assembly stays idempotent.
3. It aggregates:
   - `AttemptCount`.
   - Per-category `Tokens` with per-category coverage.
   - `TotalTokens` is the sum of `Usage.TokenTotals.Total` over attempts whose usage is collected
     and whose `TokenTotals` is non-nil. Its `TokenCoverage` is computed over all attempts.
   - `CostUSD` is the sum of non-nil costs, with `CostCoverage`.
   - Coverage uses `model.CoverageComplete|Partial|None`. A sum whose coverage is `none` is nil.
4. It copies `CLI`, `Model`, and `Effort` from `request.Auditor`.

The summary is built only from Runner-written ledger files and Runner-verified output existence.
Nothing the judge wrote is parsed as usage. `valueSchemaVersion` stays `step_value_v1` for local
observations and reports. `JudgeUsage` is an optional additive field, so `RetryReport` and
`MigrateReportDestination` keep working for pending reports made before this change.

### 4. Sheets projection and header upgrade

- `stepValueHeader` stays as the v1 constant. `stepValueHeaderV2` is v1 plus `judge_cli`,
  `judge_effort`, `audit_judge_attempts`, `audit_judge_total_tokens`, `audit_judge_token_coverage`,
  `audit_judge_cost_usd`, `audit_judge_cost_coverage`, making 38 columns (A–AL). The new constant
  `sheetRowSchemaVersion = "step_value_v2"` is written in each row's `schema_version` cell.
  `projectObservation` still validates `observation.SchemaVersion == valueSchemaVersion`.
- `projectObservation(observation, judge *JudgeUsageSummary)` appends the seven judge cells. When
  `judge` is nil (a pre-change report), `judge_cli` and `judge_effort` come from existing
  `observation.JudgeCLI` and the report's frozen auditor when available. The other five cells are
  empty. Coverage values are written as their strings. Nil sums are written as `""`, using the
  existing `intString` and `floatString` helpers.
- `validateHeader` becomes `ensureHeader`, which runs under the existing delivery lock:
  - An exact v2 header means proceed.
  - An exact v1 header means upgrade: `PUT values/'Tab'!AF1:AL1?valueInputOption=RAW` with the
    seven names, then proceed. The Sheets API returns row 1 with trailing empty cells trimmed, so a
    row that equals v1 exactly proves AF1 onward is empty.
  - Anything else is the error `worksheet header does not match step_value_v2`, and nothing is
    written.

  The PUT is idempotent. If its response is lost, the retry re-reads the header, sees v2, and
  proceeds.
- The append range becomes `A:AL`.

```text
value-audit ─┐                       judge-usage/<stage>/[<batch>/]<id>.json  (durable: exited → failed | succeeded)
correctness ─┴─ invoke → runCrosscheckOutput → recordJudgeExit ─► validate ─┬─► finish(failed)
                                                                             └─► caller writes output ─► finish(succeeded | output_write_failed)
assemble-local-report ─ summarizeJudgeUsage(judge-usage/ + outputs) ─► local-report.json.judge_usage
report-value-observations ─ ensureHeader(v1→v2 upgrade) ─ projectObservation(+7 judge cells) ─ append A:AL
```

## Decisions

- **Separate ledger directory, not usage embedded in the output files.** Failed attempts have no
  output, so a ledger is required regardless. Placing it outside `model-output/` keeps it beyond the
  sandbox's writable boundary, so a judge cannot forge its own usage. Per-attempt files avoid
  read-modify-write races and partial rewrites of a shared file.
- **Durable writes (`WriteJSONDurable`) for every ledger phase.** An atomic rename alone can be lost
  on power failure after a later, completed output survives. That would leave a measured attempt
  looking unavailable. Output files keep their existing atomic writes. The ordering guarantee only
  requires the ledger record to be durable before the output is published.
- **Success is finalized after the output write.** The ledger never says `succeeded` for an attempt
  whose output write failed. The crash window between the output and the success record is closed by
  the (stage, batch) path association resolved at summary time.
- **Path-encoded stage and batch.** A corrupt record can still be matched to its output. That rules
  out double counting between corrupt-record recovery and legacy synthesis.
- **Phased record: exit, then final.** Usage is captured before any validation that can return
  early, which satisfies PR-1. A durable exit record precedes the output file, and success is
  finalized after it. Together these satisfy PR-2 and AR-2.
- **Legacy outputs are synthesized at summary time.** Resumed audits that predate this change get
  honest `unavailable` coverage without the summary mutating the ledger.
- **Keep the local schema at `step_value_v1` and version only the sheet row.** The judge summary is
  additive and audit-level, and bumping the local version would strand pending reports in
  `RetryReport` and `MigrateReportDestination`. The row's `schema_version` describes the row layout,
  so it becomes `step_value_v2`.
- **Upgrade the header only from an exact v1 header.** This is a minimal, idempotent, append-only
  write, and any other header shape is left untouched.

## Risks / Trade-offs

- **Mixed-version reporters.** After one machine upgrades a tab to v2, an older binary on another
  machine sees a header mismatch. Its delivery stays pending, a non-blocking and retryable failure,
  until that binary is updated. This is acceptable for a development-only dataset, and it is called
  out in the docs.
- **Claude output shape.** Extraction relies on the single-line `--output-format json` result
  object. If Claude pretty-prints it, `ExtractUsage` returns a parse error and the attempt records
  usage as unavailable instead of failing. A unit test uses a real-shape fixture.
- **Oversize output loses usage.** `runBoundedOutput` discards the data past its limit, so usage
  from those attempts is unavailable. Such attempts are rare, and the summary shows them as
  coverage gaps.
- **Crash after launch, before the exit record.** That attempt's usage is lost. It cannot be
  recovered without transcript scraping, which is out of scope.

## Migration Plan

- No data migration is needed. Existing audits and pending reports deliver unchanged, with empty
  judge columns.
- The first v2 delivery to a v1 tab rewrites cells AF1:AL1. Rollback means deleting those header
  cells, and the rows appended afterwards, by hand in the sheet. Older binaries then deliver again.
- Update `docs/usage-and-cost-tracking.md` and `docs/development.md`. Describe the judge summary and
  the `step_value_v2` columns, state that consumers must count judge values once per
  `audit_run_id`, and note the mixed-version caveat.

## Verification

All tests run with `-tags dev_audit`, using the existing `crosscheckCommand` override and fake
Sheets HTTP server patterns.

- **`judgeAttemptUsage`:**
  - A Claude `--output-format json` fixture with `usage` and `total_cost_usd` yields tokens, totals,
    and a cost equal to the reported value.
  - Codex JSONL with `turn.completed` yields tokens and totals, with nil cost.
  - An unsupported adapter, nil raw output, and garbage output each yield the matching unavailable
    reason.
- **Durability and outcome:**
  - An output write that fails yields `failed/output_write_failed` and no output. On resume, a new
    attempt is launched and both attempts are counted.
  - A crash simulated after the output write and before finalization leaves an `exited` record
    beside the output. The summary reports that attempt once, with outcome `succeeded`,
    `recovered: true`, and its usage.
  - A corrupted `succeeded` ledger file beside its completed output counts as one attempt, with no
    extra legacy attempt.
  - Ledger writes go through `WriteJSONDurable`. Assert this with the existing `stateio` sync hook
    (`syncJSONDirectory`) or an equivalent seam. The exit record is synced before the output file
    exists.
- **Invokers (value and correctness):**
  - On success, a durable `exited` record exists when the output write runs. Check this from a
    wrapping write hook. After the output write, the record is `succeeded` and carries
    `audit_run_id`.
  - A nonzero exit with usage in stdout, and a response that fails to decode, each write a `failed`
    record that includes usage.
  - A `crosscheckCommand` whose `Start` fails writes no record.
  - A judge that writes a fake file into `model-output/` does not change the summary.
- **Resume:**
  - Batch A completes and batch B fails. The ledger holds both. On resume, A is skipped and B
    reruns, and the summary counts three attempts with A once.
  - An existing output with no ledger record yields one legacy unavailable attempt, and coverage is
    `partial`.
- **Summary:**
  - When every attempt reports, both coverages are complete.
  - An all-Codex audit has cost coverage `none` and a nil cost.
  - A failed correctness attempt with a diagnostic is still included.
  - Observation cost and token fields are identical with and without judge records.
- **Sheets:**
  - A v2 header produces 38-cell rows with `step_value_v2` and correct judge cells.
  - An exact v1 header causes a PUT of AF1:AL1, then an append to `A:AL`.
  - A v1 header with extra content causes no PUT and no append.
  - A failed PUT causes no append and leaves the report pending.
  - A pre-change report (nil `JudgeUsage`) produces empty judge totals.
  - A cost coverage of `none` produces an empty `audit_judge_cost_usd`.
- **Acceptance:** a real development-audit run with a Claude judge shows non-null judge tokens and
  cost in `local-report.json` and in the Sheet row. This is covered by the test plan and cannot be
  exercised by unit tests.
