## Coverage Strategy

Specifications remain the source of unit-test requirements. This plan records only additional
integration and end-to-end obligations, the acceptance testing envelope, and exceptional human-only
obligations.

Unit tests carry most of this change: price-table loading and validation, the arithmetic examples
(0.98, 0.115, 0.68, 0.134, the threshold boundary, reasoning not double-counted), model resolution,
each eligibility reason, CLI-reported precedence, per-turn pricing, totals basis and `cost_by_basis`,
native evidence shapes, and the run-view marker formatting (see `design.md` Verification Strategy).

The obligations below cover the points where those units meet real boundaries:

- the Codex rollout on disk behind Runner's private `CODEX_HOME`;
- the collector persisting and reloading `run-metrics.json` across a resume, including a pre-change
  artifact;
- the run views reading cost evidence from a real `audit.log`;
- the built binary producing priced records for a mixed workflow.

The main residual risk is that the real Codex rollout's per-request records fail to reconcile with
`turn.completed`. That would quietly turn every estimate into `request-evidence-mismatch`. Only a real
Codex run detects it, so the existing opt-in real-agent Codex test is extended rather than adding a
suite.

## Integration Tests

### INT-001: Codex request evidence read from the rollout through the private Codex home
- Covers: `cost-estimation` Request-level Standard-tier valuation policy (per-request evidence,
  cache-write observation, rollout missing or predating records); design §3.
- Boundary: `CodexAdapter.ExtractUsageWithContext` reading a rollout from the real filesystem through
  the `sessions` symlink that `prepareCodexRunnerHome` creates in the private `CODEX_HOME`.
- Setup:
  - A temp source Codex home with `sessions/YYYY/MM/DD/rollout-<ts>-<id>.jsonl` fixtures derived from
    a current real rollout. The fixture includes `token_usage_record` lines with full `usage`
    (`input_tokens`, `cached_input_tokens`, `cache_write_input_tokens`, `output_tokens`,
    `reasoning_output_tokens`), including one nonzero cache write and one record above 272,000 input.
  - Other lines: a record that omits `cache_write_input_tokens`, a compaction response, records from
    another thread, and records timestamped before `StartedAt`.
  - Further cases: a rollout with only `token_count` events, a rollout with a malformed `usage`
    payload, and a session with no rollout.
- Action: extract usage for stdout fixtures whose `thread.started` names each session.
- Assertions:
  - Request evidence lists exactly the in-window, same-thread records, compaction response included,
    with exact per-field values.
  - The omitted cache-write field is nil and a reported zero is 0.
  - The token-only rollout gives `codex-rollout-unsupported`, the malformed one gives
    `parse-failure`, and the missing one gives `codex-rollout-not-found`.
  - The context measurement and the `turn.completed` usage record are unchanged from the pre-change
    output for the same fixtures.
  - Request evidence is absent from the JSON encoding of the usage record.
- Execution: `internal/cli` package test, `go test -tags dev_audit ./...` (CI `test` job).

### INT-002: Collector prices, persists, and reloads cost evidence across resume and legacy artifacts
- Covers: `cost-capture` Run-level cost aggregation with coverage, Every recorded cost carries its
  basis; `cost-estimation` Codex invocations receive an estimate (resumed delta, nested Validator
  excluded), Full-attempt eligibility (external-user turns); `run-metrics-artifact` Artifact content
  (estimated and partial evidence), Cost basis in step records and totals, Artifacts written before
  cost estimation.
- Boundary: the metrics collector processing audit events and writing `run-metrics.json` to disk,
  then a new collector for a resumed execution session loading that file, adding attempts, and
  rewriting it.
- Setup:
  - A run directory with a schema v4 `run-metrics.json` in the pre-change shape: a Claude step with a
    reported cost and no `cost_basis`, a Codex step with null cost, and a nested Validator projection.
  - Step-end events for the resumed session:
    - a Codex step resuming an earlier Codex session, with cumulative usage and request evidence that
      reconciles with the delta;
    - an external-user Codex step with three eligible turns on two models;
    - an external-user Codex step with an unattributed turn;
    - a Codex step with an unpriced model;
    - a shell step;
    - a nested Validator producer head with a full attempt-level USD cost.
- Action: load the collector, process the events, trigger a measurement refresh, write the artifact,
  reload it in another collector, refresh again, and read it back from disk.
- Assertions:
  - The legacy Claude cost has basis `reported`, and the legacy Codex step stays null with no reason.
  - The resumed Codex step's estimate prices only its delta and carries `estimated`, the table
    version, and an `estimated-cost` native cost (full, derived, approximate, with a derivation).
  - The three-turn step's estimate equals the sum of its turn prices.
  - The unattributed step is null with `turn-unattributed` and has a `partial` native subtotal.
  - The unpriced step is null with `model-unpriced`.
  - The uncosted nested projection has no estimate. The costed nested projection keeps its producer
    cost with basis `reported` after both refreshes and the reload.
  - Run totals are `mixed`, with coverage `partial`, and `cost_by_basis` reported plus estimated
    equals the cost total exactly. No cost is aggregated under an empty basis. The two session
    rollups have their own bases, and their subtotals also sum to their totals.
  - Every native measurement passes the measurement contract validation.
  - `schema_version` is 4.
- Execution: `internal/metrics` package test, CI `test` job.

### INT-003: Run views render cost basis from a recorded audit log
- Covers: `run-complete-screen` Estimated cost marker in summary, Unavailable cost display in
  summary; `view-run` Estimated cost in selected detail.
- Boundary: `internal/runview` loading a run from an `audit.log` on disk (as the inspect view does) and
  rendering the summary table and selected detail.
- Setup:
  - An audit log with step-end events: a Claude step with a reported cost, a Codex step with an
    estimated cost, and an ineligible Codex step with reason `model-unpriced`.
  - A loop containing one reported and one estimated step, and a sub-workflow containing one estimated
    and one unpriced step.
  - A legacy step-end event that has a cost but no `cost_basis`.
- Action: build the run model from the file, render the root summary, drill into the loop, and render
  selected detail for each step.
- Assertions:
  - The estimated row shows `~$`, and the reported row and the legacy event show `$` with no marker.
  - The ineligible row shows `?` and its detail shows the reason.
  - The loop row carries `~` and the sub-workflow row carries both `~` and the partial indicator.
  - The root Total row carries `~`.
  - The estimated detail is labelled as an API-equivalent estimate, and the legend lists the marker.
- Execution: `internal/runview` package test, CI `test` job.

### INT-004: Adapter-produced Codex usage flows into collector eligibility
- Covers: `cost-estimation` Full-attempt eligibility (an optional reasoning breakdown versus a missing
  required category, check precedence, resumed and external-user turns).
- Boundary: the real `CodexAdapter.ExtractUsageWithContext` output, from real-shaped `turn.completed`
  stdout plus a rollout on disk, fed unmodified into the metrics collector's `Process`. Hand-built
  `UsageRecord`s already marked complete are not used.
- Setup: rollout and stdout fixtures for:
  - a fresh step whose `turn.completed` usage lacks `reasoning_output_tokens`;
  - a resumed step on the same thread with the same omission;
  - an external-user step with two turns, the second resuming and lacking reasoning;
  - a step whose usage lacks `cached_input_tokens`;
  - a step with an attribution limitation (a turn with no usage report, followed by one that lacks
    `cached_input_tokens`).
- Action: extract each invocation's usage and process the corresponding step-end events in order.
- Assertions:
  - The adapter marks the reasoning-less records `partial` with no reason, and the collector still
    prices them with basis `estimated` (both turns are summed for the external-user step).
  - The missing-cached record is null with `required-category-missing`.
  - The attribution-limited step is null with `turn-unattributed`, not a category reason.
- Execution: `internal/metrics` package test (importing `internal/cli`), CI `test` job.

### INT-005: Development-audit export labels cost basis
- Covers: `workflow-value-observation` Observation cost carries its basis;
  `lightweight-audit-reporting` Google Sheets is the initial external dataset (v1/v2→v3 upgrade) and
  External rows are an allowlisted high-level projection (`cost_basis`).
- Boundary: the development-audit pipeline reading a real `run-metrics.json` from disk to build
  observations. The Sheets reporter then delivers them to the existing local fake Sheets HTTP server
  used by the `internal/devaudit` delivery tests.
- Setup:
  - A source run artifact with three leaf steps: reported-only (Claude), estimated-only (Codex), and
    mixed (one Claude and one Codex record), plus a leaf with an unknown cost.
  - Fake tabs with a `step_value_v1` header, a `step_value_v2` header, a v2 header with content to
    its right, and a v3 header.
  - A pending report assembled before cost-basis recording.
- Action: prepare and validate the audit observations, then deliver to each tab.
- Assertions:
  - Observations carry basis `reported`, `estimated`, and `mixed`, and the unknown-cost leaf has none.
  - Rows carry `step_value_v3`, the numeric `cost_usd`, and the matching `cost_basis`; unknown cost
    gives two empty cells, never `0`.
  - The v1 tab gains the judge columns plus `cost_basis`, and the v2 tab gains only `cost_basis`.
  - The tab with content to the right is rejected unmodified.
  - The pending report's rows have an empty `cost_basis`.
  - Judge columns are unchanged.
- Execution: `internal/devaudit` package test, CI `test` job.

## End-to-End Tests

### E2E-001: Mixed Claude and Codex headless workflow records priced metrics
- Covers: `cost-estimation` Codex invocations receive an estimate, Embedded versioned price table,
  Model resolution, Request-level policy (short- and long-context bands, cache writes), Estimates are
  labeled; `cost-capture` CLI-reported cost captured verbatim (precedence, Codex without a CLI cost),
  Run-level cost aggregation; `run-metrics-artifact` Cost basis in step records and totals.
- Surface: built `agent-runner` binary running a workflow from the CLI.
- Setup:
  - Extend `cmd/agent-runner/context_e2e_test.go`'s pattern: temp `HOME`, project, profile config,
    and POSIX fake `claude` and `codex` scripts on `PATH`.
  - Fake Claude emits a stream-json `result` with `total_cost_usd`.
  - Fake Codex emits `thread.started` and `turn.completed` with usage that equals the sum of the
    full-usage `token_usage_record` lines it writes to the rollout under `$HOME/.codex/sessions`.
  - The workflow has:
    - a Claude step;
    - a `gpt-6.1-sol` Codex step whose requests are all short-context, one with cache writes;
    - a `gpt-6.1-sol` Codex step with one request above 272,000 input;
    - a Codex step on an unlisted model;
    - a Codex step whose rollout is not written;
    - a shell step.
  - Existing context fixtures whose records carry only `input_tokens` must keep their current
    assertions; extend them only where cost is now asserted.
- Journey: run the workflow to completion and wait for the top-level `run_end` audit event.
- Assertions:
  - In `run-metrics.json`:
    - the Claude step has its reported cost and basis `reported`;
    - the two priced Codex steps have the exact amounts computed from the fixture rates, basis
      `estimated`, the table version, and `estimated-cost` native evidence;
    - the unlisted model is null with `model-unpriced`, the missing rollout is null with
      `context-band-unknown`, and the shell step is null with no basis;
    - totals carry the sum of the three costs, coverage `partial`, basis `mixed`, and matching
      `cost_by_basis`.
  - Each `step_end` event in `audit.log` carries the same cost, `cost_basis`, and
    `cost_unavailable_reason`. The `run_end` event carries the same totals, basis, and
    `cost_by_basis`.
- Execution: `cmd/agent-runner` test in the CI `test` job.

### E2E-002: Real Codex output reconciles and is priced
- Covers: `cost-estimation` Request-level policy and Full-attempt eligibility against the current real
  Codex rollout and `turn.completed` formats (design risk: reconciliation and format drift).
- Surface: the existing opt-in `TestCodexHeadlessRealAgentE2E` (and `TestCodexChildFollowUpHeadlessRealAgentE2E`
  for a resumed session).
- Setup: the existing real-agent setup with isolated native storage from `isolatedRealAgentCallEnv`
  (temp `HOME`, temp `CODEX_HOME` with a copied `auth.json`). No new suite. If Codex cannot
  authenticate under isolation, the test skips with that reason and E2E-001 stands.
- Journey: the existing headless real-agent runs.
- Assertions:
  - Each Codex step's cost reason is never `request-evidence-mismatch` or `context-band-unknown`.
  - When the effective model is in the table, the step has a positive estimate with basis
    `estimated`.
  - Otherwise the reason is `model-unpriced`, and the test logs the model so the table can be updated.
  - The resumed child step prices only its own delta: its estimate is below the cumulative-priced
    amount.
- Execution: `./.validator/go-offline.sh go test -count=1 -timeout 30m -tags e2e_agents ./cmd/agent-runner -run '^(TestCodexHeadlessRealAgentE2E|TestCodexChildFollowUpHeadlessRealAgentE2E)$' -v`
  (opt-in, not in CI; run before acceptance when credentials exist).

## Acceptance Testing Envelope

- Environments and sandboxes:
  - a local macOS checkout, with temp directories for workflow runs;
  - `./dev.sh` to run the CLI from source;
  - fake CLIs from the E2E fixtures;
  - inspect and summary views driven under a Python `pty.fork` with `pyte` screen reconstruction, as
    described in `CLAUDE.md`.
- Credentials and secrets: the machine's existing Claude Code and Codex CLI authentication (Paul's
  accounts), used only through the CLIs. No other secrets. No OpenAI API key is needed, because nothing
  calls the API.
- Authorized effects:
  - the two scoped tests in E2E-002;
  - a few small real headless Codex and Claude workflow runs in a temp project, under $1 total model
    cost (Codex runs on the subscription);
  - every run uses isolated native session storage (temp `HOME`, temp `CODEX_HOME` with a copied
    `auth.json`);
  - read-only fetches of OpenAI's public pricing page to cross-check `prices.json`.
  Temp directories may be deleted afterwards.
- Off limits:
  - delivering to any real development-audit Google Sheet (use the local fake Sheets server);
  - editing or deleting anything under `~/.codex/sessions`, `~/.claude`, or existing run directories
    (read-only inspection, such as checking rollout fields, is allowed);
  - Agent Factory artifacts and live factory runs;
  - pushing, opening PRs, or changing GitHub state.
- Permitted substitutes:
  - Fake CLI scripts when a real CLI is unavailable or cannot authenticate under isolation, provided
    the report states that real-format reconciliation was not verified.
  - Copies of real local rollouts fed to the adapter, to check reconciliation over recorded sessions
    when live runs are not possible.
  - A run cannot be launched with `r` under a synthetic PTY, so start runs from the CLI and inspect
    them in the TUI afterwards.
- Known risk areas:
  - Reconciliation between `turn.completed` cumulative usage and in-window rollout records:
    timestamp window, resumed sessions, compaction responses, and clock skew.
  - Rollout format drift.
  - Exact cent rounding in the summary for sub-cent estimates.
  - The `~` marker combined with the partial indicator in container and Total rows.
  - Legacy artifacts and audit events without `cost_basis`.
  - Prior defects in cumulative-usage attribution on resumed sessions.
  - Accepted limitations: nested Validator attempts and non-Codex CLIs stay unpriced, a step whose
    rollout can't be found stays `context-band-unknown`, and only Standard-tier rates are modeled.

## Human-Only Testing

None.

## Coverage Map

| Requirement or journey | INT | E2E | HT |
| --- | --- | --- | --- |
| cost-estimation: Codex invocations receive an API-equivalent cost estimate | INT-002 | E2E-001, E2E-002 | — |
| cost-estimation: Embedded versioned price table | — | E2E-001 | — |
| cost-estimation: Model resolution for pricing | — | E2E-001 | — |
| cost-estimation: Request-level Standard-tier valuation policy | INT-001 | E2E-001, E2E-002 | — |
| cost-estimation: Full-attempt eligibility | INT-002, INT-004 | E2E-001, E2E-002 | — |
| cost-estimation: Estimates are labeled as estimates | INT-002 | E2E-001 | — |
| cost-capture: CLI-reported cost captured verbatim | — | E2E-001 | — |
| cost-capture: Run-level cost aggregation with coverage | INT-002 | E2E-001 | — |
| cost-capture: Every recorded cost carries its basis | INT-002 | E2E-001 | — |
| run-metrics-artifact: Artifact content | INT-002 | E2E-001 | — |
| run-metrics-artifact: Cost basis in step records and totals | INT-002 | E2E-001 | — |
| run-metrics-artifact: Artifacts written before cost estimation | INT-002 | — | — |
| audit-log-entries: Agent step-specific data | — | E2E-001 | — |
| audit-log-entries: Run end usage and cost totals | — | E2E-001 | — |
| lightweight-audit-reporting: Google Sheets is the initial external dataset | INT-005 | — | — |
| lightweight-audit-reporting: External rows are an allowlisted high-level projection | INT-005 | — | — |
| workflow-value-observation: Observation cost carries its basis | INT-005 | — | — |
| run-complete-screen: Unavailable cost display in summary | INT-003 | — | — |
| run-complete-screen: Estimated cost marker in summary | INT-003 | — | — |
| view-run: Estimated cost in selected detail | INT-003 | — | — |
