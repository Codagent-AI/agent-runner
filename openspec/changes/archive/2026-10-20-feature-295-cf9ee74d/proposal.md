## Why

Some users keep their OpenSpec project in a separate repository and point the code repository at it with a symlink: `openspec -> ../specs-repo/openspec`. The OpenSpec CLI (1.6.0) handles this already. `openspec new change`, `validate`, `status`, `list` and `archive --yes` all work through the symlink. The built-in `openspec:*` workflows still fail at one step: `workflows/core/commit-change-plan.sh` runs `git add -A -- "$change_dir"`, and git rejects it with `fatal: pathspec 'openspec/changes/<name>' is beyond a symbolic link`. Every OpenSpec workflow that commits a plan therefore stops partway through for these users.

Issue #227 and draft PR #228 tried to fix this with a much larger approach and were closed. Issue #295 asks for the simplest fix that works, even if it is a bit clunky.

**Verdict: go.** The fix is one guard in one script, the problem is confirmed (reproduced locally with git), and the cost of maintaining it is negligible. The limits are known and the issue accepts them.

## What Changes

- `commit-change-plan.sh` checks whether the repository-relative `openspec` path is a symbolic link and `change_dir` is under `openspec/`. If both are true, the script does not commit. It prints a short message saying the plan lives in another repository and must be committed by hand, advances the Validator baseline using the existing `skip_validator` rules (so `skip_validator=true` still runs no Validator command), and exits 0.
- The check runs after input validation and before the "already committed" check and `git add`, since both of those touch git paths beyond the symlink.
- This script is the only place the condition is detected. No new workflow parameters, workflow versions or Go code. Without a symlink, behavior is unchanged.
- One new test in the existing commit-change-plan tests in `workflows/embed_test.go`. It builds a temporary repository whose `openspec` is a symlink to a directory outside it, runs the script, and asserts that it exits 0, creates no commit, and runs `agent-validator skip` (through the existing stub pattern).
- A scenario in `openspec/specs/builtin-workflows/spec.md` under the requirement that covers `commit-change-plan.sh` (currently "Plan-change Validator skipping").
- A short docs note covering setup and limits: put `/openspec` in `.git/info/exclude`, commit the external repository by hand, and use YOLO mode so agents can write through the symlink.

## Capabilities

### New Capabilities
- None.

### Modified Capabilities
- `builtin-workflows`: committing the plan succeeds without a commit when `openspec` is a symlink to another repository.

## Technical Approach

A POSIX `sh` guard in `commit-change-plan.sh`, along the lines of `[ -L openspec ]` combined with a `case "$change_dir" in openspec/*)` match. The script runs from the repository root, as the existing `openspec/config.yaml` handling already assumes. The guard reuses the existing `advance_validator_baseline` helper, so `skip_validator` keeps one meaning across both callers (`core:plan-change` and `core:commit-change-plan`).

Other places that might look affected were already checked by the issue author and need no change. `openspec:archive-change` passes and its commit verification does not fail. The planning-artifact checks pass. Specs and design will confirm this list rather than widen it.

Main risk: other workflow steps that check for a clean tree would see an untracked `openspec` symlink. That is handled by documentation (`/openspec` in `.git/info/exclude`), not code, as the issue requests.

## Out of Scope

- Committing, pushing or opening PRs in the external OpenSpec repository.
- Making the archive step verify a commit in the external repository. It reports success without one.
- Add-directory or sandbox-permission support for the Claude, Codex or Copilot adapters. Agents need YOLO mode to write through the symlink.
- Detecting symlinks anywhere other than the top-level `openspec` path, or symlinked change directories under a real `openspec` directory.
- New workflow parameters, workflow versions, test suites, CI jobs or changes to `.github/workflows`.

## Impact

- Code: `workflows/core/commit-change-plan.sh` (embedded in the binary).
- Tests: `workflows/embed_test.go`, one new case.
- Specs: `openspec/specs/builtin-workflows/spec.md`, one scenario.
- Docs: a short note, most likely in `docs/built-in-workflows.md` next to the OpenSpec workflow descriptions.
- Users: OpenSpec workflows complete when `openspec` is a symlink. Those users commit the external repository themselves, and the code repository's PR does not include the plan documents. Users without a symlink see no change.
