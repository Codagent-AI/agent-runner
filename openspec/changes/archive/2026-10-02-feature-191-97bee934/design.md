## Context

Automatic post-run auditing has exactly one trigger: `init()` in `internal/devaudit/provider_enabled.go` (build tag `dev_audit`).

```go
func init() {
	builtinworkflows.RegisterBuiltinAsset("audit/run-audit-v1.0.yaml", auditWorkflow)
	if strings.HasSuffix(os.Args[0], ".test") {
		return
	}
	runner.SetDefaultPostFinalizationHook(Coordinator{Launcher: launchDetached}.AfterFinalization)
}
```

`internal/runner/runner.go` uses `defaultPostFinalizationHook()` only when one has been registered. With no registration, a finalized run never reaches the devaudit `Coordinator`. That means no reservation, no `audit-lifecycle.json`, no audit run directory, no state linkage, and no lifecycle events. The runner needs no change.

The explicit command surface does not depend on the hook:
- `internal/devaudit.HandleCommand` handles `status`, `replay`, `reconcile`, and `internal audit-run`.
- `cmd/agent-runner/audit_cmd_enabled.go` handles bare `audit` / `help` usage, `setup`, `retry`, `repair-issues`, and `republish`.
- Replay and reconcile call `Coordinator{Launcher: launchDetached}.launch(...)` directly and load `builtin:audit/run-audit-v1.0.yaml`, which the same `init()` registers.

These verification surfaces depend on the automatic trigger:
- `internal/devaudit/cli_e2e_test.go` (`//go:build dev_audit && (darwin || linux)`) builds a real `dev_audit,devaudit_e2e` binary. Its three tests wait for an automatic link after a source run:
  - `TestE2E001TaggedCLIAutomaticAuditCompletesAndRetriesWithoutDuplicate`
  - `TestE2E002TaggedCLIFailureResumeReplayAndRetryPreserveLineage`
  - `TestTaggedClaudeAuditCompletesAllStages`

  The fixture helper `singleSourceRun()` finds the source run by the presence of `audit-lifecycle.json`.
- `scripts/docker-dev-audit-smoke-container.sh` runs `openspec:audit-smoke` and then requires `audit-lifecycle.json` with exactly one active link (lines 84–93) before releasing the fake judge and running `scripts/wait-dev-audit-smoke.py`. `wait-dev-audit-smoke.py` checks reciprocal linkage, output, provenance, and terminal state. It does not check the link's trigger.
- `scripts/docker_dev_audit_smoke_test.py` exercises the container script with a fake `agent-runner` (`exit 0`) and a pre-seeded lifecycle, so it covers only the automatic-present path.

Tests that call `Coordinator` or `AfterFinalization` directly do not depend on the registered hook and are unaffected. These include `provider_test.go`'s `TestE2E001AutomaticAuditCompletesLocallyWhenSheetsIsUnavailable`, `lifecycle_test.go`, and `status_test.go`. Go test binaries already skip registration through the `.test` guard.

## Goals / Non-Goals

**Goals:**
- `dev_audit` builds launch no automatic audit and leave no audit trace for a finalized run.
- Re-enabling takes one edit: `automaticAuditEnabled = false` → `true`. Tests and the Docker smoke then return to automatic-trigger coverage with no further edits.
- Explicit audit commands, the hidden workflow asset, and the Docker smoke keep working.
- `make test` and `make lint` pass.

**Non-Goals:**
- Deleting or refactoring audit code, workflows, or existing tests.
- Any runtime, environment, or configuration toggle.
- Changes to `internal/runner`, release builds, build tags, agent-factory, or eval guests.

## Approach

### 1. Switch and gated registration (`internal/devaudit/provider_enabled.go`)

Add one package-level constant next to `init()`:

```go
// automaticAuditEnabled gates the automatic post-run audit hook. It is
// temporarily false while we evaluate whether audits justify their model cost:
// https://github.com/Codagent-AI/agent-runner/issues/191
// Set it back to true to resume automatic auditing; explicit audit commands
// work either way.
const automaticAuditEnabled = false
```

Gate only the hook registration. The asset registration stays unconditional because replay, reconcile, and `internal audit-run` load it.

```go
func init() {
	builtinworkflows.RegisterBuiltinAsset("audit/run-audit-v1.0.yaml", auditWorkflow)
	if !automaticAuditEnabled || strings.HasSuffix(os.Args[0], ".test") {
		return
	}
	runner.SetDefaultPostFinalizationHook(Coordinator{Launcher: launchDetached}.AfterFinalization)
}
```

`Enabled()` keeps returning `true`, because it reports whether the binary contains the audit capability, which it still does. `Coordinator`, `AfterFinalization`, `launchDetached`, `Replay`, `Reconcile`, and `HandleCommand` stay unchanged. Lint: `AfterFinalization` is exported, and `launchDetached` and `Coordinator.launch` are still reached from replay and reconcile, so `unused` has nothing new to report. A `const`-false condition is not flagged by the repository's golangci configuration. If a linter does flag it, add a targeted `//nolint:<linter> // temporary pause, #191` on that line. Do not restructure the code.

### 2. Tests (`internal/devaudit`)

**Skip guards.** At the top of each of the three CLI E2E tests listed in Context, add:

```go
if !automaticAuditEnabled {
	t.Skip("automatic post-run audit is paused (#191)")
}
```

Use the same message everywhere. Before finishing, grep the `dev_audit`-tagged tests for every caller of `singleSourceRun()` / `waitForLinks()` that follows a plain source run, and guard each one found. No other test changes.

**Regression test for the paused behavior.** Add a new test to `cli_e2e_test.go` that reuses `newCLIAuditFixture`:
- Name: `TestTaggedCLIPausedAutomaticAuditLaunchesNothingAndReplayStillWorks`.
- Guard: `if automaticAuditEnabled { t.Skip(...) }`, the inverse of the skip guards, so exactly one of the two sets of tests runs.
- Write it first and confirm it fails before the switch is added. Before the switch exists, the automatic link appears.

Test steps:
1. `fixture.run(true, "-C", fixture.project, "--headless", "spec-driven:audit-e2e")` succeeds.
2. Find the source run as the only directory under `<home>/.agent-runner/projects/<encoded project>/runs`. Add a small helper, `onlyRunDir()`, because `singleSourceRun()` keys on the lifecycle file. Wait a short bounded grace period (about 2 s) and then assert:
   - there is still exactly one run directory, so no audit run directory was created;
   - `audit-lifecycle.json` does not exist in the source run directory;
   - the source `state.json` has no audit metadata or links (`state.Audit == nil` or empty `Links`);
   - `fixture.modelCallCount()` shows no judge invocation.

   Read state with `stateio.ReadState`.
3. `fixture.run(true, "audit")`: the output contains `Usage: agent-runner audit`.
4. Read the execution session ID from the source `run-metrics.json` (`metrics.Artifact.Sessions[0].ExecutionSessionID`). Run `fixture.run(true, "audit", "replay", source, "--session", id)`, then `fixture.waitForLinks(source, 1, true)`. Assert that the single link's `Trigger == "replay"`.

This is the spec's acceptance evidence: no automatic audit or trace, usage still printed, and explicit replay still completes.

### 3. Docker smoke (`scripts/docker-dev-audit-smoke-container.sh`)

Replace the lifecycle discovery (lines 86–87) with a path chosen by observation. Use the same `find` as today:
- **Automatic path:** if a lifecycle file is found, keep today's behavior unchanged.
- **Replay path:** if no lifecycle file is found:
  1. Locate the single source run directory: `find "$HOME/.agent-runner/projects" -mindepth 3 -maxdepth 3 -path '*/runs/*' -type d`. Fail with `smoke: source run directory not found` unless there is exactly one.
  2. Read `run-metrics.json` with an inline `python3` snippet and take the single `sessions[].execution_session_id`. Fail with a clear `smoke:` message if it is missing.
  3. Print to stderr: `smoke: automatic audit is paused (#191); replaying execution session <id>`.
  4. Run `agent-runner audit replay "$source_dir" --session "$session_id" --project "$project"`. On non-zero exit, fail with `smoke: explicit audit replay failed`; `set -e` plus an explicit message is enough.
  5. Re-run the lifecycle discovery and continue.

Both paths then run the existing active-single-link assertion, the release, `wait-dev-audit-smoke.py`, and the protected-file check. The replayed audit is still blocked by the fake codex release file, so the "exactly one active link" assertion still holds. On every failure, the smoke keeps the artifact-retention behavior it already has.

**Script test.** Add a replay-path case to `scripts/docker_dev_audit_smoke_test.py`. `setUp` seeds both `runs/source` and `runs/audit`, but the fallback requires exactly one run directory before replay, so the case moves through these fixture states:
1. **Before the smoke:**
   - Move the fully seeded `runs/audit` directory, with its state, request, observations, model output, and report, to a staging path outside the runs tree.
   - Delete `source/audit-lifecycle.json`.
   - Write `source/run-metrics.json` as `{"sessions":[{"execution_session_id":"session"}]}`.

   The runs tree now contains only `source`. The seeded `source/state.json` already carries the reciprocal link to `audit`, which is what a real replay records.
2. **Fake `agent-runner`:**
   - Called as `audit replay <dir> --session <id> --project <dir>`, it:
     - records its argv to a file;
     - asserts that exactly one directory exists under `runs/` at that moment, writing a marker the test checks;
     - moves the staged audit directory back to `runs/audit`;
     - writes `source/audit-lifecycle.json` with one `started` link for `audit`;
     - exits 0.
   - Any other call, such as the source workflow run, exits 0.

   Point the fake at the staging path through an env var such as `SMOKE_TEST_STAGED_AUDIT`.
3. **After release:** the existing `run_smoke` updater marks the link `completed` once `release-audit` exists. It reads `self.lifecycle`, so set that to the replay-written lifecycle. `wait-dev-audit-smoke.py` then validates the restored audit directory exactly as in the automatic cases.

Assertions:
- the exit code is 0;
- stderr contains the paused/replay message;
- the single-run-directory marker was written before replay;
- the recorded argv contains `audit replay`, the source run directory, and `--session session`;
- stdout contains `development-audit smoke passed`, which proves the replay path reached the waiter.

Add a second case with the same pre-smoke state, where the fake replay exits non-zero without restoring anything. Assert that the smoke fails with the replay message and does not print `smoke passed`. The existing cases, with their pre-seeded lifecycle, keep covering the automatic path and must record no replay call.

### 4. Documentation

`docs/dev/sandbox.md`:
- In the detached-audit smoke section, add a short paragraph. It says that automatic post-run audits are paused (#191), and that while they are paused the smoke launches its audit through `agent-runner audit replay` of the source execution session and reports this. It also says the rest of the journey checks are unchanged.
- The intro sentence "Select the private development-audit build explicitly when exercising the audit lifecycle" stays. Add a note that `dev_audit` builds currently do not audit automatically.

CHANGELOG: no direct edit. The release skill curates `CHANGELOG.md` from merged PRs, and there is no `Unreleased` section to append to. The PR description carries the note.

## Decisions

- **Compile-time `const`, not `var`.** A const cannot be changed at runtime, by tests, or through ldflags. That satisfies the spec's "no runtime flag, environment variable, or configuration" rule, and flipping it is a single-line diff. A `var` would invite test overrides that blur which mode the binary is in.
- **Gate inside `init()`, not inside `Coordinator.AfterFinalization`.** Gating at registration means the runner never calls into devaudit, so no reservation or lifecycle code can run partway through. Gating inside the hook would still execute the hook body and invites partial writes.
- **Mirrored skip guards and a mirrored regression test**, keyed to the same const. Whichever value the switch has, `make test` exercises the matching behavior, and nothing besides the const needs editing on re-enable.
- **Smoke path selected by observed lifecycle**, not by reading the const or adding a probe. This was accepted in proposal review PR-1. It keeps re-enabling to one line and adds no CLI surface.
- **No CHANGELOG edit.** See Documentation.

## Risks / Trade-offs

- **Masked hook regression in the smoke once re-enabled.** If the switch is `true` but the hook breaks, the smoke falls back to replay and passes. Mitigation: the switch-guarded Go CLI E2E tests then run and fail on a missing automatic link.
- **Missed automatic-trigger test dependency.** Any such test fails `make test`, a loud failure in this PR rather than a silent regression. Mitigation: the grep sweep in step 2 and a full `make test` run.
- **Existing reserved automatic links on developer machines.** They stay untouched and remain reconcilable through `audit reconcile`. Nothing new is created.
- **Lint on a const-false branch.** This is low likelihood. Handle it with a targeted `nolint` comment as described in Approach §1.

## Migration Plan

No data migration. The rollout is the merged PR. Rebuilt `dev_audit` binaries (`make build`, `./dev.sh`, factory and eval guest builds) stop auditing immediately. Already-running detached audits finish normally.

To roll back or re-enable: set `automaticAuditEnabled = true` and rebuild. The skip guards, the regression test, and the smoke all switch to automatic-mode coverage with no further changes.

## Open Questions

None.
