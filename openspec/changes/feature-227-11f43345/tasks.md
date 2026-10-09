# Tasks

- [ ] 1. Support an OpenSpec project outside the working repository

## 1. Support an OpenSpec project outside the working repository

Implement the whole change as specified. Read these first; they are the source of truth:

- `proposal.md`
- `design.md`, Approach §1–§9
- `test-plan.md`
- `decisions.md`, especially the `approach-review` section, which supersedes earlier wrapper/body decisions
- every spec under `specs/`: `external-openspec-root`, `builtin-workflows`, `openspec-engine`, `sub-workflows`, and `cli-adapter`

Use TDD. For each behavior, write the failing unit or integration test first, then the code. Use the fake `openspec`, `agent-validator`, agent-CLI, and `gh` executables on `PATH` described in `test-plan.md`. Do not add CI jobs.

### Scope

1. **Settings.** `internal/usersettings` parses `openspec_roots` (a map from repository path to spec-root path). Values that are not strings are ignored, and the key is preserved when other settings are written.
2. **Resolver.** Create a new package `internal/openspecroot`, plus the subcommand `agent-runner internal resolve-openspec-root` in `cmd/agent-runner/internal_cmd.go`. Implement design §2:
   - precedence: param, then setting, then the working directory;
   - repository key from the main worktree (via the Git common dir);
   - `~/` and relative-path expansion, with symlinks resolved;
   - external-root validation;
   - the `create`, `continue`, and `guard` lifecycle checks;
   - JSON output: `spec_root`, `external`, `source`, `change_dir`, `location_instruction`, `validate_instruction`, `context_instruction`, and `commit_plan`;
   - when the root is not external, the exact wording and paths used today.
3. **Workspace directories.**
   - Add a workflow-level `workspace_dirs` field (`model.Workflow`) and `ExecutionContext.WorkspaceDirs`.
   - Evaluate the field lazily against params and captures when an agent, `call_agent`, or external-user turn is about to start, and when a sub-workflow step starts. Children inherit the result.
   - Normalize: drop empty values and values inside the project root, resolve symlinks, dedupe, and require absolute paths to existing directories. A failure stops the step before any spawn.
   - Add `cli.BuildArgsInput.AdditionalDirs` and the `AdditionalDirSupporter` capability. Claude, Codex (top-level option next to `--sandbox`), and Copilot emit `--add-dir`. OpenCode is unconfined. Cursor is unsupported and fails fast.
   - Confirm each flag against the installed CLI's `--help`.
4. **Engine.**
   - Change `CmdRunner.Run` to `Run(dir, args)`, and add the optional `root_param`.
   - Add the optional `engine.ContextBinder` (`BindContext`, `Bound`). Call it in `prepareSubWorkflow` after `engine.Create`, and in `runner.go` before `ValidateWorkflow`, at start and on resume.
   - Inherited engines keep the binding.
   - `buildAgentPrompt` refuses to spawn when the engine is unbound.
   - Without `root_param`, behavior is unchanged.
5. **Core workflows** (edit in place, additive only). Add an optional `context_instruction` param (default `""`) to `define-change`, `plan-change`, `review-tasks`, `implement-change`, `implement-task`, `run-validator`, `verify-change`, `accept-change`, `complete-simple-change`, and `finalize-pr`.
   - Append it to every agent and `repair:` prompt that touches change artifacts or task files, commits, or edits PR text.
   - Forward it through every nested call.
   - Also add `spec_root` and `commit_plan` to `plan-change` (the `commit-plan` `skip_if` from design §4), and `spec_root` to `implement-change`'s `check-plan`.
   - In `validate-planning-artifacts.sh`, add an optional `spec_root`, compare directories by realpath, and run `openspec validate` in the spec root.
6. **OpenSpec workflows.** Add new versions `change-v2.1`, `simple-change-v2.1`, `plan-change-v2.1`, and `implement-change-v2.1`, plus `archive-change-v1.1` and the hidden `simple-change-plan-v1.0`, following design §3.
   - Keep the top-level step IDs as today. `resolve-openspec-root` comes first; `check-spec-leak` and `report-spec-changes` come last and are skipped unless the root is external.
   - Declare `workspace_dirs: ["{{openspec.spec_root}}"]`.
   - Replace every `openspec/changes/...` literal with values from `{{openspec.*}}`.
   - Only the hidden child declares the engine, with `root_param: spec_root`.
   - Leave every `v2.0` file and `archive-change-v1.0.yaml` byte-identical.
   - Update the `create-change.sh` and `validate-change.sh` inputs (optional `spec_root` and `spec_external`).
7. **External archive** (design §4):
   - `archive-external.sh` writes the atomic, run-scoped transition record with `canonical` and `other` manifests. A fresh attempt requires the active directory and refuses a pre-existing archive. A retry is allowed only with this run's matching record.
   - `verify-archive-external.sh` checks that the active directory is gone, that exactly one new archive exists, and that `other` is unchanged. It records the canonical spec files that were added, modified, or deleted.
   - The repair prompt is narrow, plus `{{context_instruction}}`.
   - The guard step handles direct calls.
8. **Report and leak guard.** Add `report-spec-changes.sh` (design §8; read-only Git) and `check-spec-leak.sh` (design §9: commit messages, diff, and PR title and body; names each location; never rewrites history).
9. **Docs.** Document `openspec_roots` and `--param spec_root=` in the user docs under `docs/`, using placeholder paths only.

### Tests (from `test-plan.md`)

- **Unit tests** for all of the logic above.
- **INT-001 to INT-008:**
  - INT-001: resolver
  - INT-002: scripts
  - INT-003: external archive, including a fresh run against a pre-existing archive, an interrupted transition, and deleted canonical specs
  - INT-004: report
  - INT-005: engine binding through exec, with zero spawns on invalid context
  - INT-006: `workspace_dirs` to argv
  - INT-007: embedded workflow wiring and unchanged old versions
  - INT-008: rendered prompts and the leak guard with a recording `gh` and a local bare remote
- **E2E-001 to E2E-003:** testscript `.txtar` files under `cmd/agent-runner/testdata/scripts/`.

### Done when

- Every scenario in the five specs under `specs/` is implemented and covered by a passing test at the layer the test plan assigns.
- With nothing configured, OpenSpec workflow paths, commits, and adapter args are identical to today. Existing tests pass unchanged except for expected version references.
- `make fmt`, `make lint`, and `make test` pass, and `openspec validate --type change feature-227-11f43345 --strict` passes.
