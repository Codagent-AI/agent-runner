## Coverage Strategy

Specifications remain the source of unit-test requirements. This plan records only additional
integration and end-to-end obligations, the acceptance testing envelope, and exceptional human-only
obligations.

The change is a single guard in an embedded POSIX shell script. Its behavior lives at the boundary between the script, real `git`, a real filesystem symlink, and the `agent-validator` executable on `PATH`. A unit test cannot exercise that boundary meaningfully, so the one test the issue allows is an integration test in the existing `workflows/embed_test.go` commit-change-plan tests. Existing tests there already cover the non-symlink paths, and they stay as the regression net for "behavior without a symlink is unchanged".

## Integration Tests

### INT-001: Plan commit is skipped through a symlinked `openspec` directory
- Covers: Plan-change Validator skipping — scenarios "Symlinked OpenSpec directory skips the plan commit" and "Symlinked OpenSpec directory while skipping the Validator".
- Boundary: the embedded `core/commit-change-plan.sh` (read through `ReadAsset`), run by `sh`, against a real git repository whose root `openspec` entry is a real symbolic link to a directory in a separate temporary directory. It also uses a recording fake `agent-validator` on `PATH`.
- Setup: reuse `newCommitChangePlanFixture` (or the same helpers). Replace the fixture's `openspec` directory with a symlink to an external temporary directory containing `changes/demo/tasks.md`. Record `HEAD` before the run.
- Action: run the script with `{"change_name":"demo","change_dir":"openspec/changes/demo"}`. Run it again in a fresh fixture with `"skip_validator":"true"` added (table cases or subtests in one test).
- Assertions:
  - the script exits 0;
  - `HEAD` is unchanged;
  - `git diff --cached --quiet` succeeds (nothing staged);
  - output says the plan must be committed by hand;
  - the fake `agent-validator` log is exactly `skip\n` in the default case and absent/empty in the `skip_validator=true` case.
- Execution: `workflows/embed_test.go`, run by the existing `test` CI job (`go test ./...`, locally `make test` or `go test ./workflows -run TestCoreCommitChangePlan`). No new suite, CI job, or `.github/workflows` change.

## End-to-End Tests

None. Every workflow that uses the script reaches it only after agent-driven planning steps, so an automated end-to-end test would need real agent sessions. The script's contract is fully observable at the INT-001 boundary, and all three caller workflows pass inputs to it unchanged.

## Acceptance Testing Envelope

- Environments and sandboxes:
  - throwaway git repositories under a temporary directory, each with a sibling "external" repository or directory that `openspec` symlinks to;
  - the CLI from source via `./dev.sh`;
  - running `go test` on the repository;
  - running the embedded script directly through `sh`.
- Script-only workflow runs:
  - `./dev.sh run core:commit-change-plan change_name=... change_dir=...` in a scratch repository is a permitted way to exercise the script through the Runner.
  - If the `openspec` CLI (1.6.0+) is installed, `openspec new change` / `validate` through the symlink may be used to create realistic artifacts.
- Credentials and secrets: none required.
- Authorized effects: local filesystem and git operations inside temporary directories only. Remove them afterwards.
- Off limits:
  - committing to, pushing, or modifying this repository's branches or any real external OpenSpec repository;
  - creating GitHub PRs or issues;
  - running agent-driven workflows that need real agent sessions;
  - running a real `agent-validator skip` against this repository. Use a fake `agent-validator` on `PATH` in scratch repos.
- Permitted substitutes:
  - a recording shell stub for `agent-validator`;
  - a plain directory in place of a separate external git repository, when only the symlink matters.
- Known risk areas:
  - The guard must run before the "already committed" check. That check calls git on paths beyond the symlink and falls through to the failing `git add`.
  - The `skip_validator=true` interaction.
  - The `jq` vs `python3` input-parsing paths are unchanged but run before the guard.
- Accepted limitations (do not report as defects):
  - the external repository is committed by hand;
  - the archive step reports success without verifying a commit;
  - the symlink must be listed in `.git/info/exclude` as `/openspec`;
  - a symlink pointing back into the same repository also skips the commit;
  - agents need YOLO mode to write through the symlink.

## Human-Only Testing

None.

## Coverage Map

| Requirement or journey | INT | E2E | HT |
| --- | --- | --- | --- |
| Plan-change Validator skipping — symlinked OpenSpec directory skips the plan commit | INT-001 | — | — |
| Plan-change Validator skipping — symlinked OpenSpec directory while skipping the Validator | INT-001 | — | — |
