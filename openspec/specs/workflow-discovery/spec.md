# workflow-discovery Specification

## Purpose
TBD - created by archiving change feature-238-684b0ada. Update Purpose after archive.
## Requirements
### Requirement: Workflow enumeration across scopes
The system SHALL enumerate the workflows a user can launch from three scopes: project-local `<cwd>/.agent-runner/workflows/`, user-global `~/.agent-runner/workflows/`, and the embedded builtin set. Project and user directories SHALL be searched recursively, so a file in a subdirectory produces a path-style canonical name such as `team/deploy`. Builtin entries SHALL use the `namespace:name` canonical form. Enumeration SHALL produce one entry per canonical logical workflow name, backed by the version selected under the `workflow-versioning` capability.

Each entry SHALL carry the version-free canonical name, the scope (project, user, or builtin), the builtin namespace for builtin entries, the exact selected source path, and, from the selected definition, its `description` (empty when absent), its `hidden` flag, and its declared parameters in declaration order. A missing scope directory or an unresolvable home directory SHALL contribute no entries without affecting the other scopes. The New tab and intake route resolution SHALL share this enumeration.

#### Scenario: All scopes enumerated
- **WHEN** workflow enumeration is invoked
- **THEN** the result contains entries from project-local `.agent-runner/workflows/`, user-global `~/.agent-runner/workflows/`, and the embedded builtin set

#### Scenario: Empty scope produces no entries
- **WHEN** no project-local `.agent-runner/workflows/` directory exists
- **THEN** the project scope contributes zero entries and the user and builtin scopes are unaffected

#### Scenario: Entry carries selected definition metadata
- **WHEN** the user scope contains `deploy-v1.0.yaml` with a description and three declared parameters
- **THEN** the `deploy` entry has scope `user`, that description, the three parameters in declaration order, and the full path of `deploy-v1.0.yaml` as its source path

### Requirement: Shadowed workflows hidden
When a project-local workflow and a user-global workflow share a canonical logical name, the user-global workflow SHALL be excluded from the enumeration result, matching the project-over-user precedence of `workflow-name-resolution`. Shadowing SHALL compare logical names before version selection, so a project workflow shadows a user workflow of the same name even when the user copy has a newer version or the project group is invalid. Builtins use the distinct `ns:name` form and SHALL NOT be shadowed by project or user workflows.

#### Scenario: Project workflow shadows user workflow
- **WHEN** both `.agent-runner/workflows/deploy-v1.0.yaml` and `~/.agent-runner/workflows/deploy-v1.0.yaml` exist
- **THEN** only the project-local `deploy` appears in the enumeration

#### Scenario: Project workflow shadows newer user version
- **WHEN** the project has `deploy-v1.0.yaml` and the user directory has `deploy-v2.0.yaml`
- **THEN** only the project-local `deploy`, backed by `deploy-v1.0.yaml`, appears in the enumeration

#### Scenario: Builtin not shadowed by bare name
- **WHEN** `.agent-runner/workflows/finalize-pr-v1.0.yaml` exists and the builtin `core:finalize-pr` is embedded
- **THEN** both `finalize-pr` and `core:finalize-pr` appear in the enumeration

#### Scenario: User-global workflow with builtin-style path does not shadow builtins
- **WHEN** `~/.agent-runner/workflows/core/finalize-pr-v1.0.yaml` exists and the builtin `core:finalize-pr` is also embedded
- **THEN** both appear in the enumeration; the on-disk file's canonical name is `core/finalize-pr` and does not shadow the builtin

### Requirement: Ordering and grouping
Enumeration results SHALL be ordered by scope: project first, then user, then builtin. Within the project and user scopes, entries SHALL be sorted by canonical name. Within the builtin scope, entries SHALL be sorted by namespace and then by name, so each namespace's entries are contiguous. Display order of builtin namespace groups on the New tab is defined separately by `new-tab-layout`.

#### Scenario: Cross-scope ordering
- **WHEN** the project has `build`, the user has `deploy`, and the builtins include `core:finalize-pr`
- **THEN** enumeration order is `build`, `deploy`, `core:finalize-pr`

#### Scenario: Builtin namespace sub-grouping
- **WHEN** builtins include `core:finalize-pr`, `core:implement-task`, and `spec-driven:change`
- **THEN** enumeration order within builtins is `core:finalize-pr`, `core:implement-task`, `spec-driven:change`

### Requirement: Invalid workflow files shown with error
When any definition in a logical workflow group cannot be read, parsed, or validated by the workflow loader, or the group violates the versioned-filename contract, the enumeration SHALL still include one entry for that group carrying its canonical name, scope, and an error message naming the offending file. Such an entry SHALL have no source path, description, parameters, or hidden flag. This SHALL apply to project, user, and builtin scopes. An invalid group SHALL NOT prevent other groups from being enumerated.

#### Scenario: Malformed project workflow shown with error
- **WHEN** `.agent-runner/workflows/broken-v1.0.yaml` contains invalid YAML
- **THEN** the enumeration includes an entry with canonical name `broken`, scope `project`, and an error message, with no description or parameters

#### Scenario: Malformed user workflow shown with error
- **WHEN** `~/.agent-runner/workflows/bad-syntax-v1.0.yaml` contains invalid YAML
- **THEN** the enumeration includes an entry with canonical name `bad-syntax`, scope `user`, and an error message

#### Scenario: Loader validation failure shown with error
- **WHEN** a project workflow parses as YAML but fails workflow validation, such as an agent step with no `agent`
- **THEN** its entry carries the loader's validation error

#### Scenario: Malformed file does not block other entries
- **WHEN** one file in `~/.agent-runner/workflows/` is malformed and two others are valid
- **THEN** the enumeration includes all three entries, one with an error and two with full metadata

