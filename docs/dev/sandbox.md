---
title: Development Sandbox
group: Development
order: 20
description: Run Agent Runner and external evaluation payloads in the development container.
---

# Development Sandbox

Agent Runner includes a Docker-based development sandbox for running commands
against the current checkout. The sandbox builds Agent Runner from local source,
provides explicit credential pass-through, and can mount one external input
directory at a stable container path.

## Run a one-shot command

Run `scripts/sandbox-run.sh` from any directory. The script builds
`docker/dev/Dockerfile`, mounts the Agent Runner checkout read-only, copies it
inside the container, and builds a local `agent-runner` binary before executing
the requested command:

```bash
scripts/sandbox-run.sh -- agent-runner --version
```

Arguments after `--` retain their original boundaries. A single argument is
treated as a shell command for convenience; multiple arguments are executed as
an argument vector.

Use `--dry-run` to print shell-escaped Docker commands without building or
starting a container:

```bash
scripts/sandbox-run.sh --dry-run -- node -e 'console.log(process.argv[1])' 'hello world'
```

## Mount an external payload

Use `--input-dir PATH` when a separate repository supplies an evaluation suite
or another command payload:

```bash
scripts/sandbox-run.sh \
  --input-dir ../agent-evals/evals/agent-runner/example \
  -- bash /eval-input/run.sh
```

The path must name an existing host directory. Relative paths are resolved from
the caller's working directory, and the resolved directory is mounted read-only
at `/eval-input`. Only one input directory can be mounted per invocation.

Commands should write results to `/artifacts`, not `/eval-input`. By default the
host artifact directory is `artifacts/sandbox-runs/<timestamp>` under the Agent
Runner checkout. Set `--artifact-dir PATH` to choose another location. Relative
artifact paths are resolved from the Agent Runner repository root.

## Authentication and secrets

Credentials enter the sandbox only through explicit options:

- `--env NAME` passes one variable by name when it exists in the host
  environment. Its value is not included in dry-run output.
- `--env-file PATH` and `--secrets-file PATH` parse `NAME=value` entries without
  sourcing the file. `.sandbox-secrets.env` is loaded by default when present;
  use `--no-default-secrets` to disable it.
- `--mount-codex-auth` mounts `~/.codex/auth.json` read-only.
- `--mount-claude-auth` mounts Claude credentials and available settings files
  read-only.

Mounted authentication files are copied into the container's writable home by
`scripts/sandbox-sync-home.sh`. Host Codex configuration is intentionally not
copied. The sync script also creates container-local wrappers and Git credential
configuration for autonomous agent commands.

Processes in the container can read every credential passed to the run and can
write to the artifact mount. The Agent Runner source and `/eval-input` mounts are
read-only, but the container has network access and unrestricted access within
its own filesystem. Treat the container as the isolation boundary, run only
trusted payloads, and use narrowly scoped credentials.

## Local source and provenance

Each run builds Agent Runner from a copy of the current checkout inside the
container rather than using an installed release. The copy excludes Git data,
build outputs, artifacts, and worktrees. The script also passes
`AGENT_RUNNER_SOURCE_COMMIT` and `AGENT_RUNNER_SOURCE_DIRTY` into the container so
external tooling can record source provenance.

## Interactive devcontainer

The one-shot runner and `.devcontainer/eval/devcontainer.json` share
`docker/dev/Dockerfile` and `scripts/sandbox-sync-home.sh`. Use the devcontainer
when you need an interactive shell or editor session:

```bash
scripts/devcontainer-shell.sh
scripts/devcontainer-shell.sh --rebuild
```

The default devcontainer does not mount host credentials or configuration. For
a trusted checkout, `--with-host-config` creates a derived configuration under
`artifacts/devcontainer/` with the available allowlisted files mounted
read-only. Unlike the one-shot runner, the devcontainer mounts the checkout as a
writable workspace for development.

Run `scripts/sandbox-run.sh --help` and
`scripts/devcontainer-shell.sh --help` for all supported options.
