## Why

`scripts/docker-dev-audit-smoke.sh` is the product-owned detached-audit smoke. It runs through
`scripts/sandbox-run.sh`, which builds the shared `agent-runner-dev:local` tag from
`docker/dev/Dockerfile`, a tool-heavy Playwright/Go/Node image of about 7.4 GB. Neither script
removes anything it creates. A successful run leaves:

- the built image under the shared `agent-runner-dev:local` tag. When the tag already pointed at an
  older build, that older build becomes a dangling image;
- a `mktemp -d` artifact directory under `$TMPDIR` or `/tmp`, holding the smoke project, audit state,
  and model outputs.

Agents now run this smoke unattended. For example, the Agent Factory `test-flows` step ran it while
fixing #116. On the factory Mac, admission stops below 5 GiB free. The image was left behind on
2026-09-23 and again on 2026-09-25. The host's nightly cleanup deliberately keeps every `:local` tag,
so nothing ever reclaims it. The maintainer asked for cleanup to happen at the producer, based on
which resources that run owns. They ruled out age-based generic image deletion and any cleanup that
counts shared Docker layers twice.

Retention is the key problem, not the image's size alone. The same comment notes that the host
already prunes the BuildKit build cache every night. Those pruned bytes are not actually freed while
a tagged image still references the same layers. Because nothing ever removes the smoke's
`agent-runner-dev:local` image, its layers stay pinned indefinitely.

Both the producer and its leftovers are known, so the fix is small and cheap to maintain. The main
risk is the opposite mistake: deleting an image or directory that someone else owns. The
`agent-runner-dev:local` tag is shared with manual `sandbox-run.sh` use and with other tooling that
builds the same Dockerfile.

Verdict: **go**.

## What Changes

- By default, the smoke builds its sandbox image under a tag it owns for that run only:
  `agent-runner-dev-audit-smoke:<run-id>`, using a dedicated repository name. It never builds,
  retags, or removes the shared `agent-runner-dev:local` tag.
- When the smoke exits, it untags the image it owns. This happens after success, failure, or
  interrupt. Removal is not forced, so Docker releases only layers that no other image references.
- A caller who supplies a tag through the existing `IMAGE` variable (which `sandbox-run.sh` already
  honors) owns that image. The smoke builds and uses that tag but never removes it. This mirrors how
  `ARTIFACT_DIR` works and gives developers a warm tag they can reuse.
- An artifact directory that the smoke created itself is removed after a successful run. After a
  failed run it is kept, and its path is printed, so the evidence remains available as the current
  spec requires.
- A directory the caller passes in through `ARTIFACT_DIR` belongs to the caller and is never deleted.
- `docs/dev/sandbox.md` describes the cleanup and retention rules and the naming contract for tags.

### What the fix reclaims, and what it does not

Untagging does not by itself guarantee an immediate large drop in disk use. Under BuildKit, and
especially with the containerd image store in current Docker Desktop, the build cache can reference
the same layer data as the image. A fully cached rebuild can also produce the same image ID as an
existing tag, in which case removal deletes only a tag. The fix does guarantee two things:

- the smoke leaves behind no tags and no dangling images;
- after the smoke's image is untagged, nothing it produced pins those layers. The host's nightly
  build-cache prune can then actually free them.

Before the change, the retained `agent-runner-dev:local` image kept those bytes alive through every
prune.

Acceptance condition: on a Docker Desktop setup like the factory's, record `docker system df -v`
before and after a smoke run. After the run, the smoke's tag is gone and no new dangling images
exist. Any bytes that remain are listed only under the build cache, which the host reclaims. That
measurement verifies these claims; it does not set a promised number of bytes.

This is not a breaking change. The smoke's command-line use, its `ARTIFACT_DIR` and
`AUDIT_SMOKE_TIMEOUT_SECONDS` inputs, and its exit status stay the same. The only visible difference
is that the smoke no longer refreshes `agent-runner-dev:local` as a side effect.

## Capabilities

### New Capabilities
- None.

### Modified Capabilities
- `development-audit-sandbox`: adds a requirement that the product-owned Docker smoke release the
  image and artifact resources it created, and only those. It also clarifies that evidence is kept on
  failure and that caller-supplied directories are kept in every case.

## Technical Approach

The work stays inside the smoke wrapper. `sandbox-run.sh` already accepts `--image`, so the wrapper
can pass a run-unique tag without any change to the general sandbox. The wrapper also records
whether it created the artifact directory. An exit trap in the wrapper does the cleanup:

- it runs `docker image rm` on the run's tag, never `docker image prune`, `--force`, or
  `docker builder prune`;
- it deletes the artifact directory only when the wrapper created it and the smoke succeeded.

If cleanup itself fails, the trap reports it but does not change the smoke's result.

Recommended alternative, and the rejected options:

- **Chosen: a run-unique tag plus untag-on-exit, unless the caller supplies a tag.** This is based
  on ownership, avoids touching images the smoke does not own, and gets reference-counted layer
  accounting from Docker for free. The cost is a rebuild on each run. The factory host prunes the
  build cache every night (see the issue comment), so the first smoke run after a prune rebuilds
  from scratch. That rebuild downloads the Playwright base image and the toolchain, which is several
  GB and minutes of network time, and it raises peak disk use during the run. This cost is
  accepted: the smoke runs rarely, it has to be correct, and keeping a warm image is exactly the
  retention the maintainer asked to remove. A caller who wants reuse can supply and own an `IMAGE`
  tag.
- **Rejected: removing `agent-runner-dev:local` after the run.** This could delete an image that a
  developer or other tooling depends on, and it breaks their warm workflow.
- **Rejected: removing the image only when the tag did not exist before the run.** A rebuild still
  leaves the previous build dangling. It also races with concurrent builds of the shared tag.
- **Rejected: an opt-in cleanup flag.** Unattended callers would not pass it, so the factory
  problem would remain.
- **Rejected: a switch that keeps the run's image.** Keeping an image with a run-unique tag does not
  speed up the next run, which builds under a new tag anyway. It would only let such images pile up.
  A tag supplied by the caller covers the real reuse case.

Risks for design:

- **Artifact ownership on Linux hosts.** The container writes `/artifacts` as `pwuser`. On a native
  Linux Docker host, a plain host-side `rm` may be unable to delete those files. Design must choose
  between removing them from inside the container and handling ownership explicitly.
- **Cleanup on interrupt.** On SIGINT or SIGTERM, the `docker run --rm` container may still be
  running when the trap fires. An image rm that is not forced would then fail because the image is
  in use, and deleting artifacts from the host could race the container's writes. The smoke should
  name its container with the same run id (through the existing `--docker-run-arg`). The trap should
  stop and wait for that container, and only that one, before it untags the image or removes
  artifacts.
- **Stale images after a hard kill.** A SIGKILL or crash skips the trap and can leave one
  `agent-runner-dev-audit-smoke:<run-id>` image behind. These are recovered by hand, which the
  dedicated repository name makes easy. There is no automatic sweep on the next run: the factory runs
  agents in parallel, and a sweep could delete a live run's image. If a sweep is ever added, it must
  skip any image used by a running or created container.

## Out of Scope

- Changing the default image, retention, or artifact behavior of `scripts/sandbox-run.sh`, the
  devcontainer, or `scripts/docker-first-run-smoke.sh`.
- Pruning the Docker build cache, deleting images by age, or any other generic host cleanup.
- The `/private/tmp/agent-runner-test-flows-*` directories seen on the factory Mac. That name does not
  match anything this repository creates (the smoke uses `agent-runner-dev-audit-smoke.*`). They come
  from the calling agent's own scratch space, so they belong to the `codagent:test-flows` skill or the
  factory, not to this repository.
- Changing Agent Factory admission or the host's `low_disk_cleanup.sh`.

## Impact

- **Rollout:** the `agent-runner-dev:local` image already on the factory Mac, and any dangling
  predecessors, are not reclaimed by this change. Remove them once by hand outside this change,
  after confirming that no other factory tooling uses that tag.
- **Code:** `scripts/docker-dev-audit-smoke.sh`. `scripts/sandbox-run.sh` is touched only if design
  finds a missing hook. Dry-run and harness tests go in `scripts/sandbox_scripts_test.go`.
- **Docs:** the smoke section of `docs/dev/sandbox.md`.
- **Spec:** `openspec/specs/development-audit-sandbox/spec.md`.
- **Users:** unattended callers such as the factory no longer accumulate smoke images or
  successful-run artifacts. Developers who relied on the smoke refreshing `agent-runner-dev:local`
  must build that tag through `sandbox-run.sh` directly. Developers who want a warm, reusable image
  can supply their own `IMAGE` tag.
