# builtin-vars Specification

## Purpose

Defines the set of built-in template variables the runner exposes to every step, and the precedence rules governing their interaction with workflow-declared params and captured variables.

## Requirements

### Requirement: session_dir built-in variable

The runner SHALL expose `{{session_dir}}` as a built-in template variable in every step that executes within a named run. Its value is the absolute path of the current run's session directory (`~/.agent-runner/projects/<encoded-cwd>/runs/<run-id>/`).

When no session directory is set (e.g., in tests or detached execution contexts), `{{session_dir}}` SHALL NOT be available and attempts to interpolate it fail as an unresolved variable.

#### Scenario: session_dir resolves in a normal run
- **WHEN** a step's prompt or command contains `{{session_dir}}`
- **THEN** the runner replaces it with the absolute path of the run's session directory

#### Scenario: session_dir unavailable without session directory
- **WHEN** the execution context has no session directory configured
- **THEN** `{{session_dir}}` is not present in the built-in variable set

### Requirement: step_id built-in variable

The runner SHALL expose `{{step_id}}` as a built-in template variable whose value is the `id` field of the currently executing step.

`{{step_id}}` is available in: step `prompt`, `command`, `params` values, `skip_if` shell expressions, and sub-workflow `workflow` path fields.

#### Scenario: step_id resolves to current step id
- **WHEN** a step with `id: my-step` contains `{{step_id}}` in its prompt or command
- **THEN** the runner replaces it with `my-step`

#### Scenario: step_id is step-scoped
- **WHEN** two steps in the same workflow each reference `{{step_id}}`
- **THEN** each step sees its own `id`, not the other step's

### Requirement: Built-in precedence

Built-in variables have the **lowest** interpolation precedence. A workflow `params` entry or a captured variable with the same name as a built-in SHALL shadow the built-in.

#### Scenario: Param shadows built-in
- **WHEN** a workflow declares `params: [step_id]` and a caller passes `step_id: custom-value`
- **THEN** `{{step_id}}` in that step resolves to `custom-value`, not the actual step ID

#### Scenario: Captured variable shadows built-in
- **WHEN** a prior step captures output into a variable named `session_dir`
- **THEN** `{{session_dir}}` in subsequent steps resolves to the captured value, not the session directory path
