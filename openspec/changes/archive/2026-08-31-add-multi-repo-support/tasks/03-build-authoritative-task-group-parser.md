# Task: Build the Authoritative Task-Group Parser

## Goal

Create the single Runner-owned parser and resolver for configured-workspace task groups, implicit full-change task files, and implicit simple-change plans. It must derive deterministic repository ownership and canonical task inputs, validate all approved plan shapes, and provide the normalized snapshot and fingerprint required for safe repository resume before any state or workflow code consumes that contract.

## Background

You MUST read:

- `openspec/changes/add-multi-repo-support/design.md`, especially **Repository Selection and Task Groups**, **Persistence and Resume**, **Strict `tasks.md` groups rather than a second manifest**, and **Migration and Compatibility**.
- `openspec/changes/add-multi-repo-support/specs/multi-repository-workspaces/spec.md`, especially **Planned task groups**.
- `openspec/changes/add-multi-repo-support/specs/recursive-state/spec.md`, especially the task-group drift portion of **Repository selection persisted**.
- `openspec/changes/add-multi-repo-support/specs/builtin-workflows/spec.md`, especially ordered affected repositories and repository task resolution.

Relevant implementation seams:

- Add one focused Runner-owned Go package under `internal/` for every approved task-plan shape. Do not create separate OpenSpec and spec-driven parsers, and do not leave a second ownership parser in shell.
- For configured workspaces, parse the authoritative `tasks.md` headings `## Repository: <name>` in document order. Require each repository to occur in one contiguous group and every checkbox link to resolve beneath `tasks/<repository-name>/`.
- For implicit full changes, retain flat non-empty `tasks/*.md` files in numeric filename order. For implicit simple changes, retain monolithic `tasks.md` as the implementation input. Never require user-authored artifacts to contain the internal name `default`.
- Validate every linked task as a safe workspace-owned path. Reject unknown owners, repeated groups, cross-group links, duplicate links, unlinked files, empty files, unsafe traversal/absolute links, malformed headings, empty groups, and any task owned by more than one repository.
- Return ordered affected repository names, ordered canonical absolute literal-safe task paths (or an existing-loop-compatible canonical pattern) for one active repository, and a normalized group snapshot plus stable fingerprint that captures repository order, ownership, and task set.
- Expose the parser to shipped scripts through a new `agent-runner internal task-groups` command in `cmd/agent-runner/internal_cmd.go`, while runner/resume code calls the same Go package directly. The command accepts canonical `--workspace-dir` and `--change-dir`, `--plan-kind full|simple`, an optional `--repository`, and `--output repositories|task-pattern`. `repositories` prints the trimmed ordered comma-separated selection; `task-pattern` requires `--repository` and prints only that repository's canonical literal-safe loop input. Diagnostics go to stderr and every invalid plan exits non-zero without partial stdout. Snapshot/fingerprint values remain typed Go results for persistence rather than being reparsed from shell output.
- Replace the task ownership/link/filesystem portion of `workflows/core/validate-planning-artifacts.sh` with delegation to this parser. The shell script may retain unrelated planning-artifact checks but must not reinterpret task shape.
- Place unit and filesystem integration tests beside the new package and update `workflows/` script tests for delegation. The parser is a prerequisite contract for task-group drift revalidation, so its public internal API must be usable without importing workflow packages.

## Spec

The following approved requirements and scenarios are binding.

### Requirement: Planned task groups

A multi-repository change plan MUST contain an ordered list of task groups. Each task group MUST target exactly one configured repository, MUST contain all tasks assigned to that repository, and a repository MUST NOT appear in more than one task group for the change. One Runner-owned Go parser MUST be authoritative for validating the index, deriving ordered ownership, producing task patterns, and comparing task-group identity on resume. It MUST support grouped configured-workspace plans, flat implicit full-change plans, and monolithic implicit simple-change plans.

#### Scenario: Valid task groups
- **WHEN** an approved plan contains a `backend` task group followed by a `frontend` task group
- **THEN** the affected repository order is `backend`, then `frontend`, and tasks retain their order within each task group

#### Scenario: Task without repository ownership
- **WHEN** an implementation task does not belong to a task group targeting exactly one configured repository
- **THEN** plan validation fails before repository execution begins

#### Scenario: Unknown task-group repository
- **WHEN** a task group targets a repository not declared by the workspace
- **THEN** plan validation fails before repository execution begins

#### Scenario: Repeated repository task group
- **WHEN** more than one task group targets the same repository
- **THEN** plan validation fails rather than permitting interleaved repository execution

#### Scenario: Simple-change task groups
- **WHEN** a multi-repository `simple-change` plan is approved
- **THEN** it creates one small linked task file beneath `tasks/<repository>/` for each ordered repository group

#### Scenario: Implicit simple-change task shape
- **WHEN** `simple-change` runs in a project with no configured repositories
- **THEN** the parser treats the existing monolithic `tasks.md` as the one implicit repository task and preserves that planning shape

#### Scenario: Repository parameter derived from plan
- **WHEN** a top-level change advances from approved planning to repository execution
- **THEN** a top-level workspace step uses the authoritative parser to capture `affected_repositories`, persists the selection, and explicitly passes that value as `repositories` to repository-scoped children

#### Scenario: Workspace-owned task paths cross repository scope
- **WHEN** the parser returns task files for a repository-scoped implementation loop
- **THEN** it returns canonical absolute task paths so their resolution does not depend on the active repository working directory

#### Scenario: Repository task resolver supplies loop pattern
- **WHEN** a selected repository enters the built-in implementation group
- **THEN** a repository-local resolver asks the authoritative parser for that repository's canonical task pattern and the loop uses the captured pattern rather than hardcoding a directory shape

#### Scenario: Existing planning validator delegates task checks
- **WHEN** built-in plan validation checks task ownership, links, or filesystem shape
- **THEN** `validate-planning-artifacts.sh` delegates those checks to the authoritative parser instead of independently enforcing only `tasks/*.md`

### Requirement: Repository selection persisted

Agent Runner MUST persist the selected repository names and their order before repository-scoped execution begins. Resume MUST use the persisted selection rather than recomputing it from current planning artifacts.

#### Scenario: Planned task groups drift after execution starts
- **WHEN** current planning artifacts assign repositories or task-group order differently from persisted state
- **THEN** Agent Runner rejects resume rather than silently rerouting repository work

## Done When

- One Go implementation parses all three approved shapes and is the only authority for group validation, affected-repository order, active-repository task inputs, normalized snapshots, and fingerprints.
- Configured plans accept only contiguous `## Repository: <configured-name>` groups whose non-empty linked files are safe, unique, workspace-owned, and beneath the matching `tasks/<repository>/` directory; every on-disk task is linked exactly once.
- Implicit full changes preserve ordered flat `tasks/*.md`; implicit simple changes preserve monolithic `tasks.md`; neither exposes or requires `default` in authored artifacts.
- The parser returns canonical absolute literal-safe task inputs and a deterministic normalized snapshot/fingerprint covering repository order, ownership, and task set, with an API usable by both resume validation and embedded workflow resolvers.
- `agent-runner internal task-groups` implements the specified `repositories` and `task-pattern` output modes over the same package, with stable stdout, stderr-only diagnostics, non-zero invalid-plan exits, and focused command tests in `cmd/agent-runner/internal_cmd_test.go`.
- `workflows/core/validate-planning-artifacts.sh` delegates task checks to the Go authority and no direct-only or second ownership parser remains.
- Focused tests cover valid backend/frontend ordering; unknown, duplicate, missing, repeated, cross-linked, unlinked, empty, and unsafe tasks; compact configured simple plans; both implicit shapes; symlink/canonical path handling; stable snapshots; and order/owner/task drift.
- Go changes are formatted with `make fmt`; focused package and `workflows/` delegation tests pass before handing off.
