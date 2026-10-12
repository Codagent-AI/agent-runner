## ADDED Requirements

### Requirement: Plan-and-implement-change workflow

The `core` namespace SHALL include a hidden logical workflow `plan-and-implement-change`, backed by `core/plan-and-implement-change-v1.0.yaml`. It SHALL plan and implement an existing approved change in one run. It SHALL declare the parameters `change_name`, `change_dir`, `change_label`, `change_kind`, and `artifact_validation_instruction` as required, and `skip_validator` as optional with default `false`. It SHALL have exactly two top-level steps, in this order:
- `plan`, which invokes `plan-change-v1.0.yaml` with `change_name`, `change_dir`, `change_label`, `change_kind`, and `skip_validator`;
- `implement`, which invokes `implement-change-v1.0.yaml` with all six parameters.

The `implement` step SHALL run only after `plan` completes successfully. The workflow SHALL NOT create, define, accept, archive, or finalize the change, and SHALL NOT declare named sessions of its own; the planning and implementation sessions remain those of the composed workflows. Because the workflow is a single run, it SHALL produce one run identity, one resumable state, and one `run-metrics.json` covering both steps, with sub-workflow attempts recorded under the `plan` and `implement` step prefixes.

#### Scenario: Plan then implement from definition artifacts
- **WHEN** a user runs `agent-runner run core:plan-and-implement-change` with the required parameters for a change directory that holds a proposal, design, specs, and test plan but no tasks
- **THEN** the `plan` step writes, reviews, validates, and commits `tasks.md` and `tasks/*.md`
- **AND** the `implement` step then implements those tasks and runs `core/verify-change` through the acceptance handoff

#### Scenario: Plan failure stops before implementation
- **WHEN** the `plan` step fails, for example because the definition artifacts fail planning validation after repair
- **THEN** the `implement` step does not start and the run reports the failure from the `plan` step

#### Scenario: One metrics artifact covers both steps
- **WHEN** the run completes
- **THEN** its single `run-metrics.json` names `plan-and-implement-change` as the workflow and records planning attempts under `plan/` and implementation attempts under `implement/`

#### Scenario: Resume continues inside the composed workflow
- **WHEN** a run of `core:plan-and-implement-change` is interrupted during `implement` and resumed with `agent-runner --resume <run-id>`
- **THEN** the run continues from its recorded nested position without repeating the completed `plan` step

#### Scenario: Workflow is hidden but runnable
- **WHEN** a user lists workflows
- **THEN** `core:plan-and-implement-change` is not listed
- **AND** `agent-runner run core:plan-and-implement-change` and `agent-runner debug --show-workflow core:plan-and-implement-change` both resolve it

### Requirement: Plan-change Validator skipping

The `core:plan-change` workflow SHALL accept an optional `skip_validator` parameter, defaulting to `false` and accepted only as `true` or `false`. It SHALL validate the value before any planning agent runs. When it is `true`, committing the plan SHALL NOT invoke Agent Validator; in particular, it SHALL NOT run `agent-validator skip` to advance the Validator baseline. When it is `false` or omitted, plan-change SHALL behave as before this requirement. `commit-change-plan.sh` SHALL treat an absent `skip_validator` input as `false`, so its other caller, `core:commit-change-plan` (used by `openspec:simple-change`), keeps running `agent-validator skip` unchanged. Skipping validation SHALL NOT skip definition checking, task writing, task review, plan validation, or the plan commit.

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
