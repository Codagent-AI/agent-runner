- [ ] Implement the change described by these files using TDD, as `CLAUDE.md` requires: write the failing test before the code that makes it pass. Satisfy every spec scenario in `specs/` and the `INT-001` obligation in `test-plan.md`. `design.md` is the technical source of truth; the bullets below summarize it.
  - [proposal.md](proposal.md), [design.md](design.md), [test-plan.md](test-plan.md), [decisions.md](decisions.md)
  - [specs/builtin-workflows/spec.md](specs/builtin-workflows/spec.md)

  **Test first** (`workflows/embed_test.go`)
  - INT-001: add one test (or a table-driven test) to the existing commit-change-plan tests, reusing `newCommitChangePlanFixture` and `runGitOutput`.
    - Setup: replace the fixture repository's `openspec` directory with a symlink to an external temporary directory that contains `changes/demo/tasks.md`.
    - Cases: run the script with `{"change_name":"demo","change_dir":"openspec/changes/demo"}`, and again (in a fresh fixture) with `"skip_validator":"true"`.
    - Assert for each case: the script exits 0; `HEAD` is unchanged; `git diff --cached --quiet` succeeds; the output says the plan must be committed by hand.
    - Assert on the fake `agent-validator` log: exactly `skip\n` in the default case, absent or empty with `skip_validator=true`.
  - Confirm the test fails before the script change (git exits 128 with "beyond a symbolic link").

  **Script** (`workflows/core/commit-change-plan.sh`)
  - After the `change_dir` confinement check and before the "already committed" check, add:
    - if `[ -L openspec ]` and `change_dir` matches `openspec/*`, print to stdout a message naming `change_dir`, saying `openspec` is a symbolic link and the plan must be committed by hand in the repository that holds the OpenSpec project;
    - then call `advance_validator_baseline` and `exit 0`.
  - Leave everything else unchanged: no new inputs or workflow YAML edits, no new workflow versions, and no Go production code.

  **Docs** (`docs/built-in-workflows.md`, under `## OpenSpec`)
  - Add a short paragraph on keeping the OpenSpec project in another repository through an `openspec` symlink:
    - list `/openspec` (leading slash) in `.git/info/exclude`, so dirty-tree checks and `git add -A` ignore the symlink;
    - plan-commit steps skip the commit, so the external repository must be committed by hand, and its changes do not appear in this repository's PR;
    - the archive step reports success without verifying a commit;
    - agents can only write through the symlink in YOLO mode.

  **Constraints**
  - Do not add test suites or CI jobs, and do not change `.github/workflows`.

  **Done when**
  - Both scenarios added in `specs/builtin-workflows/spec.md` are covered by INT-001 and pass. The existing commit-change-plan tests still pass unchanged.
  - `make fmt`, `make lint`, and `make test` pass.
  - `openspec validate feature-295-cf9ee74d --strict` passes.
