## Context

`workflows/core/commit-change-plan.sh` is an embedded workflow script that commits a change's planning artifacts. It is called by three hidden core workflows:

- `core:plan-change` (`check-plan` → `commit-plan`), which forwards `skip_validator`;
- `core:commit-change-plan` (used by `openspec:simple-change`), which omits `skip_validator`;
- `core:commit-definition` (step `commit-definition`, skipped when `change_kind` is `spec-driven`), which forwards `skip_validator`.

The script's flow today:

1. Parse JSON stdin (`jq`, or `python3` as a fallback).
2. Validate `skip_validator`, `change_name` and `change_dir`.
3. Run the "already committed" check: `git diff --cached --quiet`, `git diff --quiet`, `git ls-files --others` and `git cat-file -e HEAD:$change_dir`.
4. Run `git add -A -- "$change_dir"`, plus `openspec/config.yaml` if it is untracked.
5. Commit, then call `advance_validator_baseline`. That helper runs `agent-validator skip` unless `skip_validator=true`.

When the repository-root `openspec` is a symlink to another repository, step 3's `git cat-file` fails because the path is not in `HEAD`. Execution falls through to step 4, where git exits 128 with `fatal: pathspec 'openspec/changes/<name>' is beyond a symbolic link`. This was reproduced locally. Everything else in the OpenSpec workflows already works through the symlink, according to the issue author's checks with openspec 1.6.0.

## Goals / Non-Goals

**Goals:**
- Make `commit-change-plan.sh` succeed without committing when `openspec` is a symlink and `change_dir` is under it, while keeping the `skip_validator` baseline rule.
- Do this in one detection point, with no new params, workflow versions or Go code.
- Cover it with one test case in the existing `workflows/embed_test.go` commit-change-plan tests.
- Add a short docs note on setup and limits.

**Non-Goals:**
- Committing or verifying anything in the external repository.
- Changing archive verification, adapter add-directory support, CI or test suites.
- Detecting symlinks below the top level, or symlinked change directories under a real `openspec/`.

## Approach

Add one guard block to `commit-change-plan.sh`. It goes after the `change_dir` confinement check and before the "already committed" check:

```sh
if [ -L openspec ]; then
  case "$change_dir" in
    openspec/*)
      printf 'commit-change-plan: openspec is a symbolic link; %s is not committed here. Commit it by hand in the repository that holds the OpenSpec project.\n' "$change_dir"
      advance_validator_baseline
      exit 0
      ;;
  esac
fi
```

- **Where it runs:** the script runs from the repository root. The existing `openspec/config.yaml` handling already relies on this, so the relative `openspec` test is correct.
- **Why before the already-committed check:** that check calls git on paths beyond the symlink, so the guard must come first.
- **Baseline handling:** the guard reuses `advance_validator_baseline`. The default path and `skip_validator=false` run `agent-validator skip`; `skip_validator=true` runs nothing.
- **Output:** the message goes to stdout and the exit code is 0. The calling step therefore succeeds and its `repair` session in `core:commit-definition` never triggers.
- **Callers:** all three caller workflows get the behavior with no YAML changes. Under `core:commit-definition`, the next `check-clean-before-implementation` step uses `--untracked-files=no`, so an untracked symlink doesn't fail it. A symlink that has been committed would leave no tracked change either.

### Test

Add a subtest to `TestCoreCommitChangePlanSkipValidatorControlsValidatorBaseline`, or a sibling test function in the same file using `newCommitChangePlanFixture`. Setup:

- Build the fixture repository.
- Replace its real `openspec` directory with a symlink to a directory in a second temporary directory that holds `changes/demo/tasks.md`.

Run the script with no `skip_validator` and with `skip_validator=true`. Assert:

- exit 0;
- `HEAD` unchanged;
- `git diff --cached --quiet` succeeds (nothing staged);
- the validator log is `"skip\n"` without the flag and empty with `skip_validator=true`;
- the output mentions committing by hand.

The fixture writes the change directory directly, so the test either builds its own repository or moves the fixture's `openspec` aside before creating the symlink. Either is fine; prefer reusing the fixture helpers.

### Spec and docs

- The spec delta is already written (`specs/builtin-workflows/spec.md`, MODIFIED "Plan-change Validator skipping").
- The docs note goes in `docs/built-in-workflows.md` under the `## OpenSpec` section, as a short paragraph titled something like "OpenSpec project in another repository". It covers:
  - list the symlink in `.git/info/exclude` as `/openspec` (leading slash), so dirty-tree checks and `git add -A` ignore it;
  - commit the plan by hand in the external repository; it does not appear in the code repository's PR;
  - the archive step reports success without verifying a commit;
  - agents can only write through the symlink in YOLO mode.

## Decisions

- **Shell guard in the script, not a workflow `skip_if`.** The issue requires a single detection point and no new workflow versions. A `skip_if` would have to be repeated in three workflow files and would skip the validator-baseline step along with the commit.
- **`[ -L openspec ]` + prefix match.** This is the simplest correct condition. Alternatives were rejected as more complex than the issue's "simplest version" asks for:
  - resolve `change_dir` and compare it with `git rev-parse --show-toplevel`;
  - detect any symlink component in the path.
- **Exit 0, message on stdout.** The issue requires the step to succeed. Writing to stderr isn't needed, because this is expected behavior, not a warning about failure.
- **Skip the `openspec/config.yaml` bootstrap staging as well.** That file is also beyond the symlink. The guard exits before that code runs.

## Risks / Trade-offs

- **A symlink pointing back into the same repository would also skip the commit.** This is accepted: it's an unusual setup, and the message tells the user to commit by hand.
- **The plan is silently left out of the code repository's PR.** The issue accepts this; the message and docs make it visible.
- **The untracked symlink shows up in `git status` and `git add -A` when it isn't excluded.** This is mitigated by the docs note (`/openspec` in `.git/info/exclude`), not by code, as the issue requires.

## Migration Plan

None. The script is embedded in the binary, and behavior without a symlink is byte-for-byte unchanged.
