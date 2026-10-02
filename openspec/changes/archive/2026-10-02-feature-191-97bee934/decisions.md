# Decisions

## propose

### Verdict: go
- **Decision:** Proceed. The change is small and reversible, and it stops a measurable, unrecorded recurring cost ($30–$60/week).
- **Alternatives considered:** no-go (the cost continues with unproven value); remove the `dev_audit` tag from builds (this would break the `agent-runner audit` readiness probe and manual replay); delete the audit code (re-enabling would be expensive).
- **Decision-bearing:** yes

### Mechanism: compile-time const gating only the hook registration
- **Decision:** Add `const automaticAuditEnabled = false` in the `dev_audit`-tagged devaudit package, with a comment linking issue #191. Gate only `runner.SetDefaultPostFinalizationHook(...)` on it. Keep `RegisterBuiltinAsset` and the command surface unconditional.
- **Alternatives considered:** a runtime/user config setting (barred by the existing "no enablement setting" requirement, and too much surface for a temporary pause); an environment variable (a hidden runtime toggle, and not a one-line revert of the default); removing the `init()` registration line outright (re-enabling would not be a clearly named switch).
- **Decision-bearing:** yes

### Tests that depend on the automatic trigger are skipped, guarded by the same switch
- **Decision:** Add `if !automaticAuditEnabled { t.Skip(...) }` to the subprocess CLI e2e tests that rely on automatic launch (E2E001, E2E002, Claude all-stages, and any others found). These tests come back automatically when the switch is flipped. Tests that call `Coordinator` directly keep running.
- **Alternatives considered:** delete the tests (contradicts the issue); rewrite them to use replay (larger diff than the issue allows).
- **Decision-bearing:** no

### Specs updated as deltas, not removed
- **Decision:** Modify the `automatic-run-audit` and `development-audit-availability` requirements so the automatic trigger is conditional on the switch, which is currently off. Keep the eligibility rules as the behavior when the switch is on.
- **Alternatives considered:** leave the specs unchanged (they would contradict the shipped behavior); remove the requirements (re-enabling would need the specs re-authored).
- **Decision-bearing:** no

### Command-name assumption
- **Decision:** The issue lists `status`, `replay`, `retry`, `setup`. The repository also has `reconcile` and `internal audit-run`. All stay working. `reconcile` only acts on existing automatic reservations, so it becomes a no-op path for new runs, which is acceptable.
- **Alternatives considered:** disable `reconcile` as well (unnecessary diff).
- **Decision-bearing:** no

## proposal-review

### PR-1: Docker development-audit smoke breaks with the hook off — applied
- **Decision:** Applied. The proposal now covers `scripts/docker-dev-audit-smoke.sh`. When the source run returns with no `audit-lifecycle.json`, the container script falls back to an explicit `agent-runner audit replay <source> --session <id> --project <project>` and logs that automatic audits are paused (#191). When an automatic lifecycle exists, it keeps the existing automatic assertion. The smoke picks its path by observing the lifecycle, so re-enabling stays a one-line const flip with no script edit. `docs/dev/sandbox.md` and the changelog document the pause.
- **Alternatives considered:**
  - Mark the smoke unavailable during the pause. Rejected: that drops coverage of the still-live detached replay and sandbox path.
  - Have the script read the Go const or a new env flag to choose its path. Rejected: re-enabling would need more than a one-line change, or it would add a runtime surface.
  - A new binary probe exposing the switch state. Rejected: it adds CLI surface for a temporary pause.
- **Trade-off accepted:** with the switch on, a broken automatic hook would make the smoke silently fall back to replay. Automatic-trigger regressions are still caught by the switch-guarded Go CLI E2E tests, which run whenever the switch is on.
- **Decision-bearing:** yes (not direction-level: it stays within the issue's scope and keeps the documented smoke working)

## spec

### Paused state specified as an ADDED requirement in `automatic-run-audit`
- **Decision:** Add "Automatic auditing is temporarily paused". It says that while paused, no automatic audit runs and no lifecycle record, link, run directory, state, or events are created for the execution, and the source outcome is unchanged. Resuming takes only flipping one source-level switch, with no runtime or config control. All explicit audit operations, including bare `audit` usage, `status`, `replay`, `reconcile`, `retry`, `setup`, and the internal entry point, keep working. "Eligible executions trigger automatic auditing" is modified so its trigger scenarios apply only while auditing is resumed, and the eligibility rules are kept.
- **Alternatives considered:** remove the trigger requirement (re-enabling would need re-authored specs); add "while resumed" qualifiers to every automatic-launch requirement (run-view independence, source authority, resumed sessions, intake coexistence). Those requirements describe an automatic audit that does not exist while paused, so they hold vacuously. Only intake routing got an explicit paused scenario, because "launch audit before intake" could otherwise be read as a precondition.
- **Decision-bearing:** no

### `development-audit-availability` modified in both automatic-trigger requirements
- **Decision:** The "Local Make build is used" scenario now says the build audits automatically only while resumed, and a scenario confirms the explicit commands stay available. "Development auditing needs no enablement setting" now says the pause is fixed at build time by one source-level switch and is not a runtime setting. A paused scenario was added.
- **Alternatives considered:** leave that capability unchanged (it would contradict the shipped behavior).
- **Decision-bearing:** no

### `development-audit-sandbox` added as a modified capability; `run-audit-replay` dropped from the modified list
- **Decision:** The Docker smoke requirement (from PR-1) lives in `development-audit-sandbox`, so that capability carries the delta. The smoke chooses its path from the observed audit state. When the source run has no lifecycle, it replays explicitly, says so, and applies the same checks. It fails if replay cannot be launched. `run-audit-replay` has no requirement change, so it gets no delta file, and the proposal capability list was updated to match.
- **Alternatives considered:** keep `run-audit-replay` listed as modified with an empty or no-op delta (this adds noise and has no observable change).
- **Decision-bearing:** no

## design

### Compile-time `const automaticAuditEnabled = false` gating registration inside `init()`
- **Decision:** Gate only `runner.SetDefaultPostFinalizationHook(...)` in `provider_enabled.go`'s `init()`, combined with the existing `.test` guard. Asset registration, `Enabled()`, and all coordinator, replay, and reconcile code stay unchanged.
- **Alternatives considered:**
  - A `var`: it can be overridden by tests or ldflags, which blurs the binary's mode.
  - Gating inside `Coordinator.AfterFinalization`: the hook body would still run and risk partial lifecycle writes.
  - Changing `internal/runner`: unnecessary, because a nil default hook is already a no-op.
- **Decision-bearing:** yes

### Mirrored test guards plus a paused-mode regression test
- **Decision:** The three automatic CLI E2E tests skip when the switch is false. A new `TestTaggedCLIPausedAutomaticAuditLaunchesNothingAndReplayStillWorks` skips when the switch is true. While paused, it asserts that there is no lifecycle, no audit run directory, no state links, and no judge calls; that bare `audit` prints usage; and that explicit replay completes with `trigger == "replay"`.
- **Alternatives considered:** asserting only through unit tests on `init()` (they cannot observe the registered hook in a `.test` binary because of the existing guard); deleting or rewriting the automatic tests (rejected by the issue).
- **Decision-bearing:** no

### Smoke replay fallback details and script tests
- **Decision:** When no lifecycle file exists, the container script finds the single run directory, reads the session from `run-metrics.json`, prints a paused message, runs `audit replay ... --project "$project"`, and then continues the existing checks. Two cases are added to `scripts/docker_dev_audit_smoke_test.py`: replay succeeds, and replay fails.
- **Alternatives considered:** none beyond those logged for PR-1.
- **Decision-bearing:** no

### No direct CHANGELOG edit
- **Decision:** Deviate from the proposal's "changelog note". `CHANGELOG.md` is curated by the release skill from merged PRs and has no `Unreleased` section, so the note goes in the PR description and `docs/dev/sandbox.md`.
- **Alternatives considered:** add an `Unreleased` section by hand (this conflicts with the release curation process).
- **Decision-bearing:** no

## test-plan

### Coverage: one tagged-binary E2E plus one script integration test
- **Decision:**
  - E2E-001 uses the real `dev_audit,devaudit_e2e` CLI fixture to prove that a paused build leaves no automatic audit or trace, that bare `audit` prints usage, and that explicit replay completes.
  - INT-001 uses the existing Python harness for the Docker smoke's replay fallback and failure paths.
  - The automatic-trigger E2E tests are mirrored by switch guards rather than deleted.
  - No new INT is added for intake routing or the untouched commands; existing tests cover them.
- **Alternatives considered:**
  - Unit tests on `init()`: not observable, because the `.test` guard skips registration.
  - A mandatory Docker smoke in CI: it needs Docker and is left to the acceptance envelope instead.
- **Decision-bearing:** no

### Acceptance envelope excludes real model invocations and GitHub/Sheets effects
- **Decision:**
  - Acceptance uses only isolated `HOME` fixtures, fake agent CLIs, and an optional local Docker smoke.
  - Real agent workflows, real OAuth/Sheets, `repair-issues`/`republish`, the real `~/.agent-runner`, and factory infrastructure are off limits.
  - Human-only testing: none.
- **Alternatives considered:** allowing one real openspec run to confirm the pause end to end (rejected: it costs money that the change exists to avoid, and the fake-CLI binary exercises the same `init()` path).
- **Decision-bearing:** no

## approach-review

### AR-1: Script replay fixture contradicts the single-run-directory discovery — applied
- **Decision:** Applied in design §3 and test-plan INT-001. The replay-path case:
  - stages the seeded audit directory outside `runs/` and deletes the source lifecycle, so the fallback first sees exactly one source run;
  - seeds `run-metrics.json`;
  - has the fake `audit replay` record the single-directory precondition, restore the staged audit directory, and write a `started` lifecycle.

  The existing updater completes the link, so `wait-dev-audit-smoke.py` validates real reciprocal artifacts. A pass on the replay path proves it reached the waiter. The failure case restores nothing.
- **Alternatives considered:**
  - Relax the script's discovery to tolerate audit directories (filtering by `runKind`). Rejected: in a real paused run only the source exists before replay, so relaxing discovery would only serve the fixture and could hide a stray audit.
  - Have the fake synthesize every audit artifact from scratch. Rejected: it duplicates `setUp`'s seeded fixtures.
- **Decision-bearing:** no

### AR-2: Unconditional "SHALL ship paused" contradicts the one-line resume — applied
- **Decision:** Applied in `specs/automatic-run-audit/spec.md`. The paused requirement now says automatic auditing is paused exactly when the source-level switch is compiled off and resumed when it is compiled on, and that its current value is off. The resume scenario stays normative, so flipping only the constant satisfies the spec.
- **Alternatives considered:** keep the unconditional mandate and rewrite the spec when re-enabling. Rejected: re-enabling would then need more than a one-line change.
- **Decision-bearing:** no

## tasks

### Single implementation task
- **Decision:** `tasks.md` holds exactly one task covering the whole change: the switch, the test guards and paused E2E test, the smoke fallback and its script tests, and the docs. It links every definition artifact and requires `make fmt`, `make test`, `make lint`, and the smoke script tests to pass. This follows the repository's prior factory change format.
- **Alternatives considered:** split into code, tests, smoke, and docs tasks (rejected because the workflow instruction requires exactly one task).
- **Decision-bearing:** no
