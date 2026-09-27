# Task: Smoke-owned Docker image and artifact cleanup

## Goal

Make `scripts/docker-dev-audit-smoke.sh` release the Docker image and artifact directory it creates,
and nothing else, so unattended runs stop leaving 7 GB images and temporary directories on the
host (issue #136). Document the ownership contract in `docs/dev/sandbox.md`.

## Background

Read these before starting:

- the change artifacts in `openspec/changes/feature-136-8b59044d/`: `proposal.md`, `design.md`
  (the authoritative mechanism), `test-plan.md` (INT-001 to INT-005), and
  `specs/development-audit-sandbox/spec.md`;
- `scripts/docker-dev-audit-smoke.sh`, the wrapper and the only production script that changes;
- `scripts/sandbox-run.sh`. It stays unchanged. It already honors `IMAGE`, `--image`, `--env`, and
  repeatable `--docker-run-arg`, and it runs `docker build -t "$IMAGE"` and then
  `docker run --rm --init ...`, with the artifact directory mounted at `/artifacts`;
- `scripts/docker-dev-audit-smoke-container.sh`. It stays unchanged. It runs inside the container
  and writes all evidence beneath `/artifacts/dev-audit-smoke.XXXXXX`;
- `scripts/sandbox_scripts_test.go`, for the existing test conventions: package `scripts_test`, a
  `repoRoot(t)` helper, and `exec.Command("bash", ...)`;
- the smoke section of `docs/dev/sandbox.md`.

Wrapper behavior to implement (see "Approach" in `design.md`):

1. **Ownership, resolved once.**
   - Build `run_id="$(date -u +%Y%m%d%H%M%S)-$$-$RANDOM"`.
   - If `${IMAGE:-}` is empty, set `IMAGE=agent-runner-dev-audit-smoke:$run_id` and `owns_image=1`.
     Otherwise the caller owns the tag.
   - If `${ARTIFACT_DIR:-}` is empty, create the directory with the existing `mktemp -d` template and
     set `owns_artifacts=1`. Otherwise the caller owns the directory.
   - Always set `container=agent-runner-dev-audit-smoke-$run_id`.
2. **Launch.**
   - Install the traps first: `trap cleanup EXIT`, `trap 'on_signal 130' INT`, and
     `trap 'on_signal 143' TERM`.
   - Run `sandbox-run.sh` in the background with the existing arguments plus `--image "$IMAGE"` and
     `--docker-run-arg "--name=$container"`, and record `child=$!`.
   - Capture the status with `status=0; wait "$child" || status=$?`, set `final_status=$status`, and
     run `exit "$status"`.
3. **`on_signal`.**
   1. Set `final_status`.
   2. Run `pkill -TERM -P "$child"`, then `kill -TERM "$child"`.
   3. Run `docker rm -f "$container"`, ignoring a missing container.
   4. Wait for the child for at most about 30 seconds, using a `kill -0` loop that polls every 0.2
      seconds, then run `wait "$child" || true`.
   5. Run `docker rm -f "$container"` once more.
   6. Run `exit "$final_status"`.
4. **`cleanup`** runs once, guarded by a flag, from the EXIT trap.
   - Start with `final_status="${final_status:-$?}"` and `set +e`.
   - If `owns_image=1` and `docker image inspect "$IMAGE"` succeeds, run `docker image rm "$IMAGE"`
     with no force. If that fails, print `smoke: could not remove image $IMAGE: <reason>` on stderr.
     If the image is absent, stay silent.
   - If `final_status` is 0 and `owns_artifacts=1`, run `chmod -R u+w -- "$ARTIFACT_DIR"` and then
     `rm -rf -- "$ARTIFACT_DIR"`.
     - If the directory is still present, print `smoke: could not remove artifact directory <path>`.
     - Otherwise print `smoke: removed artifact directory <path>`.
   - If `final_status` is not 0, print `smoke: evidence retained in <path>` on stderr, for both owned
     and caller-supplied directories.
   - End with `exit "$final_status"`. Cleanup never changes the status.

Constraints:

- The wrapper must work on bash 3.2, the macOS `/usr/bin/env bash`. Do not use timed `wait`,
  `wait -n`, `wait -p`, associative arrays, or `mapfile`. Keep `set -euo pipefail`.
- Never run any `prune`, never force-remove an image, and never remove images by pattern. Never
  name `agent-runner-dev:local` unless the caller supplied it. The only names passed to mutating
  Docker commands are the owned `$IMAGE` and `$container`.
- Host-side removal happens only after `sandbox-run.sh` has returned. The container has then exited,
  so the detached audit process cannot race the removal. Do not add deletion inside the container.
- Native Linux is supported only when the host uid matches the container's `pwuser` (1000). That is
  an existing constraint; state it in the docs, and do not work around it.

Documentation (`docs/dev/sandbox.md`, smoke section):

- the run-unique `agent-runner-dev-audit-smoke:<run-id>` tag and its removal on exit;
- `IMAGE` supplied by the caller: the tag is kept and can be reused warm;
- `ARTIFACT_DIR` supplied by the caller: the directory is always kept;
- owned artifacts are removed after success and kept after a failure or interrupt, and the path is
  printed;
- the build cache is left for the host to reclaim;
- to recover an image left behind by a killed run, use `docker image ls agent-runner-dev-audit-smoke`
  and then `docker image rm <tag>`;
- the uid prerequisite on Linux.

Update the sentence that currently says artifacts are kept in the selected directory, so it reflects
these rules.

Tests: follow TDD. Write the INT-001 to INT-005 tests from `test-plan.md` first, in the new file
`scripts/docker_dev_audit_smoke_cleanup_test.go`.

- Put a fake `docker` executable first on `PATH`. It logs each argv and uses environment knobs to
  control:
  - the exit codes of `build` and `run`;
  - whether `run` blocks;
  - whether `image rm` fails;
  - whether `image inspect` reports the image as present.
- Fake `run` writes evidence under the mounted host artifact path, including a mode-0500
  subdirectory that contains a file.
- For INT-005(c), put an `rm` stub on `PATH` that fails only for the owned artifact directory.
- Set `TMPDIR` to a temporary directory created by the test.
- Apply the shared assertions from the test plan to every test.
- Skip the signal test on Windows.
- Do not require a Docker daemon.

## Spec

The normative requirements are in `openspec/changes/feature-136-8b59044d/specs/development-audit-sandbox/spec.md`:

- "Docker smoke releases only the Docker resources it owns", all scenarios;
- "Docker smoke removes only its own artifact directory, and only after success", all scenarios;
- the modified "Development documentation states operating contracts".

## Done When

- `go test ./scripts` passes. It includes new tests that cover INT-001 to INT-005 and every scenario
  of the two ADDED requirements.
- The existing tests in `scripts/` still pass: `sandbox_scripts_test.go`, and the Python harness
  through `TestDevelopmentAuditSmokeHarness`.
- The wrapper has no bash 4-only constructs.
- `make lint` is clean for any Go changes.
- `docs/dev/sandbox.md` documents the ownership contract, the recovery command, and the Linux uid
  prerequisite.
- `scripts/sandbox-run.sh` and `scripts/docker-dev-audit-smoke-container.sh` are unchanged.
- The agent acceptance flows AT-001 to AT-003, and AT-004 when a native Linux host with a matching
  uid is available, are left for the acceptance phase as described in `test-plan.md`.
