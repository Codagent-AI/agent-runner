# Capability: step-flow-control

## Purpose

Defines step-level flow control mechanisms: continuing past failures and conditionally skipping steps based on the outcome of the previous step.

## Requirements

### Requirement: Continue on failure

A step with `continue_on_failure: true` SHALL allow the workflow to proceed to the next step even if the step fails (non-zero exit code for shell steps, non-zero exit for agent steps). The step's outcome (success or failure) is tracked and available to `skip_if` and `break_if` on subsequent steps.

#### Scenario: Failed step with continue_on_failure proceeds
- **WHEN** a shell step has `continue_on_failure: true` and exits with non-zero code
- **THEN** baton records the failure and continues to the next step

#### Scenario: Failed step without continue_on_failure halts
- **WHEN** a shell step does not have `continue_on_failure` and exits with non-zero code
- **THEN** baton stops the workflow

#### Scenario: Successful step with continue_on_failure proceeds normally
- **WHEN** a step has `continue_on_failure: true` and succeeds
- **THEN** baton proceeds to the next step normally

### Requirement: Skip if previous succeeded

A step with `skip_if: previous_success` SHALL be skipped if the immediately preceding step in the same scope succeeded. If the previous step failed (and had `continue_on_failure: true`), the step executes normally.

#### Scenario: Previous step succeeded — skip
- **WHEN** a step has `skip_if: previous_success` and the immediately preceding step succeeded
- **THEN** baton skips the step and continues to the next step

#### Scenario: Previous step failed — execute
- **WHEN** a step has `skip_if: previous_success` and the immediately preceding step failed (with `continue_on_failure: true`)
- **THEN** baton executes the step normally

#### Scenario: skip_if on first step in scope
- **WHEN** the first step in a workflow or loop body has `skip_if: previous_success`
- **THEN** baton fails at load time with a validation error (no previous step to reference)
