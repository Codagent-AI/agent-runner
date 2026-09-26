## Why

`core/implement-task` ends with `verify-task-commit`, which accepts a task only if the run's own repository gained at least one new commit with tracked changes since the task started. The gate exists to catch an implementor that exits 0 without doing the work, and it does that well.

Some changes span repositories. A task can legitimately belong in another repository, and the implementor delivers it there. The gate then fails. Its repair prompt already names this case ("the work was legitimately delivered elsewhere"), but the only way it can respond is `REPAIR_BLOCKED`, which fails the run. A human then has to make a placeholder commit in the run's repository and resume. That is the exact kind of commit the repair prompt forbids the agent to make.

This happened in run `change-2026-09-21T02-09-14-398982Z` (`builtin:openspec/change-v2.0.yaml`, change `factory-feature-support` in Agent Factory). Task 3 said its work lived in Agent Runner. `generate-code` delivered it as commits in a local Agent Runner worktree and opened Codagent-AI/agent-runner#156. Agent Factory's HEAD did not move, so the gate failed, repair correctly blocked, and the run stopped until a human committed a note in the task file.

That is the only occurrence observed so far. It is the expected pattern whenever Agent Factory drives a change that needs supporting work in Agent Runner or another sibling repository, and Agent Factory is doing that more and more. Each occurrence costs a failed run and manual intervention. The placeholder commit also hides where the work really went, so a reviewer gets less information, not more.

The no-build alternative is to split cross-repository work into separate runs, one per repository. It was not chosen for three reasons. The planner often discovers that a task belongs elsewhere only while planning a single change. The pieces of a cross-repository change are usually coupled and must land together. And enforcing the split would mean changing planning skills that live outside this repository.

**Verdict: go with caveats.** The fix is small and fits the existing repair model, where the check stays the sole authority on success. It removes a manual stop without making the gate advisory. There are three caveats:

1. The gate can only verify delivery to a repository checked out locally, with the commits pushed to a remote. Delivery it cannot verify still blocks for a human.
2. An accepted external task promises less than a local one. It promises that real, new, pushed commits exist elsewhere. It does not promise that this run validated them.
3. The gate protects against honest-but-lazy implementors, not adversarial ones.

## What Changes

- `verify-task-commit` accepts either of two kinds of evidence:
  1. **Local delivery.** This is today's rule, unchanged: new descendant commits in the run's repository with tracked changes.
  2. **Verified external delivery.** A per-task delivery record, written outside the run's worktree, names another local git repository and the commits that deliver the task. The gate accepts the record only after verifying it against that repository. Agent-supplied facts are never taken on trust.
- To accept a record, the gate checks the following:
  - **The repository is really different.** The gate resolves the named path itself to a non-bare worktree root. It compares repository identity by resolved git common directory, not by path. A worktree or side branch of the run's own repository is rejected.
  - **Each commit is real and pushed.** Every listed commit must exist in that repository and be reachable from a remote-tracking ref.
  - **Each commit changes tracked files.**
  - **Each commit is new.** Each commit's committer date must be no earlier than the task's start, allowing a small skew tolerance. When the repository's remote default branch can be determined, no commit may already be reachable from it.

  A missing, malformed, stale, or unverifiable record fails the gate the same way no local commit does today.
- The `verify-task-commit` repair prompt changes. When work was legitimately delivered elsewhere, the repair agent writes the delivery record with verified facts (repository path, branch, commits, pull request URL if one exists) instead of blocking. `REPAIR_BLOCKED` remains the outcome for work it cannot isolate, a task that conflicts with the repository, and delivery the gate cannot verify, such as unpushed commits. The repair agent only inspects the external repository and writes the record. It must not push, create branches, or otherwise modify the external repository, because that would be an unauthorized outward-facing action.
- The per-task validator's repair step (`fix-violations` in `core/run-validator`) is told what to do with a task-compliance violation for work that the task directs to another repository. It must not re-implement that work in the run repository or add filler commits. Instead it skips the violation, stating where the work belongs. Without this, the validator repair could move HEAD with local re-implementation or filler, and the gate would accept the task through the local path.
- Downstream change steps stop contradicting external delivery. `core/implement-change`'s `complete-task-index` prompt no longer claims that every task has a local implementation commit. `core/verify-change`'s `open-draft-pr` prompt lists any external delivery records from the run's session directory in the draft pull request body (repository, commits, reported pull request), so the run's own reviewers and acceptance testers can see where a task's work lives.
- `implement-task` captures what the gate needs when a task starts fresh: the start time, and a per-task record location keyed by the task and its start marker. A record from an earlier task can therefore never satisfy a later one, and resuming a task never deletes a record already written for it.
- When the gate accepts external delivery, its output says so plainly, which makes it visible in the audit log and run views. The output names the verified repository, commits, and remote branch. It labels agent-reported fields it did not check, such as the pull request URL, as "reported, unverified". It also states that this run's validator and task-compliance review did not cover the external work. The record file stays in the run's session directory as a pointer that later steps or reviewers can read.

No breaking changes. Tasks that commit locally behave exactly as they do today.

## Capabilities

### New Capabilities
- `task-delivery-gate`: How `core/implement-task` decides that a task was delivered. Covers:
  - the existing local-commit rule;
  - the new verified external-delivery record: its contents, the rules for repository identity, pushed state, and freshness, and its per-task lifecycle;
  - what repair may do;
  - how accepted external delivery is reported, including its reduced validation coverage.

### Modified Capabilities
- None. The gate's behavior is not currently specified. `step-repair` semantics, where the check is the sole success authority and `REPAIR_BLOCKED` is honored, are used unchanged.

## Technical Approach

The change stays inside the existing check-and-repair pattern and requires no runner or engine changes:

- **Record location and lifecycle.** The record lives under the run's session directory (`{{session_dir}}/output/`), keyed by the task's identity and the start marker captured at the same point as `starting_head`. That keeps it out of the run's tracked worktree, so it cannot trip `check-clean-before-task` or `check-clean`, and one run can hold a record for each of several tasks. Any pre-existing record for the key is cleared only when the task starts fresh. Resuming a failed gate re-enters at the check, per `step-repair`, and keeps a record the repair agent already wrote. In contexts where `{{session_dir}}` is unavailable, the gate falls back to local-only verification. The workflow must never fail to load or interpolate because of it.
- **Gate script.** `verify-task-commit.sh` keeps its current local path first. Only when the local path fails does it look for the record. It verifies the record using object-level git commands only (`rev-parse`, `cat-file`, `merge-base`, `rev-list`, `diff-tree`, `for-each-ref`) run with `-c core.fsmonitor=false`. It runs no worktree-touching commands against the agent-supplied path, because those can execute programs configured in that repository. The script receives the task start time and record path through `script_inputs`, alongside `starting_head`.
- **Who writes the record.** Only the repair agent writes it. The `generate-code` prompt does not change, so the normal path does not invite out-of-repository delivery, and a failed check plus one repair round is an acceptable cost for a rare case. The repair agent already resumes the implementor's session, so it has the facts it needs.
- **Threat model and anti-bypass property.** The gate guards against an honest-but-lazy implementor, one that exits successfully without doing the work, not against an agent forging evidence. To pass, such an implementor would have to produce real, new, change-bearing commits pushed from some other local repository and name them, which is not a silent no-op. Each of these fails verification:
  - fabricated SHAs;
  - old or already-merged commits;
  - commits in the run's own repository or its worktrees;
  - empty commits;
  - unpushed commits.

  Committer timestamps can be forged, so freshness is a guard against reuse, not a guarantee against a malicious agent.
- **Validation coverage.** `run-validator` and change-level steps such as `verify-change` and pull request steps run only in the run's repository, so none of them sees the external commits. This change does not extend them. Instead the gate output, the record, and the draft pull request body make the reduced coverage explicit, and the external pull request remains the review surface for that work. The per-task validator still runs before the gate. Its repair is told to skip task-compliance violations for work directed elsewhere rather than satisfy them locally, so the validator cannot turn external delivery into local filler.
- **Rejected alternatives.**
  - *Task files declare a target repository at planning time.* This needs changes to the task-planning skill outside this repository and a new task-file convention. It still needs the same commit verification at the end.
  - *A caller-declared repository list, such as an `additional_repos` parameter on `implement-change` and `implement-task`.* The gate would snapshot each repository's HEAD at task start and apply the local descendant rule there. That replaces timestamp freshness and removes the arbitrary-path problem. It is deferred as a follow-up in case timestamp freshness proves weak, because it needs caller changes (Agent Factory and ad hoc runs) and a new parameter surface. The record approach works for any run without them.
  - *An advisory (warning) gate.* This loses the protection the gate exists for.
  - *Letting repair make a placeholder commit.* This pollutes history, contradicts the current prompt's rule, and does not verify anything.

Details are left for design: the record's exact format and file naming, the skew tolerance, how the remote default branch is resolved, and the exact wording of the accepted-output text.

## Out of Scope

- Verifying delivery that has no local checkout or was never pushed, and checking pull requests through the GitHub API. Such delivery still ends in `REPAIR_BLOCKED`.
- Running the validator, task-compliance review, or change-level verification against external repositories.
- Task-file declarations of target repositories, caller-declared repository lists, and any change to task-planning skills.
- Making the gate advisory or configurable per workflow.
- New run-summary, TUI, or run-complete screen sections for external delivery, beyond the gate's step output, the record kept in the session directory, and the draft pull request body section.
- Reordering `implement-task` so the gate runs before the validator, or skipping the validator for externally delivered tasks.
- Changing `step-repair` runner semantics or the `REPAIR_BLOCKED` protocol.
- Coordinating or merging work across repositories. The gate only verifies that the work exists and was pushed.

## Impact

- `workflows/core/verify-task-commit.sh`: the external-delivery verification path.
- `workflows/core/implement-task-v1.0.yaml`: task-start marker capture, the per-task record location and lifecycle, the new gate inputs, and the revised repair prompt. This ships in the binary and affects every workflow that uses `core/implement-task` (`openspec/implement-change`, `spec-driven/implement-change`, and `core/implement-change`).
- `workflows/core/run-validator-v1.0.yaml`: a `fix-violations` prompt instruction for task-compliance violations about work delivered in another repository. This affects every caller of `core/run-validator`.
- `workflows/core/implement-change-v1.0.yaml` (`complete-task-index` wording) and `workflows/core/verify-change-v1.0.yaml` (the `open-draft-pr` body lists external delivery records). Both are prompt-only.
- `workflows/verify_task_commit_test.go`: script tests for accepted and rejected records, including the worktree-of-same-repository, unpushed, stale, and empty-commit cases.
- `openspec/specs/task-delivery-gate/`: the new spec.
- Users: cross-repository tasks proceed without manual placeholder commits. Reviewers see where the work landed and that this run did not validate it. Single-repository runs see no change.
