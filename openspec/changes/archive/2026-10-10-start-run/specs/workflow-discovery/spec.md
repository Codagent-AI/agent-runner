## ADDED Requirements

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

#### Scenario: User-global workflow with builtin-style path does not shadow builtins
- **WHEN** `~/.agent-runner/workflows/core/finalize-pr.yaml` exists and the builtin `core:finalize-pr` is also embedded
- **THEN** both appear in the enumeration — the on-disk file's canonical name is `core/finalize-pr` (bare path-style, not `core:finalize-pr`) and does not shadow the builtin

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
