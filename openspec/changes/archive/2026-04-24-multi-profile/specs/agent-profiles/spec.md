## MODIFIED Requirements

### Requirement: Config file auto-generation
When `.agent-runner/config.yaml` does not exist, the runner SHALL generate it with a single profile set named `default` containing five agents:
- `interactive_base`: default_mode=interactive, cli=claude, model=opus, effort=high
- `headless_base`: default_mode=headless, cli=claude, model=opus, effort=high
- `planner`: extends interactive_base (no overrides)
- `implementor`: extends headless_base (no overrides)
- `summarizer`: default_mode=headless, cli=claude, model=haiku, effort=low

The generated file SHALL use the new nested shape:

```yaml
profiles:
  default:
    agents:
      interactive_base: { default_mode: interactive, cli: claude, model: opus, effort: high }
      headless_base:    { default_mode: headless,    cli: claude, model: opus, effort: high }
      planner:          { extends: interactive_base }
      implementor:      { extends: headless_base }
      summarizer:       { default_mode: headless, cli: claude, model: haiku, effort: low }
```

#### Scenario: Config file missing on startup
- **WHEN** the runner starts and `.agent-runner/config.yaml` does not exist
- **THEN** the runner creates the file with the five default agents nested under `profiles.default.agents` and proceeds normally

#### Scenario: Config file already exists
- **WHEN** the runner starts and `.agent-runner/config.yaml` exists
- **THEN** the runner loads and uses it as-is without modifying it

#### Scenario: Summarizer agent resolves to claude + haiku
- **WHEN** a workflow step references `agent: summarizer` and the generated config is unchanged (so the active profile is `default`)
- **THEN** the resolved agent has default_mode=headless, cli=claude, model=haiku, effort=low

### Requirement: Profile resolution
The runner SHALL resolve an agent name to a fully-merged agent definition by walking the `extends` chain within the active profile set's `agents:` map and merging fields (child overrides parent). The resolved agent provides default_mode, cli, and optionally model, effort, and system_prompt to the executor. `extends` references SHALL be resolved only against agents within the same active profile set; extending agents in non-active profile sets is not supported.

#### Scenario: Effort unset after full merge
- **WHEN** an agent is resolved and `effort` is unset in both the child and all ancestors
- **THEN** the runner does not pass an effort parameter to the CLI adapter

#### Scenario: System prompt set in resolved agent
- **WHEN** an agent is resolved and `system_prompt` is set
- **THEN** the runner prepends it to the fullPrompt string (before the step prompt and engine enrichment), which is then routed through the existing delivery mechanism unchanged

#### Scenario: System prompt combined with engine enrichment
- **WHEN** an agent has `system_prompt` set and the engine provides enrichment for the step
- **THEN** the full prompt is ordered as: [agent system_prompt] [step prompt] [engine enrichment]

#### Scenario: Agent lookup failure on resume
- **WHEN** a resume or inherit step attempts to resolve its inherited agent and no session-originating agent is found (e.g., no prior agentic step in the session chain)
- **THEN** the runner SHALL treat the step as failed with an error indicating no agent could be resolved

#### Scenario: Multi-level inheritance within active profile
- **WHEN** in the active profile set, agent C extends B which extends A, and C sets effort, B sets model, A sets default_mode and cli
- **THEN** the resolved agent has A's default_mode and cli, B's model, and C's effort

#### Scenario: Extends references agent in another profile set
- **WHEN** the active profile is `default`, which contains agent `planner` with `extends: cloud_base`, but `cloud_base` is defined only in a different profile set `copilot`
- **THEN** config loading fails with an error indicating `cloud_base` does not exist (the active profile's agents map is the only search scope for `extends`)

### Requirement: Profile merge precedence

When both a global and a project config are loaded, the runner SHALL merge them as described in the `config-profiles` capability (same-named profile sets merge their `agents:` maps). Within a single merged profile set, agents SHALL follow the precedence rule below:

- Agents whose names appear in only one file are included as-is.
- For an agent name that appears in both files (within the same profile set name), the project agent entirely replaces the global agent of the same name. Field-level merging across files SHALL NOT occur; the project agent's full body (including its `extends`, or absence thereof) is what survives the merge.

Validation (base-agent completeness, allowed values, cycle detection) SHALL run against the merged set of agents in every profile set, not only the active one.

#### Scenario: Disjoint agent names in the same profile set
- **WHEN** both files define a `default` profile set; the global file's `default.agents` contains `headless_base` and the project file's `default.agents` contains `implementor`
- **THEN** the merged `default.agents` contains both agents

#### Scenario: Same agent name in both files within the same profile set
- **WHEN** both files define a `default` profile set containing an agent named `implementor`, the global one with `extends: headless_base` and `model: opus`, and the project one with `extends: headless_base` and `cli: copilot` (no `model`)
- **THEN** the merged `default.agents.implementor` is exactly the project version (`extends: headless_base`, `cli: copilot`, no `model`); the global `model: opus` SHALL NOT be inherited

#### Scenario: Project agent drops a field present in global
- **WHEN** within the same profile set, the global `implementor` sets `effort: high` and the project `implementor` omits `effort`
- **THEN** the merged `implementor` has no `effort` (no field-level fallback to the global file)

### Requirement: Cross-file extends resolution

Within a single profile set (after global/project merging), an agent MAY specify `extends: <name>` where `<name>` is an agent defined in that profile set in either file. The runner SHALL resolve `extends` against the merged agents map of the containing profile set. Cycle detection and missing-parent detection SHALL operate on that merged map. `extends` SHALL NOT cross profile set boundaries.

#### Scenario: Project agent extends global agent in same profile set
- **WHEN** the global file's `default.agents` defines `headless_base` and the project file's `default.agents` defines `implementor` with `extends: headless_base`
- **THEN** resolving `implementor` succeeds and inherits `default_mode`, `cli`, `model`, etc. from the global `headless_base`

#### Scenario: Global agent extends project agent in same profile set
- **WHEN** the project file's `default.agents` defines a base agent `team_base` (with `default_mode` and `cli`) and the global file's `default.agents` defines `summarizer` with `extends: team_base`
- **THEN** resolving `summarizer` succeeds and inherits from the project's `team_base`

#### Scenario: Cross-file extends references unknown agent
- **WHEN** an agent in the active profile set specifies `extends: missing` and no agent named `missing` exists in that profile set in either file
- **THEN** config loading fails with an error indicating the parent agent does not exist

#### Scenario: Cross-file inheritance cycle
- **WHEN** within the same profile set, the global file defines `a` with `extends: b` and the project file defines `b` with `extends: a`
- **THEN** config loading fails with an error indicating a cycle in the extends chain

#### Scenario: Project agent shadows then extends the original global name
- **WHEN** within the same profile set, the global file defines `headless_base` and the project file defines `headless_base` with `extends: headless_base`
- **THEN** config loading fails with a cycle error (the project agent's `extends` resolves to itself in the merged set)
