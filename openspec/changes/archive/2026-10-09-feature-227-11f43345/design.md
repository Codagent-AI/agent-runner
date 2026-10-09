## Context

Today the built-in `openspec:*` workflows assume the OpenSpec project is the repository-local `openspec/` directory:

- **Workflow YAML** hard-codes `change_dir: "openspec/changes/{{change_name}}"` and repository-local wording (`workflows/openspec/*.yaml`).
- **Scripts** resolve `openspec/...` against the script's working directory, which is the run's working directory, and run `openspec` there: `create-change.sh`, `validate-change.sh`, `archive-transition.sh`, `verify-archive-commit.sh`, and in `workflows/core/` `validate-planning-artifacts.sh` (which enforces `change_dir == openspec/changes/<name>` for `change_kind: openspec`) and `commit-change-plan.sh`.
- **The OpenSpec engine** (`internal/engine/openspec/openspec.go`) runs `exec.Command("openspec", ...)` in the process cwd. Its hooks receive `params` only. `ValidateWorkflow` runs only for the top-level workflow, in `runner.go` before step 1. A sub-workflow's engine sees only `EnrichPrompt` and `ValidateStep`, both called with `childCtx.Params`.
- **The core lifecycle workflows** (`define-change`, `plan-change`, `implement-change`, `accept-change`, `verify-change`, `complete-simple-change`) already handle an absolute `change_dir` outside the repository, because `spec-driven:*` uses one. Their prompts already say to "leave artifacts outside the repository uncommitted".
- **Captured values:** a script step with `capture_format: json` captures a `map<string,string>` that supports `{{var.field}}`. Captures persist in `state.json` and are restored on resume. Captures stay in their own scope, so they are passed into sub-workflows as `params`.
- **Settings:** `~/.agent-runner/settings.yaml` (`internal/usersettings`) is per-user. Writes preserve unknown keys through the raw YAML document.
- **Script environment:** scripts can call the runner through `$AGENT_RUNNER_EXECUTABLE internal <cmd>`, which `run-validator.sh` already uses.
- **Adapters:** `cli.BuildArgsInput` is built in `internal/exec/agent.go`, `agent_call.go`, and `external_user.go`. Codex headless runs `--sandbox workspace-write`, which blocks writes outside cwd.
- **Factory workflows** in this repository call `builtin:openspec/archive-change-v1.0.yaml` by exact version.

## Goals / Non-Goals

**Goals:**
- Resolve the spec root once per run, record it in state, and make every OpenSpec path, `openspec` call, and engine hook use it.
- Keep the default (nothing configured) byte-for-byte compatible in paths, commits, and adapter args.
- Keep the external-root branches narrow and separate from today's intricate archive and commit verification.
- Give agent sessions access to the spec root without loosening permissions.

**Non-Goals:**
- Commits in the spec repository or a commit policy.
- `spec-driven:*` workflows, intake detection, a settings-editor UI, `openspec:scaffold`, multi-repository changes (#72).
- Retrofitting older embedded workflow versions.

## Approach

### 1. Setting: `openspec_roots`

`internal/usersettings` gains `OpenSpecRoots map[string]string`, parsed from:

```yaml
openspec_roots:
  /Users/me/code/shared-repo: ~/code/specs-repo/plugins/foo
```

The loader ignores non-string values. The existing raw-document write path preserves the key when other settings are saved, and a regression test covers this. Nothing writes the key; users edit it by hand.

### 2. Resolver: `agent-runner internal resolve-openspec-root`

This is a new case in `cmd/agent-runner/internal_cmd.go`, with its logic in a new package `internal/openspecroot`, which is unit-testable without the CLI. It reads JSON on stdin:

```json
{"change_name": "foo", "spec_root": "<param or empty>", "operation": "create|continue|guard"}
```

It resolves the root:

1. **Working directory:** `wd` is the process cwd with symlinks resolved.
2. **Repository key:** `git rev-parse --path-format=absolute --git-common-dir`. If the result's basename is `.git`, the key is its parent, which is the main worktree root. Otherwise the key is `git rev-parse --show-toplevel`. Outside Git, the key is `wd`. All values have symlinks resolved.
3. **Source:**
   - a non-empty `spec_root` param gives `source=param`; `~/` is expanded and a relative path is joined to `wd`;
   - otherwise `OpenSpecRoots[key]` (keys compared after resolving symlinks) gives `source=setting`;
   - otherwise `source=default` and the root is `wd`.
4. **External flag:** `external` is true when the resolved root is not `wd`.
5. **Validation when external:** the root must exist and be a directory, and `<root>/openspec/` must be a directory. Errors name the path and the source.
6. **Lifecycle checks when external:**
   - `create` fails if `<root>/openspec/changes/<name>` exists or any `<root>/openspec/changes/archive/*-<name>` exists;
   - `continue` fails if `<root>/openspec/changes/<name>` is missing;
   - `guard` (archive called directly) does nothing beyond validation.
7. **Guard when the param is empty:** if the setting maps the repository to an external root, `guard` fails with "recorded OpenSpec root context is missing". This keeps a directly invoked archive from silently using cwd.

It prints a JSON object of strings, captured by the calling step as the map `openspec`:

| field | default root | external root |
|---|---|---|
| `spec_root` | `wd` (absolute) | resolved root |
| `external` | `false` | `true` |
| `source` | `default` | `param` / `setting` |
| `change_dir` | `openspec/changes/<name>` (relative, as today) | `<root>/openspec/changes/<name>` |
| `location_instruction` | today's exact repository-local sentence | "Keep every OpenSpec definition and planning artifact under `<change_dir>/` in the OpenSpec project at `<root>`." |
| `validate_instruction` | today's `openspec validate --type change "<name>"` sentence | same command, "run from `<root>`" |
| `context_instruction` | empty | see §6 |
| `commit_plan` | `true` | `false` |

Building the conditional prompt text in Go keeps it deterministic and testable, and records it in run state together with the root. The `AGENTS.md`/`CLAUDE.md`/`openspec/config.yaml` existence checks therefore happen once, at resolution time.

### 3. Entry workflows: resolver first, lifecycle phases stay top-level

Each user-facing entry gets a new minor version: `change-v2.1`, `simple-change-v2.1`, `plan-change-v2.1`, and `implement-change-v2.1`, plus `archive-change-v1.1` for nested use. Each new version keeps today's lifecycle phases as top-level steps, with today's step IDs, and makes two additions: a first step `resolve-openspec-root` and a last step `report-spec-changes`. `--until define|plan|implement|accept|archive|finalize` and capped resume therefore behave exactly as in `v2.0` (`run-until` only accepts top-level IDs; see `runner.go` `validateUntilStep`). For example, `change-v2.1`:

```yaml
params:
  - {name: change_name, required: true}
  - {name: spec_root, required: false, default: ""}
workspace_dirs: ["{{openspec.spec_root}}"]       # §5; evaluated lazily, after the capture exists
steps:
  - id: resolve-openspec-root
    script: resolve-openspec-root.sh            # execs "$AGENT_RUNNER_EXECUTABLE" internal resolve-openspec-root
    script_inputs: {change_name: "{{change_name}}", spec_root: "{{spec_root}}", operation: create, session_dir: "{{session_dir}}"}
    capture: openspec
    capture_format: json
  - id: validate-feature-branch                 # unchanged
  - id: create
    script: create-change.sh
    script_inputs: {change_name: "{{change_name}}", spec_root: "{{openspec.spec_root}}", spec_external: "{{openspec.external}}"}
  - id: define                                  # core define-change, params from {{openspec.*}}
  - id: plan                                    # core plan-change (+ spec_root, commit_plan, context_instruction)
  - id: implement
  - id: accept
  - id: archive                                 # archive-change-v1.1 (+ spec_root, spec_external, context_instruction)
  - id: finalize                                # core finalize-pr (+ context_instruction)
  - id: check-spec-leak                         # §9, skipped unless external
  - id: report-spec-changes                     # §8, skipped unless external
```

Every former `openspec/changes/{{change_name}}` literal becomes `{{openspec.change_dir}}`. Repository-local sentences become `{{openspec.location_instruction}}` and `{{openspec.validate_instruction}}`. `plan-change-v2.1` and `implement-change-v2.1` use `operation: continue`. On resume the capture is restored, every step re-derives the same values, and later setting edits cannot redirect the run.

**Engine boundary.** Only `simple-change` declares the engine. `simple-change-v2.1` drops the top-level `engine:` block. Its engine-dependent steps (today's `plan`, `check-planning-artifacts`, and `validate-openspec`) move into one hidden child, `simple-change-plan-v1.0.yaml`, which runs as the top-level step `plan`:

```yaml
engine: {type: openspec, change_param: change_name, root_param: spec_root}
```

The parent passes it `spec_root`, `change_dir`, and the instruction params. `--until plan` keeps its meaning. The top-level `ValidateWorkflow` no longer runs for `simple-change`, because the top level has no engine. The child's engine is bound to the root when the child starts (§7).

### 4. Scripts

- **`create-change.sh` (openspec):** new optional inputs `spec_root` and `spec_external`. Run `agent-validator detect` in cwd, the code repository, as today. If external, `cd "$spec_root"` before the collision check and `openspec new change`. If the inputs are absent, behavior is unchanged.
- **`validate-change.sh`:** optional `spec_root`; `cd` there before `openspec validate`.
- **`core/validate-planning-artifacts.sh`:** optional `spec_root` input (absent means `.`). For `openspec`, the expected-directory check compares `realpath(change_dir)` with `realpath(spec_root/openspec/changes/<name>)`. This accepts today's relative pair and an external absolute pair, and still rejects a mismatched pair. `openspec validate` runs in a subshell `cd "$spec_root"`.
- **`core/plan-change`:** new optional params `spec_root` (default `""`), passed to the script, and `commit_plan` (default `"true"`). `commit-plan` gets `skip_if: 'sh: test {{change_kind}} = spec-driven || test {{commit_plan}} = false'`. `commit-change-plan.sh` is unchanged. `simple-change-v2.1` skips its top-level `commit-plan` sub-workflow with `skip_if: 'sh: test {{openspec.commit_plan}} = false'`. `implement-change` passes `spec_root` to its `check-plan`.
- **`archive-change-v1.1.yaml`** has params `change_name`, `spec_root` (default `""`), `spec_external` (default `"false"`), and `context_instruction` (default `""`). Its steps:
  1. **`guard`:** a resolver call with `operation: guard`, run only when `spec_root` is empty. When the archive is called directly, it either fails because of the setting or confirms the default root.
  2. **`archive-transition`, `verify-archive-commit`, `advance-validator-baseline`:** unchanged scripts, with `skip_if` external.
  3. **`archive-external`:** new `archive-external.sh`, run only when external. It owns a run-scoped transition record at `{{session_dir}}/output/archive-transition/<name>-external.json`. The session directory is unique per run, so an existing record proves that this run started the transition.
     - **No record yet (first attempt):** require the active directory `<root>/openspec/changes/<name>/` to exist, and require that no `<root>/openspec/changes/archive/*-<name>` exists. Otherwise fail, naming the path. A fresh invocation can never adopt an old archive.
       - Then build two manifests of `sha256` hashes over regular files, excluding `.git/`: `canonical` covers `openspec/specs/**`, and `other` covers everything else except the active change directory.
       - Write the record atomically (temp file plus rename in the same directory) with `spec_root`, `change_name`, `run_id` (the session directory's basename), `archive_absent_at_start: true`, and both manifests.
       - Then run `openspec validate --type change <name>` and `openspec archive <name> --yes` in the spec root.
     - **Record exists (retry or resume):** fail if the record's `spec_root` or `change_name` differs from the inputs.
       - If the active directory still exists, the crash happened before the move, so run validate and archive again with the existing record.
       - If the active directory is gone, skip `openspec archive` and continue to verification.

     The repair prompt names only `<root>/openspec/changes/<name>/` as editable. It forbids `<root>/openspec/specs/`, `openspec archive`, moves, and commits. It appends `{{context_instruction}}`, which lists the spec project's instruction files.
  4. **`verify-archive-external`:** new `verify-archive-external.sh`. It requires a matching record, because without one there is nothing to verify and the step fails. Then:
     - The active directory must be gone, and exactly one `archive/<date>-<name>` must exist. It must not have existed at start, which the record proves.
     - Re-hashing `other` must match the record, ignoring the new archive directory. Otherwise the step fails, naming each file added, removed, or changed.
     - The step diffs `canonical` against its baseline and writes `archive_dir` and `canonical_changes` (added, modified, and deleted paths relative to the spec root) back into the record for the report.
     - A rerun recomputes the same result from the unchanged baseline.

  The existing `archive-transition.sh` and `verify-archive-commit.sh` stay untouched. `archive-change-v1.0.yaml` is unchanged, so factory callers pinned to it keep today's behavior.

### 5. Workspace directories (`sub-workflows`, `cli-adapter`)

- **Model:** `model.Workflow` gains ``WorkspaceDirs []string `yaml:"workspace_dirs,omitempty"` ``.
- **Context and timing:** `model.ExecutionContext` gains `WorkspaceDirs []string`, the normalized list inherited from the parent. A workflow's own `workspace_dirs` are interpolated lazily against that workflow's params and its captured variables visible at that point. This happens when an agent step, `call_agent` child, or external-user turn is about to spawn, and when a sub-workflow step starts (the child inherits the evaluated list). An undefined variable at that point fails the step before spawn. In the OpenSpec entries the only step before the capture is the resolver script, so `{{openspec.spec_root}}` is always defined when it is first needed. On resume the restored capture yields the same list.
- **Normalization:** drop empty values, require absolute paths, `EvalSymlinks` (an error means the directory is missing), require a directory, dedupe, and drop paths equal to or inside `ctx.ProjectRoot`. An error fails the step that triggered evaluation before any CLI spawns or child step runs.
- **Agent steps:** `cli.BuildArgsInput` gains `AdditionalDirs []string`, filled from `ctx.WorkspaceDirs` in `agent.go`, `agent_call.go` (child agents inherit the caller's context), and `external_user.go`.
- **Adapter capability:** a new optional interface `cli.AdditionalDirSupporter` has `AdditionalDirSupport() DirSupport`, with values `DirFlag`, `DirUnconfined`, and `DirUnsupported`. Adapters that do not implement it count as `DirUnsupported`. Before spawning, the executor fails with an error naming the CLI and the first directory when the list is non-empty and support is `DirUnsupported`.
  - **Claude:** `DirFlag`, emitting `--add-dir <dir>` per directory.
  - **Codex:** `DirFlag`, emitting `--add-dir <dir>` as a top-level option next to `--sandbox`, before `exec`.
  - **Copilot:** `DirFlag`, emitting `--add-dir <dir>`.
  - **OpenCode:** `DirUnconfined`, so it emits nothing.
  - **Cursor:** `DirUnsupported`.

  Each adapter's flag placement is pinned by an args test. Because of the project-root filter, default runs produce an empty list and unchanged args.

### 6. Prompts

Core `define-change`, `plan-change`, `review-tasks`, `implement-change`, `implement-task`, `run-validator`, `verify-change`, `accept-change`, `complete-simple-change`, and `finalize-pr`, plus `archive-change-v1.1`, gain an optional `context_instruction` param (default `""`). It is appended to every agent prompt, including `repair:` prompts, that reads or writes change artifacts or task files, edits or commits code, or writes PR text. That includes `run-validator`'s `fix-violations` commit prompt and the archive repair prompts. Every caller forwards it, including `implement-task` → `run-validator`, `accept-change` → `run-validator`, and `implement-change` → `verify-change`.

For an external root the resolver's text states:

- the code root and the spec root as absolute paths;
- that artifacts belong in the spec root, while code changes, validation, and commits belong in the code root;
- each existing spec-project instruction file (`<root>/AGENTS.md`, `<root>/CLAUDE.md`, `<root>/openspec/config.yaml`), to be read and followed;
- that spec-root files must never be staged, committed, or pushed;
- that the spec-root path must never be written into commits, the PR title or body, or any file in the code repository.

For the default root the param is empty, so prompts keep today's wording. The OpenSpec layer fills `artifact_location_instruction` and `artifact_validation_instruction` from `location_instruction` and `validate_instruction`.

### 7. Engine (`openspec-engine`)

- **Runner interface:** `CmdRunner.Run(args)` becomes `Run(dir string, args []string)`, and `realCmdRunner` sets `cmd.Dir = dir` when it is non-empty.
- **Config:** the constructor reads an optional string `root_param`.
- **Binding:** a new optional interface, `engine.ContextBinder` with `BindContext(params map[string]string) error`, is called for an engine-bearing workflow with that workflow's own params:
  - `prepareSubWorkflow` calls it right after `engine.Create` and before any child step;
  - `runner.go` calls it for a top-level engine before `ValidateWorkflow`, at start and on resume.

  The OpenSpec engine's binding behaves as follows:
  - With `root_param` set, the engine reads the value, requires a non-empty absolute path to an existing directory, and stores it.
  - Otherwise it returns an error naming the param. That error fails the workflow start, so zero `openspec` calls and zero agent spawns happen.
  - Without `root_param`, binding is a no-op.

  Every hook uses the bound directory, so engines inherited by nested sub-workflows keep the binding even though `NewSubWorkflowContext` replaces `Params`.
- **Enrichment errors:** `EnrichPrompt` stays string-only. For an engine whose binding has not succeeded, `buildAgentPrompt` checks the new `engine.ContextBinder` state (`Bound() bool`) and aborts the agent step before spawn with the binding error. This is defense in depth for contexts that bypass `prepareSubWorkflow`.
- **Paths:** output and dependency paths come from the CLI's `changeDir`, which is already absolute under `dir`.
- **Default:** without `root_param`, `dir` is `""` and the process cwd is used as today.

### 8. Spec-change report

`report-spec-changes.sh` (openspec namespace) takes `spec_root`, `change_name`, and `session_dir` as inputs and runs as the last top-level step of each entry workflow when external.

- **Spec root in Git:** if `git -C "$spec_root" rev-parse --show-prefix` succeeds, the script lists `git -C "$spec_root" status --porcelain=v1 -z --untracked-files=all -- .` entries, with paths made relative to `spec_root` by stripping the prefix.
- **Spec root not in Git:** it prints a "not under version control" line, then the active change directory or the archive directory, plus the canonical spec files recorded by `verify-archive-external`.

Output goes to stdout, which is the step's output in the run view and in `output/`. The script never runs a Git command that writes.

### 9. Leak guard

`check-spec-leak.sh` (openspec namespace) runs after `finalize`, and after `implement` in `implement-change-v2.1`. Its `skip_if` skips it when `spec_external` is false. Inputs are `spec_root` and `session_dir`. The script:

- computes the merge base with the default branch;
- reads `git log --format=%B <base>..HEAD`, `git diff <base>..HEAD`, and, when `gh pr view --json number` finds a PR for the head branch, `gh pr view --json title,body`;
- fails if any of them contains the spec root (or its unresolved form), naming each location: the commit SHA, the file path, or `PR title` / `PR body`.

The repair, run in the lead session, may edit the PR title or body through the bundled `update-pr-body.sh` and `gh pr edit --title`. It may also add a follow-up commit that removes the path from tracked files. History is never rewritten, so a path left in a pushed commit message is reported as `REPAIR_BLOCKED` for the human. This turns leakage control from prompt-only into a deterministic check.

## Decisions

- **Resolver as the first top-level step; lifecycle phases stay top-level.** This preserves `--until` caps and capped resume (AR-002). Only `simple-change`'s engine-dependent steps move into a hidden child, because the engine boundary requires it. Alternatives considered: wrapper plus a single body step (rejected because it breaks `--until plan|implement|archive`); resolving inside runner setup (rejected because it couples the core runner to OpenSpec).
- **New minor versions instead of in-place edits.** Changing step IDs in place would break resume of in-flight `v2.0` and `archive-change-v1.0` runs and exact-version callers such as the factory. The core workflow edits are additive optional params, so old versions keep working.
- **Prompt text built by the resolver.** This keeps conditional wording in tested Go code instead of shell or YAML conditionals, and records it with the root.
- **Default `change_dir` stays relative.** This preserves `commit-change-plan.sh`'s confined-path rule and today's exact args, paths, and commits.
- **Run-scoped transition record with canonical and other manifests.** A fresh run can never adopt an old archive (AR-001), and the canonical baseline makes changed-spec reporting possible (AR-003).
- **Filesystem manifest for external archive verification.** It works whether or not the spec root is a Git checkout and never touches the spec repository's index. Alternative considered: `git status` snapshots in the spec repository, rejected because it fails for non-Git roots and would conflate the user's own uncommitted spec edits.
- **Lifecycle checks only for external roots.** This honors issue requirement 8 (no behavior change by default).
- **Workflow-level `workspace_dirs` as a general schema field.** It is a small, reusable runner feature that does not depend on OpenSpec. Alternative considered: special-casing a `spec_root` param in the executor, rejected because it couples the executor to OpenSpec.
- **Engine context is bound once at workflow start.** Binding happens in `BindContext` rather than per hook, so invalid context aborts before any spawn and inherited engines keep the root (AR-004).
- **Deterministic leak guard.** The check runs after publishing steps (AR-005). It cannot prevent a push, but it makes a leak fail the run visibly.
- **Cursor reports `DirUnsupported`.** Its CLI exposes no add-directory flag the runner can rely on, and failing fast beats a mid-step write failure.

## Risks / Trade-offs

- **Leakage is detected after publication, not prevented.** An agent's push happens inside `open-draft-pr`, so a path in a pushed commit message can only be reported, not rewritten. Mitigations: the explicit context instruction in every commit and PR prompt; the leak guard (§9), which fails the run and repairs PR text; and INT-008 and the acceptance envelope's recording `gh` with a local bare remote.
- **Manifest cost on large spec roots.** Hashing every file is O(size). Spec projects are small, `.git/` is excluded, and only two passes run per archive.
- **CLI flag drift.** Adapter args tests pin the flags. Implementation must confirm each `--add-dir` against the installed CLI's `--help`. Codex must accept it as a top-level option.
- **Many prompt edits in core workflows.** These are mechanical. Embed tests assert that every agent and repair prompt carrying `change_dir` or `task_file`, or performing commits or PR edits, also interpolates `{{context_instruction}}`, including `run-validator` and the archive repair prompts.
- **Worktree key edge cases** (bare repositories, submodules). The resolver falls back to `--show-toplevel`, and the `spec_root` param always works.

## Migration Plan

- The change is additive. Nothing changes until `openspec_roots` is set or `spec_root` is passed.
- New runs pick up `v2.1` automatically. In-flight `v2.0` runs resume on their recorded files, which still use repository-local behavior.
- Rollback means reverting the commit. The `openspec_roots` key is then ignored as an unknown settings key.

## Open Questions

None blocking. If Cursor gains a dependable add-directory flag, switching its adapter to `DirFlag` is a follow-up.
