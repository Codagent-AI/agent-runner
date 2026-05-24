---
name: live-update-docker
description: Patch a running Docker smoke-test container with a freshly built local agent-runner binary. Use when the user asks to rebuild in the Docker container, live update a container, patch the Homebrew smoke container, or test local agent-runner changes inside an already-running Docker environment.
---

# Live Update Docker

Use this skill to replace the `agent-runner` binary inside an already-running Docker smoke-test container without rebuilding the image or reinstalling Homebrew packages.

## Workflow

1. Confirm the worktree and container.

   ```bash
   git status --short
   docker ps --format '{{.ID}} {{.Image}} {{.Names}}'
   ```

   In Codex sessions, Docker commands such as `docker ps`, `docker exec`, and `docker cp` may require sandbox escalation/approval.

   Prefer the single running `homebrew/brew` smoke container when present. If multiple plausible containers are running, ask which one to patch.

2. Determine the container CPU architecture.

   ```bash
   docker exec <container-id> uname -m
   ```

   Map `x86_64` to `GOARCH=amd64`; map `aarch64` or `arm64` to `GOARCH=arm64`.

3. Build a Linux binary on the host from the current checkout.

   ```bash
   GOOS=linux GOARCH=<arch> go build -o bin/agent-runner-linux-<arch> ./cmd/agent-runner
   chmod 0755 bin/agent-runner-linux-<arch>
   ```

   If the default build cannot write the host Go cache, use a sandbox-friendly cache path:

   ```bash
   GOCACHE=/private/tmp/agent-runner-go-build GOOS=linux GOARCH=<arch> go build -o bin/agent-runner-linux-<arch> ./cmd/agent-runner
   chmod 0755 bin/agent-runner-linux-<arch>
   ```

   Do not copy the host macOS binary into the container. It will fail with `Exec format error`.
   Make the built Linux binary world-executable before `docker cp`; Docker can
   preserve restrictive host modes such as `0700`, which makes `/tmp/agent-runner`
   unreadable to the `linuxbrew` user inside the container.

4. Copy and install the binary inside the running container.

   ```bash
   docker cp bin/agent-runner-linux-<arch> <container-id>:/tmp/agent-runner
   docker exec <container-id> bash -lc 'ls -l /tmp/agent-runner'
   ```

   Find the install target. Prefer an existing `agent-runner` on PATH; otherwise
   use Homebrew's Linux bin directory and ensure it exists.

   ```bash
   docker exec <container-id> bash -lc 'type -a agent-runner || true'
   docker exec <container-id> bash -lc 'target=$(command -v agent-runner || printf "%s" /home/linuxbrew/.linuxbrew/bin/agent-runner); install -d -m 0755 "$(dirname "$target")"; install -m 0755 /tmp/agent-runner "$target"; ls -l "$target"'
   ```

5. Verify the patched binary from inside the container.

   ```bash
   docker exec <container-id> bash -lc 'target=$(command -v agent-runner || printf "%s" /home/linuxbrew/.linuxbrew/bin/agent-runner); "$target" --help >/tmp/agent-runner-help.out && head -3 /tmp/agent-runner-help.out'
   ```

## Notes

- Replacing the binary does not change an already-running `agent-runner` process. The user must restart the command in the container to exercise the new build.
- Build on the host unless the user specifically asks to build inside the container. The smoke container may not have Go installed.
- Keep the container running. Do not stop, remove, or recreate it unless the user explicitly asks.
