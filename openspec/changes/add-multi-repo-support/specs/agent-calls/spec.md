## MODIFIED Requirements

### Requirement: Working-directory behavior

Agent Runner SHALL establish a starting-directory boundary for a called child from the parent's active execution scope. In a scope-aware workspace, the boundary SHALL be the canonical coordination Git worktree root and the workflow MUST have been launched from that root. In repository scope, the boundary SHALL be the canonical active repository directory, including when that configured repository is outside the workspace tree. For a legacy unscoped run whose launch directory is not inside a Git worktree, the existing launch-directory boundary SHALL remain unchanged. Loading a workflow file from a different repository MUST NOT change the boundary.

When `workdir` is omitted, the child SHALL use the parent's effective working directory. A relative `workdir` SHALL resolve from the parent's effective working directory. Every resolved starting `workdir`, including a path reached through a symbolic link, MUST remain inside the active scope boundary.

The starting-directory boundary SHALL validate only where the child process starts. It MUST NOT sandbox the called child or prevent the child, after launch, from accessing other paths permitted by its CLI, operating-system permissions, and project instructions.

#### Scenario: Omitted workdir uses parent directory
- **WHEN** a parent running from an effective working directory invokes `call_agent` without `workdir`
- **THEN** the child runs from the parent's effective working directory

#### Scenario: Relative workdir uses parent directory as its base
- **WHEN** a repository-scoped parent running from `/workspace/backend` invokes `call_agent` with `workdir: web`
- **THEN** Agent Runner resolves the child's starting directory as `/workspace/backend/web`

#### Scenario: Scope-aware launch subdirectory is rejected
- **WHEN** a scope-aware workflow is launched from a subdirectory of its coordination Git worktree
- **THEN** Agent Runner rejects the run before any called child can start

#### Scenario: Legacy non-Git call retains launch boundary
- **WHEN** an unscoped legacy workflow calls a child from a launch directory outside Git
- **THEN** the child starting-directory boundary remains that launch directory

#### Scenario: Repository outside workspace is the active boundary
- **WHEN** a workspace at `/workspace/foo` activates configured repository `backend` at `/projects/backend` and a repository-scoped parent calls an agent without `workdir`
- **THEN** the child starts within `/projects/backend` even though that repository is outside the workspace tree

#### Scenario: Workdir cannot escape the active scope
- **WHEN** a call supplies a path or symbolic link that resolves outside the active workspace or repository boundary
- **THEN** Agent Runner rejects the starting workdir without spawning the child

#### Scenario: Starting boundary does not restrict later access
- **WHEN** a child starts in repository `backend` and its project instructions require running a service or command in repository `frontend`
- **THEN** Agent Runner does not reject that later access based solely on the child's starting-directory boundary

#### Scenario: External workflow file does not move the boundary
- **WHEN** Agent Runner loads a workflow file from a location outside the active execution scope
- **THEN** called children continue to use the active workspace or repository as their starting-directory boundary
