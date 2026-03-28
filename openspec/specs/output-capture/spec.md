# Capability: output-capture

## Purpose

Defines how shell steps can capture their stdout into named variables for use in subsequent steps via template interpolation.
## Requirements
### Requirement: Shell stdout capture

A shell step with a `capture` field SHALL capture its stdout into a named variable. The captured value is available to subsequent steps via `{{var_name}}` interpolation. Output SHALL be both captured and displayed to the terminal (tee behavior).

#### Scenario: Capture stores stdout
- **WHEN** a shell step has `capture: gauntlet_output` and produces stdout
- **THEN** the stdout is stored in the variable `gauntlet_output` and available via `{{gauntlet_output}}` in subsequent steps

#### Scenario: Tee behavior
- **WHEN** a shell step has `capture: gauntlet_output`
- **THEN** stdout is displayed to the terminal in real time AND captured into the variable

#### Scenario: Captured variable used in subsequent step prompt
- **WHEN** a step's prompt contains `{{gauntlet_output}}` and a prior step captured into `gauntlet_output`
- **THEN** baton interpolates the captured value into the prompt

#### Scenario: Captured variable not set
- **WHEN** a step references `{{gauntlet_output}}` but no prior step captured into that variable
- **THEN** baton fails with a descriptive error naming the undefined variable

#### Scenario: Capture on non-shell step
- **WHEN** an agent step (headless or interactive) has a `capture` field
- **THEN** baton fails at load time with a validation error

### Requirement: Captured variable scope

Captured variables SHALL be available to all subsequent steps within the same scope — sibling steps, nested child steps, and subsequent loop iterations. Captured variables from a sub-workflow are NOT available in the parent workflow after the sub-workflow completes.

#### Scenario: Variable available to sibling steps
- **WHEN** step A captures `output` and step B (a sibling) references `{{output}}`
- **THEN** step B receives the captured value

#### Scenario: Variable available within loop iterations
- **WHEN** a shell step inside a loop captures `output` on iteration 1
- **THEN** `{{output}}` is available in the same iteration's subsequent steps, and is overwritten on each new iteration

#### Scenario: Variable does not leak from sub-workflow to parent
- **WHEN** a sub-workflow captures `internal_var` and the parent step after the sub-workflow references `{{internal_var}}`
- **THEN** baton fails with an undefined variable error

### Requirement: Shell stderr capture

Shell steps SHALL pipe stderr in addition to stdout. Stderr SHALL be teed to the terminal in real-time and stored for the audit log. See `stderr-capture` spec for full requirements.

#### Scenario: Stderr piped alongside stdout
- **WHEN** a shell step with `capture: output` executes and produces both stdout and stderr
- **THEN** stdout is captured into the variable and teed, stderr is separately teed and stored for the audit log

