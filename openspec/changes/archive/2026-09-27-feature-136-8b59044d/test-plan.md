## Coverage Strategy

Specifications remain the source of unit-test requirements. This plan records only additional
integration, end-to-end, agent-acceptance, and exceptional human-only obligations.

The risk lives at process boundaries:

- the wrapper `scripts/docker-dev-audit-smoke.sh`;
- the real `scripts/sandbox-run.sh` it launches;
- the Docker CLI;
- the host filesystem;
- signal delivery.

Automated coverage therefore uses integration tests. Each test runs the real wrapper and the real
`sandbox-run.sh` as subprocesses, with a fake `docker` executable first on `PATH` that records every
argv and simulates build and run outcomes.

No automated E2E test is planned. CI has no Docker daemon, the image is about 7 GB, and its build
needs the network. The real journey is covered by required agent acceptance on a Docker host instead.

## Integration Tests

Shared fixture for INT-001 through INT-005. The Go test must pass on Linux CI. The wrapper must also
work on bash 3.2, and AT-002 on the factory Mac is the evidence for that.

- A Go test in package `scripts_test`, file `scripts/docker_dev_audit_smoke_cleanup_test.go`, run by
  `go test ./scripts` and `make test`.
- A fake `docker` script is placed first on `PATH`. It appends each invocation's argv to a log file.
  It supports `build`, `run`, `image inspect`, `image rm`, and `rm -f`. Environment knobs set its
  behavior:
  - the exit code of `build`;
  - the exit code of `run`;
  - whether `run` blocks until it receives a signal;
  - whether `image rm` fails;
  - whether `image inspect` reports the image as present.
- To simulate container output, fake `run` writes files beneath the host path that is mounted at
  `/artifacts`. These include a subdirectory with mode 0500 that contains a file, standing in for
  restrictive modes created under confinement.
- `TMPDIR` points at a temporary directory created by the test.
- Shared assertions run for every test:
  - Mutating Docker commands (`image rm`, `rmi`, `rm`, and any `prune`) target only the tag and
    container name owned by the run. The tag counts only when the run owns the image.
  - `image rm` never carries `-f` or `--force`. `docker rm -f` is allowed on the run's own
    container.
  - No `prune` command is ever issued.
  - `agent-runner-dev:local` never appears unless the caller supplied it through `IMAGE`.
  - `build` and `run` may use a tag supplied by the caller.

### INT-001: Successful smoke releases the image and artifacts it owns
- Covers: the requirement that the Docker smoke releases only the Docker resources it owns (scenario
  "Smoke succeeds without a caller-supplied image"); the artifact-directory requirement (scenario
  "Owned directory after success"); and the scenarios "Shared development image exists" and
  "Another run's image is present", through the shared argv assertion.
- Boundary: wrapper → `sandbox-run.sh` → Docker CLI → host `TMPDIR`.
- Setup: `IMAGE` and `ARTIFACT_DIR` are unset. Fake build and run succeed, and `image inspect`
  reports the image as present.
- Action: run the wrapper.
- Assertions:
  - The exit code is 0.
  - `build` and `run` both use a tag that matches `agent-runner-dev-audit-smoke:<run-id>`.
  - `run` includes `--name=agent-runner-dev-audit-smoke-<run-id>` with the same run id.
  - `image rm` is called exactly once, on that tag, without `--force`.
  - No `agent-runner-dev-audit-smoke.*` directory remains under `TMPDIR`, including the mode-0500
    subtree.
- Execution: `go test ./scripts -run TestDevAuditSmokeCleanup`.

### INT-002: Failed smoke removes its image but keeps evidence
- Covers: scenarios "Smoke fails without a caller-supplied image" and "Owned directory after
  failure".
- Boundary: the same as INT-001.
- Setup: `IMAGE` and `ARTIFACT_DIR` are unset, and fake `run` exits 1 after writing evidence files.
- Action: run the wrapper.
- Assertions:
  - The exit code is 1, which shows the nonzero status survived errexit.
  - `image rm` is called once, without forcing, on the owned tag, through the EXIT-trap cleanup.
  - The owned artifact directory still exists and contains the evidence files.
  - Stderr contains `evidence retained in <that directory>`.
- Execution: `go test ./scripts`.

### INT-003: Images and directories supplied by the caller are never removed
- Covers: scenarios "Caller supplies an image tag" and "Caller-supplied directory after success",
  plus the rule that the wrapper never deletes a caller-supplied `ARTIFACT_DIR`, whatever the
  outcome.
- Boundary: the same as INT-001.
- Setup:
  - `IMAGE=example/caller:tag` is set.
  - `ARTIFACT_DIR` is set to a directory created by the test that already contains a sentinel file.
  - One case runs with a successful fake `run` and one with a failing fake `run`.
- Action: run the wrapper once for each case.
- Assertions:
  - `build` and `run` use `example/caller:tag`.
  - `image rm` is never called.
  - The caller's directory and its sentinel file remain in both cases.
  - The exit code matches the journey's result.
- Execution: `go test ./scripts`.

### INT-004: SIGTERM to the wrapper stops its own container and cleans up
- Covers: scenarios "Operator interrupts the smoke" and "Owned directory after interrupt".
- Boundary: wrapper signal handling → background `sandbox-run.sh` → Docker CLI client processes.
- Setup: `IMAGE` and `ARTIFACT_DIR` are unset, and fake `run` writes evidence and then blocks.
- Action: start the wrapper. Once the log shows `run`, send SIGTERM to the wrapper process only.
- Assertions:
  - The wrapper exits with 143 within a bounded time, at most 45 seconds.
  - `rm -f agent-runner-dev-audit-smoke-<run-id>` was logged, before `image rm`.
  - `image rm` was logged once, on the owned tag, without forcing.
  - The owned directory remains, and stderr names it.
  - No fake `docker` process is left running.
- Execution: `go test ./scripts`. The test is skipped on Windows.

### INT-005: Cleanup failures are reported without changing the exit status
- Covers: scenarios "Removing the image fails", "Image build fails before an image exists", and
  "Artifact removal fails".
- Boundary: the same as INT-001.
- Setup, as three cases:
  - (a) Fake `image rm` exits nonzero after a successful run.
  - (b) Fake `build` exits nonzero, and `image inspect` reports the image as absent.
  - (c) Fake `run` succeeds, and an `rm` stub first on `PATH` fails for the owned artifact
    directory. For every other path, the stub delegates to the real `rm`.
- Action: run the wrapper once for each case.
- Assertions:
  - (a) The exit code is 0, and stderr reports that the owned tag could not be removed.
  - (b) The exit code is nonzero, `image rm` is never called, and stderr has no image-removal
    warning.
  - (c) The exit code is 0, and stderr reports that the owned artifact directory could not be
    removed.
- Execution: `go test ./scripts`.

## End-to-End Tests

None. A real Docker end-to-end run cannot be executed in CI, because there is no daemon and the image
is multi-GB. AT-001 and AT-002 cover the complete journey on a real Docker host. INT-001 through
INT-005 cover every branch automatically.

## Agent Acceptance Tests

### AT-001: Unattended smoke on Docker leaves no image or artifacts behind
- Classification: Required.
- Covers:
  - the image-ownership requirement: success, the shared development image, and another run's image;
  - the artifact-directory requirement: success;
  - the proposal's measurement condition for disk reclamation.
- Actor and surface: an unattended agent caller using the `scripts/docker-dev-audit-smoke.sh` CLI.
- Setup:
  - A macOS host with Docker Desktop, like the factory Mac, and at least 15 GiB free.
  - `IMAGE` and `ARTIFACT_DIR` are unset.
  - No credentials are needed.
  - Record the image ID of `agent-runner-dev:local`, or record that it is absent. Do not build it.
- Steps:
  1. Record `docker system df -v`, `docker image ls -a`, and `ls "${TMPDIR:-/tmp}"`.
  2. Run `scripts/docker-dev-audit-smoke.sh` to completion.
  3. Record the same three outputs again.
- Expected:
  - The smoke exits 0.
  - Afterwards there is no `agent-runner-dev-audit-smoke` image, no new `<none>` dangling image, and
    no new `agent-runner-dev-audit-smoke.*` directory.
  - `agent-runner-dev:local` is unchanged: it has the same ID, or it is still absent.
  - Any bytes that remain from the run appear only under the build cache.
- Evidence: terminal transcript of the before and after outputs, and the smoke's final output lines.
- Effects and cleanup:
  - The run temporarily uses several GB of disk. It needs network access for the image build unless
    the cache is warm, and it takes several minutes.
  - If anything owned by the run remains, report it as a defect, then remove it by hand with
    `docker image rm` or `rm -rf` on the named leftover only.
- Permitted substitutes: none. If no Docker host is available, acceptance is incomplete.

### AT-002: Interrupting a live smoke releases its container and image and keeps evidence
- Classification: Required.
- Covers: the image-ownership requirement (interrupt) and the artifact-directory requirement (after
  interrupt).
- Actor and surface: an operator or supervisor using the smoke CLI, who sends SIGTERM (supervisor)
  or SIGINT (Ctrl-C).
- Setup: the same host as AT-001, with `IMAGE` and `ARTIFACT_DIR` unset.
- Steps:
  1. Start the smoke in the background.
  2. When `docker ps` shows `agent-runner-dev-audit-smoke-<run-id>`, send SIGTERM to the wrapper
     process only.
  3. Wait for the wrapper to exit.
  4. Inspect `docker ps -a`, `docker image ls`, and the stderr output.
- Expected:
  - The wrapper exits nonzero (143) promptly.
  - The run's container no longer exists, and the run's image tag no longer exists.
  - Stderr prints the retained artifact directory, and that directory exists.
- Evidence: terminal transcript showing the signal, the exit status, and the `docker ps -a` and
  `docker image ls` output before and after.
- Effects and cleanup: delete the retained artifact directory by hand after capturing evidence.
- Permitted substitutes: none.

### AT-003: A caller-owned image is reusable, and the documented recovery works
- Classification: Required.
- Covers: the image-ownership requirement (scenario "Caller supplies an image tag"), plus the modified
  documentation requirement for keeping resources and recovering a leftover image.
- Actor and surface: a developer who follows the smoke section of `docs/dev/sandbox.md`, using the
  smoke CLI.
- Setup: the same host as AT-001.
- Steps:
  1. Following the docs, run the smoke with `IMAGE=agent-runner-dev:acceptance-136`.
  2. Following the docs, find how to recover an `agent-runner-dev-audit-smoke:*` image left by a
     killed run. Apply that procedure to a leftover image, created with
     `docker tag agent-runner-dev:acceptance-136 agent-runner-dev-audit-smoke:acceptance-136`.
- Expected:
  - The smoke passes, and `agent-runner-dev:acceptance-136` still exists afterwards.
  - The docs name the tag contract, `IMAGE`, `ARTIFACT_DIR`, the retention rules, and the recovery
    command.
  - The documented recovery removes only the leftover tag.
- Evidence: terminal transcript, and the quoted documentation lines that were followed.
- Effects and cleanup: remove `agent-runner-dev:acceptance-136` afterwards.
- Permitted substitutes: none.

### AT-004: On native Linux Docker, successful-run artifacts are removed completely
- Classification: Conditional. This applies when a native Linux Docker host is available whose
  non-root user has the same uid as the container's `pwuser` (1000), which is the supported Linux
  configuration. Docker Desktop maps bind-mount ownership, so it does not exercise this boundary. A
  mismatched uid is unsupported, because the container cannot write the artifact mount, and is not
  tested.
- Covers: the artifact-directory requirement (scenario "Owned directory after success"), for files
  the container wrote.
- Actor and surface: a Linux developer or CI host using the smoke CLI.
- Setup: a Linux host whose user uid is 1000, with `IMAGE` and `ARTIFACT_DIR` unset.
- Steps: run the smoke to success, then list `${TMPDIR:-/tmp}`.
- Expected: the smoke exits 0, and no `agent-runner-dev-audit-smoke.*` directory remains.
- Evidence: terminal transcript.
- Effects and cleanup: the same as AT-001.
- Permitted substitutes: when no such host is available, this flow does not apply. Record that it was
  not applicable. INT-001, which includes the restrictive-mode subtree, remains the automated
  evidence.

## Human-Only Testing

None.

## Coverage Map

| Requirement or journey | INT | E2E | AT | HT |
| --- | --- | --- | --- | --- |
| Docker smoke releases only the Docker resources it owns: success | INT-001 | — | AT-001 | — |
| Same requirement: failure | INT-002 | — | — | — |
| Same requirement: interrupt | INT-004 | — | AT-002 | — |
| Same requirement: caller supplies an image | INT-003 | — | AT-003 | — |
| Same requirement: shared image and other runs untouched | INT-001–005 (shared assertions) | — | AT-001 | — |
| Same requirement: image rm fails, or the build fails | INT-005 | — | — | — |
| Artifact-directory removal: owned directory after success | INT-001 | — | AT-001, AT-004 | — |
| Same requirement: owned directory after failure or interrupt | INT-002, INT-004 | — | AT-002 | — |
| Same requirement: caller-supplied directory | INT-003 | — | — | — |
| Same requirement: artifact removal fails | INT-005 | — | — | — |
| Documentation states operating contracts (resource ownership) | — | — | AT-003 | — |
| Proposal disk-reclamation measurement | — | — | AT-001 | — |
