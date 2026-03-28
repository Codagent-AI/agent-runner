# console-output-formatting Specification

## Purpose
TBD - created by archiving change improve-output. Update Purpose after archive.
## Requirements
### Requirement: Step separator lines

Baton SHALL print a horizontal rule of `━` characters at a fixed width before each step heading to visually separate workflow steps.

#### Scenario: Separator printed before step header
- **WHEN** baton dispatches any step (shell, agent, loop, sub-workflow, or group)
- **THEN** a fixed-width horizontal rule of `━` characters is printed to stdout before the step heading

#### Scenario: First step includes separator
- **WHEN** the first step in a workflow is dispatched
- **THEN** the separator is still printed (no special-case omission)

#### Scenario: Skipped steps include separator
- **WHEN** a step is skipped due to `skip_if` evaluation
- **THEN** the separator and heading are still printed (with the `[skipped]` label)

### Requirement: Breadcrumb step headings

Baton SHALL replace the current `--- step N/M: stepId [type] ---` heading with a breadcrumb format that includes the step counter, the full nesting path with 1-indexed iteration numbers, and the step type. Format: `━━ step N/M: segment > segment > stepId [type] ━━`.

#### Scenario: Top-level step heading
- **WHEN** a top-level step `validate` of type `shell` is dispatched as step 1 of 5
- **THEN** the heading is printed as `━━ step 1/5: validate [shell] ━━`

#### Scenario: Step inside a loop iteration
- **WHEN** step `implement` (type `headless`) runs inside loop `task-loop` at iteration index 0, as step 1 of 3 within the loop
- **THEN** the heading is printed as `━━ step 1/3: task-loop > iteration 1 > implement [headless] ━━`

#### Scenario: Step inside a sub-workflow inside a loop
- **WHEN** step `check` (type `shell`) runs inside sub-workflow `verify-task`, invoked from step `verify` inside loop `task-loop` at iteration index 0
- **THEN** the heading is printed as `━━ step 1/2: task-loop > iteration 1 > verify > verify-task > check [shell] ━━`

#### Scenario: Skipped step heading
- **WHEN** a step `deploy` is skipped as step 3 of 5
- **THEN** the heading is printed as `━━ step 3/5: deploy [skipped] ━━`

