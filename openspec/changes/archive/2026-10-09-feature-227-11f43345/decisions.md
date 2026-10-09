# Decisions

## propose

### Verdict: go with caveats
- **Decision:** Proceed. The issue describes a concrete, current setup that cannot use `openspec:change` today, and its nine requirements give it a single clear reading. The caveat is that the archive and plan-commit verification scripts gain a second mode, so they need a narrow external branch and tests for both modes.
- **Alternatives considered:** No-go, with users forking the workflows (rejected because it forks the intricate archive/commit scripts and loses built-in fixes). Folding this into #72 multi-repo (rejected because the issue explicitly separates them).
- **Decision-bearing:** yes

### Spec root means the directory that contains `openspec/`
- **Decision:** The configured value is the OpenSpec project directory, the directory where the `openspec` CLI runs, not the `openspec/` folder itself.
- **Alternatives considered:** Point at the `openspec/` folder directly, or accept either form.
- **Decision-bearing:** no. This matches how the CLI discovers projects, and design may also accept the inner folder.

### Configuration lives in `~/.agent-runner/settings.yaml`, keyed by code repository, with a `spec_root` workflow param override
- **Decision:** Add a user-level settings key that maps a code-repository identity (defaulting to the canonical main-worktree root) to a spec root. Add an optional `spec_root` param on `openspec:*` entry workflows.
- **Alternatives considered:** Project `.agent-runner/config.yaml` (rejected because it can be checked in, which leaks the path, and it carries the profile schema). A new `--spec-root` CLI flag (rejected because `--param spec_root=` already gives a per-invocation override without new CLI surface). Keying by remote URL (left open for design).
- **Decision-bearing:** yes. The change is additive only (a new optional key and param) and breaks no persisted format.

### Resolve once in a preflight script step backed by an internal subcommand; persist through captures
- **Decision:** An `agent-runner internal` subcommand resolves and validates the root. A script step captures its JSON so run state carries it across resume.
- **Alternatives considered:** Resolve inside the runner core as a new built-in variable (rejected because it is heavier and couples the core to OpenSpec). Re-resolve in every step (rejected by requirement 9).
- **Decision-bearing:** no

### Spec-repository commits are out of scope; report changed files instead
- **Decision:** Never commit or push in the spec repository, and report the changed files at the end of the run. A configurable commit policy is deferred.
- **Alternatives considered:** Implement an optional commit policy now. The issue marks it "optionally", so deferring it does not contradict the issue.
- **Decision-bearing:** yes

### Grant agent sessions write access to an external spec root
- **Decision:** Adapters add the spec root as an extra writable directory when it is external. Codex `workspace-write` would otherwise block writes outside cwd.
- **Alternatives considered:** Run definition steps with the spec root as cwd (rejected because prompts and validator steps expect the code repository as cwd, and requirement 3 keeps code steps in the code repository).
- **Decision-bearing:** no. It is required for the feature to work and does not loosen other permissions.

### Exclude intake detection, spec-driven workflows, settings TUI, and checked-in project config
- **Decision:** Keep the scope to the `openspec:*` workflows named in the issue.
- **Alternatives considered:** Teach `core:intake` to detect an external root (deferred because users can start `openspec:change` directly).
- **Decision-bearing:** no

## proposal-review

### PR-001: Preflight collision check blocks the continue, archive, and retry paths (applied)
- **Decision:** Applied. The proposal now resolves and validates the root separately from checking the change. Only the outermost entry resolves the root, and nested workflows reuse the recorded root. The change check depends on the operation:
  - create workflows reject an existing active or archived name;
  - plan and implement workflows require the existing active artifacts;
  - archive works on the active change and, on retry or resume, accepts a transition the same run already completed.

  This matches `archive-transition.sh`, which already accepts a retry after the change directory has moved into the archive.
- **Alternatives considered:** Keep one blanket preflight and exempt archive only (rejected because plan-change and implement-change would still fail). Drop collision checks entirely (rejected because requirement 7 asks for them on new changes).
- **Decision-bearing:** no. It corrects the proposal's internal consistency without changing direction.

### PR-002: The root resolves after engine validation and enrichment need it (applied)
- **Decision:** Applied. I confirmed that `runner.go` calls `Engine.ValidateWorkflow` before any step runs and that `buildAgentPrompt` passes only `ctx.Params` to `EnrichPrompt`. Engine-bearing OpenSpec workflows are therefore split into an engine-free entry wrapper, which resolves and records the root, and an engine-bearing inner workflow that receives `spec_root` as a parameter. The proposal also now states two rules: an external root configured without a valid recorded context fails rather than falling back to cwd, and editing the setting cannot redirect an existing run.
- **Alternatives considered:** Resolve or restore the context in runner setup before engine validation (rejected because it couples the core runner to OpenSpec). Have the engine read captured variables (rejected because it adds a second root source that can disagree with the parameters).
- **Decision-bearing:** yes. The split is an internal workflow-structure decision and user-facing workflow names stay the same, so it is not a public-interface break and not a stop.

## spec

### Spec root precedence: parameter, then user setting, then repository-local default; relative parameter paths resolve against the start directory
- **Decision:** As specified in `external-openspec-root`. Nothing inside the code repository is read as a spec-root source.
- **Alternatives considered:** Also read the project's `.agent-runner/config.yaml` (rejected because the file can be checked in and would leak the path).
- **Decision-bearing:** no

### Default root keeps today's lenient preflight
- **Decision:** The strict checks (the root exists and has an `openspec/` directory) apply only to external roots. With the default root, `openspec:change` still creates `openspec/` on demand.
- **Alternatives considered:** Require `openspec/` in every case (rejected by requirement 8 on backward compatibility).
- **Decision-bearing:** no

### External archive is verified on the filesystem and creates no commit; the plan-commit step is a successful no-op
- **Decision:** Follows from requirement 6 (no commits in the spec repository) and requirement 5 (no spec files in the code repository).
- **Alternatives considered:** Commit in the spec repository (deferred along with the commit policy).
- **Decision-bearing:** no

### Changed spec files are reported from Git status, or from the change and archive directories when the spec root is not a Git checkout
- **Decision:** Where the report appears is deferred to design.
- **Alternatives considered:** Always list files from a filesystem diff snapshot (left to design as a possible refinement).
- **Decision-bearing:** no

### Only the latest workflow versions gain external-root support; `openspec:scaffold` is excluded
- **Decision:** Older embedded versions exist only so saved runs can resume, and they keep repository-local behavior. Scaffold creates a new project in the working directory, so an external root does not apply to it.
- **Alternatives considered:** Retrofit every embedded version (rejected because no new runs start on them).
- **Decision-bearing:** no

### Engine gains an optional `root_param`, with no fallback when it is configured but missing
- **Decision:** Additive engine config. Without `root_param`, existing third-party workflows keep using the process working directory.
- **Alternatives considered:** A hard-coded `spec_root` parameter name (rejected because the engine config already names its parameters, for example `change_param`).
- **Decision-bearing:** no

### Adapters add the spec root as a workspace directory without auto-approving anything, and fail fast when the CLI cannot add a directory
- **Decision:** Applies in both interactive and autonomous contexts, and the rule against loosening permissions in interactive mode is kept. The Cursor and OpenCode mappings are deferred to design.
- **Alternatives considered:** Silently skip CLIs that have no add-directory flag (rejected because the agent's writes would fail mid-step instead of up front).
- **Decision-bearing:** no

## design

### Every OpenSpec entry is a thin wrapper (resolve the root, then call a hidden body), shipped as new minor versions (v2.1, and archive-change v1.1)
- **Decision:** One pattern for all entry workflows. New versions keep in-flight v2.0 runs and callers pinned to an exact version (the factory's `archive-change-v1.0`) on today's behavior.
- **Alternatives considered:** Edit the v2.0 files in place (rejected because changed step IDs break resume). Use a wrapper only for the engine-bearing `simple-change` (rejected because it leaves two resolution patterns).
- **Decision-bearing:** yes. It is internal structure and the user-facing names are unchanged.

### The resolver is an internal subcommand that emits the root, flags, and ready-made prompt instruction strings
- **Decision:** `agent-runner internal resolve-openspec-root` is captured as the JSON map `openspec`. The conditional wording lives in tested Go code.
- **Alternatives considered:** Shell conditionals in YAML (rejected because they are hard to test and spread out).
- **Decision-bearing:** no

### Settings key `openspec_roots`, keyed by the main-worktree root
- **Decision:** This resolves the deferred spec marker. The key is the parent of the Git common dir, falling back to the toplevel, or the working directory outside Git.
- **Alternatives considered:** Remote URL as the key (rejected because forks and remotes are ambiguous and the URL does not identify a local checkout).
- **Decision-bearing:** no

### The default root is the run's working directory, and lifecycle collision checks apply only to external roots
- **Decision:** Honors issue requirement 8 exactly. I updated the spec wording, which previously said "code repository root".
- **Alternatives considered:** Apply the archived-name collision check by default as well (rejected because it changes default behavior).
- **Decision-bearing:** no

### External archive uses new separate scripts and verifies with a filesystem manifest
- **Decision:** `archive-external.sh` and `verify-archive-external.sh`. The existing scripts are untouched.
- **Alternatives considered:** Git-status snapshots in the spec repository (rejected because they fail for roots outside Git and pick up the user's own edits).
- **Decision-bearing:** no

### New general workflow field `workspace_dirs`, inherited by nested workflows, feeds the adapters' `AdditionalDirs`
- **Decision:** Added a `sub-workflows` spec delta and listed it in the proposal's capabilities. Directories inside the project root are dropped, so default runs keep identical args.
- **Alternatives considered:** Special-case `spec_root` in the executor (rejected because it couples the executor to OpenSpec).
- **Decision-bearing:** yes. It is an additive public workflow-schema field and breaks nothing.

### Adapter directory support: Claude, Codex, and Copilot use `--add-dir`; OpenCode is unconfined; Cursor is unsupported and fails fast
- **Decision:** This resolves the deferred spec marker.
- **Alternatives considered:** Let Cursor run and hope its writes succeed (rejected because the step would fail mid-way instead).
- **Decision-bearing:** no

### The spec-change report is the body's final step output
- **Decision:** This resolves the deferred spec marker. When the spec root is in Git, the report uses `git status` scoped to the spec root. Otherwise it uses the archive verification record.
- **Alternatives considered:** A summary on the run-complete screen (rejected because it needs runner UI changes for one workflow family).
- **Decision-bearing:** no

### Core workflows gain optional `context_instruction`, `spec_root`, and `commit_plan` params with defaults that keep today's behavior
- **Decision:** These params carry the prompt context and the plan-commit skip for external roots.
- **Alternatives considered:** Copy the core workflows into the openspec namespace (rejected because of duplication).
- **Decision-bearing:** no

## test-plan

### Seven integration and three end-to-end obligations, using fake `openspec`, `agent-validator`, and agent CLIs on PATH, inside the existing `test` job
- **Decision:** CI has no real `openspec` or agent CLIs. Fakes that record cwd and argv follow the established `workflows/archive_test.go` pattern. The end-to-end tests use testscript with `--until`, resume, and a fixture workflow that calls `archive-change-v1.1`.
- **Alternatives considered:** A full `openspec:change` end-to-end test with scripted agents (rejected because it is brittle and the interactive phases cannot run unattended). New CI jobs (forbidden by CLAUDE.md).
- **Decision-bearing:** no

### One human-only check, HT-001, for the interactive external-root lifecycle
- **Decision:** The definition and acceptance phases are interactive agent conversations, and CLAUDE.md states these need a human at a real terminal.
- **Alternatives considered:** No human-only testing (rejected because no agent or automated layer can confirm that real interactive agents honor the spec root's `AGENTS.md` and its write access).
- **Decision-bearing:** no

### The acceptance envelope uses a scratch `HOME`, scratch repositories, and no remotes or PRs
- **Decision:** The pass may use real `openspec` and the existing agent CLI logins locally. Pushing and creating PRs are off limits.
- **Alternatives considered:** Exercise `finalize-pr` against a real throwaway GitHub repository (rejected because it is an external effect. Superseded by AR-005: PR-body leakage is now covered by the leak guard, INT-008, and a recording fake `gh` with a local bare remote in the acceptance envelope and HT-001).
- **Decision-bearing:** no

## approach-review

### AR-001: A fresh archive could adopt an old archive without a run-scoped record (applied)
- **Decision:** Applied. `archive-external.sh` now writes an atomic, run-scoped transition record under the session directory containing `spec_root`, `change_name`, `run_id`, `archive_absent_at_start`, and the manifests.
  - A fresh attempt requires the active directory and fails if a dated archive for the name already exists.
  - A retry proceeds without the active directory only when this run's matching record exists, and re-runs archive if the move had not happened yet.
  - The spec (builtin-workflows archive requirement and two new scenarios), design §4, INT-003, and E2E-003 are updated.
- **Alternatives considered:** Keep reusing a snapshot created on first touch (rejected because it does not prove ownership).
- **Decision-bearing:** no

### AR-002: Collapsing the lifecycle into one nested step breaks `--until` caps; stopping before finalize is not enough to avoid pushes (applied)
- **Decision:** Applied. I confirmed that `validateUntilStep` accepts only top-level IDs and that `verify-change`'s `open-draft-pr` pushes and creates the PR.
  - The latest entry workflows now keep today's top-level phase IDs, with `resolve-openspec-root` first and the leak check and report last.
  - Only `simple-change`'s engine-dependent steps move into a hidden child, which runs as top-level `plan`.
  - `workspace_dirs` now evaluates lazily against params and captures, so it works at the top level.
  - The acceptance envelope now requires `--until plan`, or a local bare `origin` with a recording fake `gh`.
  - Updated: proposal, the builtin-workflows spec (new "keep their top-level phases" requirement), the sub-workflows spec, design §3 and §5, INT-006, INT-007, E2E-001, and the envelope.
- **Alternatives considered:** Keep the wrapper and document the cap migration (rejected because it breaks a working public behavior for default runs and contradicts requirement 8).
- **Decision-bearing:** yes. It reverses the earlier wrapper-plus-body structure, but it is internal structure that preserves the public names and `--until` behavior, so it is not a stop.

### AR-003: There was no canonical-spec baseline for reporting changed specs (applied)
- **Decision:** Applied. The transition record now holds separate `canonical` (`openspec/specs/**`) and `other` manifests. `other` must be unchanged. `canonical` is diffed into added, modified, and deleted lists that the report consumes, and the baseline is kept across retries.
  - Updated: design §4, the builtin-workflows spec (verification records canonical changes), INT-003, and INT-004.
- **Alternatives considered:** Rely on `git status` (rejected because there is no Git in non-Git roots).
- **Decision-bearing:** no

### AR-004: `EnrichPrompt` cannot fail, so invalid engine context would silently drop enrichment (applied)
- **Decision:** Applied. A new optional `engine.ContextBinder.BindContext(params)` hook is called when an engine-bearing workflow starts (in `prepareSubWorkflow`, and in `runner.go` before `ValidateWorkflow`, including on resume). It validates that `root_param` is non-empty, absolute, and an existing directory, and stores the binding. Invalid context fails the workflow start with zero `openspec` calls and zero agent spawns.
  - Inherited engines keep the binding in nested workflows, and `buildAgentPrompt` refuses to spawn when the engine is unbound.
  - Updated: the openspec-engine spec (binding, a nested scenario, and a no-spawn assertion), design §7, and INT-005, which now runs through the exec package.
- **Alternatives considered:** Change `EnrichPrompt` to return an error (rejected because it changes the engine interface for every engine; the optional binder is additive).
- **Decision-bearing:** no

### AR-005: Leakage mitigation claimed a PR-body test that does not exist (applied)
- **Decision:** Applied. A new deterministic `check-spec-leak.sh` step runs after publishing steps when the root is external. It scans the branch's commit messages and diff and the PR title and body (through `gh`), fails naming each location, and its repair may fix PR text or add commits but never rewrites history.
  - Added: the spec requirement text and a scenario, design §9, INT-008 (recording fake `gh` and local bare remote, including replacement of the generated block), the acceptance-envelope substitute, and an HT-001 observation. I corrected the earlier test-plan decision that overstated coverage.
- **Alternatives considered:** Leave leakage prompt-only (rejected because the reviewer showed no behavioral verification existed).
- **Decision-bearing:** no. It reinforces issue requirement 5 within scope.

### AR-006: `run-validator` and the archive repair prompts were missing the context instruction (applied)
- **Decision:** Applied. `core:run-validator` and `archive-change-v1.1` gain the optional `context_instruction` param, forwarded by every caller and appended to `fix-violations` and both archive repair prompts. The archive repair keeps its narrow editable-path rule.
  - Updated: design §6, the builtin-workflows spec repair scenario, INT-007 wiring assertions, and INT-008, which checks the rendered prompts.
- **Alternatives considered:** None. The omission was a gap.
- **Decision-bearing:** no

## tasks

### One implementation task covering the whole change
- **Decision:** `tasks.md` holds a single task. It points to design §1–§9 and the test plan, and lists nine scope areas, the INT and E2E obligations, and done-when criteria.
- **Alternatives considered:** Split by component (not allowed: the step requires exactly one task).
- **Decision-bearing:** no

## Post-implementation trim

After the implementation was green, three parts were removed to keep the change small. The project is pre-release, and `docs/writing-workflows.md` already supports editing a versioned workflow file in place.

1. **No copied workflow versions.** The `change-v2.1`, `simple-change-v2.1`, `plan-change-v2.1`, `implement-change-v2.1` and `archive-change-v1.1` copies were deleted, and their behavior moved into the existing `v2.0` / `v1.0` files. Breaking resume of in-flight `v2.0` runs is accepted. `archive-change-v1.0` called with only `change_name` (as the Agent Factory does) behaves as before. The hidden `simple-change-plan-v1.0` child was also removed: `simple-change` no longer declares the OpenSpec engine, because none of its steps is an OpenSpec artifact step, so the engine contributed nothing there.
2. **Simpler external archive.** The snapshot helper, transition record and post-archive verification script were removed. `archive-external.sh` checks its inputs, treats an existing archive with no active directory as a completed archive (resume), fails if both exist, and otherwise runs `openspec validate` and `openspec archive` in the spec root. It still never commits in the spec root. The resolver's `guard` operation went with it.
3. **No leak scanner.** `check-spec-leak.sh` and its steps were removed. The resolver's context instruction tells every agent step, including PR finalization, not to write the spec-root path into commits, PR text or code-repository files. `report-spec-changes.sh` now lists `git status` entries under a Git spec root, or otherwise the change's active or archive directory.
