# Capability: step-flow-control

## Purpose

Defines step-level flow control mechanisms: continuing past failures and conditionally skipping steps based on the outcome of the previous step.

## MODIFIED Requirements

### Requirement: Continue on failure

A step with `continue_on_failure: true` SHALL allow the workflow to proceed to the next step even if the step fails (non-zero exit code for shell steps, non-zero exit for agent steps). The step's outcome (success or failure) is tracked and available to `skip_if` and `break_if` on subsequent steps. For a step with `repair`, the outcome is the check's terminal result after repair; internal repair attempts SHALL NOT be visible to `continue_on_failure`, `skip_if`, or `break_if`.

#### Scenario: Failed step with continue_on_failure proceeds
- **WHEN** a shell step has `continue_on_failure: true` and exits with non-zero code
- **THEN** Agent Runner records the failure and continues to the next step

#### Scenario: Failed step without continue_on_failure halts
- **WHEN** a shell step does not have `continue_on_failure` and exits with non-zero code
- **THEN** Agent Runner stops the workflow

#### Scenario: Successful step with continue_on_failure proceeds normally
- **WHEN** a step has `continue_on_failure: true` and succeeds
- **THEN** Agent Runner proceeds to the next step normally

#### Scenario: Repaired step counts as success
- **WHEN** a check with `repair` fails, is repaired, and passes, and the next step has `skip_if: previous_success`
- **THEN** the next step is skipped

#### Scenario: Exhausted repair with continue_on_failure proceeds
- **WHEN** a check with `repair` and `continue_on_failure: true` exhausts its repair budget
- **THEN** Agent Runner records the failure and continues to the next step

### Requirement: Explicit non-blocking warning status

A workflow step MAY declare `warn_on_failure: true` to designate its failed or exhausted terminal outcome as a non-blocking warning. Such a step SHALL preserve its underlying execution outcome and output, SHALL terminate with status `warning`, and SHALL allow execution to continue to the next step without also requiring `continue_on_failure: true`. Warning behavior MUST be explicit; ordinary continued failures, retry attempts, internal repair attempts, and branch-control failures SHALL NOT automatically make the completed run contain warnings. For a step with `repair`, `warn_on_failure` applies to the check's terminal outcome only.

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

#### Scenario: Exhausted repair becomes warning
- **WHEN** a check with `repair` and `warn_on_failure: true` exhausts its repair budget
- **THEN** the step has terminal status `warning`, retains failed as its underlying outcome, and execution continues
