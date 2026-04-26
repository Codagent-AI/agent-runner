## ADDED Requirements

### Requirement: Pre-validation catches broken sub-workflows before run start

For fresh runs that are not builtin workflows (i.e., not skipped by the pre-validation skip rule defined in `workflow-pre-validation`), every reachable sub-workflow SHALL be loaded and validated before any step in the root workflow executes. Errors that today would surface lazily at sub-workflow dispatch SHALL surface at run start.

For builtin workflow runs (which the skip rule excludes from pre-validation), broken sub-workflows continue to surface lazily at dispatch — the agent-runner repo's build-time agent-validator check is responsible for ensuring builtins do not ship broken.

#### Scenario: Missing sub-workflow file fails at run start, not at dispatch
- **WHEN** a non-builtin root workflow references `workflow: workflows/missing.yaml` and the file does not exist
- **THEN** pre-validation fails before any root step executes, with an error naming the missing file and the referencing step

#### Scenario: Sub-workflow with broken sessions fails at run start
- **WHEN** a non-builtin root reaches a sub-workflow that has a `session: implementor` reference but no workflow in the composition tree declares `implementor`
- **THEN** pre-validation fails before any root step executes, with an error naming the unresolved reference and the file that contains it

#### Scenario: Project workflow with a broken sub-workflow fails at run start
- **WHEN** the cwd contains `.agent-runner/workflows/deploy.yaml` referencing a sub-workflow with a syntax error and `agent-runner deploy` is invoked
- **THEN** pre-validation fails before any deploy step executes (project workflows are not skipped)

#### Scenario: Builtin run falls back to lazy dispatch failure
- **WHEN** a builtin root workflow (e.g., `agent-runner core:finalize-pr`) reaches a broken sub-workflow at runtime
- **THEN** the failure surfaces at dispatch, as in pre-existing behavior — pre-validation does not run for builtin roots, since builtins are gated by the agent-runner repo's build-time check
