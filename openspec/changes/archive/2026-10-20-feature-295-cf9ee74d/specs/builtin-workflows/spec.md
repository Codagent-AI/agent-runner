## MODIFIED Requirements

### Requirement: Plan-change Validator skipping

The `core:plan-change` workflow SHALL accept an optional `skip_validator` parameter, defaulting to `false` and accepted only as `true` or `false`. It SHALL validate the value before any planning agent runs. When it is `true`, committing the plan SHALL NOT invoke Agent Validator; in particular, it SHALL NOT run `agent-validator skip` to advance the Validator baseline. When it is `false` or omitted, plan-change SHALL behave as before this requirement. `commit-change-plan.sh` SHALL treat an absent `skip_validator` input as `false`, so its other caller, `core:commit-change-plan` (used by `openspec:simple-change`), keeps running `agent-validator skip` unchanged. Skipping validation SHALL NOT skip definition checking, task writing, task review, plan validation, or the plan commit. When the repository-root `openspec` path is a symbolic link and the change directory is under `openspec/`, `commit-change-plan.sh` SHALL NOT stage or commit anything. It SHALL report that the plan lives outside the repository and must be committed there by hand, apply the same `skip_validator` rule to the Validator baseline as on a normal commit, and exit successfully. Every workflow that commits a plan through `commit-change-plan.sh` gets this behavior with no new parameter. When `openspec` is not a symbolic link, committing the plan SHALL behave as before.

#### Scenario: Validator baseline advances by default
- **WHEN** `core:plan-change` runs without `skip_validator`
- **THEN** committing the plan runs `agent-validator skip` exactly as before

#### Scenario: Validator skipped during planning
- **WHEN** `core:plan-change` runs with `skip_validator=true`
- **THEN** the plan is committed and no Agent Validator command runs during the plan step

#### Scenario: Plan already committed while skipping
- **WHEN** `core:plan-change` runs with `skip_validator=true` and the change directory is already committed
- **THEN** the commit step succeeds without running `agent-validator skip`

#### Scenario: Other commit-plan caller is unchanged
- **WHEN** `core:commit-change-plan` runs `commit-change-plan.sh` without a `skip_validator` input
- **THEN** the script commits the change and runs `agent-validator skip` exactly as before

#### Scenario: Invalid planning skip value rejected
- **WHEN** `core:plan-change` receives a `skip_validator` value other than `true` or `false`
- **THEN** deterministic parameter validation fails before any planning agent runs

#### Scenario: Composed workflow skips every Validator path
- **WHEN** `core:plan-and-implement-change` runs with `skip_validator=true`
- **THEN** neither its `plan` step nor its `implement` step invokes Agent Validator
- **AND** the run still completes through draft pull-request creation and the acceptance handoff

#### Scenario: Symlinked OpenSpec directory skips the plan commit
- **WHEN** `commit-change-plan.sh` runs without a `skip_validator` input, the repository-root `openspec` is a symbolic link to a directory in another repository, and the change directory is under `openspec/`
- **THEN** the script exits successfully, the repository gains no new commit, and nothing is staged
- **AND** it reports that the plan must be committed by hand in the repository that holds the OpenSpec project
- **AND** it runs `agent-validator skip`

#### Scenario: Symlinked OpenSpec directory while skipping the Validator
- **WHEN** `commit-change-plan.sh` runs with `skip_validator=true`, the repository-root `openspec` is a symbolic link, and the change directory is under `openspec/`
- **THEN** the script exits successfully without committing and without running any Agent Validator command
