# external-openspec-root Specification

## Purpose
TBD - created by archiving change feature-227-11f43345. Update Purpose after archive.
## Requirements
### Requirement: OpenSpec root sources and precedence

Agent Runner SHALL determine an OpenSpec project root ("spec root") for each run of a built-in `openspec:*` entry workflow. The spec root is the directory that contains the `openspec/` project directory. The sources SHALL be consulted in this order, and the first one that supplies a value wins:

1. the `spec_root` workflow parameter supplied for the invocation;
2. a user-level mapping in `~/.agent-runner/settings.yaml` from the code repository to a spec root;
3. the run's working directory, normally the code repository root, which is the repository-local default and today's behavior.

The user-level mapping SHALL be the `openspec_roots` key of `~/.agent-runner/settings.yaml`. It is a mapping from an absolute code-repository path to a spec-root path, and a spec-root path may start with `~/`. The repository key SHALL be the root of the repository's main worktree, so every Git worktree of the same repository resolves to the same entry. Outside a Git repository, the key is the working directory. Keys and values SHALL be compared after symlinks are resolved. Entries whose value is not a string SHALL be ignored. A relative `spec_root` parameter SHALL be resolved against the directory the run was started from. The resolved spec root SHALL be absolute and SHALL have symlinks resolved. The spec root is *external* when it is not the run's working directory. Agent Runner SHALL NOT read a spec root from any file inside the code repository.

#### Scenario: Parameter overrides the setting
- **WHEN** the user setting maps the current repository to `/work/specs-a` and the user runs `agent-runner run openspec:change change_name=foo --param spec_root=/work/specs-b`
- **THEN** the run uses `/work/specs-b` as the spec root

#### Scenario: Setting supplies the root when no parameter is given
- **WHEN** the user setting maps the current repository to `/work/other-repo/plugins/foo` and no `spec_root` parameter is supplied
- **THEN** the run uses `/work/other-repo/plugins/foo` as the spec root

#### Scenario: Worktree resolves to the main repository's mapping
- **WHEN** the user setting maps a repository to `/work/specs` and the run starts in a worktree of that repository under `./worktrees/feature-x`
- **THEN** the run uses `/work/specs` as the spec root

#### Scenario: Nothing configured
- **WHEN** no `spec_root` parameter is supplied and the user setting has no entry for the current repository
- **THEN** the spec root is the run's working directory, the spec root is not external, and every OpenSpec path is the repository-local `openspec/` path used today

#### Scenario: Spec root inside another repository's subdirectory
- **WHEN** the resolved spec root is `/work/other-repo/plugins/foo` and `/work/other-repo` is the Git root of that checkout
- **THEN** OpenSpec paths resolve under `/work/other-repo/plugins/foo/openspec/` rather than `/work/other-repo/openspec/`

### Requirement: Spec root resolved once and recorded in run state

The spec root SHALL be resolved exactly once per run, by the outermost built-in `openspec:*` entry workflow the user started. The resolved context SHALL be recorded in run state. It SHALL include at least the code root, the spec root, whether the spec root is external, and the absolute change directory. Nested `openspec:*` workflows invoked from a parent SHALL use the context passed down by the parent and SHALL NOT consult the user setting. On resume, every step SHALL use the recorded context. Changes to the user setting after a run starts SHALL NOT affect that run. The nested `archive-change` workflow does not resolve the root itself: when a caller invokes it with only `change_name`, it keeps its repository-local behavior.

#### Scenario: Nested archive reuses the parent's root
- **WHEN** `openspec:change` resolved an external spec root and later invokes its nested archive workflow
- **THEN** the archive workflow operates on the parent's recorded spec root without reading user settings again

#### Scenario: Setting edited mid-run
- **WHEN** a run of `openspec:change` recorded spec root `/work/specs-a`, the user changes the setting to `/work/specs-b`, and then resumes the run
- **THEN** the resumed run and all later steps use `/work/specs-a`

#### Scenario: Archive invoked directly with only a change name
- **WHEN** a caller invokes `archive-change-v1.0` with only `change_name`
- **THEN** it archives repository-locally and verifies the archive commit, whatever the user setting says

### Requirement: Spec root preflight validation

Before any definition, planning, implementation, or archive work, the entry workflow SHALL validate the resolved spec root. The run SHALL fail fast with a message naming the spec root path and the specific problem in these cases:

- the spec root does not exist or is not a directory;
- the spec root does not contain an `openspec/` project directory.

The message SHALL say whether the root came from the parameter or the user setting. When the spec root is not external, the preflight SHALL keep today's behavior and SHALL NOT newly require `openspec/` to exist before `openspec new change` creates it.

#### Scenario: Configured spec root does not exist
- **WHEN** the user setting maps the repository to `/work/missing` and that path does not exist
- **THEN** the run fails before its first agent step with a message naming `/work/missing`, stating that it does not exist, and naming the user setting as the source

#### Scenario: Configured spec root is not an OpenSpec project
- **WHEN** `--param spec_root=/work/plain-dir` is supplied and `/work/plain-dir/openspec/` does not exist
- **THEN** the run fails before its first agent step with a message stating that `/work/plain-dir` is not an OpenSpec project

#### Scenario: Default root keeps today's preflight
- **WHEN** nothing is configured and the code repository has no `openspec/` directory yet
- **THEN** `openspec:change` proceeds and creates the change exactly as it does today

### Requirement: Change validation per lifecycle operation

When the spec root is external, each entry workflow SHALL check the named change according to its lifecycle operation once the spec root has been validated. When the spec root is not external, today's change checks apply unchanged. The operations are:

- Workflows that create a change (`openspec:change`, `openspec:simple-change`) SHALL fail if the spec root already has an active change directory or a dated archive directory for that change name.
- Workflows that continue an existing change (`openspec:plan-change`, `openspec:implement-change`) SHALL fail if the spec root has no active change directory for that name.
- Archive SHALL require the active change directory, unless a dated archive directory for the change exists and the active directory does not, in which case it SHALL report that the change is already archived and succeed. If both exist, archive SHALL fail.

Each failure message SHALL name the change, the spec root, and the conflicting or missing path.

#### Scenario: New change collides with an active change
- **WHEN** `openspec:change change_name=foo` starts and `<spec_root>/openspec/changes/foo/` exists
- **THEN** the run fails with a message naming `foo` and the existing path

#### Scenario: New change collides with an archived change
- **WHEN** `openspec:change change_name=foo` starts and `<spec_root>/openspec/changes/archive/2026-01-02-foo/` exists
- **THEN** the run fails with a message naming `foo` and the archived path

#### Scenario: Continuing an existing change
- **WHEN** `openspec:implement-change change_name=foo` starts and `<spec_root>/openspec/changes/foo/` exists
- **THEN** preflight passes without a collision error

#### Scenario: Continuing a missing change
- **WHEN** `openspec:plan-change change_name=foo` starts and `<spec_root>/openspec/changes/foo/` does not exist
- **THEN** the run fails with a message naming `foo` and the expected path

#### Scenario: Archive retried after the move completed
- **WHEN** an earlier archive attempt already moved `foo` into `<spec_root>/openspec/changes/archive/` and the run is resumed
- **THEN** archive reports that `foo` is already archived and continues instead of reporting a missing change

### Requirement: Prompts state both roots

When the spec root is external, every agent prompt in the built-in `openspec:*` workflows that reads or writes OpenSpec artifacts SHALL state the code root and the spec root as absolute paths. It SHALL name the absolute change directory as the location for definition and planning artifacts. Prompts for implementation steps SHALL state that code changes, validation, and commits belong in the code root. When the spec root is not external, prompts SHALL keep today's repository-local wording.

#### Scenario: Definition prompt with external root
- **WHEN** the define step of `openspec:change change_name=foo` runs with external spec root `/work/specs`
- **THEN** the agent's prompt names `/work/specs/openspec/changes/foo/` as the artifact location and also names the code root

#### Scenario: Implementation prompt with external root
- **WHEN** an implementation task step runs with an external spec root
- **THEN** the prompt names the task file's absolute path under the spec root and states that code changes and commits belong in the code root

### Requirement: Spec project instructions surfaced

When the spec root is external, the definition, acceptance, and archive prompts in the built-in `openspec:*` workflows SHALL direct the agent to read each of these files that exists under the spec root and to follow it as the spec project's working rules: `AGENTS.md`, `CLAUDE.md`, and `openspec/config.yaml`. Files that do not exist SHALL NOT be mentioned.

#### Scenario: Spec root has AGENTS.md
- **WHEN** `/work/specs/AGENTS.md` exists and the define step runs with spec root `/work/specs`
- **THEN** the define prompt directs the agent to read and follow `/work/specs/AGENTS.md`

#### Scenario: Spec root has no instruction files
- **WHEN** the external spec root has no `AGENTS.md`, `CLAUDE.md`, or `openspec/config.yaml`
- **THEN** prompts do not mention spec-project instruction files

### Requirement: No leakage into the code repository

When the spec root is external, Agent Runner SHALL NOT create an `openspec/` directory in the code repository. It SHALL NOT stage or commit spec-root files in the code repository. Content that Agent Runner itself writes into the code repository or publishes from it, such as PR bodies and the commit messages it generates, SHALL NOT contain the external spec root path or any path under it. Prompts for code-repository commit, repair, validator-fix, and PR steps, including PR finalization, SHALL instruct the agent not to include the external spec root path in commits, PR text, or checked-in files. This rule is enforced by prompt instruction only; Agent Runner does not scan commits or PR text for the path.

#### Scenario: No stray openspec directory
- **WHEN** `openspec:change` runs to completion with an external spec root in a code repository that has no `openspec/` directory
- **THEN** the code repository still has no `openspec/` directory

#### Scenario: PR body omits the external path
- **WHEN** PR finalization creates or updates the pull request for a run with external spec root `/work/private/specs`
- **THEN** the PR body written by Agent Runner does not contain `/work/private/specs`

#### Scenario: Finalize prompt carries the no-path rule
- **WHEN** the PR finalization step runs with external spec root `/work/private/specs`
- **THEN** its prompt instructs the agent never to write the spec-root path into commit messages, the PR title or body, or code-repository files

### Requirement: Spec repository changes reported, never committed

When the spec root is external, Agent Runner SHALL NOT run `git add`, `git commit`, or `git push` in the spec root's repository. When the run completes, including runs that stop after archive or PR finalization, Agent Runner SHALL report which files under the spec root the run changed so the user can commit them. The report SHALL show paths relative to the spec root and SHALL NOT be written into any file committed in the code repository. If the spec root is inside a Git working tree, the report SHALL list the spec-root files with uncommitted changes. Otherwise it SHALL state that the spec root is not under version control and list the change's active directory or dated archive directory, whichever exists.

The report SHALL appear as the output of the workflow's final step.

#### Scenario: Spec repository left uncommitted
- **WHEN** `openspec:change` completes with an external spec root inside a Git checkout
- **THEN** the spec repository's HEAD is unchanged from the start of the run, and the run output lists the changed spec-root files

#### Scenario: Spec root not under version control
- **WHEN** a run completes with an external spec root that is not inside a Git working tree
- **THEN** the run output states that the spec root is not version-controlled and lists the change's archive directory

