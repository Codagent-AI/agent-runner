## Coverage Strategy

Specifications remain the source of unit-test requirements. This plan records only additional integration and end-to-end obligations, the acceptance testing envelope, and exceptional human-only obligations.

Unit tests are expected to cover:
- resolver decision logic: precedence, external detection, lifecycle checks, and instruction text;
- `openspec_roots` parsing and write preservation;
- engine `root_param` handling with a fake `CmdRunner`;
- `workspace_dirs` normalization;
- per-adapter `--add-dir` argument construction.

The obligations below cover the boundaries where those pieces meet real Git, real filesystems, real subprocesses, the embedded workflow set, and the public CLI.

All automated tests run in the existing `test` job (`go test -tags dev_audit ./...`). CI has no real `openspec` binary or agent CLIs, so tests put fake `openspec`, `agent-validator`, and agent executables on `PATH`. The fakes record their working directory and argv. This follows the established pattern in `workflows/archive_test.go` and `workflows/embed_test.go`.

## Integration Tests

### INT-001: Resolver against real Git repositories, worktrees, and settings
- Covers: `external-openspec-root`: OpenSpec root sources and precedence, Spec root preflight validation, Change validation per lifecycle operation.
- Boundary: `agent-runner internal resolve-openspec-root`, real `git` (common dir, worktrees), the real filesystem (symlinks), and `~/.agent-runner/settings.yaml` loaded through `internal/usersettings`.
- Setup: a temporary `HOME`; a code repository with one linked worktree under `worktrees/`; a separate spec repository whose OpenSpec project is in a subdirectory (`plugins/foo/openspec/`); a symlink pointing at the spec subdirectory; a plain directory without `openspec/`.
- Action: invoke the subcommand from the main checkout and from the worktree. Cover: param only; setting only (key given as a symlinked path); both; neither; `~/` and relative param values; nonexistent root; root without `openspec/`; `create` with an active collision and with an archived collision; `continue` with the change missing and present; `guard` with an empty param while the setting maps an external root.
- Assertions:
  - Exit status, and stderr naming the path and the source (`param` or `setting`).
  - The emitted JSON fields: `spec_root` (absolute, symlinks resolved), `external`, `change_dir`, `commit_plan`.
  - The worktree resolves to the main checkout's mapping.
  - The default case emits relative `openspec/changes/<name>` and today's exact location and validate wording.
  - `context_instruction` lists only the instruction files (`AGENTS.md`, `CLAUDE.md`, `openspec/config.yaml`) that exist.
- Execution: `cmd/agent-runner` or `internal/openspecroot` package tests; `go test ./...`.

### INT-002: OpenSpec scripts run against the external root with a real filesystem and Git
- Covers: `builtin-workflows`: OpenSpec workflows use the resolved spec root, OpenSpec plan commit with an external spec root; `external-openspec-root`: No leakage into the code repository.
- Boundary: `create-change.sh`, `validate-change.sh`, `core/validate-planning-artifacts.sh`, and the `core/plan-change` `commit-plan` skip. These run as subprocesses against real temporary Git repositories, with a fake `openspec` that records its cwd and creates `openspec/changes/<name>` relative to that cwd.
- Setup: a code repository on a feature branch; an external spec root inside a separate Git repository; a fake `agent-validator` (`detect` exits 2).
- Action: run each script with external inputs. Then run each script again with no new inputs, which is the default regression path.
- Assertions:
  - With an external root, the fake `openspec` ran in the spec root.
  - `agent-validator detect` ran in the code repository.
  - The change directory exists under the spec root.
  - The code repository has no `openspec/` directory, and its HEAD and index are unchanged.
  - `validate-planning-artifacts.sh` accepts a matching absolute pair and rejects a mismatched pair.
  - With the default root, output, paths, and the plan commit are identical to the current behavior.
- Execution: `workflows/*_test.go` (for example a new `external_root_test.go`); `go test ./workflows`.

### INT-003: External archive transition and verification
- Covers: `builtin-workflows`: OpenSpec archive with an external spec root; `external-openspec-root`: Change validation per lifecycle operation (archive retry).
- Boundary: `archive-external.sh` and `verify-archive-external.sh` run as subprocesses with a fake `openspec` that moves the change into `archive/<date>-<name>` and rewrites a canonical spec file. Both a Git and a non-Git spec root are used.
- Setup: a spec root holding an active change, `openspec/specs/` (some files the fake rewrites, some it leaves unchanged, one it deletes, and one with a pre-existing uncommitted edit), and unrelated files; a session directory.
- Action, eight cases:
  - successful transition and verification;
  - verification with the active directory left behind;
  - an unrelated spec-root file modified between transition and verification;
  - a rerun after the transition completed (simulating resume);
  - an interrupted transition: the record is written and the fake crashes before moving, then a rerun;
  - a fresh session against a pre-existing dated archive for the same name, with no active directory;
  - a fresh session with the active directory missing;
  - the same journey with a non-Git spec root.
- Assertions:
  - Success leaves exactly one archive directory and no active directory, and neither repository's HEAD or index changes.
  - A leftover directory fails, naming it.
  - A foreign modification fails, naming the file.
  - The rerun after a completed move skips `openspec archive` (the fake records zero additional calls) and passes.
  - The interrupted case re-runs validate and archive once and passes.
  - Both fresh-session cases fail before any `openspec` call, naming the pre-existing archive or the missing active directory.
  - A record whose `spec_root` or `change_name` differs from the inputs fails.
  - `canonical_changes` lists exactly the added, modified, and deleted canonical files relative to the spec root. Unchanged files and the pre-existing edit are excluded.
  - The existing `archive_test.go` cases for `archive-transition.sh` and `verify-archive-commit.sh` still pass unchanged.
- Execution: `workflows/archive_test.go` or a sibling file; `go test ./workflows`.

### INT-004: Spec-change report reads but never writes the spec repository
- Covers: `external-openspec-root`: Spec repository changes reported, never committed.
- Boundary: `report-spec-changes.sh`, real Git in a spec repository whose spec root is a subdirectory, and a non-Git spec root.
- Setup: a spec repository with tracked, modified, and untracked files inside the spec subdirectory and outside it; for the non-Git case, a record produced by `verify-archive-external.sh` with added, modified, and deleted canonical specs.
- Action: run the report for the Git subdirectory root and for the non-Git root.
- Assertions:
  - Listed paths are relative to the spec root and exclude files outside it.
  - The non-Git output states that the root is not version-controlled and lists the archive directory and each added, modified, and deleted canonical spec file, but not unchanged ones.
  - The spec repository's HEAD, index, and refs are byte-identical before and after.
- Execution: `workflows/*_test.go`; `go test ./workflows`.

### INT-005: Engine binds the spec root and runs `openspec` there, including through nested workflows
- Covers: `openspec-engine`: Engine configuration, OpenSpec CLI working directory.
- Boundary: the `realCmdRunner` subprocess with a fake `openspec` on `PATH` that records its cwd and emits `status` and `instructions` JSON. Also the exec package's sub-workflow preparation and agent-prompt construction, with a recording process runner standing in for the agent CLI.
- Setup: a temporary spec root, and fixture workflows:
  - an engine-bearing child (`root_param: spec_root`) that runs an agent step whose ID is an artifact and invokes a nested sub-workflow without `spec_root` that also runs an artifact step;
  - the same child invoked with `spec_root` missing, empty, and pointing at a nonexistent directory;
  - an engine without `root_param`.
- Action: execute the fixtures through the exec package.
- Assertions:
  - Every recorded `openspec` cwd equals the spec root, including the nested workflow's enrichment call.
  - The enrichment `<output_path>` is under the spec root.
  - Each invalid-context case fails the child workflow's start with an error naming `spec_root`, and both the fake `openspec` and the recording agent runner register zero calls.
  - Without `root_param`, the cwd is the process cwd.
- Execution: `internal/engine/openspec` and `internal/exec` tests; `go test ./internal/...`.

### INT-006: `workspace_dirs` flows from workflow YAML to the spawned CLI's argv
- Covers: `sub-workflows`: Workflow workspace directories; `cli-adapter`: Additional workspace directories.
- Boundary: the loader, sub-workflow context creation, the agent, `call_agent`, and external-user executors, and the real adapters' argument construction, with a recording process runner standing in for the CLI.
- Setup: fixture workflows covering:
  - a parent with `workspace_dirs: ["{{spec_root}}"]` invoking a child without the field, which runs an agent step;
  - a top-level workflow with `workspace_dirs: ["{{openspec.spec_root}}"]` whose first script step captures that map;
  - a nested child that adds a second directory;
  - a value equal to the project root;
  - a nonexistent directory.

  Profiles for Claude, Codex (headless, conservative), Copilot, OpenCode, and Cursor.
- Action: execute each fixture through the exec package with each profile.
- Assertions:
  - Claude, Copilot, and Codex argv contain `--add-dir <dir>` per normalized directory. For Codex it is a top-level option alongside `--sandbox workspace-write`, and no broader sandbox flag appears.
  - OpenCode argv is identical to a run without directories.
  - Cursor fails before spawn, naming the CLI and the directory.
  - A directory equal to the project root yields argv identical to today's.
  - A nonexistent directory fails the first evaluating step before any spawn or child step.
  - The capture-based value is applied to later agent steps.
  - `call_agent` children receive the caller's directories.
  - Resume re-derives the same list.
- Execution: `internal/exec` tests; `go test ./internal/exec/...`.

### INT-007: Embedded OpenSpec workflow set loads, validates, and wires the context
- Covers: `builtin-workflows`: OpenSpec workflows use the resolved spec root, OpenSpec entry workflows keep their top-level phases, Engine-bearing OpenSpec workflows receive the root before engine startup; `external-openspec-root`: Prompts state both roots, Spec project instructions surfaced, No leakage into the code repository.
- Boundary: the embedded FS, the loader, workflow-version discovery, and pre-validation (`agent-runner validate`) over the real built-in YAML.
- Setup: none beyond the embedded set.
- Action: discover and pre-validate `openspec:change`, `simple-change`, `plan-change`, and `implement-change`. Load `simple-change-plan-v1.0` and `archive-change-v1.1`.
- Assertions:
  - Discovery resolves each entry to `v2.1`.
  - Each entry's top-level step IDs equal its `v2.0` step IDs, with `resolve-openspec-root` first and `check-spec-leak` and `report-spec-changes` last.
  - No entry declares an engine. Only the hidden `simple-change-plan` child does, with `root_param: spec_root`, and it is not listed.
  - The new files contain no literal `openspec/changes/`.
  - Every agent and `repair:` prompt that references `change_dir` or `task_file`, or that commits or edits PR text, interpolates `{{context_instruction}}`. This includes `run-validator`'s `fix-violations`, `verify-change`'s `open-draft-pr`, and both archive repairs. Every caller forwards the param.
  - `archive-change-v1.0.yaml` and the `v2.0` entries are byte-identical to their pre-change contents.
- Execution: `workflows/embed_test.go`; `go test ./workflows`.

### INT-008: Rendered prompts and the leak guard with external context
- Covers: `external-openspec-root`: Prompts state both roots, Spec project instructions surfaced, No leakage into the code repository.
- Boundary:
  - the real `run-validator` and `archive-change-v1.1` workflows executed through the exec package, with a recording agent runner;
  - `check-spec-leak.sh` run as a subprocess against a real Git repository with a local bare remote and a recording fake `gh` that serves a configurable PR title and body and records edits.
- Setup:
  - an external spec root containing `AGENTS.md`;
  - resolver output used as `context_instruction`;
  - validator results that force the `fix-violations` repair;
  - a failing archive transition that forces the archive repair;
  - branches whose commit messages, diffs, PR title, or PR body do or do not contain the spec root.
- Action: run the validator repair, the archive repair, and the leak guard over each branch and PR fixture, including a PR body whose runner-generated block is replaced while human text outside the markers is preserved.
- Assertions:
  - The rendered `fix-violations` and archive-repair prompts contain both roots, the `AGENTS.md` path, and the no-leak instruction.
  - The archive repair lists only the change directory as editable.
  - The guard passes on clean fixtures and fails on each leaking fixture, naming the commit SHA, the file path, `PR title`, or `PR body`.
  - The guard never runs a Git command that writes and never force-pushes.
- Execution: `internal/exec` and `workflows/*_test.go`; `go test ./...`.

## End-to-End Tests

### E2E-001: Preflight through the public CLI
- Covers: `external-openspec-root`: OpenSpec root sources and precedence, Spec root resolved once and recorded in run state, Spec root preflight validation.
- Surface: `agent-runner run openspec:change change_name=foo [--param spec_root=…] --until resolve-openspec-root`, and `--until create`.
- Setup: a testscript with `HOME` set to the work directory; a Git code repository on a feature branch; `.agent-runner/settings.yaml` under `HOME` mapping the repository; an external spec root with `openspec/`; fakes for `openspec` and `agent-validator`.
- Journey:
  1. Run with the setting pointing at a missing directory.
  2. Run with a param pointing at a directory without `openspec/`.
  3. Run with a valid setting.
  4. Run with a param overriding the setting.
  5. Run with a valid setting and `--until create`.
- Assertions:
  - Runs 1 and 2 exit non-zero before any agent step, with messages naming the path and the source.
  - Runs 3 and 4 exit 0 with the `--until` stop.
  - The run's `state.json` records the captured `openspec` map with the expected `spec_root` (the setting's in run 3, the param's in run 4).
  - Run 5 stops after the top-level `create` step, the fake `openspec` logs `new change foo` in the spec root, and the change directory exists there.
  - No `openspec/` directory appears in the code repository.
- Execution: `cmd/agent-runner/testdata/scripts/openspec_external_root_preflight.txtar`; `go test ./cmd/agent-runner -run TestScript`.

### E2E-002: Resume keeps the recorded root after the setting changes
- Covers: `external-openspec-root`: Spec root resolved once and recorded in run state.
- Surface: `agent-runner run openspec:change change_name=foo`, then `agent-runner -resume <run-id>`.
- Setup: as in E2E-001. The repository starts on its default branch, so the top-level `validate-feature-branch` step fails. A fake `openspec` logs its cwd. A fake agent CLI configured in the test profile exits non-zero, so the run stops at the first agent step.
- Journey:
  1. Start the run with the setting pointing at spec root A; it fails at feature-branch validation.
  2. Switch to a feature branch.
  3. Repoint the setting to spec root B.
  4. Resume.
- Assertions:
  - On resume, the fake `openspec` logs `new change foo` with cwd A.
  - `A/openspec/changes/foo/` exists and B is untouched.
  - The resolver is not re-run (the state shows the capture restored).
  - The run then stops at the fake agent step.
- Execution: `cmd/agent-runner/testdata/scripts/openspec_external_root_resume.txtar`; `go test ./cmd/agent-runner -run TestScript`.

### E2E-003: External archive through a workflow, and the direct-call guard
- Covers: `builtin-workflows`: OpenSpec archive with an external spec root, the latest archive invoked directly without a recorded root; `external-openspec-root`: Spec repository changes reported, never committed.
- Surface: `agent-runner run` on a project fixture workflow that invokes `builtin:openspec/archive-change-v1.1.yaml` and then the report script, the same nesting the entry workflows use.
- Setup:
  - code and spec Git repositories with recorded HEADs;
  - a fake `openspec` that archives;
  - a fake `agent-validator`;
  - a second fixture workflow that calls `archive-change-v1.1` with only `change_name` while the setting maps an external root.
- Journey: run the archive fixture with `spec_root` and `spec_external=true`; run it again in a new run after creating a fresh active change while the earlier archive exists, plus a stale dated archive of the same name; then run the guard fixture.
- Assertions:
  - The first run exits 0.
  - The spec root has exactly one archive directory and no active directory.
  - Both repositories keep their HEADs.
  - The code repository has no `openspec/` directory.
  - Step output lists the changed spec-root files relative to the spec root.
  - The second run fails before `openspec archive`, naming the pre-existing archive directory.
  - The guard run exits non-zero with "recorded OpenSpec root context is missing", and the fake `openspec` records no `archive` call.
- Execution: `cmd/agent-runner/testdata/scripts/openspec_external_root_archive.txtar`; `go test ./cmd/agent-runner -run TestScript`.

## Acceptance Testing Envelope

- **Environments and sandboxes:**
  - The local machine, working only under `{{session_dir}}/scratch/acceptance-test`.
  - Throwaway Git repositories created there: a code repository plus a separate spec repository whose OpenSpec project lives in a subdirectory.
  - Run the CLI from source with `./dev.sh`.
  - Set `HOME` to a scratch directory so that `~/.agent-runner/settings.yaml` is a scratch file.
  - The real `openspec` CLI is installed (`/opt/homebrew/bin/openspec`) and may be used against the scratch projects.
- **Credentials and secrets:** the agent CLIs (Claude, Codex) and their existing local logins may be used for headless agent steps in scratch repositories. No other credentials are needed.
- **Authorized effects:**
  - Local file and Git operations inside the scratch directory only, including pushes to a scratch local bare remote.
  - Agent CLI usage at normal cost, kept to a few runs.
  - Scratch repositories may be deleted afterwards.
- **Off limits:**
  - Pushing to any network remote and creating or editing real GitHub pull requests. `verify-change`'s `open-draft-pr` pushes and creates the PR during `implement`, so stopping before `finalize` is not enough. Either cap runs with `--until plan`, or give the scratch code repository a local bare repository as `origin` and put the recording fake `gh` first on `PATH`.
  - The user's real `~/.agent-runner/settings.yaml`, real spec repositories, and this repository's own `openspec/` tree.
  - `/tmp`, `/private/tmp`, and `$TMPDIR` for working files.
- **Permitted substitutes:**
  - Fake `agent-validator` and fake agent CLIs, when a real one would need an interactive human or the step is not under test.
  - `--until` and resume to reach later steps without full interactive phases.
  - A fixture workflow that invokes `builtin:openspec/archive-change-v1.1.yaml` directly, to exercise archive without a full lifecycle.
  - A local bare repository as `origin` and a recording fake `gh` (writes received titles and bodies to the scratch directory), so implementation and finalization can run end to end. Inspect the recorded PR title and body and `git log` on the bare remote for the spec-root path.
- **Known risk areas:**
  - Resume and retry around archive (prior defect cluster in `archive_test.go`).
  - Codex `--add-dir` placement and flag drift across installed CLI versions.
  - Leakage of the external path into commits or PR text. Prompts prevent it and the leak guard detects it after publication; a pushed commit message cannot be repaired automatically.
  - Worktree key resolution.
  - Default-path regressions in `commit-change-plan.sh` and `validate-planning-artifacts.sh`.
  - Accepted limitations: Cursor fails fast with an external root; older `v2.0` and `archive-change-v1.0` keep repository-local behavior; no spec-repository commits.

## Human-Only Testing

### HT-001: Interactive external-root lifecycle at a real terminal
- **Reason:** the definition and acceptance phases of `openspec:change` and `openspec:simple-change` are interactive agent conversations. The repository guidance states that flows which launch a run and need a real conversation with an agent require a human at a real terminal, because a synthetic PTY cannot drive them.
- **Prerequisites:** all INT and E2E tests pass, and the exploratory acceptance pass has covered preflight, resume, archive, and the report with substitutes.
- **Instructions:**
  1. In a scratch code repository on a feature branch, with a scratch local bare repository as `origin` and the recording fake `gh` first on `PATH`, map it to a scratch spec root through `~/.agent-runner/settings.yaml`. The spec root should be a subdirectory of another Git repository and contain an `AGENTS.md`.
  2. Run `./dev.sh run openspec:simple-change change_name=demo` with a Claude lead and a Codex implementor, and answer the planning conversation briefly.
  3. Let the run implement and archive.
- **Required decision or observation:** confirm each of the following:
  - The agent mentioned and followed the spec root's `AGENTS.md`.
  - Planning artifacts were written under the spec root without permission prompts for that directory beyond the normal ones.
  - Code commits landed only in the code repository, which contains no `openspec/` directory and no mention of the spec path.
  - The change was archived under the spec root.
  - The recorded PR title and body and the pushed commit messages do not contain the spec path, and `check-spec-leak` passed.
  - The final step listed the changed spec files, and the spec repository has no new commits.

## Coverage Map

| Requirement or journey | INT | E2E | HT |
| --- | --- | --- | --- |
| external-openspec-root: OpenSpec root sources and precedence | INT-001 | E2E-001 | — |
| external-openspec-root: Spec root resolved once and recorded in run state | — | E2E-001, E2E-002 | — |
| external-openspec-root: Spec root preflight validation | INT-001 | E2E-001 | — |
| external-openspec-root: Change validation per lifecycle operation | INT-001, INT-003 | — | — |
| external-openspec-root: Prompts state both roots | INT-001, INT-007, INT-008 | — | HT-001 |
| external-openspec-root: Spec project instructions surfaced | INT-001, INT-007, INT-008 | — | HT-001 |
| external-openspec-root: No leakage into the code repository | INT-002, INT-007, INT-008 | E2E-001, E2E-003 | HT-001 |
| external-openspec-root: Spec repository changes reported, never committed | INT-004 | E2E-003 | HT-001 |
| builtin-workflows: OpenSpec workflows use the resolved spec root | INT-002, INT-007 | E2E-003 | HT-001 |
| builtin-workflows: OpenSpec plan commit with an external spec root | INT-002 | — | — |
| builtin-workflows: OpenSpec archive with an external spec root | INT-003 | E2E-003 | HT-001 |
| builtin-workflows: OpenSpec entry workflows keep their top-level phases | INT-007 | E2E-001, E2E-002 | — |
| builtin-workflows: Engine-bearing workflows receive the root before engine startup | INT-005, INT-007 | — | — |
| openspec-engine: Engine configuration / OpenSpec CLI working directory | INT-005 | — | — |
| sub-workflows: Workflow workspace directories | INT-006 | — | — |
| cli-adapter: Additional workspace directories | INT-006 | — | HT-001 |
