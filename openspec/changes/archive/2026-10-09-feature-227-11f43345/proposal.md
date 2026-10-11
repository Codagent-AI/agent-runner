## Why

The built-in `openspec:*` workflows assume that a code repository's OpenSpec project sits at the repository-local `openspec/` directory. Workflow YAML passes `change_dir: "openspec/changes/{{change_name}}"`. The bundled scripts (`create-change.sh`, `archive-transition.sh`, `verify-archive-commit.sh`, `commit-change-plan.sh`, `validate-planning-artifacts.sh`) resolve `openspec/changes`, `openspec/changes/archive`, and `openspec/specs` against the working directory. The OpenSpec engine runs the `openspec` CLI in the process cwd. Plan and archive commits assume the change directory is tracked in the code repository. Nothing lets a user point these steps somewhere else.

That assumption fails for a real setup that already exists. The code lives on a long-running branch of a shared repository, and its OpenSpec project (`openspec/specs`, `openspec/changes`, the archive, an `AGENTS.md` with working rules, and explainer sources every change updates) lives in a subdirectory of a *different* repository. The shared repository must not mention that path. Today in this setup:

- the change has no canonical specs to write deltas against, so MODIFIED capabilities cannot be resolved;
- `openspec validate` and `openspec archive` run against the wrong project, or no project at all;
- the spec project's own `AGENTS.md` rules never reach the agent;
- a stray `openspec/` tree gets created and committed in a repository that must stay clean of it.

The user cannot run Agent Runner's main lifecycle workflow (`openspec:change`) on this code at all. Hand-editing a copy of the workflows means forking the scripts, the engine behaviour, and the commit and archive verification, and it loses every future built-in fix. #72 (multi-repo changes, where each owning repository keeps its own `openspec/specs`) is a different problem. This proposal covers one code repository whose single OpenSpec project lives elsewhere.

## What Changes

- **Configurable OpenSpec root.** A user-level setting maps a code repository to an external OpenSpec project directory, meaning the directory that contains `openspec/`, which may be a subdirectory of another checkout. An optional per-invocation `spec_root` workflow parameter overrides it. Both live outside the code repository, so private paths never enter shared files.
- **One resolution, recorded in run state.** A preflight step resolves the root once at the outermost `openspec:*` entry workflow the user invokes. The order is parameter, then setting, then the repository-local default. The step validates the root, records the result in run state, and passes it to every nested workflow as a normalized parameter. Nested workflows such as `archive-change` reuse the recorded root and never read settings again. Resumed and later steps (acceptance, archive, PR finalization) also use the recorded value. Editing the setting afterwards cannot point an existing run at a different root. If an external root is configured and the recorded context is missing or invalid, the run fails instead of falling back to the working directory.
- **Preflight validation, per lifecycle operation.** Resolving the root is separate from checking the change, and each kind of entry workflow checks the change differently. In every case the root must exist and be an OpenSpec project, or the run fails fast with a clear message.
  - Workflows that create a change (`change`, `simple-change`) also fail if a change with that name already exists, active or archived.
  - Workflows that continue a change (`plan-change`, `implement-change`) instead require the existing active artifacts they consume.
  - Archive works on the existing active change. On retry or resume, it still accepts a transition the same run has already completed, which keeps the recovery path `archive-transition.sh` supports today.
- **Consistent use of the resolved root.** Definition and planning artifact writes, artifact paths in prompts, every `openspec` CLI invocation (new, validate, status, instructions, archive), the OpenSpec engine hooks, and archival all use the resolved root. With an external root configured, nothing falls back to the repository-local `openspec/`, and no `openspec/` directory is created in the code repository.
- **Separate code root and spec root.** Implementation, validator, git, and PR steps keep running in the code repository. Definition, acceptance-artifact, and archive work targets the spec root. Prompts name both paths explicitly. Agent sessions get write access to the spec root, for example through an additional writable directory for sandboxed CLIs such as Codex `workspace-write`.
- **Spec project instructions.** If the spec root holds `AGENTS.md` or `CLAUDE.md`, or OpenSpec project context in `openspec/config.yaml`, definition, acceptance, and archive prompts point the agent at those files.
- **Separate version control for the spec repository.** With an external root, the plan commit and archive commit in the code repository leave out spec artifacts, Agent Runner does not commit or push in the spec repository, and the run ends by reporting which spec-root files changed so the user can commit them.
- **No leakage.** The external spec path is not written into PR bodies or other files committed to the code repository.
- **Backward compatible.** With nothing configured, behaviour and paths are exactly as today. This change is not **BREAKING**.

## Capabilities

### New Capabilities
- `external-openspec-root`: resolving, validating, recording, and propagating the OpenSpec project root, including the user-level setting, the per-invocation override, preflight failures, the code-root and spec-root split in prompts, spec-project instruction loading, no-leakage rules, and the end-of-run report of spec-repository changes.

### Modified Capabilities
- `openspec-engine`: OpenSpec CLI calls (`status`, `instructions`) run with the resolved spec root as their working directory, and resolve artifact and template paths against it.
- `builtin-workflows`: the `openspec:*` workflows and their bundled scripts (create, plan commit, planning-artifact validation, archive transition, archive-commit verification) derive every OpenSpec path from the resolved root and change their commit behaviour when the root is external.
- `sub-workflows`: workflows may declare `workspace_dirs`. Nested workflows inherit them, and the runner passes them to agent steps as additional workspace directories. This is how agent sessions get access to an external spec root.
- `cli-adapter`: when a step runs with an external spec root, adapters grant the agent session write access to that directory in addition to the working directory, without otherwise loosening permissions.

## Technical Approach

- **Setting shape.** Add a recognized key to `~/.agent-runner/settings.yaml`: a map from a code-repository identity to an absolute spec-root path. The proposal defaults that identity to the canonical root of the repository's main worktree, so worktrees created under `./worktrees` resolve to the same entry, and leaves the exact key to design. The user settings file was chosen over project `.agent-runner/config.yaml`: the project file can be checked in, and it carries the profile schema. Per-invocation override: an optional `spec_root` parameter on the `openspec:*` entry workflows (`--param spec_root=<path>`).
- **Resolution as an internal command.** A small `agent-runner internal` subcommand reads the parameter, the setting, and the git context. It validates the root for the requested lifecycle operation (create, continue, or archive) and emits JSON, including the code root, the spec root, an `external` flag, and the absolute change directory. A script step at the top of each user-facing entry workflow calls it and captures the output. Captured values already persist in run state, which covers resume. Nested workflows receive `spec_root` and `change_dir` as parameters, replacing the hard-coded `openspec/changes/{{change_name}}` literals, and they run no resolution step of their own.
- **Initialization boundary before any engine call.** The runner calls `Engine.ValidateWorkflow` before any step runs, both at startup and on resume, and `EnrichPrompt` reads only `ctx.Params`, not captured variables. A root resolved by a script step inside an engine-bearing workflow would therefore arrive too late. To fix this:
  - the latest entry workflows declare no engine at the top level, and keep their lifecycle phases as top-level steps after the resolver, so `--until` caps keep working;
  - engine-dependent steps (only in `simple-change`) run in a hidden child workflow that receives the normalized `spec_root` parameter;
  - that child's engine binds the root when the child starts, before any `openspec` call or agent spawn.

  Resolving inside runner setup was considered and rejected because it would couple the core runner to OpenSpec.
- **Scripts.** Shell scripts take the spec root and change directory as inputs. They run `openspec` with the spec root as cwd. With an external root they skip code-repository git staging for spec paths. Archive verification for an external root checks the filesystem result (change moved into the archive, specs updated) instead of a code-repository commit. The default path keeps today's git-snapshot verification unchanged.
- **Engine.** The OpenSpec engine sets the CLI's working directory to the resolved root, which it takes from a workflow parameter named in engine config. When that config is absent, the engine still uses the process cwd, so existing third-party workflow configs stay valid. Built-in engine-bearing workflows always name the parameter and always receive it from their parent entry workflow.
- **Prompts.** Shared core workflows (`define-change`, `plan-change`, `implement-change`, `accept-change`) already take `change_dir` and instruction parameters. The OpenSpec layer supplies absolute spec-root paths, both roots, and a spec-instructions hint through those parameters, which keeps `core:` workflows convention-neutral.
- **Main risk.** The archive and plan-commit verification scripts are the most intricate part of the OpenSpec layer. Giving them a second mode raises the risk of regressing the default path. Design should keep the external mode a separate, narrow branch, and tests must cover both modes.

## Out of Scope

- Committing or pushing in the spec repository, including any configurable commit policy. The issue lists this as optional, and this change only reports which spec files changed.
- Multi-repository changes where several code repositories each own specs (#72).
- `spec-driven:*` workflows and other non-OpenSpec conventions.
- Intake (`core:intake`) detecting an external root when it recommends a workflow. Users start `openspec:*` workflows directly.
- A settings-editor TUI for the new key. Users edit `settings.yaml` by hand or pass the parameter.
- Project-level, checked-in configuration of an external root.

## Impact

- **Code:** `internal/usersettings` (new key), `cmd/agent-runner` internal subcommand, `internal/engine/openspec` (CLI cwd), `internal/cli` adapters (extra writable directory), and possibly the step executor's plumbing for per-step extra directories.
- **Workflows:** `workflows/openspec/*.yaml` and their scripts (new minor versions that resolve the root first, a hidden engine-bearing child for `simple-change`, and unchanged user-facing names and top-level step IDs), plus `workflows/core/commit-change-plan.sh` and `validate-planning-artifacts.sh`, which special-case `openspec/changes/<name>`. Embedded workflow tests in `workflows/*_test.go` need cases for both modes.
- **Users:** none unless they opt in. Opted-in users get correct behaviour against an external OpenSpec project, and they commit spec-repository changes themselves.
- **Specs:** a new `external-openspec-root` spec, plus deltas to `openspec-engine`, `builtin-workflows`, and `cli-adapter`.
