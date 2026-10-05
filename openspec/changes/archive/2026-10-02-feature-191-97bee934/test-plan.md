## Coverage Strategy

Specifications remain the source of unit-test requirements. This plan records only:
- additional integration and end-to-end obligations;
- the acceptance testing envelope;
- exceptional human-only obligations.

The change is a compile-time gate in the `dev_audit` binary's `init()`. Go test binaries already skip hook registration through the `.test` guard, so no in-process unit test can observe the gate. The meaningful proof needs a real tagged binary (E2E-001). The Docker smoke's new replay fallback is shell logic, and it gets an integration obligation against the script's existing fake-CLI harness (INT-001).

Both new obligations are mirrored to the switch. The existing automatic-trigger CLI E2E tests stay in the suite and run again when `automaticAuditEnabled` is `true`. Today they are skipped by design:
- `TestE2E001TaggedCLIAutomaticAuditCompletesAndRetriesWithoutDuplicate`
- `TestE2E002TaggedCLIFailureResumeReplayAndRetryPreserveLineage`
- `TestTaggedClaudeAuditCompletesAllStages`

Exactly one of the two mirrored sets runs under `make test`, whichever way the switch is set.

No integration obligation is added for intake routing, reconcile, `status`, `retry`, or `setup`:
- With no hook registered, the runner's existing nil-hook path applies, and existing runner tests cover it.
- The command handlers are untouched, and their existing tests in `cmd/agent-runner/audit_cmd_enabled_test.go` and `internal/devaudit` keep running.

## Integration Tests

### INT-001: Docker smoke container script falls back to explicit replay
- Covers: `development-audit-sandbox`, specifically "Product-owned Docker smoke proves the detached audit journey" (scenarios "Smoke replays while automatic auditing is paused", "Replay cannot be launched", and "Smoke keeps automatic assertion when auditing is resumed").
- Boundary: the real `scripts/docker-dev-audit-smoke-container.sh` and `scripts/wait-dev-audit-smoke.py`, run as subprocesses against a fake `agent-runner` on `PATH` and a fixture runs tree.
- Setup:
  - Use the existing `scripts/docker_dev_audit_smoke_test.py` harness and fake `mktemp`.
  - Before the smoke:
    - move the seeded `runs/audit` directory to a staging path outside the runs tree, so that only `runs/source` exists;
    - delete `source/audit-lifecycle.json`;
    - seed `source/run-metrics.json` with one session, `session`.
  - The fake `agent-runner`:
    - on `audit replay <dir> --session session --project <dir>`:
      - records its argv;
      - records that exactly one run directory existed;
      - moves the staged audit directory back into `runs/`;
      - writes the lifecycle with one `started` link;
    - otherwise: exits 0.
  - The existing updater marks the link `completed` after release, so `wait-dev-audit-smoke.py` validates the restored audit artifacts.
  - For the failure case, the fake replay exits non-zero and restores nothing.
- Action: run the container script as the existing tests do.
- Assertions:
  - Success case:
    - the exit code is 0;
    - stderr contains the paused/replay message referencing #191;
    - exactly one run directory existed when replay was called;
    - the recorded argv contains `audit replay`, the source run directory, and `--session session`;
    - stdout contains `development-audit smoke passed`, which proves the replay path reached the waiter.
  - Failure case:
    - the exit code is non-zero;
    - stderr contains the replay-failure message;
    - stdout does not contain `smoke passed`.
  - Existing cases with a pre-seeded lifecycle still pass unchanged, so the automatic path issues no replay call.
- Execution: `python3 scripts/docker_dev_audit_smoke_test.py`. Run it in the same CI and validator phase as the existing script tests. Requires `bash` and `python3`; no Docker.

## End-to-End Tests

### E2E-001: Paused `dev_audit` binary leaves no automatic audit, and explicit audit commands still work
- Covers:
  - `automatic-run-audit`: "Automatic auditing is temporarily paused" (scenarios "Paused build finalizes an eligible execution", "Paused build leaves source outcome unchanged", "Explicit replay works while paused", and "Audit usage remains available while paused") and "Eligible executions trigger automatic auditing" (scenario "Paused build does not trigger audit").
  - `development-audit-availability`: "Paused development build keeps explicit audit commands" and "Eligible local execution completes while paused".
  - These are the issue's acceptance criteria.
- Surface: the real `agent-runner` CLI built with `-tags dev_audit,devaudit_e2e` by `newCLIAuditFixture` in `internal/devaudit/cli_e2e_test.go`. It is not a `.test` binary, so `init()` runs as it does in production development builds.
- Setup: the existing fixture provides an isolated `HOME` and project, fake Codex/Claude CLIs that count model calls, a local fake Sheets server, and the hermetic `spec-driven:audit-e2e` workflow. The test skips when `automaticAuditEnabled` is `true`.
- Journey:
  1. `agent-runner -C <project> --headless spec-driven:audit-e2e`
  2. Wait a bounded grace period (about 2 s), then inspect the runs directory.
  3. `agent-runner audit`
  4. `agent-runner audit replay <source-run-dir> --session <execution-session-id>`, with the ID taken from the source `run-metrics.json`.
- Assertions:
  - The source command succeeds.
  - Exactly one run directory exists, so no audit run directory was created.
  - The source has no `audit-lifecycle.json`.
  - The source `state.json` has no audit links.
  - The fake judge's model-call count is zero after the source run.
  - `audit` exits 0 and prints `Usage: agent-runner audit`.
  - The replay exits 0, and the source lifecycle reaches exactly one `completed` link with `trigger == "replay"`.
- Execution: `go test -tags dev_audit ./internal/devaudit -run TestTaggedCLIPausedAutomaticAuditLaunchesNothingAndReplayStillWorks` and `make test`. Platform constraint: darwin and linux, as for the file's build tag.

## Acceptance Testing Envelope

- Environments and sandboxes:
  - the local checkout, including `./dev.sh` (development-audit build from source) and `go build -tags dev_audit[,devaudit_e2e]` binaries written to temporary directories;
  - the Docker development sandbox (`scripts/sandbox-run.sh --dev-audit`, `scripts/docker-dev-audit-smoke.sh`) when Docker is available locally.

  Use an isolated `HOME`, for example a temp directory, for any run that writes run or audit state.
- Credentials and secrets: none are required or should be used. The hermetic fixtures use fake Codex and Claude CLIs and no Google connection. Do not import host credentials into the sandbox.
- Authorized effects:
  - Local temporary run and audit directories, removed or left under a temp root.
  - Running `scripts/docker-dev-audit-smoke.sh`, which builds a run-unique image and removes it and its successful-run artifacts on exit. This costs only local Docker build time.
  - Explicit `audit replay`, `audit status`, and bare `audit` against fixture runs under the isolated `HOME`.
- Off limits:
  - Real model invocations, including real openspec or spec-driven workflows with real agents, because they cost money, which is the point of this change.
  - `audit setup` with real OAuth material, `audit retry` against a real spreadsheet, and `audit repair-issues` / `audit republish`, which create or modify GitHub issues.
  - The developer's real `~/.agent-runner` state.
  - agent-factory, Fly, or eval guest infrastructure.
- Permitted substitutes:
  - fake agent CLIs from the existing E2E and smoke fixtures in place of real models;
  - if Docker is unavailable, INT-001 plus a direct local run of the container script logic against a tagged binary with an isolated `HOME`, in place of the full Docker smoke.

  Record the limitation if a substitute is used.
- Known risk areas:
  - Hidden automatic-trigger test dependencies outside the three named CLI E2E tests. A full `make test` run detects them.
  - The smoke's replay discovery: run-directory and session lookup in the container's `HOME` layout.
  - A lint finding on the const-false branch. Handle it with a targeted `nolint` only.
  - Accepted limitation: with the switch back on, a broken hook makes the smoke fall back to replay. The mirrored Go E2E tests carry that detection.
  - Release and untagged builds must remain free of any audit surface. This is unchanged, but worth one spot check with an untagged build.

## Human-Only Testing

None.

## Coverage Map

| Requirement or journey | INT | E2E | HT |
| --- | --- | --- | --- |
| automatic-run-audit: Automatic auditing is temporarily paused (no audit or trace; source unchanged; replay and usage still work) | — | E2E-001 | — |
| automatic-run-audit: Eligible executions trigger automatic auditing (paused scenario) | — | E2E-001 | — |
| development-audit-availability: paused build keeps explicit audit commands; no automatic audit while paused | — | E2E-001 | — |
| development-audit-sandbox: Docker smoke replay fallback and failure path; automatic path unchanged | INT-001 | — | — |
