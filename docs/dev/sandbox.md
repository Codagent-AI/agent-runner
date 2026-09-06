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

## Development-audit builds

The normal sandbox build is deliberately untagged: it has no private audit
command, hidden audit workflow, or automatic audit hook. Select the private
development-audit build explicitly when exercising the audit lifecycle:

```bash
scripts/sandbox-run.sh --dev-audit -- agent-runner audit help
scripts/sandbox-run.sh --dry-run --dev-audit -- agent-runner --version
```

`--dev-audit` builds with the `dev_audit` tag and injects
`/agent-runner-source` as the authoritative Agent Runner source root. The
source mount is distinct from the disposable copy used for compilation and
from `/eval-input`. At audit launch, Runner snapshots and verifies that mounted
tree before source-verified correctness publication. A worktree can have a
complete source tree while its host Git indirection is unavailable in the
container; that condition is recorded as unavailable launch-time Git metadata,
not as a clean checkout and not as a substitute for build diagnostics.

Automatic audit eligibility is intentionally narrow: the resolved canonical
workflow reference must be in the `openspec/` or `spec-driven/` namespace (with
an optional `builtin:` prefix), and only a finalized top-level execution with
an execution-session identity qualifies. A display name, project directory, or
an absolute path that merely contains those words does not qualify. The audit
resolves the source run's recorded profile set at launch using normal profile
layering, inheritance, and built-in defaults, then freezes the resolved
`crosscheck` CLI, model, and reasoning effort for both model stages.

On Linux the development image uses Bubblewrap to make the ordinary filesystem
read-only and grants write access only to the audit-owned model-output tree.
It requires unprivileged user namespaces and Linux 5.11 or newer for closing
inherited descriptors on model exec. The opt-in Docker invocation uses the
repository's `docker/dev/dev-audit-seccomp.json`, derived from a pinned Moby
default-deny profile with user/mount namespace and mount-operation exceptions.
It retains seccomp filtering and adds no container capabilities or privileged mode. If
that OS boundary cannot be established, the linked audit records a diagnostic
and does not start the model command. This is write confinement only: existing
read access and network behavior are unchanged. On macOS the existing
parameterized `sandbox-exec` boundary remains in use; Sequoia acceptance must
be run and recorded separately.

The product-owned detached-audit smoke is available after the Docker image and
Go dependencies have been provisioned:

```bash
scripts/docker-dev-audit-smoke.sh
```

The smoke adds the separate `devaudit_smoke` build tag through
`--dev-audit --dev-audit-smoke`; ordinary `dev_audit` builds contain no smoke
workflow. The fixture tag only registers a hidden workflow and retains the
production sandbox and lifecycle code.

It uses a temporary project and fake local model responses with no host
credentials. It keeps artifacts in the selected artifact directory and waits
for both the linked audit's terminal lifecycle and terminal run state after the
source CLI has returned. It checks reciprocal source/session linkage, model
outputs, validated observations, local report, and verified mounted provenance.
That distinction matters: source completion never waits for auditing in normal
Runner operation. The smoke does not call external model, GitHub, or reporting
services; missing reporting configuration is expected to remain a local audit
warning. Set `ARTIFACT_DIR` to retain evidence in a chosen host directory and
`AUDIT_SMOKE_TIMEOUT_SECONDS` to change the default 45-second audit wait. Timeout,
invalid output, or missing terminal state fails the smoke and preserves evidence.

Run the production Linux confinement regressions in the same Docker environment:

```bash
scripts/sandbox-run.sh --dev-audit --no-default-secrets \
  --docker-run-arg --network=none -- \
  'cd /tmp/agent-runner-local && AGENT_RUNNER_REQUIRE_LINUX_SANDBOX=1 go test -tags dev_audit ./internal/devaudit -run "^TestLinuxAudit" -count=1 -v'
```

The required-sandbox setting makes unavailable Linux confinement a test failure.
The tests exercise real subprocess writes, descendants, inherited descriptors,
escape attempts, disposable runtime state with synthetic authentication, and
setup failure. The smoke supervisor's timeout and invalid-result regressions
also run through `go test ./scripts`.

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
