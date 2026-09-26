## Context

`workflows/core/implement-task-v1.0.yaml` runs these steps: `check-task-file` → `check-clean-before-task` → `record-task-start` (captures `task_start_head`) → `generate-code` → `run-validator` → `check-clean` → `commit-leftovers-if-needed` → `verify-task-commit` → `session-report`.

`verify-task-commit` is a script step (`workflows/core/verify-task-commit.sh`). It reads `{"starting_head": …}` from stdin, parsing it with `jq` when available and falling back to `python3`. It fails in three cases: HEAD is unchanged, HEAD is not a descendant of the starting HEAD, or the range changes nothing. Its inline repair resumes the implementor session with a budget of 1 and today ends with `REPAIR_BLOCKED` for work delivered elsewhere.

Relevant runtime facts:

- Under `step-repair`, the check is the only authority on success. Resuming a run that failed at an exhausted or blocked inline check re-enters at the check with a fresh budget. Completed earlier steps and their captures are not re-run.
- A script step's stdout and stderr are written to the audit log's `step_end` event (`internal/exec/script.go`, `emitScriptEnd`).
- A script step may use `capture_format: json` to capture a map of strings. Later steps can reference its fields as `{{name.field}}` in prompts, `script_inputs`, and commands.
- The runner always sets a session directory for a run (`internal/runner/runner.go`, `resume.go`). Builtin scripts are already materialized under `<session_dir>/bundled/<namespace>/`, so `implement-task` already needs a session directory in practice.
- Interpolating `{{session_dir}}` fails when the directory is unset. Workflows can't fall back conditionally.

## Goals / Non-Goals

**Goals:**
- Let a task pass `verify-task-commit` through a verified external delivery record, as specified in `specs/task-delivery-gate/spec.md`.
- Keep the local rule and its messages unchanged.
- Verify the record against git objects only, without executing anything the external repository configures.
- Make the record path deterministic per task execution, discoverable, and preserved across resume.
- Stop the validator repair and downstream prompts from contradicting external delivery, and surface records in the draft pull request body.

**Non-Goals:**
- No Go runner, engine, or executor changes. This is YAML plus bundled shell scripts.
- No network access (`git fetch`, the GitHub API).
- No validator or task-compliance run against the external repository.
- No reordering of `implement-task` and no skipping of the validator for external tasks. The `fix-violations` instruction is enough to stop local filler.

## Approach

### Workflow changes (`implement-task-v1.0.yaml`)

```
record-task-start ──► prepare-task-delivery (new script step) ──► generate-code ──► … ──► verify-task-commit
                          capture: task_delivery (json map)                                  script_inputs:
                            record_path, started_at                                            starting_head
                                                                                               started_at
                                                                                               record_path
```

1. **New step `prepare-task-delivery`.** This script step runs `prepare-task-delivery.sh` with `capture: task_delivery` and `capture_format: json`. Its `script_inputs` are:
   - `session_dir: "{{session_dir}}"`
   - `task_file: "{{task_file}}"`
   - `starting_head: "{{task_start_head}}"`

   The script:
   - validates its inputs, the same way `verify-task-commit.sh` does;
   - sets `started_at=$(date -u +%s)`;
   - derives `task_key` from the basename of `task_file` without `.md`, replacing every character outside `[A-Za-z0-9._-]` with `_`;
   - builds `record_path="$session_dir/output/task-delivery/${task_key}-${started_at}-$(printf %.12s "$starting_head").json"`;
   - runs `mkdir -p` on the directory and removes any file already at `record_path`;
   - prints `{"record_path": "...", "started_at": "..."}`.

   The step runs only when a task starts fresh. On resume inside the task it is already complete, so the capture persists and nothing is removed.

2. **`verify-task-commit` inputs** gain two keys:
   - `started_at: "{{task_delivery.started_at}}"`
   - `record_path: "{{task_delivery.record_path}}"`

3. **Repair prompt rewrite.** It keeps the existing inspection and local-commit instructions, and the rule against empty or placeholder commits. It replaces the "delivered elsewhere → `REPAIR_BLOCKED`" branch with this instruction: if the task's work was delivered as commits in a local checkout of a different repository, and those commits are pushed, write `{{task_delivery.record_path}}` as JSON in the shape `{"repository": "<absolute path>", "commits": ["<full sha>", …], "branch": "<name>", "pull_request": "<url>"}`, where `branch` and `pull_request` are optional. The prompt also tells the agent to make no commit in this repository and not to end with `REPAIR_BLOCKED`. `REPAIR_BLOCKED` stays the ending for:
   - unpushed work;
   - work with no local checkout;
   - tasks that conflict with the repository;
   - changes that can't be isolated.

   The prompt forbids pushing, creating or moving branches, committing, or otherwise modifying the external repository. The agent may only inspect that repository with read-only git commands and write the record. Unpushed work therefore ends in `REPAIR_BLOCKED`, and a human decides whether to push. The prompt keeps "Never declare success yourself; the check that runs after you decides."

4. **`generate-code` is unchanged.**

5. **Validator repair (`run-validator-v1.0.yaml`, `fix-violations`).** One paragraph is added to the prompt. It covers a task-compliance review violation saying the task's work is missing, when the task file directs that work to a different repository and it was delivered there. For that case the agent must not re-implement the work here or add commits for it. It skips the violation with `agent-validator update-review skip <#> "delivered in <repository>; see task file"`. All other instructions are unchanged. `fix-violations` inherits the implementor session, which read the task file, so it has the context to recognize the case. The step order does not change: the validator still runs before the gate, a skipped violation lets the validator loop finish, and the gate then fails and repair writes the record.

### Downstream prompt changes (prompt-only)

- **`core/implement-change-v1.0.yaml`, `complete-task-index`.** The first sentence becomes "…has now completed successfully, each with its implementation commit or a verified delivery in another repository, and, unless `skip_validator` is `true`, task-compliance validation."
- **`core/verify-change-v1.0.yaml`, `open-draft-pr`.** One instruction is added to the body bullet. If `{{session_dir}}/output/task-delivery/` contains `*.json` records, the agent adds a "Delivered in other repositories" section with one entry per record: repository, commits, and the pull request (if reported). It notes that this change's validation did not cover that work. If there are no records, the section is omitted. The glob is harmless when the directory is missing, as in a standalone `verify-change` run.

### Gate script (`verify-task-commit.sh`)

Input: a JSON object with a required `starting_head` (a hex string), plus optional `started_at` (decimal epoch seconds) and `record_path` (an absolute path). Either both optional keys are present or neither is. A mismatched pair exits 2, the same as other invalid input.

Flow:

1. **Local path (unchanged).** If HEAD ≠ `starting_head`, apply today's three checks in order, with today's messages and exit codes. On success, print today's `implementation task produced N commit(s)` and exit 0. The record is never read here.
2. **HEAD unchanged, no `record_path` input.** Print today's "did not produce a commit" message and exit 1.
3. **HEAD unchanged, no file at `record_path`.** Print today's message, then a second stderr line: `no external delivery record at <record_path>`. Exit 1.
4. **Parse the record** using `jq`, falling back to `python3` (the existing dual-parser pattern). The parser emits a normalized line form: `repository`, `branch`, `pull_request`, then one commit ID per line. Rejection cases, each exiting 1 with a `malformed external delivery record: <reason>` message:
   - the file is larger than 64 KiB;
   - it is not a JSON object;
   - `repository` is missing, not a string, or not absolute;
   - `commits` is missing, not an array, or empty;
   - `commits` has more than 100 entries;
   - any commit ID is not 7–64 lowercase hex characters;
   - any string field contains a control character.

   Rejecting control characters keeps the line-oriented hand-off safe.
5. **Resolve repository identity.** All git calls against the external path go through a helper:

   ```sh
   xgit() { GIT_OPTIONAL_LOCKS=0 git -C "$repo" -c core.fsmonitor=false "$@"; }
   ```

   - `xgit rev-parse --is-bare-repository` must print `false`, and `xgit rev-parse --show-toplevel` must succeed. The worktree root is that output. Otherwise, fail with "not a usable repository worktree".
   - Compute each repository's common directory as `cd "$(git -C <root> rev-parse --git-common-dir)" && pwd -P`, resolved relative to that root, once for the external repository and once for the run repository. If the two are equal, fail with "named repository is the run repository". This catches worktrees, symlinked paths, and subdirectories of the run repository. A non-repository directory nested inside the run repository also resolves upward to the run repository and is rejected.
6. **Per-commit checks,** in the listed order, with the first failure reported as `commit <id>: <reason>`:
   - **Exists:** `xgit rev-parse --verify --quiet "<id>^{commit}"` gives the full ID.
   - **Pushed:** `xgit for-each-ref --contains <full> --format='%(refname)' refs/remotes/`, excluding refs that end in `/HEAD`. At least one ref must remain. The first remaining ref is kept for reporting.
   - **Change-bearing:** compare trees. `xgit rev-parse <full>^{tree}` must differ from `<full>^1^{tree}`. For a root commit, it must differ from the empty tree ID, which `xgit hash-object -t tree /dev/null` computes, so SHA-256 repositories work too.
   - **Fresh:** `xgit cat-file commit <full>`. Take the committer line's epoch field, which must be ≥ `started_at - 300`.
   - **Not merged:** `xgit for-each-ref --format='%(refname) %(symref)' refs/remotes/`. For each `*/HEAD` with a symref target, `xgit merge-base --is-ancestor <full> <target>` must fail. If there is no such symref, skip this check.
7. **Accept.** Print to stdout:

   ```
   task delivered outside this repository
   repository: <resolved root>
   commit: <full> (contained in <remote ref>)
   ...
   branch (reported, unverified): <branch>            # when present
   pull request (reported, unverified): <url>          # when present
   note: this run's validator and task-compliance review did not cover the external work
   record: <record_path>
   ```

   Exit 0.

The only commands the gate runs are `rev-parse`, `for-each-ref`, `cat-file`, `merge-base`, and `hash-object` (reading `/dev/null`, without `-w`). None of these refreshes the index, runs hooks, or runs diff or textconv drivers. `GIT_OPTIONAL_LOCKS=0` and `core.fsmonitor=false` protect against the rare internal index refresh. No command writes refs, the index, or the worktree.

### Data flow on the observed case

1. `generate-code` delivers the task in an Agent Runner worktree, pushes, and opens a PR.
2. `verify-task-commit` fails at step 3 and prints the record path.
3. Repair resumes the implementor, which writes the record.
4. The check reruns. The repository resolves to the Agent Runner common directory, which differs from Agent Factory's. The commits exist, are contained in `refs/remotes/origin/<branch>`, have tree changes, are newer than the task start, and are not on `origin/HEAD`.
5. The gate accepts, and the workflow continues to `session-report` and the next task.

## Decisions

1. **The record lives in the session directory, with a per-execution path computed by a new script step.** A shell `command` can capture only one string, and the prompt and the gate both need the path and the start time. A JSON map capture delivers both, and it also gives the prompt an exact path, so the agent never computes one. *Alternative:* storing the record under the run repository's git directory (`git rev-parse --git-path`), which would avoid needing `session_dir`. It was rejected because the record would sit apart from the run evidence the spec requires it to be kept with.
2. **The "no session directory" fallback is at the script level, not the workflow level (spec updated).** `{{session_dir}}` can't be interpolated conditionally, and every runner-launched run has a session directory. Builtin script materialization already depends on it. The spec now states the fallback where it can actually be observed: `verify-task-commit` with no record location does local-only verification. This keeps the script usable standalone and in tests.
3. **Change detection compares trees, not diffs.** Comparing tree IDs is object-level, can't invoke diff or textconv drivers, and matches the local rule's "range changes tracked files".
4. **Pushed state is `for-each-ref --contains` over `refs/remotes/`, excluding `*/HEAD`.** It works offline and reflects what the agent pushed. The agent's own push updates remote-tracking refs, since the checkout is a clone with the default fetch refspec.
5. **The skew tolerance is 300 seconds** (written into the spec). It absorbs clock drift and one-second resolution, and it is still far shorter than the minutes an implementor session takes. The start time is captured before `generate-code` starts, so every legitimate commit is later than it.
6. **The record is parsed with the existing jq-or-python3 dual path.** This keeps the script's dependency profile unchanged. Normalized line output plus the control-character rejection keeps it POSIX `sh`.
7. **The gate prints the record path on the unchanged-HEAD failure.** This is how a human or a repair agent finds the location, and it lets a human unblock a stopped run by writing the record and resuming.
8. **The validator is handled through its repair prompt, not by reordering steps.** *Alternative:* move `verify-task-commit` before `run-validator` and skip the validator when external delivery is accepted. That was rejected because the gate must run after validator fixes and leftover commits, so it sees the task's final local state, and splitting the gate into pre-validator and post-validator phases adds complexity. The prompt approach keeps the validator active for any local changes the task did make.

## Risks / Trade-offs

- **Forged committer dates or commits pushed to throwaway branches can pass.** This is accepted under the honest-but-lazy threat model the proposal names. A declared-repository HEAD snapshot remains a follow-up if that proves weak.
- **Stale remote-tracking refs.** If the agent pushed through a different remote name or a URL-only push, remote-tracking refs may not update, and the gate reports the commit as unpushed. That fails safe: the run blocks for a human, who can fetch and resume.
- **A PR merged before the gate runs blocks the task.** This is accepted as rare. The failure message names the reason.
- **Behavior depends on the git version.** The script uses no flags newer than git 2.7 (`for-each-ref --contains`), so macOS and Linux CI are fine.
- **The repair budget stays 1.** The repair round now either writes the record or blocks. No additional attempts are needed.

## Migration Plan

This is additive. There is no persisted-state change, because captures from older in-flight runs lack `task_delivery`:
- A run resumed from state written before this change would hit `{{task_delivery.*}}` as an undefined field at `verify-task-commit`.
- `implement-task` is a pre-release hidden builtin, and the project prefers clarity over accidental compatibility. Runs stopped mid-task on an older binary must restart the task.
- This is recorded here and not mitigated.

Rollback means reverting the YAML and scripts.

## Testing Strategy

- `workflows/verify_task_commit_test.go` covers the script end to end against temporary git fixtures: a bare "remote", a clone used as the external repository, and the run repository. Cases:
  - the existing local cases, unchanged;
  - no `record_path`;
  - a missing record, whose stderr includes the path;
  - malformed records, one per rule;
  - accept, where the output contains the repository, full commit IDs, the remote ref, the unverified labels, and the coverage note;
  - a fabricated ID, an unpushed commit, an empty commit, a root commit with files, a stale commit (created with `GIT_COMMITTER_DATE`), and a merged commit (`refs/remotes/origin/HEAD` pointing at a branch that contains it);
  - no remote HEAD, a mixed good and bad list, a worktree of the run repository, a subdirectory of the run repository, a bare repository, and a non-repository directory;
  - a non-descendant or empty local commit together with a valid record, which is still rejected;
  - `core.fsmonitor` set to a marker-writing script, where no marker appears, and refs, index, and status are identical before and after.
- A new `workflows` script test for `prepare-task-delivery.sh` covers the output map shape, directory creation, removal of a pre-existing file at the path, `task_key` sanitization, and invalid inputs.
- A workflow-shape test loads `builtin:core/implement-task-v1.0.yaml` and asserts:
  - `prepare-task-delivery` comes after `record-task-start` and before `generate-code`;
  - `verify-task-commit`'s `script_inputs` reference `task_delivery.started_at` and `task_delivery.record_path`;
  - the repair prompt contains `{{task_delivery.record_path}}` and the no-push/no-modify instruction;
  - the `run-validator` `fix-violations` prompt contains the external-work skip instruction;
  - the `complete-task-index` and `open-draft-pr` prompts contain the external delivery wording.
- Resume preservation follows from `step-repair` semantics, since completed steps are not re-run. An integration test in `internal/runner` can confirm it using a fake repair that writes the record and an interrupted run.
