- [ ] Implement the change described by these files using TDD, as `CLAUDE.md` requires: write each failing test before the code that makes it pass. Satisfy every spec scenario in `specs/` and every `INT-*` and `E2E-*` obligation in `test-plan.md`. `design.md` is the technical source of truth; the bullets below summarize it.
  - [proposal.md](proposal.md), [design.md](design.md), [test-plan.md](test-plan.md), [decisions.md](decisions.md)
  - [specs/implementation-completion-loop/spec.md](specs/implementation-completion-loop/spec.md), [specs/builtin-workflows/spec.md](specs/builtin-workflows/spec.md), [specs/run-failure-evidence/spec.md](specs/run-failure-evidence/spec.md)

  **Workflow** (`workflows/core/implement-change-v2.0.yaml`, edited in place, no new version)
  - **`implement`:**
    - Add `capture: implementor_response`.
    - End the prompt with the completion-record rules. When the implementor believes the change is done, it writes `{{session_dir}}/output/implementation-complete.md`. When only a human can unblock it, it writes `implementation-blocked.md`. A compaction is not a stopping point, and the turn ends only after writing one of the two records.
  - Delete `check-implementation-not-blocked`, `complete-evidence`, `check-evidence-not-blocked`, and `check-evidence`. Add one loop step, `complete-implementation`, with `loop: {max: 12}` and no `continue_on_failure`. Put a comment above it giving the arithmetic from design.md: the longest accepted verdict sequence is 11 judgments, so the 12th is always terminal.
  - **Loop children, in order:**
    1. `check-completion-not-blocked`: `script: check-not-blocked.sh` with the blocked file.
    2. `check-completion`: `script: check-implementation-complete.sh`, with `continue_on_failure: true` and `break_if: success`.
    3. `check-progress`: `script: check-implementation-progress.sh`, with no `continue_on_failure`.
    4. `judge-completion`: `agent: tester`, `session: new`, `mode: autonomous`, and no `tools`. Its prompt follows the design's judge section:
       - Inputs: `completion-problems.txt`, the checklist, the evidence directory, `git log` and `git diff {{implementation_start_head}}..HEAD`, and `acceptance-assumptions.md` and `implementor-decisions.md` when present.
       - `{{implementor_response}}` goes inside `<implementor-response>` tags with the standard untrusted-data notice.
       - Inspection only: no tests, builds, or product runs, and no new evidence.
       - The four verdicts in precedence order, leaning toward `continue`.
       - On `blocked`, write the blocked record with the cause and the decision needed.
       - On `fix-record`, change only the Code, Evidence, and Status cells and/or write `implementation-complete.md`, from saved evidence. Commit only the checklist, with the prefix `[judge-completion]`, when `{{change_dir}}` is inside the repository.
       - Write `{{session_dir}}/output/completion-verdict.json` last.
    5. `check-completion-verdict`: `script: check-completion-verdict.sh`, with `capture: completion_verdict` and `capture_format: json`.
    6. `check-judge-not-blocked`: `script: check-not-blocked.sh`.
    7. `accept-complete`: `command: "true"`, with `skip_if: 'sh: test "{{completion_verdict.verdict}}" != complete'` and `break_if: success`.
    8. `continue-implementation`: `session: implementor-agent`, `mode: autonomous`, `tools: [call_agent]`, `capture: implementor_response`, and `skip_if: 'sh: test "{{completion_verdict.verdict}}" != continue'`. Its prompt:
       - wraps `{{completion_verdict.remaining}}` in `<remaining-work>` tags with the untrusted-data notice;
       - says to keep working from the checklist, the git log, and that list;
       - repeats the compaction and stopping rules;
       - keeps the `implement` step's evidence, commit (`[{{step_id}}]`), `{{artifact_validation_instruction}}`, and no-Agent-Validator rules.
  - Leave every step from `audit` onward unchanged, including `complete-evidence-after-review`, `recheck-evidence`, `check-clean`, and `verify-implementation-commits`. `evidence-problems.txt` keeps its contract.

  **Bundled scripts** (new and executable, under `workflows/core/`, all requiring `jq`, inputs as JSON on stdin)
  - **`check-implementation-complete.sh`**
    - Inputs: `checklist_file`, `evidence_dir`, `plan_file`, `problems_file` (`evidence-problems.txt`), `start_head`, `completion_file`, and `report_dir`.
    - Pipe the evidence inputs to `$script_dir/check-evidence.sh`; do not copy its rules.
    - Require a non-empty completion record.
    - Require at least one commit in `start_head..HEAD` whose subject does not start with `[judge-completion]`.
    - Require every row of the first table with Status and Evidence columns to be `verified`. Parse `\|` escapes as `check-evidence.sh` does.
    - Write `completion-problems.txt` (one `- ` line per unmet condition, naming the row IDs) and `completion-check.json` (`{"passed", "only_unverified_with_reason"}`).
    - Exit 0 only when every condition holds.
  - **`check-implementation-progress.sh`**
    - If `continue-snapshot.json` exists and both HEAD and the checklist's sha256 are unchanged, print `implementation stalled: continue pass <n> added no commit and changed no checklist row` and exit 1. Otherwise, delete the snapshot.
    - Remove any `completion-verdict.json`.
    - Open a judgment in `completion-loop.jsonl`, reusing an open judgment that has no verdict yet.
  - **`check-completion-verdict.sh`**
    - Inputs: `verdict_file`, `check_file`, `ledger_file`, `blocked_file`, `remaining_file`, `snapshot_file`, `checklist_file`, `max_continue: "3"`, and `max_fix_record: "2"`.
    - Validate the verdict: it must be one of the four, `continue` needs a non-empty `remaining`, `complete` requires `only_unverified_with_reason`, and `blocked` requires a non-empty blocked record. On failure, print `completion judge returned no valid verdict: <why>`.
    - Record the verdict on the open judgment, idempotently.
    - Enforce the bounds. A 4th `continue` writes `implementation-remaining.md`, prints `implementation incomplete: <k> items remain after 3 continue passes` followed by the list, and exits 1. A 3rd consecutive `fix-record` prints `implementation record could not be fixed after 2 fix-record verdicts` and exits 1.
    - On an accepted `continue`, write `implementation-remaining.md` and `continue-snapshot.json`.
    - Print `{"verdict": "...", "remaining": "<markdown bullets>"}`.
  - Confirm that the embed and asset-list tests (`workflows/embed_test.go`) pick up the new files.

  **Failure-reason wording** (Go)
  - `internal/model`:
    - Add `FailureRecord.OutputBeforeRepair bool` (`json:"outputBeforeRepair,omitempty"`) and `RepairFrame.CheckBeforeLastRepair bool` (persisted with the frame).
  - `internal/exec/repair.go`:
    - Set the frame flag when a repair attempt starts its work (before `runInlineRepairAgent`, and in `armRerunReplay`), and clear it in `settleInternalCheck`.
    - The guarded-action block that runs before any attempt leaves the flag false.
    - `terminalFailed` copies the flag onto `ctx.LastFailure`.
    - `repairEndData` emits `repair_output_before_repair: true` when the flag is set.
  - `internal/exec/failure.go` `ClassifyFailure`:
    - With the flag set, render `<id> failed: before repair: <line>`, or `<id> failed with exit code N before repair` when stderr is empty.
    - Keep the `; blocked: …` and ` after N repair attempts` suffixes unchanged.
    - Leave `ClassifyCrash` and its precedence in `classifyRunFailure` untouched: a crashed repair agent keeps its unlabeled infrastructure reason, and `TestNestedRepairCrashReasonKeepsAttemptCount` must still pass.
  - `internal/runview`:
    - Add `FailureEvidence.OutputBeforeRepair`.
    - `applyRepairStepEnd` reads `repair_output_before_repair`, and `classifiedFailureReason` passes it through.
    - Older logs without the field render unlabeled.

  **Tests from `test-plan.md`**
  - **Script tests** (INT-003): a new `workflows/completion_loop_scripts_test.go`, following the existing script-test pattern with temporary git repositories. Use one shared fixture set for `check-evidence.sh` and `check-implementation-complete.sh`.
  - **Workflow shape:** update `TestCoreImplementChangeV2Shape` and the step lists in `workflows/verify_change_test.go`, plus `embed_test.go` if needed:
    - the new step order and loop children;
    - no `complete-evidence`;
    - the judge uses `agent: tester` and `session: new`;
    - `continue-implementation` uses `implementor-agent`.
  - **Embedded-workflow tests in `internal/exec`:**
    - INT-001 (verdict paths) and INT-002 (bounds, stall, invalid verdicts), modeled on `verify_change_workflow_test.go`: a stub runner plays the agents, while the real scripts and git run.
    - INT-005 (resume of earlier-layout state: a missing `complete-evidence` target, and a completed `implement` without `implementor_response`), in `internal/runner` or `internal/exec`.
  - **Failure-reason tests:** `internal/exec` repair and classifier tests, plus INT-004 in `internal/runview` historical integration tests (cases (a) through (g) and the legacy fixture).
  - **End-to-end tests:** E2E-001, E2E-002 (Cases A and B), and E2E-003 in `cmd/agent-runner`, using the fake-CLI pattern of `repair_blocked_e2e_test.go`. E2E-001 and E2E-002 run the embedded implement-change with `--until audit` and `skip_validator=true`.

  **Docs**
  - `docs/built-in-workflows.md`, the `core:implement-change` row: describe the completion loop, which covers:
    - the completion record;
    - the deterministic completion check;
    - the fresh `tester` judge and its four verdicts;
    - at most 3 continue passes, plus stall detection;
    - that it replaces the pre-audit evidence repair.
  - Describe the `before repair` failure-reason wording wherever failure reasons are documented (`docs/run-state-and-audit.md`, if it covers them).

  **Constraints**
  - Do not add CI jobs or test suites. All new tests run under the existing `make test` (`go test ./...`).
  - Do not change `core/implement-change-v1.0.yaml`, the v1.0 and v2.0 change workflows, the `simple-change` workflows, `verify-change`, or anything after `audit` in the edited workflow.
  - Do not add a Claude Stop hook or any other CLI-specific mechanism.
  - Run `make fmt` and `make lint`.

  **Done when**
  - Every scenario in the three spec files is covered and passes, along with INT-001 through INT-005 and E2E-001 through E2E-003.
  - Existing repair, crash, and workflow tests still pass, `make test` and `make lint` are green, and `openspec validate feature-312-6e9090b8 --strict` passes.
