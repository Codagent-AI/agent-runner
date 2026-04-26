## ADDED Requirements

### Requirement: Pre-validation surfaces layered-config and profile errors

Pre-validation (see `workflow-pre-validation`) SHALL exercise the same layered config load that runtime agent-step resolution uses, so configuration errors that today only surface at the moment an agent step is dispatched SHALL instead surface before the run begins.

The layered load SHALL combine built-in defaults, `~/.agent-runner/config.yaml`, and the project's `.agent-runner/config.yaml` when present, validate the resulting config through the same loader the runtime uses, and walk every agent referenced by the workflow through its `extends` chain in the active profile set.

Errors from layered-config validation SHALL include the profile set name, agent name, field name, invalid value, and (where the schema knows them) allowed values. The originating layer file is **best-effort**: errors include the list of layer files that were loaded rather than a precise file-of-record, because the current layered-config loader merges and validates without retaining per-field origin metadata. Adding origin tracking is a separate, future change.

#### Scenario: Invalid effort in global config fails before run start
- **WHEN** `~/.agent-runner/config.yaml` sets `profiles.default.agents.implementor.effort: extreme` and a fresh, non-builtin run references the `implementor` agent
- **THEN** pre-validation fails before any step executes with a structured error naming the profile set `default`, the agent `implementor`, the field `effort`, the invalid value `extreme`, the allowed values, and a best-effort layer list that includes `~/.agent-runner/config.yaml`

#### Scenario: Project config overrides resolved in pre-validation
- **WHEN** the project's `.agent-runner/config.yaml` overrides `profiles.default.agents.planner.model` and a fresh, non-builtin run references the `planner` agent
- **THEN** pre-validation resolves the effective `(cli, model, effort)` triple using the merged config and probes that triple, not the unmerged global value

#### Scenario: Profile resolution failure names the chain
- **WHEN** an agent definition extends a parent profile that does not exist in the active profile set
- **THEN** pre-validation fails with an error naming the agent, the missing parent, the active profile set, and the best-effort layer list searched
