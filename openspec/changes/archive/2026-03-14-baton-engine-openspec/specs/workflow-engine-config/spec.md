## ADDED Requirements

### Requirement: Engine block in workflow schema

The workflow schema SHALL accept an optional `engine` block. The `type` field SHALL be required. All other fields SHALL be passed through to the engine as opaque configuration — the engine validates its own config.

#### Scenario: Workflow with engine block
- **WHEN** a workflow YAML has `engine: { type: openspec, change_param: change_name }`
- **THEN** baton parses the engine block, extracts `type`, and passes the remaining fields to the engine

#### Scenario: Workflow without engine block
- **WHEN** a workflow YAML has no `engine` block
- **THEN** baton runs with no engine (existing behavior)

#### Scenario: Engine block missing type
- **WHEN** a workflow YAML has `engine: { change_param: change_name }` without `type`
- **THEN** baton fails at load time with a descriptive error

### Requirement: Engine-aware step matching

Baton SHALL pass the workflow step ID to engine hooks (`enrichPrompt`, `validateStep`), and the engine determines whether it applies. No explicit mapping field exists on steps — the convention is that step IDs match engine-managed entity IDs.

#### Scenario: Engine receives step ID in hooks
- **WHEN** baton calls `enrichPrompt` or `validateStep`
- **THEN** baton passes the current step's ID to the engine, which decides whether to act on it

#### Scenario: Engine ignores unrecognized step ID
- **WHEN** the engine receives a step ID it does not manage
- **THEN** it returns no enrichment / skips validation (no-op)
