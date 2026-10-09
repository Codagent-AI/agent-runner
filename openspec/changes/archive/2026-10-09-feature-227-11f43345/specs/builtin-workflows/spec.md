## ADDED Requirements

### Requirement: OpenSpec workflows use the resolved spec root

The latest version of each built-in `openspec:*` workflow (`change`, `simple-change`, `plan-change`, `implement-change`, and the nested `archive-change`) SHALL derive every OpenSpec path from the run's recorded spec root. Those paths are the change directory, task files, the archive directory, and canonical specs. Every `openspec` CLI invocation these workflows make (`new`, `validate`, `status`, `instructions`, `archive`) SHALL run with the spec root as its working directory. When the spec root is external, no step SHALL read, write, or validate the code repository's `openspec/` paths. Implementation, validator, Git, and PR steps SHALL continue to run in the code repository. Older embedded versions SHALL keep their existing repository-local behavior. They remain for resuming saved runs and for callers that reference an exact version. When the latest `archive-change` is invoked without a spec root while the user setting maps the repository to an external spec root, it SHALL fail before archiving, as required by `external-openspec-root`. `openspec:scaffold` is excluded because it creates a new OpenSpec project in the working directory.

#### Scenario: Change created in the external root
- **WHEN** `openspec:change change_name=foo` runs with external spec root `/work/specs`
- **THEN** the change is created at `/work/specs/openspec/changes/foo/` and the code repository gains no `openspec/changes/foo/`

#### Scenario: Validation runs against the external project
- **WHEN** a step of an `openspec:*` workflow runs `openspec validate --type change foo` with external spec root `/work/specs`
- **THEN** the command runs with `/work/specs` as its working directory and validates against `/work/specs/openspec/specs/`

#### Scenario: Task loop reads tasks from the external root
- **WHEN** `openspec:implement-change change_name=foo` runs with external spec root `/work/specs`
- **THEN** the task loop iterates over task files under `/work/specs/openspec/changes/foo/tasks/` while implementation commits land in the code repository

#### Scenario: Default root is unchanged
- **WHEN** an `openspec:*` workflow runs with no spec root configured
- **THEN** it uses `openspec/changes/<change>/`, `openspec/changes/archive/`, and `openspec/specs/` relative to the code repository, and runs `openspec` there, exactly as before this change

#### Scenario: Latest archive invoked directly without a recorded root
- **WHEN** the latest `archive-change` is invoked with only `change_name`, and the user setting maps the repository to external spec root `/work/specs`
- **THEN** it fails before running `openspec archive`, with a message stating that the recorded OpenSpec root context is missing

### Requirement: OpenSpec plan commit with an external spec root

When the spec root is external, the plan-commit step of the built-in OpenSpec workflows SHALL NOT stage or commit the change directory or `openspec/config.yaml` in the code repository. It SHALL NOT commit in the spec repository. It SHALL succeed without creating a code-repository commit for planning artifacts. When the spec root is not external, the plan-commit step SHALL keep today's behavior.

#### Scenario: Plan committed with an external root
- **WHEN** planning finishes for change `foo` with an external spec root
- **THEN** the plan-commit step succeeds, the code repository's HEAD does not change for planning artifacts, and the spec repository's HEAD does not change

#### Scenario: Plan committed with the default root
- **WHEN** planning finishes for change `foo` with no spec root configured
- **THEN** the plan-commit step commits `openspec/changes/foo/` in the code repository as it does today

### Requirement: OpenSpec archive with an external spec root

When the spec root is external, the archive workflow SHALL validate and archive the change by running `openspec` in the spec root. It SHALL verify the outcome against the spec root's filesystem. Verification SHALL fail if the active change directory still exists, if there is not exactly one dated archive directory for the change, or if the transition modified files under the spec root outside the change's archive directory and `openspec/specs/`. Archive SHALL NOT create, verify, or require a commit in either repository. Its repair prompts SHALL name the absolute spec-root paths that the agent may edit. A fresh archive invocation SHALL require the active change directory and SHALL fail if a dated archive directory for that change already exists, so it never adopts an archive it did not create. Only a retry or resume of the same run that recorded the start of the transition MAY proceed without the active change directory. Such a retry SHALL pass verification without re-running `openspec archive` when the move already completed, and SHALL re-run validate and archive when the move had not happened yet. Verification SHALL record which canonical spec files under `openspec/specs/` the transition added, modified, or deleted. When the spec root is not external, archive SHALL keep today's code-repository commit verification.

#### Scenario: External archive succeeds without a commit
- **WHEN** archive runs for change `foo` with external spec root `/work/specs` and `openspec archive` moves the change
- **THEN** `/work/specs/openspec/changes/foo/` no longer exists, exactly one `/work/specs/openspec/changes/archive/<date>-foo/` exists, the step succeeds, and neither repository gains a commit

#### Scenario: External archive left the active directory behind
- **WHEN** archive runs with an external spec root and the active change directory still exists after the transition
- **THEN** verification fails with a message naming the leftover directory

#### Scenario: Fresh archive refuses a pre-existing archive
- **WHEN** a new run invokes archive for `foo` with external spec root `/work/specs`, and `/work/specs/openspec/changes/archive/2025-12-01-foo/` already exists
- **THEN** archive fails before running `openspec archive`, naming the existing archive directory

#### Scenario: Retry after an interrupted transition
- **WHEN** a run's external archive recorded its start, `openspec archive` moved `foo`, and the process stopped before verification, and the run is then resumed
- **THEN** archive does not re-run `openspec archive`, verification succeeds, and the record lists the canonical spec files that changed

#### Scenario: External archive repair targets spec-root paths
- **WHEN** the archive transition fails with an external spec root `/work/specs` and the repair agent is invoked
- **THEN** the repair prompt limits edits to `/work/specs/openspec/changes/<change>/`, forbids edits under `/work/specs/openspec/specs/`, and names the spec project's instruction files

### Requirement: Engine-bearing OpenSpec workflows receive the root before engine startup

No user-facing `openspec:*` entry workflow SHALL declare the OpenSpec engine at its top level. Engine-dependent steps SHALL run inside a hidden child workflow that declares the engine. The entry workflow invokes that child after it has resolved and recorded the spec root, passing the resolved `spec_root` as a parameter. The user-facing logical workflow names SHALL remain unchanged.

#### Scenario: Engine validation sees the external root at startup
- **WHEN** `openspec:simple-change change_name=foo` starts with only the user setting pointing at external spec root `/work/specs`
- **THEN** the OpenSpec engine's first `openspec status` call runs in `/work/specs`, not in the code repository

#### Scenario: User-facing name unchanged
- **WHEN** a user lists workflows after this change
- **THEN** `openspec:simple-change` is still listed under that name, and its engine-bearing inner workflow is not listed

### Requirement: OpenSpec entry workflows keep their top-level phases

The latest `openspec:*` entry workflows SHALL keep their existing lifecycle phases as top-level steps with their existing step IDs, so that `--until <phase>` and capped resume behave as in the previous version. They SHALL add a first top-level step, `resolve-openspec-root`, and closing top-level steps, `check-spec-leak` and `report-spec-changes`, which run only for an external spec root.

#### Scenario: Run capped at plan
- **WHEN** a user runs `agent-runner run openspec:change change_name=foo --until plan` with or without an external spec root
- **THEN** the run resolves the root, executes the phases through `plan`, and stops successfully without dispatching `implement`

#### Scenario: Resolver runs before every other step
- **WHEN** any latest `openspec:*` entry workflow starts
- **THEN** its first executed step is `resolve-openspec-root`, and no agent step runs before it completes
