# Decisions

## proposal: verdict
- **Decision:** Go. The problem recurs, the producer and its leftovers are known, and a fix inside the
  smoke wrapper is small.
- **Alternatives considered:** No-go, leaving the problem to host-level cleanup. The maintainer's
  comment on the issue rules this out.
- **Decision-bearing:** yes

## proposal: image ownership model
- **Decision:** The smoke builds under a run-unique tag and runs `docker image rm` on that tag at
  exit. It never touches `agent-runner-dev:local`. It does not prune, force-remove, or clear the
  build cache.
- **Alternatives considered:**
  - Remove `agent-runner-dev:local` after the run. Rejected: the tag is shared with manual sandbox
    use and other tooling.
  - Remove the image only when the smoke created the tag. Rejected: rebuilds leave dangling images,
    and concurrent builds of the tag can race.
  - An opt-in cleanup flag. Rejected: unattended callers would not pass it.
- **Decision-bearing:** yes. The smoke will no longer refresh the shared dev image as a side effect.

## proposal: artifact retention
- **Decision:** A temporary directory that the smoke created is removed after success. After failure
  it is kept, and its path is printed. A directory the caller passes through `ARTIFACT_DIR` is never
  deleted.
- **Alternatives considered:**
  - Always delete. Rejected: this breaks the spec requirement that failures preserve evidence.
  - Delete caller directories too. Rejected: it is unsafe to delete a directory the user chose.
- **Decision-bearing:** yes

## proposal: developer opt-out (superseded by PR-2)
- **Decision:** A documented opt-out keeps the run's image for local iteration. Cleanup stays the
  default.
- **Alternatives considered:** No opt-out. Rejected: without one, every local iteration pays for a
  rebuild.
- **Decision-bearing:** no. This is a low-risk default. The decision was replaced during proposal
  review; see PR-2 below.

## proposal: test-flows temp directories
- **Decision:** The `/private/tmp/agent-runner-test-flows-*` directories are out of scope. Their name
  does not match anything this repository creates. They come from the calling agent's scratch space,
  which the test-flows skill or the factory owns.
- **Alternatives considered:**
  - Stop for direction, on the grounds that the issue can be read two ways. Rejected: the issue's
    stated source, the script's own `mktemp` directory, is covered by this change.
  - Edit the factory-fix test-flows prompt. Rejected: that fix is speculative, and the skill itself
    is outside this repository.
- **Decision-bearing:** yes. It is recorded so that reviewers can confirm it.

## proposal: scope of sandbox-run.sh
- **Decision:** The default behavior of `sandbox-run.sh` does not change. The smoke uses the existing
  `--image` option.
- **Alternatives considered:** Add cleanup to every `sandbox-run.sh` invocation. Rejected: callers of
  the general sandbox intentionally reuse a warm image.
- **Decision-bearing:** no

## proposal-review: PR-1 disk reclamation claim (applied)
- **Decision:** The proposal no longer claims that untagging frees the full image size. It now says
  that the fix removes tags and dangling images and stops pinning layers, so the host's nightly
  build-cache prune can free them. It also adds a `docker system df -v` before/after acceptance
  condition that verifies which Docker store still holds the bytes.
- **Alternatives considered:** Keep the original claim. Rejected: BuildKit and containerd share layer
  data with the cache, so the claim overstates what untagging frees.
- **Decision-bearing:** yes

## proposal-review: PR-2 developer opt-out replaced by caller-owned IMAGE (applied)
- **Decision:** The "keep the run image" opt-out is dropped. A caller who supplies `IMAGE` owns the
  tag: the smoke uses it and never removes it. This mirrors `ARTIFACT_DIR`. Without `IMAGE`, the
  smoke uses a run-unique tag and removes it.
- **Alternatives considered:**
  - Keep the opt-out. Rejected: a kept run-unique image does not warm the next run, which builds
    under a new tag, and such images accumulate.
  - Drop the opt-out with no replacement. Rejected: developers would have no warm reusable tag.
- **Decision-bearing:** yes

## proposal-review: PR-3 rebuild cost under cache pruning (applied)
- **Decision:** The issue comment confirms that the host prunes the build cache every night. The
  proposal now states that the first smoke run after a prune is a full multi-GB rebuild, and accepts
  that cost. A caller who wants reuse can supply and own an `IMAGE` tag.
- **Alternatives considered:** Reuse a shared image by default for factory callers. Rejected: that is
  exactly the unowned retention the maintainer asked to remove.
- **Decision-bearing:** yes

## proposal-review: PR-4 interrupt cleanup ordering (applied)
- **Decision:** A design note now requires the smoke to name its container with the run id. On
  interrupt, the trap stops and waits for that container before it untags the image (without
  forcing) or removes artifacts.
- **Alternatives considered:** `docker image rm --force`. Rejected: it could remove an image other
  containers are using.
- **Decision-bearing:** no

## proposal-review: PR-5 existing leaked image (applied)
- **Decision:** A rollout note now says to remove the existing factory `agent-runner-dev:local`
  image once by hand, outside this change, after confirming that no other tooling uses it.
- **Alternatives considered:** Have the smoke delete the shared tag. Rejected: that breaks the
  ownership rule.
- **Direction-level?** No. The change does not depend on this operational follow-up.
- **Decision-bearing:** no

## proposal-review: PR-6 naming contract and no automatic sweep (applied)
- **Decision:** The tag contract is fixed as `agent-runner-dev-audit-smoke:<run-id>`. Images left by
  a hard kill are recovered by hand. There is no automatic sweep on the next run, because parallel
  factory runs could have a live image deleted. If a sweep is ever added, it must skip images used
  by running or created containers.
- **Alternatives considered:** A sweep of stale tags on the next run. Rejected because of the
  concurrency race.
- **Decision-bearing:** yes

## spec: capability layout
- **Decision:** The existing `development-audit-sandbox` spec gains two requirements, one for Docker
  image ownership and one for artifact-directory ownership. The documentation requirement is modified
  to cover the resource contract. The smoke-journey requirement is unchanged, because it already
  requires that failure evidence be preserved.
- **Alternatives considered:**
  - A new standalone capability. Rejected: the behavior belongs to the existing smoke contract.
  - Modify the smoke-journey requirement instead. Rejected: it would repeat the failure-preservation
    wording that is already there.
- **Decision-bearing:** no

## spec: interrupted runs keep owned artifacts
- **Decision:** An interrupted run is treated as unsuccessful. Its owned artifact directory is kept,
  and its path is printed. Its image is still removed, because the image is not evidence.
- **Alternatives considered:** Remove the artifacts on interrupt. Rejected: the operator may interrupt
  a run precisely in order to inspect a hang.
- **Decision-bearing:** no

## spec: cleanup failures do not change exit status
- **Decision:** A failure to remove the image or the artifacts is reported on stderr with the leftover
  tag or path. The smoke's exit status still reflects only the journey.
- **Alternatives considered:** Fail the smoke when cleanup fails. Rejected: a cleanup failure would
  hide the journey's actual result.
- **Decision-bearing:** no

## spec: Linux artifact ownership deferred to design
- **Decision:** The spec requires owned artifacts to be removed completely after success, including
  files the container wrote. Whether they are removed from inside the container or from the host is
  deferred to design.
- **Alternatives considered:** None at the spec level. This is an implementation choice.
- **Decision-bearing:** no

## design: artifact removal on Linux
- **Decision:** The wrapper exports `AUDIT_SMOKE_REMOVE_ON_SUCCESS=1` only for directories it owns.
  After a passing journey, the container script removes its own `smoke_root` as `pwuser`. The host
  then removes the directory it created with `mktemp`, which is empty by that point. The deferred
  marker in the spec is resolved, and no normative text changes.
- **Alternatives considered:**
  - Host-only `rm`. Rejected: it fails for container-owned files on native Linux.
  - Chmod or chown from inside the container. Rejected: it adds a step and leaves ownership edge
    cases.
  - A host-side helper container that runs `rm`. Rejected: it needs the image that is about to be
    untagged.
- **Decision-bearing:** yes

## design: signal-safe supervision
- **Decision:** The wrapper runs `sandbox-run.sh` in the background and uses `wait`. On INT or TERM
  it does the following:
  - terminates the child's Docker clients, then the child itself;
  - runs `docker rm -f` on the container named for the run, and only that container;
  - waits for the child for a bounded time, then removes that container once more;
  - cleans up and exits with 130 or 143.
- **Alternatives considered:**
  - Run the child in the foreground with an EXIT trap. Rejected: bash defers the trap until
    `docker run` returns.
  - Force-remove the image. Rejected: the spec requires removal without forcing.
- **Decision-bearing:** no

## design: distinguish "never built" from "removal failed"
- **Decision:** The wrapper runs `docker image inspect` before it removes the owned tag. If the image
  is absent, it stays silent. If removal fails, it reports the failure on stderr.
- **Alternatives considered:** Always attempt the removal and filter the error text. Rejected: the
  error text depends on the Docker version.
- **Decision-bearing:** no

## design: test strategy
- **Decision:** A Go test uses a fake `docker` on `PATH`, and a Python regression covers the
  container-side removal. A manual `docker system df -v` check on Docker Desktop serves as acceptance.
- **Alternatives considered:** CI tests against a real Docker daemon. Rejected: CI has no daemon, and
  the image is about 7 GB.
- **Decision-bearing:** no

## test-plan: no automated E2E
- **Decision:** Automated coverage is integration tests only. They run the real wrapper and the real
  `sandbox-run.sh` with a fake `docker` on `PATH`, plus the Python harness for the container script.
  The real Docker journey is covered by required agent acceptance (AT-001 to AT-003).
- **Alternatives considered:** A CI end-to-end test against real Docker. Rejected: CI has no daemon,
  the image is multi-GB, and the build needs the network.
- **Decision-bearing:** no

## test-plan: Linux ownership acceptance is conditional
- **Decision:** AT-004, which runs on native Linux Docker, applies only when such a host is
  available. INT-006 is the automated evidence.
- **Alternatives considered:** Make AT-004 required. Rejected: the factory host is macOS with Docker
  Desktop, which cannot exercise the ownership boundary.
- **Decision-bearing:** no

## approach-review: AR-1 status capture and EXIT-trap backstop (applied)
- **Decision:** The design now specifies the following:
  - the status is captured with `status=0; wait "$child" || status=$?`;
  - cleanup lives only in a run-once EXIT trap that starts with `set +e`, reads
    `final_status="${final_status:-$?}"`, and ends with `exit "$final_status"`;
  - both signal handlers and the normal path end with `exit`.
  INT-002 now asserts that the nonzero status survives errexit and that the image is untagged
  through the EXIT trap.
- **Alternatives considered:** Drop `set -e` around the supervision block. Rejected: that gives no
  backstop for other errexit paths.
- **Decision-bearing:** no

## approach-review: AR-2 Linux ownership premise (applied, with a different mechanism)
- **Decision:** The Context now states the real ownership facts:
  - on macOS, Docker Desktop maps bind-mount ownership;
  - native Linux is supported only when the host uid matches `pwuser` (1000);
  - with a mismatched uid, the container cannot write the mode-0700 mount, so the smoke already
    fails, and it stays unsupported.
  AT-004 now requires a host with a matching uid. In-container deletion is dropped, and the host
  runs `chmod -R u+w` and `rm -rf` after the container has exited. This covers restrictive modes.
  The design entry "artifact removal on Linux" is superseded.
- **Alternatives considered:**
  - Keep in-container deletion as defense-in-depth, which the reviewer preferred. Rejected: it
    creates the AR-3 race, and host removal as the owning user achieves the same result.
  - Have the wrapper `chmod` the mount so that mismatched uids are supported. Rejected: that is new
    scope and not needed for the issue.
- **Decision-bearing:** yes

## approach-review: AR-3 race with the detached audit process (applied by eliminating the race)
- **Decision:** Removal now happens on the host only after `sandbox-run.sh` has returned. By then
  the `--rm` container has exited, and every process in it, including the detached audit, is gone.
  No writer can remain. INT-006 and the container-script change are removed.
- **Alternatives considered:**
  - Wait for the audit process to exit inside the container. Rejected: it needs new process-exit
    tracking.
  - Retry `rm`. Rejected: it masks the race instead of removing it.
- **Decision-bearing:** no

## approach-review: AR-4 bash 3.2 compatibility (applied)
- **Decision:** The design requires the wrapper to be compatible with bash 3.2. The bounded wait is
  a `kill -0` loop that polls every 0.2 seconds until a deadline, then reaps the child with
  `wait || true`. AT-002 on the factory Mac is the evidence for bash 3.2, and the test plan notes
  this.
- **Alternatives considered:** Use `wait -n` or `wait -p` from bash 4 or later. Rejected: they are
  unavailable on the target host.
- **Decision-bearing:** no

## approach-review: AR-5 shared test assertion (applied)
- **Decision:** The shared assertion is rephrased:
  - mutating Docker commands target only the tag and container owned by the run;
  - `image rm` never carries `-f` or `--force`, while `docker rm -f` on the run's own container is
    allowed;
  - no `prune` is ever issued;
  - `agent-runner-dev:local` appears only when the caller supplied it;
  - `build` and `run` may use a caller-supplied tag.
  INT-005(c) now simulates removal failure with an `rm` stub, because `chmod u+w` makes a mode-0500
  subtree removable.
- **Alternatives considered:** None.
- **Decision-bearing:** no

## write-tasks: single task
- **Decision:** There is one implementation task, `tasks/01-smoke-resource-cleanup.md`, indexed from
  `tasks.md`. It covers the wrapper changes, the Go integration tests INT-001 to INT-005, and the
  documentation.
- **Alternatives considered:** Split the tests or the docs into separate tasks. Rejected: the
  workflow requires exactly one task, and the change is small.
- **Decision-bearing:** no
