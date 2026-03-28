# Capability: step-model

## Purpose

Defines per-step model override for agent steps and ctrl-c termination behavior for headless agent steps.

## Requirements

### Requirement: Per-step model override

A step MAY include a `model` field specifying which model the agent should use. When present, baton SHALL pass the model to the agent invocation. When absent, the agent uses its default model. The `model` field is only valid on agent steps (headless or interactive), not shell steps.

#### Scenario: Model specified on agent step
- **WHEN** a headless step has `model: sonnet`
- **THEN** baton passes the model flag to the claude invocation

#### Scenario: No model specified
- **WHEN** a step does not have a `model` field
- **THEN** baton invokes the agent without a model flag, using the agent's default

#### Scenario: Model on shell step
- **WHEN** a shell step has a `model` field
- **THEN** baton fails at load time with a validation error

### Requirement: Headless mode ctrl-c termination

When an agent step is running in headless mode, pressing ctrl-c (SIGINT) SHALL terminate the spawned agent subprocess and exit baton cleanly. The user must be able to interrupt a long-running headless agent step without leaving orphaned processes.

#### Scenario: Ctrl-c terminates headless agent subprocess
- **WHEN** a headless agent step is running and the user sends SIGINT (ctrl-c)
- **THEN** baton kills the spawned agent subprocess and exits with a non-zero exit code

#### Scenario: State file preserved on ctrl-c
- **WHEN** a headless agent step is interrupted via ctrl-c
- **THEN** the state file reflects the interrupted step so the workflow can be resumed
