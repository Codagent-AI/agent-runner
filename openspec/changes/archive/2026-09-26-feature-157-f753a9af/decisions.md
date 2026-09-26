# Decisions

## propose

1. **Verdict: go with caveats.**
   - Alternatives considered: no-go (keep the manual placeholder commit); go without caveats.
   - Decision-bearing: yes. The caveat is that only delivery to a local checkout can be verified. Anything else still blocks.

2. **Approach: a delivery record written by the repair agent, verified by the gate against a local git repository.**
   - Alternatives considered: task files declare a target repository at planning time (needs changes to the planning skill outside this repository); an advisory gate (loses the protection against no-op implementors); letting repair make a placeholder commit (pollutes history and verifies nothing).
   - Decision-bearing: yes. This follows the issue's first suggested approach and keeps the step-repair rule that the check is the sole success authority.

3. **The gate verifies the record and never trusts it.** Every listed commit must exist and be reachable in a repository other than the run's own, change tracked files, and be newer than the task's start.
   - Alternatives considered: accepting any well-formed record; checking the PR through the GitHub API.
   - Decision-bearing: yes. It keeps the issue's requirement that the gate still catches an implementor that did nothing.

4. **The record lives under `{{session_dir}}/output/`, isolated per task.**
   - Alternatives considered: a file in the run's worktree (would trip the clean-tree checks and need a commit); a note in the task file (that is the placeholder commit).
   - Decision-bearing: no. It is an implementation default taken from the existing `result_file` pattern in `verify-change`.

5. **Only the repair agent writes the record. The `generate-code` prompt does not change.**
   - Alternatives considered: telling `generate-code` to write the record directly, which skips one failed check but invites out-of-repository delivery on the normal path.
   - Decision-bearing: no.

6. **Accepted external delivery is reported only through the gate's step output (audit log and run views) and the record file. There are no new run-summary or UI surfaces.**
   - Alternatives considered: a dedicated run-summary or run-complete screen section.
   - Decision-bearing: no. The issue frames reporting as optional ("could record").

7. **Remote-only delivery (no local checkout) is out of scope and still ends in `REPAIR_BLOCKED`.**
   - Alternatives considered: GitHub API verification of pushed commits and PRs.
   - Decision-bearing: no. It keeps the scope small, and the observed case used a local worktree.

8. **No direction-level stop.** The change stays within this repository, does not break a public interface or a persisted format (the new record file is additive, and the existing gate behavior is unchanged), and matches the issue's reading.
   - Decision-bearing: no.

## review-proposal

Findings from `proposal-review-findings.json`. None is direction-level: every applied change stays inside this repository, keeps the issue's intent, and breaks no public interface or persisted format.

1. **PR-1 (significant), applied.** The proposal now states that an accepted external task promises only that real, new, pushed commits exist elsewhere, not that this run validated them. The gate requires the commits to be reachable from a remote-tracking ref. Its output says that the validator and task-compliance review did not cover the work. The record file in the session directory is the pointer for later steps.
   - Alternatives considered: extending the validator to external repositories (rejected as out of scope); only recording push state without checking it (rejected because it would be trusting the agent).
   - Decision-bearing: yes. Requiring pushed state narrows what passes: unpushed external work still blocks.

2. **PR-2 (significant), applied.** Repository identity is compared by resolved git common directory. The gate resolves the path to a non-bare worktree root itself, so worktrees of the run's own repository are rejected.
   - Alternatives considered: comparing paths or `--show-toplevel` (bypassable through a worktree).
   - Decision-bearing: yes. The anti-bypass property depends on it.

3. **PR-3 (significant), applied.** The threat model is now named as honest-but-lazy. Freshness uses committer date with a skew tolerance, and commits already reachable from the external remote default branch are rejected when that branch can be determined. A task whose external PR is merged before the gate runs will block for a human. That is accepted as rare.
   - Alternatives considered: timestamps only; HEAD snapshots through declared repositories (see PR-4).
   - Decision-bearing: yes.

4. **PR-4 (minor), applied as documentation.** A caller-declared repository list (`additional_repos`) is recorded as a rejected alternative and deferred as a follow-up, because it needs caller changes. The record approach stays as the baseline.
   - Decision-bearing: no.

5. **PR-5 (minor), applied.** The proposal now states that there is one observed occurrence (replacing "becoming routine") and when the pattern is expected to recur. It also explains why splitting cross-repository work into per-repository runs is not preferred: tasks turn out to belong elsewhere during single-change planning, the pieces are coupled, and enforcing the split would mean changing planning skills outside this repository.
   - Decision-bearing: no.

6. **PR-6 (minor), applied.** The record lifecycle is now stated. The record is cleared only on a fresh task start (at the `starting_head` capture point), keyed by task identity plus start marker, and preserved when a failed gate is resumed. When `session_dir` is unavailable, the gate falls back to local-only verification and never fails to load.
   - Decision-bearing: no.

7. **PR-7 (minor), applied.** Verification is limited to object-level git commands run with `-c core.fsmonitor=false`. Unchecked fields such as the PR URL are labeled "reported, unverified" in the gate output.
   - Alternatives considered: verifying the PR URL through the GitHub API (out of scope).
   - Decision-bearing: no.

## spec

1. **The proposal lists one capability, so there is one spec file: `specs/task-delivery-gate/spec.md`. No existing spec is modified.**
   - Alternatives considered: adding the gate to `builtin-workflows`.
   - Decision-bearing: no.

2. **The external record is consulted only when HEAD is unchanged. Local commits that don't descend from the starting HEAD, or that change no tracked files, fail even if a valid record exists.**
   - Alternatives considered: falling through to the record whenever the local check fails.
   - Decision-bearing: yes. Broken local history should never be masked by a claim about another repository.

3. **"Pushed" means reachable from any remote-tracking ref (`refs/remotes/*`). "Already merged" means reachable from any remote's recorded default branch (`refs/remotes/<remote>/HEAD`). When no remote records a default branch, that check is skipped.**
   - Alternatives considered: checking only `origin`; querying remotes over the network.
   - Decision-bearing: no.

4. **Every listed commit must pass every check. One failing commit fails the record, and the failure message names that commit and the check it failed.**
   - Alternatives considered: accepting the record if at least one commit passes.
   - Decision-bearing: no.

5. **A root commit (no parent) counts as change-bearing if it contains files.**
   - Decision-bearing: no.

6. **Resuming a blocked gate keeps the current task execution's record. A human can therefore unblock by writing a valid record instead of making a placeholder commit.**
   - Decision-bearing: no. This follows from `step-repair`'s rule that resume re-enters at the check.

7. **The external-repository safety rule forbids executing repository-configured programs (fsmonitor, hooks, diff/textconv drivers) and mutating refs, the index, or the worktree. The exact command set is left to design.**
   - Decision-bearing: no.

8. **Deferred to design: the record format and file naming, the skew tolerance value, and how the start time is captured.**
   - Decision-bearing: no.

## design

1. **A new `prepare-task-delivery` script step, placed after `record-task-start`, captures a JSON map (`record_path`, `started_at`). The record path is `<session_dir>/output/task-delivery/<task_key>-<started_at>-<head12>.json`.**
   - Alternatives considered: a shell `command` capture (only one string); storing the record under the run repository's git directory (separated from the run evidence).
   - Decision-bearing: no.

2. **Spec updated: the "no session directory" fallback moves from `implement-task` to the `verify-task-commit` script (no record location means local-only verification). The spec scenario is replaced accordingly.**
   - Alternatives considered: conditional interpolation (not supported); runner changes (out of scope).
   - Decision-bearing: no. Runner-launched runs always have a session directory, and builtin script materialization already needs one.

3. **Spec updated: the record's JSON shape (`repository`, `commits`, optional `branch` and `pull_request`), the 300-second skew tolerance, and the rule that a failed gate prints the expected record path. This completes the design deferrals.**
   - Decision-bearing: no.

4. **Change-bearing is detected by comparing tree IDs (the empty tree ID for root commits). Pushed state uses `for-each-ref --contains` over `refs/remotes/`, excluding `*/HEAD`. The merged check uses `refs/remotes/*/HEAD` symrefs. The only git commands run are `rev-parse`, `for-each-ref`, `cat-file`, `merge-base`, and `hash-object` (without `-w`), with `GIT_OPTIONAL_LOCKS=0` and `-c core.fsmonitor=false`.**
   - Alternatives considered: `diff-tree` (could invoke drivers); `git log`/`show` (porcelain).
   - Decision-bearing: no.

5. **The record is parsed with the existing jq-or-python3 dual path. Limits: 64 KiB, at most 100 commits, 7–64 character hex IDs, no control characters.**
   - Decision-bearing: no.

6. **Runs stopped mid-task on an older binary will fail at `verify-task-commit` on resume, because the `task_delivery` capture is missing. This is accepted as pre-release, with no mitigation.**
   - Alternatives considered: a default or fallback for the missing capture (adds complexity for a transient case).
   - Decision-bearing: no.

7. **The repair budget stays at 1, and `generate-code` is unchanged.**
   - Decision-bearing: no.

## test-plan

1. **Four integration obligations, all using real scripts, real git, and the real repair cycle, with only the agents faked:**
   - INT-001: repair-recorded external delivery through the builtin `implement-task`;
   - INT-002: record isolation between tasks;
   - INT-003: resume after a blocked gate keeps a human-written record;
   - INT-004: the YAML wiring contract.

   They follow the existing harnesses in `internal/exec/verify_change_workflow_test.go` and `internal/runner/resume_repair_test.go`.
   - Alternatives considered: script-only tests, which miss the wiring.
   - Decision-bearing: no.

2. **No automated E2E. The only thing a CLI-driven E2E would add is the real agent CLI, and acceptance covers that.**
   - Alternatives considered: a CLI E2E with a stub agent CLI (duplicates INT-001 through INT-003).
   - Decision-bearing: no.

3. **Two required acceptance flows through `./dev.sh` with a real implementor agent:**
   - AT-001: autonomous cross-repository continuation;
   - AT-002: a human unblocks a stopped run by pushing, writing the record, and resuming.

   Both use only local temporary repositories and a local bare remote, so they have no network side effects. Agent token cost is authorized. No substitutes are permitted.
   - Decision-bearing: no.

4. **Human-only testing: none.**
   - Decision-bearing: no.

## review-approach

Findings from `approach-review-findings.json`. None is direction-level: every applied change is a prompt-only or definition-only change inside this repository, consistent with the issue.

1. **AR-1 (high), applied.** The `fix-violations` prompt in `core/run-validator` now tells the agent to skip task-compliance violations for work that the task directs to another repository, without re-implementing it or committing. The spec adds the requirement "Validator repair does not re-implement external work". The design adds Approach §5 and Decision 8. The test plan adds an INT-001 validator variant, INT-004 wiring assertions, and a conditional AT-003.
   - Alternatives considered: reordering the gate before the validator and skipping the validator for external tasks. Rejected because the gate must see the final local state after validator fixes and leftover commits, and splitting the gate adds complexity.
   - Decision-bearing: yes. The prompt change affects every caller of `core/run-validator`, but it only applies to violations about work directed to another repository.

2. **AR-2 (medium), applied.** The spec and the repair prompt now forbid the repair agent from pushing, creating or moving branches, committing, or modifying the external repository. It only inspects and writes the record. AT-002 now requires the blocked outcome and treats a repair-initiated push as a defect.
   - Alternatives considered: explicitly permitting the repair to push. Rejected because it is an unauthorized outward-facing action and would widen the gate's trust.
   - Decision-bearing: yes.

3. **AR-3 (medium), applied (the preferred option, prompt-only).** The `complete-task-index` wording now allows for verified delivery in another repository. The `open-draft-pr` prompt lists `{{session_dir}}/output/task-delivery/*.json` records in a "Delivered in other repositories" draft PR body section. The spec adds the requirement "Downstream steps acknowledge external delivery". The proposal's What Changes, Out of Scope, and Impact sections were updated. Review-assumptions and acceptance-handoff prompts were not changed, because the PR body is the reviewer-facing surface that acceptance testers already read.
   - Alternatives considered: narrowing the proposal's claims to the gate output only.
   - Decision-bearing: yes. It adds prompt changes in `implement-change` and `verify-change`.

4. **AR-4 (low), applied.** AT-001, AT-002, and AT-003 now have explicit pass, fail (product defect), and invalid-setup (retry once) criteria.
   - Decision-bearing: no.

## tasks

1. **Exactly one implementation task, `tasks/01-external-task-delivery.md`, linked from `tasks.md`. It follows the repository's convention of index entries plus per-task files.** It covers:
   - the scripts;
   - the `implement-task` wiring;
   - the four prompt edits;
   - the docs note;
   - the script tests and INT-001 through INT-004.

   Acceptance flows AT-001 through AT-003 are left to the acceptance phase.
   - Alternatives considered: splitting the work into script, wiring, and prompt tasks. The instruction required a single task.
   - Decision-bearing: no.

2. **A short docs note in `docs/built-in-workflows.md` is included in the task, although no artifact required it, so users can discover the behavior.**
   - Decision-bearing: no.
