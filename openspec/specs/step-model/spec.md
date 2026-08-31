# Capability: step-model

## Purpose

Defines per-step model and CLI overrides for agent steps.
## Requirements
### Requirement: Per-step model override
A step MAY include a `model` field specifying which model the agent should use. When present, the runner SHALL pass the model to the CLI adapter, overriding the model from the resolved agent profile. When absent, the profile's model is used (which may itself be unset, in which case no model is passed to the CLI). The `model` field is only valid on agent steps, not shell steps.

#### Scenario: Model specified overrides profile
- **WHEN** an agent step has `agent: autonomous_base` (profile model=opus) and `model: sonnet`
- **THEN** the runner passes sonnet to the CLI adapter, not the profile's model

#### Scenario: No model on step, profile has model
- **WHEN** an agent step does not have a `model` field and the resolved profile has model=opus
- **THEN** the runner passes opus to the CLI adapter

#### Scenario: No model on step, profile has no model
- **WHEN** an agent step does not have a `model` field and the resolved profile has no model set
- **THEN** the runner invokes the CLI adapter without a model override

#### Scenario: Model on shell step
- **WHEN** a shell step has a `model` field
- **THEN** the runner fails with a validation error

### Requirement: Per-step CLI override
A step MAY include a `cli` field specifying which CLI backend to use. When present, it SHALL override the cli from the resolved agent profile. When absent, the profile's cli is used. If both the step and the resolved profile omit `cli`, the runner SHALL fall back to `claude`. The `cli` field is only valid on agent steps, not shell steps.

#### Scenario: CLI specified overrides profile
- **WHEN** an agent step has `agent: autonomous_base` (profile cli=claude) and `cli: codex`
- **THEN** the runner uses the Codex adapter for that step

#### Scenario: CLI not specified, uses profile
- **WHEN** an agent step has no `cli` field and the resolved profile has cli=claude
- **THEN** the runner uses the Claude adapter

#### Scenario: CLI on shell step
- **WHEN** a shell step has a `cli` field
- **THEN** the runner fails with a validation error

### Requirement: `model` field rejected on UI steps

The `model` field SHALL NOT be valid on `mode: ui` steps. UI steps are not agent steps; they have no model concept. Validation SHALL fail at workflow-load time when a UI step sets `model`.

#### Scenario: UI step with model field
- **WHEN** a step has `mode: ui` and sets `model: opus`
- **THEN** validation fails with an error indicating that `model` is not valid on UI steps

### Requirement: `cli` field rejected on UI steps

The `cli` field SHALL NOT be valid on `mode: ui` steps. UI steps are not agent steps; they have no CLI adapter. Validation SHALL fail at workflow-load time when a UI step sets `cli`.

#### Scenario: UI step with cli field
- **WHEN** a step has `mode: ui` and sets `cli: claude`
- **THEN** validation fails with an error indicating that `cli` is not valid on UI steps

### Requirement: `model` field rejected on script steps

The `model` field SHALL NOT be valid on `script:` steps. Script steps are not agent steps; the model concept does not apply to a bundled script. Validation SHALL fail at workflow-load time when a script step sets `model`.

#### Scenario: Script step with model field
- **WHEN** a step declares `script: detect.sh` and sets `model: opus`
- **THEN** validation fails with an error indicating that `model` is not valid on script steps

### Requirement: `cli` field rejected on script steps

The `cli` field SHALL NOT be valid on `script:` steps. Script steps are not agent steps; they do not invoke a CLI adapter. Validation SHALL fail at workflow-load time when a script step sets `cli`.

#### Scenario: Script step with cli field
- **WHEN** a step declares `script: detect.sh` and sets `cli: codex`
- **THEN** validation fails with an error indicating that `cli` is not valid on script steps

### Requirement: Workflow scope declaration
A scope-aware workflow MUST declare exactly one default `scope` value of `workspace` or `repositories`. A workflow that omits `scope` MUST retain legacy execution behavior.

#### Scenario: Workspace default scope
- **WHEN** a workflow declares `scope: workspace`
- **THEN** workflow validation accepts the workspace default

#### Scenario: Repository default scope
- **WHEN** a workflow declares `scope: repositories`
- **THEN** workflow validation accepts the repository default

#### Scenario: Invalid workflow scope
- **WHEN** a workflow declares a scope other than the string `workspace` or `repositories`
- **THEN** workflow validation fails with an invalid-scope error

#### Scenario: Legacy workflow without scope
- **WHEN** a workflow and all of its steps omit `scope`
- **THEN** Agent Runner preserves the workflow's execution behavior from before scope support

#### Scenario: Scoped step without workflow default
- **WHEN** a workflow omits `scope` but one of its steps declares `scope`
- **THEN** workflow validation fails and requires the workflow to declare its default scope

### Requirement: Step scope override
A non-sub-workflow step in a workspace-scoped workflow MAY override its execution scope. The `scope` field MUST accept the same `workspace` and `repositories` values for agent, shell, script, UI, group, and loop steps. A sub-workflow step MUST NOT declare `scope` because the referenced workflow's declared scope is authoritative.

#### Scenario: Step uses workspace default
- **WHEN** a step in a `scope: workspace` workflow omits `scope`
- **THEN** the step uses the workspace default

#### Scenario: Repository-scoped step override
- **WHEN** a step in a `scope: workspace` workflow declares `scope: repositories`
- **THEN** workflow validation accepts the repository override

#### Scenario: Redundant workspace override
- **WHEN** a step in a `scope: workspace` workflow declares `scope: workspace`
- **THEN** workflow validation accepts the explicit workspace scope

#### Scenario: Scope on each supported step type
- **WHEN** an agent, shell, script, UI, group, or loop step declares a permitted scope in a workspace-scoped workflow
- **THEN** workflow validation accepts the scope field for that step type

#### Scenario: Scope on sub-workflow step
- **WHEN** a sub-workflow step declares `scope`
- **THEN** workflow validation fails and directs the author to declare the intended default on the referenced workflow

#### Scenario: Invalid step scope
- **WHEN** a step declares a scope other than the string `workspace` or `repositories`
- **THEN** workflow validation fails with an invalid-scope error

### Requirement: Repository workflows remain repository-scoped
A workflow whose default scope is `repositories` MUST NOT contain a step that overrides scope to `workspace`.

#### Scenario: Step uses repository default
- **WHEN** a step in a `scope: repositories` workflow omits `scope`
- **THEN** the step remains repository-scoped

#### Scenario: Redundant repository override
- **WHEN** a step in a `scope: repositories` workflow declares `scope: repositories`
- **THEN** workflow validation accepts the explicit repository scope

#### Scenario: Workspace override inside repository workflow
- **WHEN** a step in a `scope: repositories` workflow declares `scope: workspace`
- **THEN** workflow validation fails and requires the workspace work to move into a workspace-scoped parent

### Requirement: Repository workflow target parameter
A workflow whose default scope is `repositories` MUST declare a required ordered `repositories` parameter. Configured workspaces MUST supply it explicitly. In a project with no repository declarations, Agent Runner MUST satisfy the parameter internally with the transparent implicit repository so existing standalone entry points remain compatible.

#### Scenario: Repository workflow declares targets
- **WHEN** a `scope: repositories` workflow declares `repositories` as a required parameter
- **THEN** workflow validation accepts the repository target contract

#### Scenario: Repository workflow omits targets
- **WHEN** a `scope: repositories` workflow omits the `repositories` parameter or declares it as optional
- **THEN** workflow validation fails before the workflow can be invoked

#### Scenario: Standalone repository workflow
- **WHEN** a user invokes a valid `scope: repositories` workflow directly in a configured workspace
- **THEN** the workflow invocation requires an explicit `repositories` value

#### Scenario: Standalone repository workflow in implicit project
- **WHEN** a user invokes the same workflow directly with no repository declarations
- **THEN** Agent Runner supplies the implicit target without requiring the user to enter `default`

#### Scenario: Parent invokes repository workflow
- **WHEN** a parent invokes a valid `scope: repositories` child workflow
- **THEN** the child invocation requires the parent to supply `repositories`

