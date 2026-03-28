# Capability: sub-workflows

## Purpose

Defines how workflow steps can delegate to other workflow files, including parameter passing and session inheritance across workflow boundaries.

## Requirements

### Requirement: Sub-workflow invocation

A step with a `workflow` field SHALL load and execute the referenced workflow file. The step MUST NOT have `prompt`, `command`, or `mode` — it delegates entirely to the sub-workflow. The sub-workflow executes in the same process as the parent.

#### Scenario: Sub-workflow executes successfully
- **WHEN** a step has `workflow: workflows/run-gauntlet.yaml` and the referenced file exists
- **THEN** baton loads the sub-workflow, executes its steps, and continues with the next step in the parent

#### Scenario: Sub-workflow file not found
- **WHEN** a step has `workflow: workflows/missing.yaml` and the file does not exist
- **THEN** baton fails with a descriptive error naming the missing file

#### Scenario: Sub-workflow step is mutually exclusive with prompt/command/mode
- **WHEN** a step has both `workflow` and `prompt` (or `command` or `mode`)
- **THEN** baton fails at load time with a validation error

### Requirement: Parameter passing to sub-workflows

A step with `workflow` MAY include a `params` map that passes values to the sub-workflow. Values support `{{var}}` interpolation. The sub-workflow SHALL receive only the parameters explicitly passed — it MUST NOT implicitly inherit the parent's parameter scope.

#### Scenario: Parameters passed to sub-workflow
- **WHEN** a step has `workflow: workflows/implement-task.yaml` and `params: { task_file: "{{task_file}}" }`
- **THEN** the sub-workflow receives `task_file` as a parameter and can reference it via `{{task_file}}`

#### Scenario: Missing required parameter
- **WHEN** a sub-workflow declares a required parameter and the parent step's `params` map does not include it
- **THEN** baton fails with a descriptive error naming the missing parameter

#### Scenario: Sub-workflow does not inherit parent params implicitly
- **WHEN** the parent workflow has a parameter `change_name` but the step's `params` map does not pass it
- **THEN** the sub-workflow cannot reference `{{change_name}}`

### Requirement: Session inheritance

A step with `session: inherit` SHALL resume the most recent session from the parent workflow that invoked the current sub-workflow. This allows a sub-workflow's agent steps to continue the session chain started in the parent.

#### Scenario: Inherit resumes parent session
- **WHEN** a sub-workflow step has `session: inherit` and the parent workflow has an active session
- **THEN** the step resumes the parent's most recent session

#### Scenario: Inherit with no parent session
- **WHEN** a sub-workflow step has `session: inherit` but no parent workflow session exists
- **THEN** baton fails with a descriptive error

#### Scenario: Inherit in a top-level workflow
- **WHEN** a step in a top-level workflow (not a sub-workflow) has `session: inherit`
- **THEN** baton logs a warning and falls back to a new session (the agent executor's existing try/catch ensures this is non-fatal)

### Requirement: Session resume scoping

`session: resume` SHALL only resume sessions created within the same workflow file. It MUST NOT reach across sub-workflow boundaries to resume a session from a parent or child workflow.

#### Scenario: Resume finds session in same workflow
- **WHEN** a step has `session: resume` and a prior step in the same workflow file created a session
- **THEN** the step resumes that session

#### Scenario: Resume with no prior session in same workflow
- **WHEN** a step has `session: resume` but no prior step in the same workflow file created a session
- **THEN** baton fails with a descriptive error

#### Scenario: Resume does not cross sub-workflow boundary
- **WHEN** a parent workflow invokes a sub-workflow that created sessions, and the next parent step has `session: resume`
- **THEN** the parent step resumes the parent's own most recent session, not the sub-workflow's
