# Task: Workflow Discovery Package

## Goal

Create `internal/discovery/` — a new package that enumerates all workflows available to the user across three scopes (project-local, user-global, and builtin), returning a sorted, deduplicated list with full metadata. This is the foundation consumed by the new tab in the list TUI.

## Background

The existing codebase resolves a single workflow by name (`resolveWorkflowArg` in `cmd/agent-runner/main.go`) but has no function that lists all available workflows. This task introduces that enumeration capability as a clean, standalone package.

**Scopes and resolution:**
- **Project-local**: `.agent-runner/workflows/` in the current working directory. Canonical name is the file path relative to this directory with extension stripped (e.g. `my-workflow`, `team/deploy`).
- **User-global**: `~/.agent-runner/workflows/`. Same naming rules. Hidden when a project-local workflow has the same canonical name (shadowing, matching the precedence in `internal/loader/` and `cmd/agent-runner/main.go`'s `resolveWorkflowArg`).
- **Builtin**: the `workflows/embed.go` embed.FS (`builtinworkflows.FS`). Canonical name is `<namespace>:<name>` where namespace is the top-level subdirectory name (e.g. `core:finalize-pr`, `openspec:change`). Builtins cannot be shadowed by bare-name workflows because the naming scheme is distinct.

**Ordering:**
- Results ordered: project scope first, then user scope, then builtin scope.
- Within each scope: alphabetical by canonical name.
- Within builtins: sub-grouped by namespace (namespaces alphabetical, then entries within each namespace alphabetical).

**Malformed files:**
- If a `.yaml`/`.yml` file in the project or user directory fails to parse, include an entry with `ParseError` set. The canonical name is still derived from the path. Description, Params, and other parsed fields are absent. This must not prevent other entries from being returned.
- Builtins are validated at build time and cannot have parse errors at enumeration time.

**Parsing:** Use the existing workflow loader to parse each file. Look at `internal/loader/` — specifically how it loads and validates a workflow YAML into a `model.Workflow`. The `model.Workflow` struct (in `internal/model/step.go`) has `Name`, `Description string`, `Params []Param`, and `Steps []Step`. The `model.Param` struct has `Name string`, `Required *bool` (nil defaults to required), and `Default string`.

**Existing patterns to follow:**
- `internal/model/descriptor.go` — `WorkflowDescriptor` type and `ResolverConfig` for reference on how canonical names are derived from paths. The `CanonicalName()` logic there is the existing display-name derivation. The new discovery package's naming rules are simpler (no repo-root fallback needed — just the scope-relative path).
- `workflows/embed.go` — exports `builtinworkflows.FS` (type `embed.FS`). Use `fs.WalkDir` to enumerate all embedded files.
- Always try `.yaml` first, then `.yml` when scanning on-disk directories. Skip non-YAML files silently.
- Follow TDD: write a failing test first.

**Key files to read before starting:**
- `internal/model/step.go` — `Workflow`, `Param` types
- `internal/model/descriptor.go` — canonical name derivation patterns
- `internal/loader/` — existing workflow loading/parsing
- `workflows/embed.go` — embed.FS structure
- `cmd/agent-runner/main.go`, specifically `resolveWorkflowArg()` (around line 756) — shadowing/precedence logic to match

## Spec

### Requirement: Workflow enumeration across scopes
The system SHALL enumerate all available workflows across three scopes — project-local, user-global, and builtin — and return them as a list of entries. Each entry SHALL carry: the workflow's canonical name (e.g. `my-workflow` or `core:finalize-pr`), its `Description` field (from the parsed YAML, empty string if absent), its scope (project/user/builtin), its source path, and its parameter definitions.

#### Scenario: All scopes enumerated
- **WHEN** workflow enumeration is invoked
- **THEN** the result contains entries from project-local `.agent-runner/workflows/`, user-global `~/.agent-runner/workflows/`, and the embedded builtin set

#### Scenario: Empty scope produces no entries
- **WHEN** no project-local `.agent-runner/workflows/` directory exists
- **THEN** the project scope contributes zero entries; user and builtin scopes are unaffected

### Requirement: Shadowed workflows hidden
When a project-local workflow has the same canonical name as a user-global workflow, the user-global workflow SHALL be excluded from the enumeration result. Shadowing follows the same precedence as `workflow-name-resolution`: project shadows user. Builtins use a distinct namespace syntax (`ns:name`) and cannot be shadowed by bare-name workflows.

#### Scenario: Project workflow shadows user workflow
- **WHEN** both `.agent-runner/workflows/deploy.yaml` and `~/.agent-runner/workflows/deploy.yaml` exist
- **THEN** only the project-local `deploy` appears in the enumeration; the user-global one is excluded

#### Scenario: Builtin not shadowed by bare name
- **WHEN** `.agent-runner/workflows/finalize-pr.yaml` exists and the builtin `core:finalize-pr` is embedded
- **THEN** both appear in the enumeration (different canonical names)

### Requirement: Ordering and grouping
Enumeration results SHALL be ordered by scope: project first, then user, then builtin. Within each scope, entries SHALL be sorted alphabetically by canonical name. Within the builtin scope, entries SHALL be sub-grouped by namespace (e.g. all `core:*` together, all `spec-driven:*` together), with namespaces themselves sorted alphabetically.

#### Scenario: Cross-scope ordering
- **WHEN** project has `build`, user has `deploy`, builtin has `core:finalize-pr`
- **THEN** enumeration order is: `build`, `deploy`, `core:finalize-pr`

#### Scenario: Builtin namespace sub-grouping
- **WHEN** builtins include `core:finalize-pr`, `core:implement-task`, `spec-driven:change`
- **THEN** enumeration order within builtins is: `core:finalize-pr`, `core:implement-task`, `spec-driven:change`

### Requirement: Malformed workflow files shown with error
When a YAML file in the project-local or user-global workflows directory fails to parse, the enumeration SHALL include an entry for that file with an error indicator and the parse error message. The entry SHALL still carry the canonical name (derived from the file path) and scope, but its description, params, and other parsed fields SHALL be absent. Builtin workflows are embedded at build time and are not subject to parse errors at enumeration time.

#### Scenario: Malformed project workflow shown with error
- **WHEN** `.agent-runner/workflows/broken.yaml` contains invalid YAML
- **THEN** the enumeration includes an entry with canonical name `broken`, scope `project`, and an error indicator carrying the parse error message

#### Scenario: Malformed user workflow shown with error
- **WHEN** `~/.agent-runner/workflows/bad-syntax.yaml` contains invalid YAML
- **THEN** the enumeration includes an entry with canonical name `bad-syntax`, scope `user`, and an error indicator carrying the parse error message

#### Scenario: Malformed file does not block other entries
- **WHEN** one file in `~/.agent-runner/workflows/` is malformed and two others are valid
- **THEN** the enumeration includes all three entries — one with an error, two with full metadata

## Done When

All spec scenarios above are covered by tests and passing. `internal/discovery/` exports an `Enumerate` function callable with an `fs.FS` (builtins), a project directory path, and a user directory path. The package has no dependency on TUI code.
