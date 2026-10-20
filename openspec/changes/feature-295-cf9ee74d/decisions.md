# Decisions

## propose: verdict go
- **Decision:** Go. The issue scope is a single guard in `commit-change-plan.sh`, plus one test, one spec scenario and a docs note.
- **Alternatives considered:** No-go (rejected: the problem is real and reproduced, and the fix is cheap). The larger #227/#228 approach (rejected by the issue author).
- **Decision-bearing:** yes

## propose: guard placement before the "already committed" check
- **Decision:** Detect the symlink after input validation and before both the already-committed check and `git add`.
- **Alternatives considered:** Guard only `git add`. Rejected because the already-committed check uses `git cat-file -e HEAD:$change_dir`, which fails for paths beyond the symlink and falls through to the failing `git add` anyway.
- **Decision-bearing:** no (follows from the issue's "skips committing" behavior)

## propose: honor skip_validator on the symlink path
- **Decision:** Advance the baseline through the existing `advance_validator_baseline` helper, so `skip_validator=true` still runs no Validator command.
- **Alternatives considered:** Always run `agent-validator skip` (rejected: it would contradict the existing Plan-change Validator skipping requirement).
- **Decision-bearing:** no

## propose: detection condition
- **Decision:** `openspec` at the repository root is a symlink AND `change_dir` starts with `openspec/`.
- **Alternatives considered:** Detect any symlink component in `change_dir` (rejected: broader than the issue asks for). Compare resolved git toplevels (rejected: more complex, and "simplest version" was requested).
- **Decision-bearing:** no

## propose: spec location
- **Decision:** Add the scenario under "Plan-change Validator skipping", the requirement in `builtin-workflows/spec.md` that governs `commit-change-plan.sh`. The spec has no requirement named after commit-change-plan.
- **Alternatives considered:** Add a new standalone requirement (acceptable fallback if the spec step judges that clearer).
- **Decision-bearing:** no

## propose: docs location
- **Decision:** Put the short note in `docs/built-in-workflows.md` next to the OpenSpec workflow descriptions.
- **Alternatives considered:** `docs/setup.md` or `docs/troubleshooting.md`.
- **Decision-bearing:** no

## proposal-review: findings
- **Decision:** `proposal-review-findings.json` was an empty array, so there was nothing to apply, reject, or escalate. `proposal.md` is unchanged.
- **Alternatives considered:** none
- **Decision-bearing:** no

## spec: modify "Plan-change Validator skipping"
- **Decision:** Modify the existing requirement instead of adding a new one. "Plan-change Validator skipping" is the only requirement that governs `commit-change-plan.sh`, and the issue asks for a scenario in that requirement. The change adds one normative sentence group and two scenarios: symlink with the default baseline advance, and symlink with `skip_validator=true`. The existing scenarios already cover the non-symlink behavior.
- **Alternatives considered:** A new standalone requirement (rejected: the issue points to the existing requirement). One scenario only (rejected: the interaction with `skip_validator` is observable and deserves its own scenario).
- **Decision-bearing:** no

## spec: no scenario for the docs note or the archive step
- **Decision:** No spec scenarios for the docs note (`.git/info/exclude`, manual external commit, YOLO mode). The archive step's behavior is unchanged, so it gets no new scenario either.
- **Alternatives considered:** Spec the docs content (rejected: documentation is not observable runtime behavior).
- **Decision-bearing:** no

## spec: user-facing message
- **Decision:** The spec requires a message saying the plan must be committed by hand in the external repository, but not its exact wording.
- **Alternatives considered:** Pin the exact text (rejected: that would over-constrain the implementation).
- **Decision-bearing:** no

## design: shell guard placement and form
- **Decision:** A single `[ -L openspec ]` + `openspec/*` prefix guard in `commit-change-plan.sh`, placed after input validation and before the already-committed check. It reuses `advance_validator_baseline` and exits 0 with a stdout message.
- **Alternatives considered:** A `skip_if` in each caller workflow (rejected: three detection points, and it would also skip the baseline advance). Comparing resolved toplevels (rejected: more complex than requested).
- **Decision-bearing:** no

## design: callers covered
- **Decision:** `core:commit-definition` also calls the script and gets the behavior with no YAML change. Its following clean-tree check ignores untracked files, so no further change is needed.
- **Alternatives considered:** Excluding commit-definition (rejected: the spec covers every caller of the script, and the failure mode is the same).
- **Decision-bearing:** no

## design: docs location and test shape
- **Decision:** The docs note goes under `## OpenSpec` in `docs/built-in-workflows.md`. The test reuses `newCommitChangePlanFixture` with `openspec` replaced by a symlink to an external temp directory, and covers the default and `skip_validator=true` cases.
- **Alternatives considered:** A standalone test fixture (acceptable if reusing the helper gets awkward).
- **Decision-bearing:** no

## test-plan: one integration obligation, no E2E or human-only tests
- **Decision:** INT-001 is the single new test the issue allows. It is in `workflows/embed_test.go`, uses real git, a real symlink and a fake agent-validator, and covers both the default and the `skip_validator=true` case. There is no E2E test, because every caller workflow reaches the script only after agent-driven steps. There is no human-only testing.
- **Alternatives considered:** An E2E test through `./dev.sh run core:commit-change-plan` (rejected for automation: it duplicates INT-001 and would be a new kind of suite. It is left to the acceptance pass instead). Separate tests per caller workflow (rejected: the issue asks for one test).
- **Decision-bearing:** no

## approach-review: findings
- **Decision:** `approach-review-findings.json` was an empty array, so there was nothing to apply, reject, or escalate. The definition artifacts are unchanged.
- **Alternatives considered:** none
- **Decision-bearing:** no

## tasks: single implementation task
- **Decision:** `tasks.md` holds exactly one task covering the test (INT-001), the script guard and the docs note, following the format of the archived feature-280 change.
- **Alternatives considered:** Separate tasks for test, script and docs (rejected: the step requires exactly one task, and the change is small).
- **Decision-bearing:** no
