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
When `.agent-runner/config.yaml` does not exist, the runner SHALL generate it with five default profiles:
- `interactive_base`: default_mode=interactive, cli=claude, model=opus, effort=high
- `headless_base`: default_mode=headless, cli=claude, model=opus, effort=high
- `planner`: extends interactive_base (no overrides)
- `implementor`: extends headless_base (no overrides)
- `summarizer`: default_mode=headless, cli=claude, model=haiku, effort=low

#### Scenario: Config file missing on startup
- **WHEN** the runner starts and `.agent-runner/config.yaml` does not exist
- **THEN** the runner creates the file with the five default profiles and proceeds normally

#### Scenario: Config file already exists
- **WHEN** the runner starts and `.agent-runner/config.yaml` exists
- **THEN** the runner loads and uses it as-is without modifying it

#### Scenario: Summarizer profile resolves to claude + haiku
- **WHEN** a workflow step references `agent: summarizer` and the generated config is unchanged
- **THEN** the resolved profile has default_mode=headless, cli=claude, model=haiku, effort=low

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
The runner SHALL resolve a profile name to a fully-merged profile by walking the `extends` chain and merging fields (child overrides parent). The resolved profile provides default_mode, cli, and optionally model, effort, and system_prompt to the executor.

#### Scenario: Effort unset after full merge
- **WHEN** a profile is resolved and `effort` is unset in both child and all ancestors
- **THEN** the runner does not pass an effort parameter to the CLI adapter

#### Scenario: System prompt set in resolved profile
- **WHEN** a profile is resolved and `system_prompt` is set
- **THEN** the runner prepends it to the fullPrompt string (before the step prompt and engine enrichment), which is then routed through the existing delivery mechanism unchanged

#### Scenario: System prompt combined with engine enrichment
- **WHEN** a profile has `system_prompt` set and the engine provides enrichment for the step
- **THEN** the full prompt is ordered as: [profile system_prompt] [step prompt] [engine enrichment]

#### Scenario: Profile lookup failure on resume
- **WHEN** a resume or inherit step attempts to resolve its inherited profile and no session-originating profile is found (e.g., no prior agentic step in the session chain)
- **THEN** the runner SHALL treat the step as failed with an error indicating no profile could be resolved

#### Scenario: Multi-level inheritance
- **WHEN** profile C extends B which extends A, and C sets effort, B sets model, A sets default_mode and cli
- **THEN** the resolved profile has A's default_mode and cli, B's model, and C's effort

