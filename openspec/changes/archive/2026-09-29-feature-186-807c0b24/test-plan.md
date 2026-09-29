## Coverage Strategy

Specifications remain the source of unit-test requirements. This plan records only additional integration and end-to-end obligations, the acceptance testing envelope, and exceptional human-only obligations.

The table-driven collector tests described in `design.md` ("Testing Strategy") are the base layer. They cover:
- marker selection and precedence;
- check, review, thread, and comment classification;
- bot expectation, freshness, and grace;
- timing bounds;
- the error taxonomy.

They run `ci-wait.sh` / `ci_wait.py` as a real subprocess against a fake `gh`, one scenario per spec scenario, and are not repeated here. The obligations below add the boundaries those tests cannot see:
- whether the embedded `core:finalize-pr` workflow actually wires the collector into the runner, the gates, the loop budget, and the lead session;
- whether the collector's GraphQL query matches GitHub's real schema.

No automated E2E test against live GitHub is added. It would need a real PR, real CI, and real review-bot latency, so it would be slow, flaky, and would create external effects. The acceptance pass covers the live boundary instead (see the envelope).

## Integration Tests

### INT-001: Passing run makes no agent turns after push
- Covers: CI waiting runs outside agent sessions; CI report and terminal marker contract (gates read the script report unchanged).
- Boundary: embedded `core:finalize-pr` loaded through the real loader and executed by `runner.RunWorkflow`. The embedded namespace is materialized, and `ci-wait.sh` / `ci_wait.py` run as real subprocesses. The real gate scripts evaluate the captured reports.
- Setup: a test `ProcessRunner` that:
  - executes `RunScript` for real, with a fake `gh` first on `PATH`;
  - stubs `RunShell` (`record-pull-request`);
  - records every `RunAgent` call with its prompt, returning canned success output.

  The fake `gh` serves a green, mergeable snapshot with no bots. Small timings are injected through a test-only override of the collector defaults, for example an env var read by `ci_wait.py` when stdin omits timing keys. The implementation chooses the mechanism, and the workflow YAML must not change for tests.
- Action: run `core:finalize-pr` with default params.
- Assertions:
  - exactly one `RunAgent` call, for `push-pr`;
  - `ci_report` and `final_ci_report` both end with `CI_PASSED`;
  - the loop ran one iteration;
  - the workflow succeeded.
- Execution: `internal/runner` (for example `finalize_pr_ci_wait_test.go`), run by `make test`.

### INT-002: Failing cycles resume the lead with the report, respect the budget, and fall through
- Covers: Lead session resumed only for fix cycles; Fix-cycle budget and fall-through preserved.
- Boundary: same as INT-001.
- Setup: the fake `gh` serves a failed-check snapshot, with a failed-job log from `gh run view --log-failed`, for every wait. `ci_fix_cycles=2`.
- Action: run `core:finalize-pr`.
- Assertions:
  - `RunAgent` calls are `push-pr` followed by exactly two `fix-pr` calls, one per cycle, each resuming the `lead-agent` session;
  - each `fix-pr` prompt contains that cycle's report, including the failed check name, the log excerpt, and `CI_FAILED`;
  - the loop ends without failing the workflow;
  - the final wait runs;
  - the workflow fails at `final-ci-status-gate`.
- Variant: a sequence of `CI_COMMENTS` then green. Assert that `fix-pr` runs once, the loop breaks on the second wait, and the workflow succeeds.
- Execution: `internal/runner`, run by `make test`.

### INT-003: Pending cycles and fatal collector errors flow through existing gate paths
- Covers: Lead session resumed only for fix cycles (pending cycle does not resume the lead); Bounded external calls and error handling (no pull request, authentication failure in final verification); Expected review bots (an incomplete review finishes with a warning).
- Boundary: same as INT-001.
- Setup and assertions, three runs:
  - **Pending:** the fake `gh` keeps a CI check pending past the injected deadline for the first wait, then goes green. Assert that no `fix-pr` call is made for the pending iteration and the workflow succeeds.
  - **No pull request:** the fake `gh pr view` fails with `no pull requests found` for every call. Assert that every wait step fails with an empty capture, `fix-pr` is never called, and the workflow fails at `final-ci-status-gate`.
  - **Incomplete review:** `review_bots=coderabbitai`, CI is green, and the bot never starts. Assert that no agent call is made after `push-pr` and the workflow finishes with the incomplete-review warning from `final-review-incomplete-gate`.
- Execution: `internal/runner`, run by `make test`.

### INT-004: GraphQL snapshot query is valid against GitHub's schema
- Covers: Check polling and merge state; Review feedback classification; Expected review bots and freshness. These requirements depend on the field names in the query being real.
- Boundary: the literal query string in `ci_wait.py` checked against GitHub's public GraphQL schema.
- Setup: a vendored copy of GitHub's public GraphQL schema (SDL) under `testdata/`, or `gh api graphql` introspection when network access and credentials are available. The test must not require network access by default.
- Action: parse and validate the query against the schema.
- Assertions:
  - no unknown fields or types, for the primary query and for every page-continuation query;
  - the selected fields include:
    - `statusCheckRollup` contexts with `checkSuite.app.slug` and check-run `completedAt`;
    - head `checkSuites` `createdAt`;
    - review `commit.oid`;
    - comment `updatedAt`;
    - `READY_FOR_REVIEW_EVENT` and `HEAD_REF_FORCE_PUSHED_EVENT` timeline items;
    - `pageInfo` on every paginated connection.
- Execution: `workflows/` Go test, run by `make test`. If adding a GraphQL parser dependency is judged too heavy, replace this with a recorded real `gh api graphql` response fixture, captured once from a real PR and checked into `testdata/`. Every collector test then parses that fixture's shape. Record the choice in the implementing task.

## End-to-End Tests

None. The public journey is `agent-runner run core:finalize-pr`. INT-001 to INT-003 already run the embedded workflow through the real runner, loader, script materialization, and gates. The only missing fidelity is live GitHub and a live agent, and an automated E2E cannot provide either without real external effects and cost. The acceptance pass covers the live GitHub boundary.

## Acceptance Testing Envelope

- **Environments and sandboxes:**
  - a local checkout of this repository on the change branch, with `./dev.sh`;
  - the change's own pull request on `Codagent-AI/agent-runner`, and other existing PRs in that repository, for read-only inspection;
  - fake-`gh` harnesses from the automated tests, for driving states that cannot be produced safely on real PRs.
- **Credentials and secrets:** the operator's existing local `gh` authentication. No other credentials exist or are needed. Do not print tokens.
- **Authorized effects:**
  - read-only `gh` calls against any PR in `Codagent-AI/agent-runner`;
  - running the materialized collector (`workflows/core/ci-wait.sh` with a JSON stdin) directly against the change's own PR, and against recently merged or open PRs that have CodeRabbit reviews, to compare the report and marker with the PR's visible state;
  - running the collector with timings long enough to observe a real CodeRabbit review on the change's own PR after a push the delivery workflow makes anyway.

  A live `./dev.sh run core:finalize-pr` is **not** authorized. Its `fix-pr` step can resolve and reply to review threads through `codagent:fix-pr`, which would violate the off-limits rules below. The failing and fix path is covered by INT-002.
- **Off limits:**
  - creating new pull requests;
  - posting, editing, or resolving comments, reviews, or threads on any PR;
  - re-running or cancelling CI on PRs other than the change's own;
  - pushing to branches other than the change branch;
  - merging;
  - changing repository settings or bot configuration.
- **Permitted substitutes:** the fake-`gh` harness, when a needed state cannot be observed on a real PR. Examples: a hung `gh` call, authentication failure, a merge conflict, a rate-limited bot, or `UNKNOWN` mergeability. Prefer real read-only PRs for green, failed, comment-bearing, and CodeRabbit-reviewed states.
- **Known risk areas:**
  - review-bot identity matching across check-run app slug, status creator login, and review author login, especially the `[bot]` suffix and case;
  - the freshness point that uses commit time instead of push time;
  - pagination: check contexts, reviews, threads, thread comments, and top-level comments beyond the first page;
  - push-point derivation (check-suite time or force-push event), missing push timestamps, and pre-ready bot completions;
  - bot checks with non-success conclusions (skipped or neutral) that must not count as a completed review;
  - stdout purity: progress must never reach `ci_report`, and the marker must be the final non-empty line;
  - process-group cleanup on timeout, so no orphaned `gh` processes are left behind;
  - `fix-pr` prompt size when several failed logs are embedded.
- **Accepted limitations from design:**
  - a bot's first-ever review is missed when the bot posts no head status;
  - rate-limited bots wait until the deadline;
  - comment-classification rules duplicate `codagent:wait-ci` and may drift.

## Human-Only Testing

None.

## Coverage Map

| Requirement or journey | INT | E2E | HT |
| --- | --- | --- | --- |
| CI waiting runs outside agent sessions | INT-001, INT-003 | — | — |
| CI report and terminal marker contract | INT-001, INT-002 | — | — |
| Check polling and merge state | INT-004 | — | — |
| Review feedback classification | INT-004 | — | — |
| Expected review bots and freshness | INT-003, INT-004 | — | — |
| Complete evidence before passing | INT-004 | — | — |
| Bounded external calls and error handling | INT-003 | — | — |
| Lead session resumed only for fix cycles | INT-002, INT-003 | — | — |
| Fix-cycle budget and fall-through preserved | INT-002 | — | — |
