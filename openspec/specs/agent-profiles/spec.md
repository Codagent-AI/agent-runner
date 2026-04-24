# agent-profiles Specification

## Purpose

Defines named, inheritable agent profiles that bundle CLI choice, default mode, model, effort, and system prompt into reusable units, so workflow steps reference a profile by name rather than re-declaring each attribute. Profiles support single-parent `extends` inheritance, per-step overrides for `mode`, `model`, and `cli`, and auto-generation of a default config with `interactive_base`, `headless_base`, `planner`, and `implementor` profiles when none exists.
## Requirements
### Requirement: Profile schema
Each agent profile SHALL have a name (the YAML key) and MAY include: `default_mode` (interactive|headless), `cli` (claude|codex), `model` (string), `effort` (low|medium|high), `system_prompt` (string), and `extends` (string referencing another profile name).

#### Scenario: All fields specified
- **WHEN** a profile specifies default_mode, cli, model, effort, system_prompt
- **THEN** the runner loads all values from the profile

#### Scenario: Optional fields omitted
- **WHEN** a profile omits model, effort, or system_prompt
- **THEN** the runner treats those fields as unset and does not pass them to the CLI adapter

#### Scenario: Unrecognized field
- **WHEN** a profile specifies a field not in the schema
- **THEN** the runner ignores it without error

#### Scenario: Invalid effort value
- **WHEN** a profile specifies an effort value not in (low, medium, high)
- **THEN** config loading SHALL fail with a validation error indicating the invalid effort value

#### Scenario: Invalid default_mode value
- **WHEN** a profile specifies a default_mode value not in (interactive, headless)
- **THEN** config loading SHALL fail with a validation error indicating the invalid default_mode value

### Requirement: Base profile completeness
A profile without `extends` SHALL specify at least `default_mode` and `cli`. A profile with `extends` MAY omit any field, inheriting from its parent.

#### Scenario: Base profile missing default_mode
- **WHEN** a profile has no `extends` and omits `default_mode`
- **THEN** config loading fails with a validation error indicating the missing field

#### Scenario: Base profile missing cli
- **WHEN** a profile has no `extends` and omits `cli`
- **THEN** config loading fails with a validation error indicating the missing field

#### Scenario: Child profile omits default_mode
- **WHEN** a profile has `extends` and omits `default_mode`
- **THEN** the runner inherits `default_mode` from the parent profile

### Requirement: Profile inheritance
A profile MAY specify `extends: <parent_name>`. The child inherits all fields from the parent and overrides only the fields it explicitly sets. Inheritance is single-parent. Cycles SHALL be detected and rejected at config load time.

#### Scenario: Child overrides one field
- **WHEN** a child profile extends a parent and overrides only `model`
- **THEN** the resolved profile has the parent's default_mode, cli, effort, and system_prompt, plus the child's model

#### Scenario: Inheritance cycle detected
- **WHEN** profile A extends B and profile B extends A
- **THEN** config loading fails with an error indicating a cycle in the extends chain

#### Scenario: Extends nonexistent profile
- **WHEN** a profile specifies `extends: nonexistent`
- **THEN** config loading fails with an error indicating the parent profile does not exist

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

### Requirement: Step agent attribute
An agent step SHALL specify an `agent` field naming a profile when its session strategy is `new`. When the session strategy is `resume` or `inherit`, the `agent` field SHALL NOT be specified; the step inherits the profile from the session-originating step. Shell steps SHALL NOT have an `agent` field.

#### Scenario: New session with agent specified
- **WHEN** an agent step has `session: new` and `agent: interactive_base`
- **THEN** the runner resolves that profile for the step's execution and associates it with the session

#### Scenario: New session missing agent field
- **WHEN** an agent step has `session: new` but no `agent` field
- **THEN** validation fails with an error indicating the agent field is required for new sessions

#### Scenario: Resume session inherits agent
- **WHEN** an agent step has `session: resume` and does not specify `agent`
- **THEN** the runner uses the agent profile from the session-originating step

#### Scenario: Resume session specifies agent
- **WHEN** an agent step has `session: resume` and specifies an `agent` field
- **THEN** validation fails with an error indicating agent cannot be specified on resume steps

#### Scenario: Inherit session inherits agent
- **WHEN** an agent step has `session: inherit` and does not specify `agent`
- **THEN** the runner uses the agent profile from the session-originating step

#### Scenario: Inherit session specifies agent
- **WHEN** an agent step has `session: inherit` and specifies an `agent` field
- **THEN** validation fails with an error indicating agent cannot be specified on inherit steps

#### Scenario: Shell step with agent field
- **WHEN** a shell step specifies an `agent` field
- **THEN** validation fails with an error indicating agent is not valid on shell steps

### Requirement: Step mode override
An agent step MAY include a `mode` field (interactive|headless) to override the resolved profile's `default_mode` for that step. When omitted, the runner SHALL use the profile's `default_mode`.

#### Scenario: Mode override on resume step
- **WHEN** an agent step has `session: resume` and `mode: headless`, and the inherited profile has `default_mode: interactive`
- **THEN** the runner executes the step in headless mode

#### Scenario: No mode override
- **WHEN** an agent step does not specify `mode`
- **THEN** the runner uses the resolved profile's `default_mode`

#### Scenario: Mode override on new session step
- **WHEN** an agent step has `session: new`, `agent: interactive_base`, and `mode: headless`
- **THEN** the runner executes the step in headless mode, overriding the profile's default

### Requirement: Session strategy defaults
When a step does not specify a `session` field, the runner SHALL apply defaults: the first agentic step (one with a `prompt` field) in a workflow defaults to `session: new`; all subsequent agentic steps default to `session: resume`.

#### Scenario: First agentic step with no session field
- **WHEN** the first agentic step in a workflow omits the `session` field
- **THEN** the runner treats it as `session: new`

#### Scenario: Subsequent agentic step with no session field
- **WHEN** a non-first agentic step in a workflow omits the `session` field
- **THEN** the runner treats it as `session: resume`

#### Scenario: Explicit session overrides default
- **WHEN** a non-first agentic step specifies `session: new`
- **THEN** the runner uses `session: new`, not the default of resume

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

### Requirement: Global config file location

The runner SHALL load a global agent config from `~/.agent-runner/config.yaml` (where `~` is the invoking user's home directory) when that file exists, in addition to the project-local `.agent-runner/config.yaml`. The global file is optional; the runner SHALL NOT fail if it is absent.

#### Scenario: Global config absent
- **WHEN** the runner starts and `~/.agent-runner/config.yaml` does not exist
- **THEN** the runner proceeds using only the project-local config (auto-generating it if also absent, per existing behavior)

#### Scenario: Global config present, project config present
- **WHEN** both `~/.agent-runner/config.yaml` and `.agent-runner/config.yaml` exist
- **THEN** the runner loads both files and proceeds with the merged profile set

#### Scenario: Global config present, project config absent
- **WHEN** `~/.agent-runner/config.yaml` exists and `.agent-runner/config.yaml` does not exist
- **THEN** the runner generates the project-local config (per existing auto-generation behavior), loads the global file, and proceeds with the merged profile set

#### Scenario: Global config invalid YAML
- **WHEN** `~/.agent-runner/config.yaml` exists but contains invalid YAML
- **THEN** config loading fails with an error indicating the global file path and the parse error

### Requirement: Global config is not auto-generated

The runner SHALL NOT create `~/.agent-runner/config.yaml` automatically. Users who want a global config create the file manually. (A future change will add an opt-in setup flow that may populate this file.)

#### Scenario: Global file missing on startup
- **WHEN** the runner starts and `~/.agent-runner/config.yaml` does not exist
- **THEN** the runner SHALL NOT create that file or its parent directory

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

