# task-delivery-gate Specification

## Purpose
TBD - created by archiving change feature-157-f753a9af. Update Purpose after archive.
## Requirements
### Requirement: Local delivery

`core/implement-task` SHALL capture the run repository's HEAD before `generate-code` runs. `verify-task-commit` SHALL accept the task through local delivery when three things hold: the current HEAD differs from that starting HEAD, it descends from the starting HEAD, and the range between them changes tracked files. It SHALL reject local commits that do not descend from the starting HEAD, and local commits that change no tracked files. Either rejection SHALL fail the gate with a message naming the defect, and SHALL NOT be rescued by an external delivery record. The external delivery record SHALL be consulted only when HEAD is unchanged. Local-delivery behavior and its accepting output SHALL be the same as before external delivery existed.

#### Scenario: Local commit accepted
- **WHEN** a task leaves one new descendant commit that modifies a tracked file in the run repository
- **THEN** `verify-task-commit` succeeds, reports the number of commits produced, and does not read any external delivery record

#### Scenario: No local commit and no record
- **WHEN** HEAD is unchanged since the task started and no external delivery record exists for the task
- **THEN** `verify-task-commit` fails with a message stating that the task did not produce a commit

#### Scenario: Non-descendant local commit is not rescued
- **WHEN** HEAD moved to a commit that is not a descendant of the starting HEAD and a valid external delivery record exists
- **THEN** `verify-task-commit` fails with a message stating that the task ended at a non-descendant commit

#### Scenario: Empty local commits are not rescued
- **WHEN** the task produced local commits that change no tracked files and a valid external delivery record exists
- **THEN** `verify-task-commit` fails with a message stating that the commits contain no tracked changes

### Requirement: External delivery record

When a task's work was delivered in another repository, the task SHALL be able to claim external delivery through a per-task external delivery record, which the task owns and which lives in the run's session directory, outside the run repository's worktree. The record SHALL name the local path of the repository where the work landed and a non-empty list of commit IDs that deliver the task. It MAY name a branch and a pull request URL. Writing the record SHALL NOT create, modify, or stage any file in the run repository's worktree. A record that cannot be parsed, has no repository path, or has an empty commit list SHALL fail the gate with a message identifying the malformed record.

The record SHALL be a JSON object with `repository` (string path), `commits` (array of commit ID strings), and optional `branch` and `pull_request` strings. Unknown fields SHALL be ignored. `core/implement-task` SHALL tell the repair agent the exact record path for the current task execution, and a failing gate SHALL print that path in its failure output.

#### Scenario: Record does not dirty the run repository
- **WHEN** a repair agent writes the external delivery record for the current task
- **THEN** `git status --porcelain` in the run repository reports no change caused by the record

#### Scenario: Record path is discoverable from a failed gate
- **WHEN** `verify-task-commit` fails because HEAD is unchanged and no record exists
- **THEN** its failure output includes the record path expected for the current task execution

#### Scenario: Malformed record rejected
- **WHEN** HEAD is unchanged and the record for the task has an empty commit list
- **THEN** `verify-task-commit` fails with a message identifying the record as malformed

### Requirement: External repository identity

`verify-task-commit` SHALL resolve the record's repository path itself to the root of a non-bare git worktree. It SHALL treat the named repository as external only when that repository's resolved git common directory differs from the run repository's. Paths that do not resolve to a non-bare worktree root SHALL be rejected. So SHALL repositories that share the run repository's common directory, such as an additional worktree of the run repository or the run repository reached through another path. In either case the gate SHALL fail with a message stating why the repository is not an acceptable external repository.

#### Scenario: Separate repository accepted as external
- **WHEN** the record names a clone of a different repository whose listed commits pass commit verification
- **THEN** `verify-task-commit` succeeds through external delivery

#### Scenario: Worktree of the run repository rejected
- **WHEN** the record names another worktree of the run repository, and the listed commits are on a side branch there
- **THEN** `verify-task-commit` fails with a message stating that the named repository is the run repository

#### Scenario: Non-repository path rejected
- **WHEN** the record names a directory that is not a git worktree, or names a bare repository
- **THEN** `verify-task-commit` fails with a message stating that the path is not a usable repository worktree

### Requirement: External commit verification

`verify-task-commit` SHALL accept external delivery only when every commit listed in the record, verified in the named external repository, meets all of the following:
- it exists as a commit object;
- it is reachable from at least one remote-tracking ref;
- it changes at least one tracked file relative to its first parent, or contains files if it has no parent;
- its committer date is no earlier than the task's recorded start time minus a skew tolerance of 300 seconds;
- it is not reachable from the default branch recorded for any of the repository's remotes. This check applies only when such a default branch is recorded.

If any listed commit fails a check, the gate SHALL fail with a message naming that commit and the check it failed. Commit identifiers SHALL be resolved to full commit IDs before these checks. A listed identifier that does not name a commit SHALL fail the "exists" check.

#### Scenario: Pushed new commit accepted
- **WHEN** HEAD is unchanged and the record lists one commit that exists in the external repository, is contained in a remote-tracking branch, modifies a tracked file, was committed after the task started, and is not on any remote default branch
- **THEN** `verify-task-commit` succeeds through external delivery

#### Scenario: Fabricated commit rejected
- **WHEN** the record lists an ID that does not name a commit in the external repository
- **THEN** `verify-task-commit` fails with a message naming that ID as not found

#### Scenario: Unpushed commit rejected
- **WHEN** the record lists a commit that exists only on a local branch of the external repository
- **THEN** `verify-task-commit` fails with a message stating that the commit is not reachable from any remote-tracking ref

#### Scenario: Empty commit rejected
- **WHEN** the record lists a pushed commit that changes no tracked files
- **THEN** `verify-task-commit` fails with a message stating that the commit contains no tracked changes

#### Scenario: Stale commit rejected
- **WHEN** the record lists a pushed commit whose committer date is more than 300 seconds before the task started
- **THEN** `verify-task-commit` fails with a message stating that the commit predates the task

#### Scenario: Already-merged commit rejected
- **WHEN** the record lists a commit that is reachable from the external repository's recorded remote default branch
- **THEN** `verify-task-commit` fails with a message stating that the commit is already on the default branch

#### Scenario: One bad commit fails the record
- **WHEN** the record lists two commits and only one passes every check
- **THEN** `verify-task-commit` fails, naming the commit that failed

#### Scenario: No recorded remote default branch
- **WHEN** the external repository's remotes record no default branch and every listed commit passes the other checks
- **THEN** `verify-task-commit` succeeds through external delivery

### Requirement: Verification does not execute repository-configured programs

When verifying an external delivery record, `verify-task-commit` SHALL NOT run commands that execute programs configured by the external repository. Such programs include filesystem monitors, hooks, and diff or text-conversion drivers. Verification SHALL NOT modify the external repository's refs, index, or worktree.

#### Scenario: Configured filesystem monitor is not run
- **WHEN** the external repository configures `core.fsmonitor` to a program that writes a marker file, and a record naming that repository is verified
- **THEN** the marker file is not created, whether verification passes or fails

#### Scenario: External repository unchanged by verification
- **WHEN** a record naming an external repository is verified
- **THEN** that repository's refs, index, and worktree status are the same before and after verification

### Requirement: External delivery record lifecycle

Each record SHALL be scoped to one task execution, identified by the task and the start marker captured when that execution started. When a task starts fresh, `core/implement-task` SHALL make sure no record from an earlier task or an earlier execution of the same task is visible to that execution's gate. Resuming a run that stopped at or inside `verify-task-commit` SHALL keep any record already written for the current task execution. When `verify-task-commit` is invoked without a record location, it SHALL verify local delivery only.

#### Scenario: Earlier task's record does not satisfy a later task
- **WHEN** task 1 was accepted through external delivery and task 2 then leaves HEAD unchanged without writing a record
- **THEN** task 2's `verify-task-commit` fails with a message stating that the task did not produce a commit

#### Scenario: Resume preserves the record
- **WHEN** a repair agent writes a valid record, the run is interrupted before the check reruns, and the run is resumed
- **THEN** the resumed check reads that record and succeeds through external delivery

#### Scenario: Resume after a blocked gate uses a record written by a human
- **WHEN** `verify-task-commit` stopped as blocked, a human then writes a valid record for the current task execution, and the run is resumed
- **THEN** the check reruns and succeeds through external delivery

#### Scenario: No record location supplied
- **WHEN** `verify-task-commit` receives only a starting HEAD, and HEAD is unchanged
- **THEN** it fails with a message stating that the task did not produce a commit, and reads no record

### Requirement: Repair records external delivery

The `verify-task-commit` repair agent SHALL be instructed to handle work it has verified was legitimately delivered in another local repository and pushed: it SHALL write the external delivery record for the current task execution, SHALL make no commit in the run repository, and SHALL NOT end with `REPAIR_BLOCKED`. It SHALL still end with `REPAIR_BLOCKED` in these cases: the task conflicts with the run repository, task-related changes cannot be isolated safely, or the external delivery is one the gate cannot verify (no local checkout, or commits not pushed). It SHALL NOT be instructed to create empty or placeholder commits. It SHALL be instructed not to push, create or move branches, commit, or otherwise modify the external repository. It only inspects that repository and writes the record. Whether the task passes SHALL be decided only by the rerun of `verify-task-commit`.

#### Scenario: Repair records delivery and the gate passes
- **WHEN** `generate-code` delivered the task as pushed commits in a local checkout of another repository, and `verify-task-commit` fails on its first run
- **THEN** the repair agent writes the external delivery record without committing in the run repository, the rerun of `verify-task-commit` succeeds, and the workflow continues to the next step

#### Scenario: Unverifiable delivery still blocks
- **WHEN** the task's work exists only as unpushed commits in another repository
- **THEN** the repair agent ends with `REPAIR_BLOCKED` and the step fails as blocked

#### Scenario: Repair does not push unpushed external work
- **WHEN** the task's work exists only as unpushed commits in another repository, and the repair runs
- **THEN** the external repository's refs and remote-tracking refs are unchanged after the repair, and the step fails as blocked

#### Scenario: Repair claim without valid record does not pass
- **WHEN** the repair agent reports success but writes a record whose commits fail verification
- **THEN** the rerun of `verify-task-commit` fails and the step fails once the repair budget is exhausted

### Requirement: Validator repair does not re-implement external work

The per-task validator repair (`fix-violations` in `core/run-validator`) SHALL be instructed to handle task-compliance violations about work that the task directs to a different repository. It SHALL NOT re-implement that work in the run repository and SHALL NOT add commits to satisfy the violation. Instead it SHALL skip the violation, stating the repository where the work belongs. Validator repairs for all other failures SHALL be unchanged.

#### Scenario: Compliance violation for externally directed work is skipped
- **WHEN** `run-validator` reports that a task directing its work to another repository is not implemented in the run repository
- **THEN** the `fix-violations` prompt instructs the agent to skip that violation with the external repository as the reason, and not to change or commit in the run repository

#### Scenario: External task passes after validation without local filler
- **WHEN** validation runs for an externally delivered task, the validator repair skips the external-work violation, and the repair then writes a valid record
- **THEN** the run repository has no new commit, and `verify-task-commit` succeeds through external delivery

### Requirement: Downstream steps acknowledge external delivery

`core/implement-change`'s task-index step SHALL NOT tell the lead agent that every task produced a local implementation commit. It SHALL allow for tasks accepted through external delivery. When `verify-task-commit` accepts a record, it SHALL mark it accepted next to the record (`<record>.accepted`, holding the accepting output), and a fresh task start SHALL remove any stale marker along with a stale record. `core/verify-change`'s draft pull request step SHALL be instructed to read only the accepted external deliveries in the run's session directory, never records the gate rejected, and to list each record's repository, commits, and reported pull request in the draft pull request body, marked as delivered in another repository. When there are no records, the pull request body SHALL NOT include that section.

#### Scenario: Draft pull request lists external deliveries
- **WHEN** a change run accepted one task through external delivery, and `open-draft-pr` runs in the same run
- **THEN** the `open-draft-pr` prompt directs the agent to the run's external delivery records and to list them in the draft pull request body

#### Scenario: Task index wording allows external delivery
- **WHEN** `complete-task-index` runs after a task was accepted through external delivery
- **THEN** its prompt describes completed tasks as having either a local implementation commit or a verified external delivery

### Requirement: Reporting external delivery

When `verify-task-commit` accepts external delivery, its output SHALL:
- state that the task was delivered outside the run repository;
- name the resolved external repository path, each verified commit ID, and a remote-tracking ref that contains the commits;
- show agent-reported fields it did not verify, such as the pull request URL and branch name, labeled as reported and unverified;
- state that pushed state was judged from the external clone's local remote-tracking refs, without contacting the remote;
- state that this run's validator and task-compliance review did not cover the external work.

This output SHALL appear as the step's output in the audit log and run views. The record SHALL remain in the session directory after the run completes.

#### Scenario: Accepted output names the delivery
- **WHEN** `verify-task-commit` accepts a record that lists two commits and a pull request URL
- **THEN** the step output states external delivery, lists the repository path, both commit IDs, and a containing remote-tracking ref, shows the pull request URL labeled as reported and unverified, and states that the validator did not cover the external work

#### Scenario: Output visible in audit
- **WHEN** a run's task is accepted through external delivery
- **THEN** that step's recorded output in the run's audit log contains the external delivery statement

#### Scenario: Record retained after the run
- **WHEN** a run with an externally delivered task completes
- **THEN** that task's external delivery record is still present in the run's session directory

