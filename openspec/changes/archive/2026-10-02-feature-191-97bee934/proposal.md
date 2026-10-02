## Why

Every `dev_audit` build launches a detached post-run audit after each eligible workflow run finalizes. That covers Paul's local `make build` and `./dev.sh`, the agent-factory Fly guests (built with `-tags dev_audit`), and eval guests. Each audit runs a model judge using the source run's `lead` role, which is Opus at high effort for fix and feature runs. From Sep 21 to 28 that came to 81 audits, about 242 minutes, and an estimated $30–$60 of Claude usage. None of that spend is recorded. A spot check also found the judge's adverse verdicts misattributing blame, so the value is unproven while the cost is real and recurring.

We need to stop paying for audits now, without throwing away the audit subsystem, while we decide whether it earns its cost. The change is explicitly **temporary**. Re-enabling has to be a one-line revert. The companion change Codagent-AI/agent-factory#60 handles the factory side.

**Verdict: go.** It is small, reversible, low-risk, and stops a measurable ongoing cost. The alternatives are worse:
- Dropping the `dev_audit` tag from builds would also remove the `agent-runner audit` commands that agent-factory's readiness probe and manual replay depend on.
- Adding a runtime config setting is barred by the existing "no enablement setting" requirement, and it is more surface than a temporary pause deserves.
- Deleting the code would make re-enabling expensive.

## What Changes

- `dev_audit` builds no longer register the automatic post-finalization audit hook. A finalized workflow run launches no audit and writes no audit lifecycle link, audit run directory, or audit state for that run.
- One clearly named compile-time switch controls registration: `automaticAuditEnabled = false` in `internal/devaudit`, with a comment linking issue #191. Flipping it to `true` restores today's behavior exactly.
- The explicit `agent-runner audit` command surface stays fully working in `dev_audit` builds:
  - usage output for bare `audit`;
  - `status`, `replay`, `reconcile`, `retry`, `setup`;
  - the internal `audit-run` entry point that replay launches.

  The hidden audit workflow asset stays registered.
- Tests that depend on the automatic trigger are skipped while the switch is off, keyed to the same switch, so they return automatically when it is flipped back on. No audit code, workflows, or other tests are deleted.
- The supported Docker development-audit smoke (`scripts/docker-dev-audit-smoke.sh`) keeps working during the pause. Today it requires an automatic audit lifecycle after the source run, so with the hook off it would fail. The smoke will instead fall back to an explicit `agent-runner audit replay` of the source run's execution session when the source returns with no audit lifecycle, and the rest of its checks run unchanged. When an automatic lifecycle is present, it keeps today's automatic-trigger assertion. `docs/dev/sandbox.md` documents the pause and the replay path.
- Release and untagged builds are unchanged. They already contain no audit capability.

Not breaking: no public interface, persisted format, or release-build behavior changes. Behavior changes only in `dev_audit` development builds.

## Capabilities

### New Capabilities
- None.

### Modified Capabilities
- `automatic-run-audit`: eligible executions no longer trigger an automatic audit while the automatic-audit switch is off. Eligibility rules, reconciliation, and launch semantics are kept as the behavior that applies when the switch is on.
- `development-audit-availability`: a development-audit build still contains the audit capability, commands, and hidden workflow. It no longer *automatically* audits eligible executions while the switch is off. The "Local Make build is used" scenario and the "Development auditing needs no enablement setting" requirement change accordingly. The switch is compile-time source, not a runtime or user setting.
- `development-audit-sandbox`: the product-owned Docker smoke falls back to an explicit `audit replay` when the source run returns with no automatic audit, and keeps the automatic-trigger assertion otherwise.

`run-audit-replay` has no requirement change. Explicit replay keeps working unchanged while the automatic hook is disabled.

## Technical Approach

The only trigger is `internal/devaudit/provider_enabled.go` `init()`, which calls `runner.SetDefaultPostFinalizationHook(Coordinator{Launcher: launchDetached}.AfterFinalization)`. When no default hook is registered, `internal/runner` already runs without a post-finalization hook, so nothing in the runner needs to change.

- Add `const automaticAuditEnabled = false`, with a comment referencing issue #191 and the temporary pause, in the `dev_audit`-tagged devaudit package. Gate only the `SetDefaultPostFinalizationHook` call on it.
  - `RegisterBuiltinAsset` for the audit workflow stays unconditional, because replay and reconcile load it.
  - The existing `.test` binary guard stays as it is.
- `Coordinator`, `launchDetached`, `Replay`, `Reconcile`, and `HandleCommand` stay untouched. They remain reachable through the explicit commands.
- Leaving the coordinator compiled but unregistered may trigger `unused` lint findings. They are expected to be absent because replay and reconcile still use `Coordinator.launch` and `launchDetached`. If a finding does appear, resolve it with the narrowest local fix and no deletions.
- Tests:
  - Skip the subprocess CLI tests that need the automatic trigger, guarded by `if !automaticAuditEnabled { t.Skip(...) }` with the same issue reference. These are `TestE2E001TaggedCLIAutomaticAuditCompletesAndRetriesWithoutDuplicate`, `TestE2E002TaggedCLIFailureResumeReplayAndRetryPreserveLineage`, and `TestTaggedClaudeAuditCompletesAllStages`, plus any others found to rely on the automatic launch.
  - Tests that drive `Coordinator` directly keep running.
  - Add one focused regression test showing that a finalized eligible run in a `dev_audit` binary creates no audit link or state.
  - Confirm that bare `audit` still prints usage and that explicit `audit replay` still launches.
- Docker smoke (`scripts/docker-dev-audit-smoke-container.sh`):
  - After the source CLI returns, if no `audit-lifecycle.json` exists, the script reads the source run's execution session from its metrics artifact. It then runs `agent-runner audit replay <source-run-dir> --session <id> --project "$project"` and logs that automatic audits are paused (#191).
  - The replayed audit is still gated by the fake Codex release file. That keeps the existing "exactly one active linked audit" assertion, `wait-dev-audit-smoke.py`, which does not check the trigger, and the protected-write sandbox check valid as they are.
  - The smoke picks its path by observing the lifecycle, not by reading the Go const. Flipping the switch back on therefore restores the automatic path with no script edit, which preserves the one-line re-enable.
  - Trade-off: with the switch on, a broken automatic hook would make the smoke fall back to replay instead of failing. That regression is still caught by the switch-guarded Go CLI E2E tests, which run again whenever the switch is on.
- Update the `automatic-run-audit` and `development-audit-availability` specs as delta specs. Update `docs/dev/sandbox.md` so the smoke description explains the pause and the replay fallback, and add a changelog note that automatic audits are paused.

## Out of Scope

- Deleting or refactoring audit code, the hidden audit workflow, Sheets delivery, judge prompts, or existing tests.
- A runtime, user, or project setting to turn audits on or off.
- Fixing judge misattribution, recording audit cost, or otherwise improving audit value. That evaluation happens separately while the audit is paused.
- Changes to agent-factory, Fly guest builds, or eval harnesses (Codagent-AI/agent-factory#60).
- Changing which builds carry the `dev_audit` tag.

## Impact

- Code: `internal/devaudit/provider_enabled.go` (switch and gated registration), skip guards in `internal/devaudit/cli_e2e_test.go` and any other test that depends on the automatic trigger, one new regression test, and the replay fallback in `scripts/docker-dev-audit-smoke-container.sh`. Its fixture-based script tests use a fake `agent-runner` and a pre-seeded lifecycle, so they keep exercising the automatic-present path.
- Docs: `docs/dev/sandbox.md` (smoke description) and `CHANGELOG.md`.
- Specs: delta specs for `automatic-run-audit` and `development-audit-availability`.
- Users and systems: local `dev_audit` builds, factory Fly guests, and eval guests stop spawning audits and stop the associated model spend. Manual `agent-runner audit replay` remains the only way to audit a run. agent-factory's readiness probe (`agent-runner audit`) is unaffected.
- Risk: low. A missed test dependency would show up as a `make test` failure, not a production regression.
