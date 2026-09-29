## Why

The development audit (`agent-runner audit`, `internal/devaudit`) runs an agent judge for every eligible
top-level OpenSpec or spec-driven run, but none of that judge's usage is recorded. The judge is the
source run's lead agent (`resolveAuditor` in `internal/devaudit/lifecycle.go`), so fix and feature runs
are audited by Opus at high effort. Each audit starts one fresh CLI session per value batch
(`invokeCrosscheckValueBatch`) and one for correctness (`invokeCrosscheckCorrectness`). Both parse the
CLI's structured result only for the response and session ID, and drop the usage and reported cost.

In the Sep 21–28 analysis, 81 audits took about 242 minutes, which works out to an estimated $30–60 of
Claude usage. None of it shows up in audit artifacts, run metrics, or the value dataset. The audit
exists to measure what each workflow step is worth relative to its cost. Its own cost is currently
invisible, so we cannot tell whether auditing is worth what it costs or whether a cheaper judge
profile would do. Every audit that runs before this change adds more cost that can never be
attributed later.

## What Changes

- Every launched judge CLI attempt in an audit records usage (value batches and the correctness pass):
  judge CLI, resolved model, reasoning effort, token usage by category, and the CLI-reported USD cost.
  This uses the adapters' existing `cli.UsageExtractor` on the raw output the audit already captures:
  the Claude JSON result, and Codex `turn.completed` events when the judge is Codex. The accounting
  unit is the launched attempt, not the validated judge output. Usage is extracted from captured
  stdout as soon as the process exits, before the exit status or response is checked. So a nonzero
  exit, a missing or invalid structured response, a decode failure, and each retry still record
  whatever usage they reported, or an explicit unavailable record. This matches
  `agent-usage-collection` and `cost-capture`, which keep usage from failed steps.
- Each attempt's usage record is written durably, and atomically, before its judge output is treated
  as complete. A value batch output skipped on resume therefore always has recoverable usage, and a
  failed attempt's usage survives even though it produced no output. Today batch provenance is not
  serialized with outputs (`json:"-"`) and `value-batch-provenance.json` is written only after the
  whole batch loop. If resume finds an output with no usage record, as happens for audits started
  before this change, that attempt is recorded as usage-unavailable, not counted as complete.
- The local audit report (`local-report.json`) gets an audit-level judge usage summary: the judge
  identity, per-attempt records (failed attempts and retries included), and aggregate tokens and cost
  over every launched attempt with explicit coverage (`complete` / `partial` / `none`). A launched
  attempt with unavailable usage or cost counts in the coverage denominator. Aggregates follow the `agent-usage-collection` and `cost-capture`
  rules. Unknown values stay null and are never zero, and no cost is computed from tokens.
- The Google Sheet value dataset gets a new header version. It appends audit-level judge columns
  after the existing `step_value_v1` columns: judge CLI, judge effort, judge token usage, judge cost,
  and judge usage coverage. `judge_model` already exists. Each observation row from an audit carries
  that audit's judge totals. The column names say the values are per audit, so consumers
  de-duplicate by `audit_run_id` instead of summing across step rows.
- The reporter accepts the new header. When a configured tab still has the exact `step_value_v1`
  header, the reporter upgrades it in place by writing only the new trailing header cells. Existing
  rows and columns are not touched, and older rows read the new columns as empty (unknown). Any other
  header mismatch still blocks delivery without modifying the sheet, as today.
- Judge usage extraction never fails the audit. If usage cannot be extracted, the record is marked
  unavailable and the audit continues.

No existing columns, local fields, or run-metrics schemas are removed or reordered.

## Capabilities

### New Capabilities
- `audit-judge-usage`: collecting, persisting, and aggregating usage and cost for the audit's own
  judge CLI invocations, including coverage semantics and resume behavior.

### Modified Capabilities
- `lightweight-audit-reporting`: a new versioned worksheet header with appended audit-level judge
  columns, and a one-time in-place upgrade of an exact `step_value_v1` header.
- `workflow-value-observation`: the local report's approved high-level fields include the audit-level
  judge usage summary alongside the existing judge model identity.

## Technical Approach

The model-invocation paths already hold everything needed. `runCrosscheckOutput` returns the raw
stdout that `auditSessionID` inspects, and the adapter is already resolved. Once `runCrosscheckOutput`
returns, whether or not it succeeded, the audit type-asserts `cli.UsageExtractor` and extracts a
`model.UsageRecord` along with any reported USD cost. It then durably appends that attempt's record
to an audit-owned usage ledger before it looks at the exit status or the response. The ledger may be
a per-attempt file or part of an output record that is written atomically; design chooses which.
Value batch and correctness outputs are marked complete only after their usage record is persisted.
The report assembly stage (`assembleLocalReportStage`) rolls up every ledger record into the
audit-level summary. The
Sheets projection (`projectObservation`) appends the summary columns to every row of that audit.

Key decisions:

- **Reuse adapter extraction, do not add a parser.** The Claude and Codex extractors already produce
  the canonical usage vocabulary and respect `cost-capture`. Each judge invocation is a fresh
  session, so there is no cumulative-cost delta to attribute. A Codex judge gets token usage and a
  null cost, because Codex reports no USD cost and computing one from tokens is prohibited.
- **Account per launched attempt, persisted before completion.** Counting only successful, validated
  calls would leave out failed calls and retries that still cost tokens. It would also shrink the
  coverage denominator, so the audit would look cheaper and more complete than it was. Persisting
  usage before an output counts as done closes the crash window between writing the output and
  writing provenance, where a resumed audit would skip the batch and lose its usage.
- **Audit-level totals on step rows, not a new tab or per-step split.** Rows are per step
  observation, and value batches do not map one-to-one onto steps. Splitting judge cost across steps
  would invent an allocation. A separate tab would add a second destination and header to validate,
  set up, and keep retry-safe. Repeating clearly named per-audit columns keeps the dataset to one tab,
  and consumers can still aggregate correctly.
- **Upgrade the header in place when it is exactly v1.** Without this, the operator would have to edit
  the live sheet by hand before any judge data could arrive. Writing only new trailing header cells
  over an exact v1 header does not reorder or rewrite data, and existing rows stay valid.

The detailed field list, exact column names, header version string, and how unavailable usage
renders in the sheet are left to specs and design.

## Out of Scope

- Computing, estimating, or converting cost from tokens for judges that report no USD cost (Codex,
  Copilot credits). The `cost-capture` policy stays unchanged.
- Changing which agent or profile performs the audit, or adding a cheaper-judge setting.
- Recording audit judge usage in the source run's `run-metrics.json` or folding it into source-run
  cost totals.
- Backfilling judge usage for audits completed before this change, or re-delivering already-delivered
  rows.
- Usage for non-model audit work (evidence preparation, GitHub publication, Sheets delivery).
- Displaying judge cost in the TUI or `audit status` output beyond what the local report provides.

## Impact

- Code: `internal/devaudit` (`value_audit.go`, `correctness.go`, `crosscheck_output.go`,
  `sheets_delivery.go`), with read-only use of `internal/cli` usage extractors and `internal/model`
  usage types.
- Artifacts: a new audit-owned judge usage ledger (or equivalent atomic per-output usage records), additive provenance serialization for `model-output/*.json` and the correctness
  output, and `local-report.json`. The existing local-report schema version is either kept or bumped
  per design, and reports without judge usage keep delivering.
- External dataset: the value worksheet gains trailing columns through a versioned header. The first
  delivery after the upgrade rewrites the header row of an exact v1 tab. Downstream sheet consumers
  must treat the judge columns as per audit.
- Specs: new `audit-judge-usage`, deltas to `lightweight-audit-reporting` and
  `workflow-value-observation`.
- Docs: `docs/usage-and-cost-tracking.md` and development-audit docs note that judge usage is recorded
  and how to read it.
- Development-audit builds only. Untagged and release builds are unaffected.
