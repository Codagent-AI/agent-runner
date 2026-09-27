## Context

`scripts/docker-dev-audit-smoke.sh` is a thin wrapper around `scripts/sandbox-run.sh`:

1. It creates an artifact directory with `mktemp -d "${TMPDIR:-/tmp}/agent-runner-dev-audit-smoke.XXXXXX"`,
   unless `ARTIFACT_DIR` is set.
2. It calls `sandbox-run.sh` in the foreground with `--dev-audit --dev-audit-smoke
   --no-default-secrets`, the artifact directory, `--network=none`, and the container command
   `bash /agent-runner-source/scripts/docker-dev-audit-smoke-container.sh`.

`sandbox-run.sh` runs `docker build -t "$IMAGE"`, where `IMAGE` defaults to `agent-runner-dev:local`
and can be set through the environment or `--image`. It then runs `docker run --rm --init ...`, with
the artifact directory bind-mounted at `/artifacts`. The container runs as `pwuser`. The container
script creates `smoke_root="$(mktemp -d /artifacts/dev-audit-smoke.XXXXXX)"` and writes the whole
journey beneath it: project, home, and Runner state. On success it prints
`development-audit smoke passed; artifacts retained in $smoke_root`.

Nothing is ever removed. Several constraints shape the fix:

- `agent-runner-dev:local` is shared with manual sandbox use and other tooling.
- Factory agents may run smokes in parallel.
- The wrapper's `mktemp -d` directory has mode 0700 and belongs to the host user. The container runs
  as `pwuser` (uid 1000 in the Playwright base image).
  - On macOS Docker Desktop, bind-mount ownership is mapped to the host user.
  - On native Linux, the container can write into the directory only when the host uid matches
    `pwuser`. With a mismatched uid, the smoke already fails before it reaches success, so a working
    run always leaves files that the host user owns.
  - Files can still end up with restrictive modes, for example read-only directories created under
    audit confinement.
- The detached audit process runs inside the container. The container script waits only for
  terminal audit state, not for that process to exit. The process stops only when the container
  ends.
- When a bash script waits on a foreground child, it defers trapped signals until the child exits.
- `#!/usr/bin/env bash` resolves to bash 3.2.57 on the factory Mac. That version has no timed
  `wait`, `wait -n`, or `wait -p`. The existing scripts already use idioms that work on bash 3.2.
- The wrapper runs under `set -euo pipefail`.

Existing tests:

- `scripts/sandbox_scripts_test.go` tests `sandbox-run.sh` through dry runs.
- `scripts/docker_dev_audit_smoke_test.py`, run from `TestDevelopmentAuditSmokeHarness`, tests the
  container script with local stub CLIs on `PATH`.
- No test runs real Docker.

## Goals / Non-Goals

**Goals:**
- Meet the two ADDED requirements in `specs/development-audit-sandbox/spec.md`:
  - release the image the smoke owns, and only that image;
  - release the artifact directory the smoke owns, and only after success.
- Clean up after success, failure, and interrupt (SIGINT and SIGTERM), including the case where a
  SIGTERM reaches only the wrapper process.
- Remove artifacts the same way on macOS Docker Desktop and on native Linux Docker, which is
  supported when the host uid matches `pwuser`, with no race against processes in the container.
- Run on bash 3.2.
- Make every cleanup path testable without a Docker daemon.

**Non-Goals:**
- Changing the default behavior of `sandbox-run.sh`.
- Build-cache pruning, age-based or pattern-based sweeps, or recovery after SIGKILL.
- Cleaning up callers' scratch directories.
- Supporting native Linux hosts whose uid does not match `pwuser`. They are already unsupported
  (the container cannot write the artifact mount), and this change does not alter that.

## Approach

### Wrapper lifecycle (`scripts/docker-dev-audit-smoke.sh`)

```
resolve ownership ─► start sandbox-run.sh in background ─► wait
                                                            │
             ┌──────────────────────────────────────────────┤
     child exits (status S)                        INT/TERM received
             │                                              │
             │                              terminate child tree, remove own
             │                              container, wait child (bounded)
             ▼                                              ▼
       cleanup(S) ◄─────────────────────────────── cleanup(130/143)
             │
   untag owned image (non-forced) ; remove owned artifacts only if S == 0 ;
   print retained evidence path if S != 0 ; exit S
```

**1. Resolve ownership up front.**

- The wrapper computes a run id once: `run_id="$(date -u +%Y%m%d%H%M%S)-$$-$RANDOM"`. It contains
  only lowercase-safe characters, so it is valid both in a Docker tag and in a container name.
- `owns_image`: if `${IMAGE:-}` is empty, set `IMAGE="agent-runner-dev-audit-smoke:$run_id"` and
  `owns_image=1`. Otherwise use the caller's tag and set `owns_image=0`.
- `owns_artifacts`: if `${ARTIFACT_DIR:-}` is empty, create the directory with the existing `mktemp`
  template and set `owns_artifacts=1`. Otherwise set `owns_artifacts=0`.
- `container="agent-runner-dev-audit-smoke-$run_id"`. The container is always named this way, even
  when the caller supplies the image, so interrupt handling can target it exactly.

**2. Launch.**

- The wrapper passes `--image "$IMAGE"` to `sandbox-run.sh` explicitly, along with
  `--docker-run-arg "--name=$container"` and the existing arguments.
- It installs its traps before it starts the child:
  - `trap cleanup EXIT`;
  - `trap 'on_signal 130' INT`;
  - `trap 'on_signal 143' TERM`.
- It runs `sandbox-run.sh` in the background and records `child=$!`, so trapped signals interrupt
  the wait instead of being deferred.
- It captures the status in a way that is safe under errexit: `status=0; wait "$child" ||
  status=$?`. It then sets `final_status=$status` and runs `exit "$status"`. The EXIT trap performs
  cleanup.

**Status contract.** Cleanup exists only in the EXIT trap, which runs once and is guarded by a flag.
It therefore also acts as a backstop: any unexpected errexit after ownership is resolved still
untags the owned image. It works as follows:

- On entry, `cleanup` reads `final_status="${final_status:-$?}"`. An unexpected errexit therefore
  counts as a failure, and the artifacts are kept.
- `cleanup` runs `set +e`, so a failing cleanup command cannot end it early.
- `cleanup` finishes with `exit "$final_status"`.

**3. Interrupt handling.** `on_signal <status>` does the following:

1. Set `final_status` to the given status: 130 for INT, 143 for TERM.
2. Run `pkill -TERM -P "$child"` so the `docker build` and `docker run` client processes stop. A
   cancelled build client cancels the BuildKit session, so the build cannot tag the image after
   cleanup has run.
3. Send TERM to `$child`.
4. Run `docker rm -f "$container"`. The wrapper ignores a "no such container" result. This name is
   unique to the run, so the command cannot affect another run.
5. Wait for the child, for at most about 30 seconds. Bash 3.2 has no timed `wait`, so this is a
   loop that checks `kill -0 "$child"` every 0.2 seconds until a deadline, followed by a final
   `wait "$child" || true` to reap the child.
6. Repeat `docker rm -f "$container"` once, in case the container was created while the wrapper was
   shutting down.
7. Run `exit "$final_status"`, which triggers cleanup through the EXIT trap.

**4. Cleanup** runs once, from the EXIT trap only.

- Image:
  - When `owns_image=1` and `docker image inspect "$IMAGE"` succeeds, the wrapper runs
    `docker image rm "$IMAGE"`, with no `--force`.
  - If removal fails, it prints `smoke: could not remove image $IMAGE: <reason>` on stderr.
  - When the image is absent, for example because the build failed, it does nothing silently.
- Artifacts, success (status 0) with `owns_artifacts=1`: the wrapper runs
  `chmod -R u+w -- "$ARTIFACT_DIR"`, then `rm -rf -- "$ARTIFACT_DIR"`.
  - `chmod -R` without `-L` does not follow symlinks.
  - If anything remains afterwards, it prints `smoke: could not remove artifact directory $ARTIFACT_DIR`
    on stderr.
  - Otherwise it prints `smoke: removed artifact directory $ARTIFACT_DIR`.
- Artifacts, failure (status not 0), whether the directory is owned or supplied by the caller: the
  wrapper prints `smoke: evidence retained in $ARTIFACT_DIR` on stderr.
- The wrapper exits with the journey status, or with 130/143 after an interrupt. Cleanup outcomes
  never change that status.

The wrapper never runs `docker image prune`, `docker builder prune`, `docker system prune`, an image
removal by pattern, or a command that names `agent-runner-dev:local`. The only Docker names it passes
to `docker image rm` or `docker rm` are `$IMAGE` (when owned) and `$container`.

### Removing container-written artifacts (resolves the deferred Linux-ownership question)

The host removes artifacts, and only after `sandbox-run.sh` has returned. By then the
`docker run --rm` container has exited, and so has every process inside it, including the detached
audit process. Nothing can still be writing into the tree, so there is no race between removal and
late audit writes.

In every supported configuration, the host user owns the files:

- on macOS, because Docker Desktop maps bind-mount ownership;
- on native Linux, because the host uid must match `pwuser`.

`chmod -R u+w` handles restrictive modes that were created under confinement. The container script
`scripts/docker-dev-audit-smoke-container.sh` is unchanged. Its final line, which says that artifacts
were retained under the container path, is followed by the wrapper's own removal line on the host.
The wrapper removes only a directory it owns. A caller-supplied `ARTIFACT_DIR` is never touched.

## Decisions

- **Supervise the child in the background with `wait`, instead of running it in the foreground.**
  This is the only way a SIGTERM delivered to the wrapper alone runs the trap promptly. Otherwise
  bash defers the trap until `docker run` returns.
- **Name the container after the run id.** Interrupt cleanup can then force-remove exactly its own
  container, and an unforced image removal can succeed afterwards. Forcing is used only on the
  run-owned container, never on images.
- **Delete on the host, after the container has exited.** The host runs `chmod -R u+w` and then
  `rm -rf`. The container is gone, so this cannot race the detached audit process. In every
  supported configuration the host user owns the files. Alternatives considered:
  - Delete from inside the container, as an earlier version of this design proposed. Rejected: it
    races a still-running detached audit process that writes under `$HOME/.agent-runner`. Avoiding
    that race would require new process-exit tracking. It also rested on the false premise that a
    mismatched Linux uid can produce a successful run.
  - A host-side `docker run --rm -v … rm -rf` helper. Rejected: it needs an image after the wrapper
    has decided to untag it, and it adds a second container lifecycle.
- **Put cleanup only in the EXIT trap.** Signal handlers and the normal path both end with `exit`.
  One guarded cleanup then covers success, failure, interrupt, and unexpected errexit.
- **Stay compatible with bash 3.2.** The bounded wait is a `kill -0` polling loop, and the wrapper
  uses no `wait -n`, `wait -p`, associative arrays, or other bash 4 features. CI runs on Linux with
  a newer bash, so the manual AT-002 run on the factory Mac is the evidence for bash 3.2.
- **Check `docker image inspect` before removing the image.** This lets the wrapper tell "never built"
  (stay silent, as the spec requires) apart from "removal failed" (report it).
- **Keep `sandbox-run.sh` unchanged.** Its existing `--image`, `--env`, and `--docker-run-arg`
  options are enough.
- **Test with a fake `docker` on `PATH`.** This follows the repository's convention of local stubs
  and needs no daemon in CI.

## Risks / Trade-offs

- **Cold rebuild after the host prunes the build cache.** This cost is accepted in the proposal.
  Callers can set `IMAGE` to keep a warm tag.
- **SIGKILL or a crash skips cleanup.** It can leave one `agent-runner-dev-audit-smoke:<run-id>`
  image and a stopped container name behind. The docs describe how to recover them by hand. There is
  no automatic sweep, because concurrent factory runs make one unsafe.
- **Signal arrives between build and run.** `pkill -P` plus the second container removal cover a
  container created during shutdown. Parent-child relationships among processes may differ if Docker
  Desktop wraps the CLI. The fallback is that the image rm fails, gets reported, and leaves one tag.
  This is visible and can be recovered by hand.
- **Identical image IDs.** A fully cached build can produce an image with the same ID as another tag.
  Unforced `docker image rm <tag>` then only untags, which is the intended outcome.
- **`$RANDOM` and PID collisions.** A collision would require two runs in the same second with the
  same PID and random value. The risk is negligible.

## Verification

- **Go test in `scripts/` with a fake `docker` placed first on `PATH`.**
  - The fake logs each argv to a file and simulates:
    - `build`, with a configurable exit code;
    - `run`, which writes files into the mounted `/artifacts` host path and exits with a
      configurable code, or sleeps so the interrupt case can be tested;
    - `image inspect`, `image rm` with a configurable failure, and `rm -f`.
  - Scenarios covered:
    - success removes the owned tag without `--force`, and the owned directory is gone;
    - failure keeps the directory and prints its path on stderr;
    - caller-supplied `IMAGE` and `ARTIFACT_DIR` are kept;
    - an image rm failure is reported and the smoke still exits 0;
    - a build failure produces no image-removal warning;
    - SIGTERM to the wrapper removes the named container, untags the image, keeps the directory,
      and exits 143;
    - mutating Docker commands (`image rm`, `rm`, `rmi`, and any `prune`) target only the tag and
      container name owned by the run;
    - `image rm` never carries `-f` or `--force`;
    - `agent-runner-dev:local` never appears unless the caller supplied it;
    - a mode-0500 subtree that the fake container wrote is still removed after success;
    - a failed journey keeps its nonzero status under errexit and still untags the image, through
      the EXIT trap.
  - The existing Python harness for the container script is unchanged, because the container script
    is unchanged.
- **Manual acceptance on Docker Desktop.** Run the real smoke with `docker system df -v` before and
  after, as the proposal requires:
  - no new tag and no dangling image remain;
  - the temporary directory is gone after success;
  - `agent-runner-dev:local` is unchanged.

## Migration Plan

- Update the smoke section of `docs/dev/sandbox.md` with:
  - the ownership rules: run-unique tag, `IMAGE`, `ARTIFACT_DIR`, removal on success, retention on
    failure;
  - that the build cache is left for the host to reclaim;
  - recovery by hand: `docker image ls agent-runner-dev-audit-smoke` and `docker image rm`.
- One-time operational follow-up outside this change: remove the `agent-runner-dev:local` image that
  already leaked on the factory Mac, after checking that nothing else uses it.
- The docs also state the existing prerequisite that on native Linux the host uid must match the
  container's `pwuser`.
- Rollback: revert the wrapper and docs changes. Nothing is persisted.
