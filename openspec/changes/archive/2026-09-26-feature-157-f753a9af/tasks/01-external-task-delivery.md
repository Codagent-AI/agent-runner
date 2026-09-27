# Task: Accept verified external task delivery in verify-task-commit

## Goal

Let `core/implement-task` continue past `verify-task-commit` when a task's work was legitimately delivered as pushed commits in another local repository. It does this through a per-task external delivery record that the gate verifies against git objects. The gate must never take the record on trust. Local delivery keeps working exactly as today.

Also make the prompts consistent with external delivery:
- the `verify-task-commit` repair writes the record and never pushes or modifies the external repository;
- the validator repair skips compliance violations for work directed elsewhere instead of re-implementing it locally;
- `complete-task-index` stops claiming every task has a local commit;
- the draft PR body lists external deliveries.

No Go runner, engine, or executor code changes. Everything is workflow YAML, bundled shell scripts, and tests.

## Background

Read these first. They are the source of truth:
- `openspec/changes/feature-157-f753a9af/proposal.md`: motivation (GitHub issue #157), scope, and threat model (honest-but-lazy implementors).
- `openspec/changes/feature-157-f753a9af/design.md`: exact step wiring, gate algorithm, git command set, output format, and decisions.
- `openspec/changes/feature-157-f753a9af/specs/task-delivery-gate/spec.md`: normative requirements and scenarios.
- `openspec/changes/feature-157-f753a9af/test-plan.md`: required INT-001 to INT-004 obligations.
- `openspec/changes/feature-157-f753a9af/decisions.md`: why each choice was made.

Current code:
- `workflows/core/implement-task-v1.0.yaml`:
  - `record-task-start` captures `task_start_head`;
  - `verify-task-commit` is a script step with an inline `repair` (`session: resume`, `max: 1`) whose prompt currently ends in `REPAIR_BLOCKED` for work delivered elsewhere.
- `workflows/core/verify-task-commit.sh`:
  - reads `{"starting_head": …}` from stdin, parsing with `jq` and falling back to `python3`;
  - fails when HEAD is unchanged ("implementation task did not produce a commit; refusing to advance to the next task"), not a descendant, or has no tracked changes;
  - on success prints `implementation task produced N commit(s)`.
- `workflows/verify_task_commit_test.go`: script tests with git fixtures (`runGit`, `mustWriteFile` helpers).
- `workflows/core/run-validator-v1.0.yaml`: the `fix-violations` prompt, run with `session: inherit`.
- `workflows/core/implement-change-v1.0.yaml`: the `complete-task-index` prompt.
- `workflows/core/verify-change-v1.0.yaml`: the `open-draft-pr` prompt.
- The integration harness pattern is `internal/exec/verify_change_workflow_test.go`: a fake `ProcessRunner` executes shell and script steps for real and fakes agents, and steps are dispatched with `DispatchStep` and `model.NewRootContext` with `SessionDir`.
- Resume harness patterns are in `internal/runner/resume_repair_test.go`.
- Map captures (`capture_format: json`) are referenced as `{{name.field}}` (`internal/textfmt/interpolation.go`).

### Implementation outline (see design.md for full detail)

1. **New `workflows/core/prepare-task-delivery.sh` and a step `prepare-task-delivery`** between `record-task-start` and `generate-code`.
   - Step settings: `capture: task_delivery`, `capture_format: json`.
   - `script_inputs`: `session_dir: "{{session_dir}}"`, `task_file: "{{task_file}}"`, `starting_head: "{{task_start_head}}"`.
   - Output: `{"record_path": "<session_dir>/output/task-delivery/<task_key>-<started_at>-<head12>.json", "started_at": "<epoch>"}`.
   - `task_key` is the basename of the task file without `.md`, with every character outside `[A-Za-z0-9._-]` replaced by `_`.
   - The script runs `mkdir -p` on the directory, removes any existing file at the path, and validates its inputs.
2. **`verify-task-commit` inputs** add `started_at: "{{task_delivery.started_at}}"` and `record_path: "{{task_delivery.record_path}}"`.
3. **Extend `verify-task-commit.sh`:**
   - Input: `started_at` and `record_path` are optional but must come as a pair. A mismatched pair or invalid values exit 2.
   - If HEAD moved: keep today's local path unchanged, and never read the record.
   - If HEAD is unchanged:
     - with no `record_path`, fail with today's message;
     - with no file at the path, fail with today's message plus the line `no external delivery record at <path>`;
     - otherwise parse the record (`jq`, falling back to `python3`) and reject it as `malformed external delivery record: <reason>` for any of these: larger than 64 KiB, not an object, `repository` missing or not absolute, `commits` missing or empty, more than 100 commits, a non-hex ID (7–64 lowercase characters), or control characters in any string.
   - Repository identity:
     - run all external git calls as `GIT_OPTIONAL_LOCKS=0 git -C "$repo" -c core.fsmonitor=false …`;
     - require `rev-parse --is-bare-repository` to print `false`, and resolve the root with `--show-toplevel`;
     - compare `pwd -P` of each repository's `--git-common-dir` with the run repository's, and reject if they are equal.
   - Per-commit checks, reporting `commit <id>: <reason>`:
     - it exists (`rev-parse --verify --quiet <id>^{commit}`);
     - it is pushed (`for-each-ref --contains <c> refs/remotes/`, ignoring `*/HEAD`);
     - it is change-bearing (its tree differs from its first parent's tree, or from the empty tree via `hash-object -t tree /dev/null` for a root commit);
     - it is fresh (committer epoch from `cat-file commit` ≥ `started_at - 300`);
     - it is not merged (not an ancestor of any `refs/remotes/*/HEAD` symref target; skip this check if there is none).
   - Allowed git subcommands against the external repository: only `rev-parse`, `for-each-ref`, `cat-file`, `merge-base`, and `hash-object` (without `-w`).
   - On accept, print exactly the lines in design.md §7:
     - `task delivered outside this repository`;
     - `repository:`;
     - one `commit: <full> (contained in <ref>)` line per commit;
     - optional `branch (reported, unverified):` and `pull request (reported, unverified):` lines;
     - the validator coverage `note:` line;
     - `record: <path>`.

     Then exit 0.
4. **Rewrite the `verify-task-commit` repair prompt** as described in design.md §3:
   - keep the inspection, isolated local commit, no-placeholder, and never-declare-success rules;
   - for pushed external delivery, write `{{task_delivery.record_path}}` in the JSON shape given in the spec, make no local commit, and do not end with `REPAIR_BLOCKED`;
   - forbid pushing, creating or moving branches, committing, or modifying the external repository;
   - end with `REPAIR_BLOCKED` for unpushed work, work with no local checkout, a task that conflicts with the repository, or changes that can't be isolated.
5. **`run-validator-v1.0.yaml` `fix-violations`:** add the paragraph from design.md §5, which skips task-compliance violations for work the task directs to another repository with `agent-validator update-review skip <#> "delivered in <repository>; see task file"`, without re-implementing the work or committing.
6. **`implement-change-v1.0.yaml` `complete-task-index`:** change the first sentence to "…has now completed successfully, each with its implementation commit or a verified delivery in another repository, and, unless `skip_validator` is `true`, task-compliance validation."
7. **`verify-change-v1.0.yaml` `open-draft-pr`:** if `{{session_dir}}/output/task-delivery/*.json` records exist, add a "Delivered in other repositories" PR body section listing each record's repository, commits, and reported pull request, noting that this change's validation did not cover that work. Omit the section when there are no records.
8. **Docs:** in `docs/built-in-workflows.md`, extend the `core:implement-task` row, or add a short note, to say that a task delivered as pushed commits in another local repository passes via a verified delivery record.

## Spec

Implement every requirement and scenario in `openspec/changes/feature-157-f753a9af/specs/task-delivery-gate/spec.md`:
- Local delivery
- External delivery record
- External repository identity
- External commit verification
- Verification does not execute repository-configured programs
- External delivery record lifecycle
- Repair records external delivery
- Validator repair does not re-implement external work
- Downstream steps acknowledge external delivery
- Reporting external delivery

## Test Plan

Use TDD: write each failing test before the production change.
- **Script tests** in `workflows/verify_task_commit_test.go` cover every accept and reject rule. Fixtures: a bare remote, an external clone with `refs/remotes/origin/HEAD`, and the run repository. Include:
  - a worktree of the run repository and a subdirectory of the run repository;
  - a bare repository and a non-repository directory;
  - a fabricated, an unpushed, an empty, a stale (`GIT_COMMITTER_DATE`), and a merged commit;
  - a root commit with files;
  - no remote HEAD;
  - a mixed good and bad commit list;
  - each malformed-record rule;
  - no `record_path`, and a missing record file (stderr includes the path);
  - a non-descendant or empty local commit together with a valid record, which must still be rejected;
  - the exact accept output;
  - a `core.fsmonitor` marker script, where no marker appears and refs, index, and status are identical before and after.

  All existing cases stay green.
- **A new script test for `prepare-task-delivery.sh`** covers the output map, directory creation, removal of a pre-existing file, `task_key` sanitization, and invalid inputs. If the materialized script list is asserted anywhere (for example `workflows/onboarding_scripts_test.go` or `embed_test.go`), register the new asset there.
- **INT-001:**
  - the main path through the builtin `implement-task`: fake `generate-code` pushes in an external clone, and fake repair writes the record to the path from its prompt;
  - the unpushed variant;
  - the validator variant (`skip_validator=false`, faked task-compliance failure, and a `fix-violations` prompt assertion with no run-repository commit).

  Location: `internal/exec`.
- **INT-002:** two executions sharing a session directory, where the second can't use the first's record. Location: `internal/exec`.
- **INT-003:** a run blocked at `verify-task-commit`, then a human-written record, then resume. The check passes without re-running `record-task-start` or `prepare-task-delivery`, and the audit `step_end` stdout contains the delivery statement. Location: `internal/runner`.
- **INT-004:** the wiring contract across the four builtin YAMLs, as listed in test-plan.md.

Acceptance flows AT-001 to AT-003 run later in the acceptance phase. They are not part of this task's automated suite.

## Done When

- `prepare-task-delivery.sh` exists and is wired into `implement-task-v1.0.yaml` as specified.
- `verify-task-commit.sh` implements local-first verification plus verified external delivery exactly as in design.md, and `verify-task-commit` receives `started_at` and `record_path`.
- The `verify-task-commit` repair, `fix-violations`, `complete-task-index`, and `open-draft-pr` prompts carry the new instructions.
- The docs note is added.
- The script tests and INT-001 to INT-004 exist and pass. Targeted runs: `go test ./workflows -run 'VerifyTaskCommit|PrepareTaskDelivery'`, `go test ./internal/exec -run ImplementTask`, and `go test ./internal/runner -run TaskDelivery`.
- `openspec validate feature-157-f753a9af --strict` passes.
- `make fmt`, `make lint`, and `make test` pass.
