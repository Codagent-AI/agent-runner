## Coverage Strategy

The specifications remain the source of unit-test requirements. This plan records only the integration, end-to-end, agent-acceptance, and human-only obligations that come on top of them.

**Unit and script level** (not inventoried here): `workflows/verify_task_commit_test.go` and a new `prepare-task-delivery.sh` test run the real scripts against temporary git fixtures. They cover every accept and reject rule in `task-delivery-gate`: repository identity, per-commit checks, malformed records, the no-record-location fallback, the unchanged local path, output wording, and the fsmonitor and no-mutation safety rules. These are the broad base.

**Integration level:** proves the wiring that scripts alone can't. The wiring is:
- the `implement-task` YAML threads the prepared record path and start time into both the repair prompt and the gate;
- the `step-repair` cycle turns a written record into a passing gate;
- resuming keeps the record.

**Automated end-to-end:** none is added. A CLI-driven run needs a real agent CLI to act as implementor. INT-001 already drives the real workflow definition with real scripts and git, so an automated E2E would duplicate it at much higher cost and flakiness.

**Agent acceptance:** runs the delivered behavior through the public CLI with a real implementor agent. It covers the two journeys from the issue: autonomous continuation, and a human unblocking a stopped run by writing a record.

## Integration Tests

### INT-001: implement-task accepts repair-recorded external delivery
- **Covers:** External delivery record; Repair records external delivery; Reporting external delivery; External delivery record lifecycle (fresh-start path computation).
- **Boundary:** the builtin `core/implement-task-v1.0.yaml`, loaded by `loader.LoadWorkflow` and dispatched with `DispatchStep`. The real `prepare-task-delivery.sh` and `verify-task-commit.sh` run through the script executor, the real inline-repair cycle runs, and real git runs in three temporary repositories. Only agent steps are faked.
- **Setup:**
  - Temporary repositories:
    - a run repository with one commit;
    - a bare "remote";
    - an external clone of that remote with `refs/remotes/origin/HEAD` pointing at `main`.
  - A task file in the run repository, and a temporary session directory.
  - A fake process runner, following the `acceptanceRoundRunner` pattern in `internal/exec/verify_change_workflow_test.go`:
    - it executes shell and script steps for real;
    - it fakes `generate-code` by committing a change on a feature branch in the external clone and running `git push -u origin feature`;
    - it fakes the repair agent by extracting the record path from its prompt and writing a JSON record that lists the pushed commit and a PR URL.
  - `skip_validator: "true"`.
- **Action:** dispatch the workflow's steps from `check-task-file` through `verify-task-commit`.
- **Assertions:**
  - The outcome is success.
  - `verify-task-commit` ran twice with one repair in between.
  - The repair prompt contained a path under `<session_dir>/output/task-delivery/`, and that path is the record the gate read.
  - The gate's final stdout contains `task delivered outside this repository`, the external repository root, the full commit ID, `refs/remotes/origin/feature`, the PR URL labeled `reported, unverified`, and the validator coverage note.
  - The run repository's HEAD and `git status --porcelain` are unchanged.
  - The record file still exists afterward.
- **Variant (same test file):**
  - The fake `generate-code` commits without pushing.
  - The fake repair writes the record anyway.
  - The assertions are: the outcome is failed after the repair budget is exhausted, and the gate's stderr names the commit as not reachable from any remote-tracking ref.
- **Validator variant (same test file):**
  - `skip_validator: "false"`.
  - The fake runner's `run-validator.sh` reports a task-compliance violation ("task not implemented") on its first run and passes on the next.
  - The fake `fix-violations` agent records its prompt, makes no change, and returns. This simulates a compliant skip.
  - The assertions are:
    - the recorded `fix-violations` prompt contains the external-work skip instruction;
    - the run repository gains no commit across the whole task;
    - `verify-task-commit` succeeds through external delivery after one repair.
- **Execution:** `internal/exec` (for example `implement_task_workflow_test.go`), run by `make test`.

### INT-002: a fresh task start isolates records between tasks
- **Covers:** External delivery record lifecycle (an earlier task's record doesn't satisfy a later task).
- **Boundary:** two sequential executions of the builtin `implement-task` steps that share one session directory and one run repository, with the real scripts and the real repair cycle.
- **Setup:** the INT-001 fixture, run twice with different task files (or the same task file twice).
  - The first execution delivers externally, as in INT-001.
  - In the second, the fake `generate-code` does nothing and the fake repair writes nothing.
- **Action:** dispatch the first execution, then the second.
- **Assertions:**
  - The first execution succeeds.
  - The second execution's `prepare-task-delivery` produced a record path different from the first.
  - The second execution fails at `verify-task-commit`, with the "did not produce a commit" message and a stderr line naming its own record path.
  - The first execution's record file is still present.
- **Execution:** `internal/exec`, in the same file as INT-001, run by `make test`.

### INT-003: resuming a blocked gate keeps the record and passes
- **Covers:** External delivery record lifecycle (resume keeps the record; a human-written record after a block); Local delivery (unchanged path is not touched by resume).
- **Boundary:** `internal/runner` run and resume with persisted state, the `step-repair` resume re-entry at the inline check, and the real `verify-task-commit.sh` and `prepare-task-delivery.sh`.
- **Setup:**
  - A workflow run through the runner with a temporary session directory. It is either the builtin `implement-task` or a fixture that mirrors its `record-task-start` → `prepare-task-delivery` → agent → `verify-task-commit` shape with the same scripts and inputs.
  - The external fixture from INT-001, with the commit already pushed.
  - A fake agent whose repair ends with `REPAIR_BLOCKED`, so the first run fails as blocked.
  - After the failure, the test reads the record path from the check's failure stderr and writes a valid record there.
- **Action:** run, observe the blocked failure, write the record, then resume the run by session ID.
- **Assertions:**
  - The resumed run re-enters at `verify-task-commit` without re-running `record-task-start` or `prepare-task-delivery`.
  - The record written between the two runs was not removed.
  - The check succeeds through external delivery, and the run completes.
  - The audit log's `step_end` for the passing `verify-task-commit` contains the external delivery statement in `stdout`.
- **Execution:** `internal/runner` (next to `resume_repair_test.go`), run by `make test`.

### INT-004: workflow wiring contract
- **Covers:** Repair records external delivery (the prompt carries the record path and the no-push/no-modify rule); Validator repair does not re-implement external work; Downstream steps acknowledge external delivery; External delivery record lifecycle (step order).
- **Boundary:** the embedded YAML, loaded through the real loader and validator.
- **Setup:** none beyond the embedded builtins.
- **Action:** load `builtin:core/implement-task-v1.0.yaml`, `builtin:core/run-validator-v1.0.yaml`, `builtin:core/implement-change-v1.0.yaml`, and `builtin:core/verify-change-v1.0.yaml`.
- **Assertions:**
  - The workflow loads and validates.
  - `prepare-task-delivery` is a script step with `capture: task_delivery` and `capture_format: json`, placed after `record-task-start` and before `generate-code`.
  - `verify-task-commit`'s `script_inputs` include `starting_head`, `started_at`, and `record_path`, bound to `task_start_head` and `task_delivery.*`.
  - The repair prompt references `{{task_delivery.record_path}}`, forbids pushing to or modifying the external repository, and still contains `REPAIR_BLOCKED` guidance and the "never declare success" rule.
  - The `fix-violations` prompt contains the instruction to skip task-compliance violations for work directed to another repository, without re-implementing it or committing.
  - The `complete-task-index` prompt no longer claims that every task has a local implementation commit, and it mentions verified delivery in another repository.
  - The `open-draft-pr` prompt references `{{session_dir}}/output/task-delivery/` and a "Delivered in other repositories" section.
- **Execution:** `workflows` or `internal/exec` package tests, run by `make test`.

## End-to-End Tests

None. INT-001 through INT-003 exercise the real workflow definition, scripts, repair cycle, resume, and git at the highest layer that avoids a real agent CLI. The only thing a CLI-driven automated E2E would add is the real implementor, and AT-001 covers that.

## Agent Acceptance Tests

### AT-001: a cross-repository task continues without a placeholder commit
- **Classification:** Required.
- **Covers:** Repair records external delivery; External commit verification; Reporting external delivery; the issue's desired behavior.
- **Actor and surface:** a workflow user running `implement-task` headlessly through the CLI (`./dev.sh`), with a real implementor agent profile.
- **Setup:**
  - Temporary directories:
    - a run repository (git, one commit) that contains a task file;
    - a bare repository acting as the remote;
    - a clone of it as the external repository, with `origin/HEAD` set.
  - The task file tells the agent to implement a trivial change, such as adding a line to a README, **in the external repository's absolute path**. The agent commits on a new branch, pushes it to `origin`, and makes no change in the run repository.
  - An Agent Runner profile with an `implementor` agent available for the run repository.
- **Steps:**
  1. From the run repository, start the `core/implement-task` workflow (for example `./dev.sh -C <run-repo> <implement-task ref> task_file=<task> skip_validator=true`).
  2. Wait for completion.
  3. Inspect the run with the CLI or run view and its audit log.
- **Expected:**
  - The run completes successfully, with no human action.
  - `verify-task-commit` shows one failed attempt, a repair, then success. Its output says the task was delivered outside the repository and names the external repository, the pushed commit, the containing remote ref, and the coverage note.
  - The run repository has no new commit and a clean status.
  - The external repository has the pushed commit.
  - A record file exists under the run's `output/task-delivery/`.
- **Pass, fail, and invalid-run criteria:**
  - Pass: every expectation above holds.
  - Fail (product defect):
    - the gate rejects a record of pushed commits that the agent wrote correctly;
    - the output is missing required fields;
    - the repair commits in the run repository;
    - the repair pushes to or modifies the external repository.
  - Invalid setup, retry once and then report as a limitation:
    - `generate-code` itself commits in the run repository or ignores the task file's instructions about the external repository;
    - `generate-code` fails to push when told to.
- **Evidence:**
  - the terminal output or run view text of the `verify-task-commit` step;
  - `git log -1` and `git status` in both repositories;
  - the record file's contents;
  - the matching `step_end` audit line.
- **Effects and cleanup:** real agent CLI usage costs a small number of tokens and is authorized. Every repository is local and temporary, and nothing is pushed to any network remote. Delete the temporary directories afterward.
- **Permitted substitutes:** None.

### AT-002: a human unblocks a stopped run by pushing and writing the record
- **Classification:** Required.
- **Covers:** Repair records external delivery (unverifiable delivery still blocks); External commit verification (unpushed rejected); External delivery record lifecycle (a human-written record on resume); the discoverability of the record path.
- **Actor and surface:** a workflow user, using the CLI (`./dev.sh`, `-resume`), with a real implementor agent.
- **Setup:** the AT-001 layout, except that the task file tells the agent to commit in the external repository **without pushing**.
- **Steps:**
  1. Run `implement-task` as in AT-001, and observe that it stops.
  2. Read the failure output for the expected record path.
  3. In the external repository, push the branch.
  4. Write a record at that path (repository, commit).
  5. Resume the run with `./dev.sh -C <run-repo> -resume <run-id>`.
- **Expected:**
  - The first run fails at `verify-task-commit` as blocked, with a repair response ending in `REPAIR_BLOCKED`.
  - The external repository's branches and remote-tracking refs are unchanged by the repair. The bare remote has not received the branch.
  - The failure output shows the record path.
  - After the push, the record, and the resume, the gate passes through external delivery, and the run completes with no commit in the run repository.
- **Pass, fail, and invalid-run criteria:**
  - Pass: every expectation above holds.
  - Fail (product defect):
    - the repair pushes, creates branches, or commits in either repository;
    - the repair writes a record and the gate accepts unpushed commits;
    - the failure output lacks the record path;
    - the resumed run does not accept the valid human-written record.
  - Invalid setup, retry once and then report as a limitation: `generate-code` pushes despite the task file, or commits in the run repository.
- **Evidence:**
  - the failure output with the record path;
  - the record written;
  - the resumed run's `verify-task-commit` output;
  - the run repository's `git log -1` before and after.
- **Effects and cleanup:** same as AT-001.
- **Permitted substitutes:** None.

### AT-003: validator repair does not re-implement external work
- **Classification:** Conditional. It applies when the `agent-validator` CLI is installed and a task-compliance review can be configured in a temporary repository.
- **Covers:** Validator repair does not re-implement external work.
- **Actor and surface:** a workflow user running `implement-task` through `./dev.sh` with the validator enabled (`skip_validator=false`), and a real implementor agent.
- **Setup:**
  - The AT-001 layout, with a run repository on a feature branch that already has one earlier local commit, so the branch diff is non-empty, as it is for task 2 or later.
  - Agent Validator configured in the run repository, with the task-compliance review enabled (for example through `validator-setup`, or a minimal config).
  - A task file that directs the work to the external repository and tells the agent to push, as in AT-001.
- **Steps:** run `implement-task` with `skip_validator=false`, wait for completion, and inspect the run.
- **Expected:**
  - If task-compliance reports the task as unimplemented, `fix-violations` skips that violation with the external repository as the reason.
  - No commit appears in the run repository from `fix-violations` or the repair.
  - `verify-task-commit` accepts through external delivery, and the run completes.
  - If the validator passes without reporting the violation, the flow still passes as long as the run repository gets no new commit.
- **Pass, fail, and invalid-run criteria:**
  - Fail: any `fix-violations` or repair commit in the run repository that re-implements or stubs the external work.
  - Invalid setup, retry once: the validator cannot run because of configuration problems unrelated to this change.
- **Evidence:**
  - the `fix-violations` response, or the validator log showing the skip;
  - `git log` of the run repository before and after;
  - the `verify-task-commit` output.
- **Effects and cleanup:** same as AT-001, plus validator review token usage.
- **Permitted substitutes:** if the condition does not hold, INT-001's validator variant stands in, and the report must say so.

## Human-Only Testing

None.

## Coverage Map

| Requirement or journey | INT | E2E | AT | HT |
| --- | --- | --- | --- | --- |
| Local delivery (unchanged path under resume) | INT-003 | — | — | — |
| External delivery record | INT-001 | — | AT-001 | — |
| External commit verification | INT-001 (unpushed variant) | — | AT-001, AT-002 | — |
| External delivery record lifecycle | INT-001, INT-002, INT-003, INT-004 | — | AT-002 | — |
| Repair records external delivery (including no push or modification) | INT-001, INT-004 | — | AT-001, AT-002 | — |
| Reporting external delivery | INT-001, INT-003 | — | AT-001 | — |
| Cross-repository task continues autonomously (issue journey) | INT-001 | — | AT-001 | — |
| Human unblocks a stopped run without a placeholder commit | INT-003 | — | AT-002 | — |
| Validator repair does not re-implement external work | INT-001 (validator variant), INT-004 | — | AT-003 | — |
| Downstream steps acknowledge external delivery | INT-004 | — | — | — |
