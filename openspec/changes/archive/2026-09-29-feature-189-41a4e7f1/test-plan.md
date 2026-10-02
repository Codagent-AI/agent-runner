## Coverage Strategy

Specifications remain the source of unit-test requirements. This plan records only additional
integration and end-to-end obligations, the acceptance testing envelope, and exceptional human-only
obligations.

The following are unit-level behaviors and are **not** listed here:
- adapter extraction mapping in `judgeAttemptUsage`
- coverage and summation rules
- legacy synthesis
- projecting cells from a summary

The obligations below cover the boundaries where those units must work together with real files, real
subprocesses, the OS sandbox, the audit workflow runner, and the Sheets HTTP contract. Every test
builds with `-tags dev_audit`, and each runs under `make test`, which uses `-tags dev_audit`.

## Integration Tests

### INT-001: Judge subprocess usage is persisted before output, for success and failure
- Covers: `audit-judge-usage` requirements "Every launched judge attempt records usage", "Usage is
  extracted from judge output regardless of outcome", and "Judge usage is durable before judge output
  completes".
- Boundary: the real `invokeCrosscheckValueBatch` and `invokeCrosscheckCorrectness` →
  `runCrosscheckOutput` → a real child process → adapter `ExtractUsage` → the `judge-usage/` ledger
  on disk.
- Setup:
  - A temp audit session with prepared evidence, using the existing `crosscheck_regression_test.go`
    fixture helpers.
  - `crosscheckCommand` is overridden to exec a shell script that prints fixed output:
    - A Claude-shaped fixture is a one-line `--output-format json` result object carrying `usage`,
      `total_cost_usd`, `session_id`, and `structured_output`.
    - A Codex-shaped fixture is JSONL with `thread.started` and `turn.completed` events carrying
      `usage`.
- Action: run each invoker through the scenarios below.

  | Scenario | Script behavior | Expected ledger result |
  | --- | --- | --- |
  | (a) Valid response | Exit 0 with a valid response | One record with `audit_run_id`, stage and batch, tokens, and cost for Claude only. A durable `exited` record exists before `model-output/<batch>.json` or `correctness.json` is written; check this from a wrapping write hook. The record becomes `succeeded` only after that write. |
  | (b) Nonzero exit | Exit 1 after printing usage | One `failed` record, category `process_exit`, usage retained. |
  | (c) Bad response | Exit 0 with an undecodable or invalid structured response | One `failed` record, category `response_invalid`, usage retained. |
  | (d) Launch failure | Command whose `Start` fails | No ledger file is created. |
  | (e) Forged file | Script writes a `judge-usage`-shaped JSON file into `model-output/` | That file never appears in the ledger or the summary. |
  | (f) No usage | Output has no usage | One record with usage `unavailable`, cost `null`, and the stage outcome unchanged. |
| (g) Output write fails | Valid response, then the output write is forced to fail (read-only target or injected writer) | The record is `failed/output_write_failed` with its usage, and no output exists. |
| (h) Durability | Ledger writes are observed through the `stateio` directory-sync seam (`syncJSONDirectory`) | Both the exit and final phases sync before returning, and the exit sync precedes output publication. |

- Assertions: see the expected result for each scenario in the table above.
- Execution: `internal/devaudit/judge_usage_integration_test.go`, run by `go test -tags dev_audit
  ./internal/devaudit`.

### INT-002: Resume and legacy outputs through the real value stage and report assembly
- Covers: the scenarios "Audit resumes after a completed batch", "Output exists without a usage
  record", and "Later batch fails", plus the "Audit-level judge usage summary" requirement and the
  modified `workflow-value-observation` "Local report carries judge usage once".
- Boundary: `runAuditStage("value-audit")` → `ensureValueOutputs` across two process-level
  invocations. Then `runAuditStage("assemble-local-report")` → `local-report.json`.
- Setup: two prepared value packages. The fake judge script succeeds for batch A. For batch B it
  fails on the first call and succeeds on the second, controlled by a marker file. A separate
  fixture has a pre-existing `model-output/<batch>.json` and no ledger.
- Action: run the value stage, which fails on batch B. Run it again, which resumes. Then run
  correctness and assemble. For the legacy fixture, run assembly only.
- Assertions:
  - Batch A's judge is invoked once in total.
  - The ledger holds three records: A succeeded, B failed, B succeeded.
  - `local-report.json.judge_usage.attempt_count` is 4, counting correctness.
  - Coverages are `complete`.
  - Every observation's `cost` group is identical to a run with no ledger.
  - For the legacy fixture, the summary contains one `legacy` unavailable attempt, and token and cost
    coverage are `partial` or `none` as the data dictates, never `complete`.
  - Re-running assembly yields a byte-identical `judge_usage`.
  - **Crash recovery:** a fixture has an `exited` record beside a completed output, simulating a
    crash between the output write and finalization. It yields one attempt, reported as
    `succeeded (recovered)` with its usage.
  - **Corrupt record:** a fixture has a corrupted ledger file beside its completed output. It yields
    exactly one unavailable attempt for that batch, and no additional legacy attempt.
  - **Failed output write:** after an `output_write_failed` attempt, resume launches a new attempt,
    and both are counted.
- Execution: `internal/devaudit/judge_usage_integration_test.go`.

### INT-003: Sheets header upgrade and v2 projection against the HTTP contract
- Covers: the modified `lightweight-audit-reporting` requirements "Google Sheets is the initial
  external dataset" and "External rows are an allowlisted high-level projection". This includes all
  of their added scenarios: upgrade, content to the right, upgrade write failure, audit-level judge
  columns, unreported cost, and pre-change reports.
- Boundary: the real `SheetsReporter.Deliver` → `net/http` → an `httptest` server. The server
  implements the `values` GET, PUT, and `:append` endpoints, and records every request.
- Setup:
  - A connection store, as in the existing `sheets_delivery_test.go` tests.
  - The server's row 1 is configurable, and so is the PUT failure.
  - Reports are built with the following judge usage: a summary, a summary with cost coverage
    `none`, and `JudgeUsage == nil`.
- Action: deliver against each of these tabs:
  - (a) an exact v2 header
  - (b) an exact v1 header
  - (c) a v1 header with a non-empty AF1
  - (d) an exact v1 header where the PUT returns 500
  - (e) an exact v1 header where the PUT succeeds but its response is dropped, then delivery is
    retried
- Assertions:
  - (a) Rows appended to `A:AL` have exactly 38 cells, `schema_version=step_value_v2`, and identical
    judge cells on every row of the audit. Nil sums are `""`, never `"0"`.
  - (b) Exactly one PUT, to `'Tab'!AF1:AL1`, with the seven column names in order, happens before
    the append. No request touches any other range.
  - (c) There is no PUT and no append, and the error is a schema mismatch. The report stays pending.
  - (d) There is no append, the report stays pending with a delivery error, and a later retry
    succeeds.
  - (e) The retry does not issue a duplicate append for observations already present.
- Execution: `internal/devaudit/sheets_delivery_test.go`.

## End-to-End Tests

### E2E-001: Tagged CLI audit records judge usage through to the Sheet row
- Covers: the journey from issue acceptance item 1 through the public CLI, with the Claude judge and
  then the Codex judge. The steps are: source run → automatic audit → ledger → local report → v1
  header upgrade → delivered row → `audit retry` idempotence.
- Surface: the `agent-runner` binary built with `-tags dev_audit,devaudit_e2e`, via the existing
  `newCLIAuditFixture` in `cli_e2e_test.go`.
- Setup:
  - Extend `writeE2EFakeCodex` so its stdout includes usage. For Codex it emits a `turn.completed`
    event with `usage`. With `AUDIT_E2E_CLAUDE=1` it emits a Claude result object with `usage` and
    `total_cost_usd`.
  - The fake Sheets server starts with the **v1** header, supports PUT to `AF1:AL1`, and afterwards
    serves the v2 header.
- Journey:
  - Run `agent-runner -C <project> --headless spec-driven:audit-e2e` and wait for the linked audit to
    complete.
  - Run `agent-runner audit retry <auditDir>`.
  - Repeat the journey in the existing Claude variant, `TestTaggedClaudeAuditCompletesAllStages`.
- Assertions:
  - **Local report:** `local-report.json.judge_usage` has the frozen CLI, model, and effort. It has
    the attempt count equal to the fake judge's call count, non-null total tokens, and token
    coverage `complete`.
  - **Cost:** for Claude, a non-null cost equal to the sum of the fixture's `total_cost_usd` values,
    with coverage `complete`. For Codex, a null cost with coverage `none`.
  - **Sheet header and row:** the server saw exactly one header PUT. The row has 38 cells, with
    `schema_version=step_value_v2` and judge cells matching the summary. For Codex,
    `audit_judge_cost_usd` is empty.
  - **Retry:** it appends no row and makes no model call.
  - **Source metrics:** the source run's `run-metrics.json` totals are unchanged by the audit.
- Execution: `internal/devaudit/cli_e2e_test.go`, run under `make test` on darwin and linux.

## Acceptance Testing Envelope

- **Environments and sandboxes:**
  - This developer machine (darwin) with a `dev_audit`-tagged build. `./dev.sh` does not carry the
    tag, so build to a temporary path with `go build -tags dev_audit -o <tmp>/agent-runner
    ./cmd/agent-runner`. Do not use `make build`.
  - Real `claude` and `codex` CLIs are on `PATH`.
  - The macOS `sandbox-exec` audit sandbox is available.
  - Source runs to audit already exist under `~/.agent-runner/projects/*/runs/`. `agent-runner audit
    replay <run> --session <id> [--project <dir>]` re-audits one without re-running the source
    workflow.
  - The `devaudit_e2e` fixture build and a local `httptest` Sheets server are available for
    exploration that must not touch the real sheet.
- **Credentials and secrets:**
  - The Google Sheets connection record at `~/.agent-runner/development-audit-connection.json`,
    mode 0600. It holds the OAuth refresh material and the configured spreadsheet and tab. Never
    print or copy its contents.
  - The Claude and Codex CLIs use their own existing logins.
  - `gh` uses the user's GitHub auth.
- **Authorized effects:**
  - Up to **two** real audit replays of small, successful source runs. At least one must use a
    Claude judge; the source run's lead profile decides this. Each costs roughly $0.40–$1.00 of
    Claude usage.
  - Delivery of those audits' rows to the configured Sheet tab.
  - The one-time v1→v2 header upgrade of that tab, which writes only cells AF1:AL1. This is the
    issue's acceptance item 2, so it is authorized. Record the tab name and the appended row
    `observation_id`s in the acceptance evidence. No cleanup of the delivered rows is required; they
    are real audit observations.
  - A correctness stage that confirms a genuine `workflow_execution` defect may file a real
    `[auto-audit]` issue. That is normal product behavior. Do not create, edit, or close issues
    manually.
- **Off limits:**
  - Editing or deleting existing Sheet rows, columns, or other tabs.
  - Changing the connection record or destination, other than `audit migrate` onto a scratch tab
    the operator already created.
  - Running more than two paid real audits.
  - Modifying source run directories.
  - Pushing, releasing, or running `make build`.
- **Permitted substitutes:**
  - If the real Sheet is inaccessible, for example because the credential has expired, use the
    `devaudit_e2e` fixture server for Sheet behavior. Still run one real Claude-judge audit, and
    capture its `local-report.json` `judge_usage` with the delivery left pending. Report the Sheet
    half of acceptance item 2 as unverified.
  - If no Claude-profile source run exists, replay a Codex-judged run. Report that a non-null cost
    could not be shown because Codex reports no USD cost.
- **Known risk areas:**
  - The shape of Claude's `--output-format json` output. A pretty-printed or changed result object
    would make usage unavailable instead of failing the audit.
  - `runBoundedOutput` discards output over its 256 KiB limit, so attempts that hit it record
    unavailable usage. This is an accepted limitation.
  - Mixed-version reporters: after the tab is upgraded, older binaries leave deliveries pending.
    This is an accepted limitation.
  - A crash between launch and the exit record loses that attempt. This is an accepted limitation.
  - Earlier defect clusters in this package: audit recovery, retry, and delivery idempotence
    (`repair-development-audit-recovery`), sandbox write boundaries, and structured-output
    decoding. Exploration should probe resume after an interrupted value stage, and `audit retry`
    after a failed header upgrade.

## Human-Only Testing

None.

## Coverage Map

| Requirement or journey | INT | E2E | HT |
| --- | --- | --- | --- |
| audit-judge-usage: Every launched judge attempt records usage | INT-001, INT-002 | E2E-001 | — |
| audit-judge-usage: Usage is extracted from judge output regardless of outcome | INT-001 | E2E-001 | — |
| audit-judge-usage: Judge usage is durable before judge output completes | INT-001, INT-002 | — | — |
| audit-judge-usage: Audit-level judge usage summary | INT-002 | E2E-001 | — |
| audit-judge-usage: Judge usage stays separate from source-run metrics | INT-002 | E2E-001 | — |
| lightweight-audit-reporting: Google Sheets is the initial external dataset (header upgrade) | INT-003 | E2E-001 | — |
| lightweight-audit-reporting: External rows are an allowlisted high-level projection (judge columns) | INT-003 | E2E-001 | — |
| workflow-value-observation: Local value records contain approved high-level fields (judge summary once) | INT-002 | — | — |
| Issue acceptance: usage parsed from Claude JSON result and Codex events into the audit record | INT-001 | E2E-001 | — |
