# Capability: step-flow-control

## Purpose

Defines step-level flow control mechanisms: continuing past failures and conditionally skipping steps based on the outcome of the previous step.
## Requirements
### Requirement: Continue on failure

A step with `continue_on_failure: true` SHALL allow the workflow to proceed to the next step even if the step fails (non-zero exit code for shell steps, non-zero exit for agent steps). The step's outcome (success or failure) is tracked and available to `skip_if` and `break_if` on subsequent steps.

#### Scenario: Failed step with continue_on_failure proceeds
- **WHEN** a shell step has `continue_on_failure: true` and exits with non-zero code
- **THEN** Agent Runner records the failure and continues to the next step

#### Scenario: Failed step without continue_on_failure halts
- **WHEN** a shell step does not have `continue_on_failure` and exits with non-zero code
- **THEN** Agent Runner stops the workflow

#### Scenario: Successful step with continue_on_failure proceeds normally
- **WHEN** a step has `continue_on_failure: true` and succeeds
- **THEN** Agent Runner proceeds to the next step normally

### Requirement: Skip if previous succeeded

A step with `skip_if: previous_success` SHALL be skipped if the immediately preceding step in the same scope succeeded. If the previous step failed (and had `continue_on_failure: true`), the step executes normally.

#### Scenario: Previous step succeeded — skip
- **WHEN** a step has `skip_if: previous_success` and the immediately preceding step succeeded
- **THEN** Agent Runner skips the step and continues to the next step

#### Scenario: Previous step failed — execute
- **WHEN** a step has `skip_if: previous_success` and the immediately preceding step failed (with `continue_on_failure: true`)
- **THEN** Agent Runner executes the step normally

#### Scenario: skip_if on first step in scope
- **WHEN** the first step in a workflow or loop body has `skip_if: previous_success`
- **THEN** Agent Runner fails at load time with a validation error (no previous step to reference)

### Requirement: Explicit non-blocking warning status

A workflow step MAY declare `warn_on_failure: true` to designate its failed or exhausted terminal outcome as a non-blocking warning. Such a step SHALL preserve its underlying execution outcome and output, SHALL terminate with status `warning`, and SHALL allow execution to continue to the next step without also requiring `continue_on_failure: true`. Warning behavior MUST be explicit; ordinary continued failures, retry attempts, and branch-control failures SHALL NOT automatically make the completed run contain warnings.

#### Scenario: Designated failure becomes warning

- **WHEN** a step with `warn_on_failure: true` reaches a failed terminal outcome
- **THEN** the step has terminal status `warning`, retains failed as its underlying outcome, and execution continues

#### Scenario: Designated exhaustion becomes warning

- **WHEN** a counted loop with `warn_on_failure: true` exhausts without reaching its break condition
- **THEN** the loop has terminal status `warning`, retains exhaustion as its underlying outcome, and execution continues

#### Scenario: Ordinary blocking failure remains failed

- **WHEN** a failed step does not declare `warn_on_failure: true` and is not otherwise allowed to continue
- **THEN** the step remains failed and stops the workflow

#### Scenario: Recovered retry does not remain warning

- **WHEN** an earlier attempt fails but the containing retry flow later reaches its success condition
- **THEN** the recovered failure does not contribute a warning to the completed run

### Requirement: Warning propagation and successful completion

An otherwise successful container with one or more terminal warning descendants SHALL visibly reflect that it contains warnings. If the workflow reaches its end with one or more originating warning steps and no blocking failure, Agent Runner SHALL mark it completed and success-class, exit with status zero, prevent resume as for an ordinary completed run, and expose the display status `complete with warnings (N)`, where N counts originating warning steps only. Ancestor containers MUST NOT increase N.

#### Scenario: One warning completes successfully

- **WHEN** one designated step ends with warning status and all remaining workflow steps complete without a blocking failure
- **THEN** the workflow exits zero and displays `complete with warnings (1)`

#### Scenario: Multiple origins are counted once each

- **WHEN** two originating steps end with warning status beneath one or more ancestor containers
- **THEN** the workflow displays `complete with warnings (2)` and does not count the warning-bearing ancestors

#### Scenario: Blocking failure takes precedence

- **WHEN** a workflow contains a warning step and later reaches a blocking failure
- **THEN** the workflow's terminal result is failed rather than complete with warnings

#### Scenario: Clean completion remains unchanged

- **WHEN** a workflow reaches its end with no terminal warning origins
- **THEN** its terminal display remains `completed`

#### Scenario: Completed-with-warnings run is not workflow-resumable

- **WHEN** a run has terminal status `complete with warnings (N)`
- **THEN** it is treated as completed rather than as a failed or interrupted workflow eligible for workflow resume

