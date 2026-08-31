## ADDED Requirements

### Requirement: Child workflow scope is authoritative
A sub-workflow invocation MUST use the referenced child workflow's declared scope. The parent workflow's default scope MUST NOT replace the child's declared scope, and a sub-workflow step MUST NOT override it.

#### Scenario: Workspace parent invokes workspace child
- **WHEN** a workspace-scoped parent invokes a workspace-scoped child
- **THEN** the child executes once in workspace scope

#### Scenario: Workspace parent invokes repository child
- **WHEN** a workspace-scoped parent invokes a repository-scoped child with the required `repositories` parameter
- **THEN** the child executes once for each selected repository in order

#### Scenario: Repository child invoked directly
- **WHEN** the same repository-scoped child is invoked as a top-level workflow with `repositories`
- **THEN** it uses the same repository-scoped behavior as when composed under a parent

#### Scenario: Parent default does not replace child default
- **WHEN** a parent and child declare different scopes
- **THEN** the child uses its own declared scope rather than inheriting the parent's default

#### Scenario: Scope override on sub-workflow step
- **WHEN** a sub-workflow step declares `scope`
- **THEN** workflow validation fails before the child can execute

### Requirement: Valid scoped workflow composition
A workspace-scoped workflow MAY invoke workspace-scoped, repository-scoped, or legacy unscoped children. A repository-scoped workflow MAY invoke repository-scoped or legacy unscoped children but MUST NOT invoke a workspace-scoped child.

#### Scenario: Repository parent invokes repository child
- **WHEN** a repository-scoped parent invokes a repository-scoped child while a repository is active
- **THEN** the child executes once in the active repository context without starting another fan-out

#### Scenario: Workspace parent invokes mixed-scope children
- **WHEN** a workspace-scoped parent invokes a workspace child and a repository child in sequence
- **THEN** the workspace child executes once and the repository child executes for each selected repository

#### Scenario: Repository parent invokes workspace child
- **WHEN** a repository-scoped parent references a workspace-scoped child
- **THEN** workflow composition validation fails and requires the workspace child to be invoked from a workspace-scoped parent

#### Scenario: Scoped parent invokes legacy child
- **WHEN** a scoped parent invokes an unscoped legacy child
- **THEN** the child retains legacy behavior and executes once in the parent's effective execution context

### Requirement: Scoped context propagation
Sub-workflows MUST receive workspace and active-repository execution context without implicitly inheriting ordinary workflow parameters.

#### Scenario: Workspace context crosses child boundary
- **WHEN** a parent invokes a child workflow
- **THEN** the canonical workspace root and workspace-owned run identity, state, and audit context remain unchanged in the child

#### Scenario: Active repository crosses child boundary
- **WHEN** a child workflow is invoked while repository `backend` is active
- **THEN** the child receives backend as its active repository context

#### Scenario: Repository targets passed explicitly
- **WHEN** a parent invokes a repository-scoped child
- **THEN** the child receives `repositories` only when the parent includes it in the sub-workflow `params` map

#### Scenario: Missing repository targets
- **WHEN** a parent in a configured workspace invokes a repository-scoped child without passing its required `repositories` parameter
- **THEN** Agent Runner fails before the child begins execution

#### Scenario: Implicit child target remains compatible
- **WHEN** a parent with no repository declarations invokes a repository-scoped child without an explicit target value
- **THEN** Agent Runner supplies the transparent implicit repository and executes the child once without exposing `default`
