# Task: Config package and CLI adapter effort support

## Goal

Create the `internal/config` package for loading, validating, and resolving agent profiles from `.agent-runner/config.yaml`, and add effort level support to CLI adapter arg construction. This is purely additive — no existing behavior changes.

## Background

Agent configuration is being centralized into named profiles. This task builds the foundation: the config system that loads profiles and the CLI adapter changes that support the new effort field.

### Config package (`internal/config`)

Create a new Go package at `internal/config/` with these types and functions:

- **`Profile` struct**: fields are `DefaultMode`, `CLI`, `Model`, `Effort`, `SystemPrompt`, `Extends` — all strings, all optional via `yaml:",omitempty"`. The YAML keys should be snake_case (`default_mode`, `cli`, `model`, `effort`, `system_prompt`, `extends`).
- **`ResolvedProfile` struct**: same fields as Profile but `DefaultMode` and `CLI` are guaranteed populated after resolution. `Model`, `Effort`, `SystemPrompt` may be empty (meaning "not set").
- **`Config` struct**: has a `Profiles map[string]*Profile` field (YAML key: `profiles`).
- **`LoadOrGenerate(path string) (*Config, error)`**: if the file exists, read and parse it. If not, write a default config file and return that. After loading, validate all profiles (base completeness, extends references, cycles).
- **`(c *Config) Resolve(name string) (*ResolvedProfile, error)`**: walk the `extends` chain, merge fields (child overrides parent), return a fully-merged ResolvedProfile. Use a visited set for cycle detection during resolution.

**Validation rules:**
- A profile without `extends` must have `default_mode` and `cli` set.
- A profile with `extends` can omit any field (inherited from parent).
- `extends` must reference a profile name that exists in the config.
- Cycles in the extends chain (A→B→A) must be detected and rejected.
- Unrecognized fields on profiles should be silently ignored (YAML's default behavior with the struct).

**Default config** (generated when `.agent-runner/config.yaml` is missing):
```yaml
profiles:
  interactive_base:
    default_mode: interactive
    cli: claude
    model: opus
    effort: high
  headless_base:
    default_mode: headless
    cli: claude
    model: opus
    effort: high
  planner:
    extends: interactive_base
  implementor:
    extends: headless_base
```

### CLI adapter effort support

The existing `BuildArgsInput` struct in `internal/cli/adapter.go` needs a new `Effort string` field. Both adapters must emit the appropriate flag when effort is non-empty.

**Key files:**
- `internal/cli/adapter.go` — `BuildArgsInput` struct. Add `Effort string` field.
- `internal/cli/claude.go` — `ClaudeAdapter.BuildArgs()`. When `input.Effort` is non-empty, append the effort flag. Claude Code uses `--effort <level>` (values: low, medium, high).
- `internal/cli/codex.go` — `CodexAdapter.BuildArgs()`. When `input.Effort` is non-empty, append the effort flag. Codex uses `--effort <level>`.
- `internal/cli/adapter_test.go` — existing adapter tests. Follow the test patterns here.

**Conventions to follow:**
- The project uses standard Go testing (no testify or other frameworks).
- Test files are co-located with the source (e.g., `internal/config/config_test.go`).
- YAML parsing uses `gopkg.in/yaml.v3` (already a dependency).
- Error messages should be descriptive (see existing patterns in `internal/model/step.go` validation).

## Spec

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
When `.agent-runner/config.yaml` does not exist, the runner SHALL generate it with four default profiles:
- `interactive_base`: default_mode=interactive, cli=claude, model=opus, effort=high
- `headless_base`: default_mode=headless, cli=claude, model=opus, effort=high
- `planner`: extends interactive_base (no overrides)
- `implementor`: extends headless_base (no overrides)

#### Scenario: Config file missing on startup
- **WHEN** the runner starts and `.agent-runner/config.yaml` does not exist
- **THEN** the runner creates the file with the four default profiles and proceeds normally

#### Scenario: Config file already exists
- **WHEN** the runner starts and `.agent-runner/config.yaml` exists
- **THEN** the runner loads and uses it as-is without modifying it

### Requirement: Profile resolution
The runner SHALL resolve a profile name to a fully-merged profile by walking the `extends` chain and merging fields (child overrides parent). The resolved profile provides default_mode, cli, and optionally model, effort, and system_prompt to the executor.

#### Scenario: Effort unset after full merge
- **WHEN** a profile is resolved and `effort` is unset in both child and all ancestors
- **THEN** the runner does not pass an effort parameter to the CLI adapter

#### Scenario: System prompt inherited through extends
- **WHEN** a child profile does not set `system_prompt` and its parent has `system_prompt` set
- **THEN** the resolved profile has the parent's `system_prompt`

#### Scenario: Multi-level inheritance
- **WHEN** profile C extends B which extends A, and C sets effort, B sets model, A sets default_mode and cli
- **THEN** the resolved profile has A's default_mode and cli, B's model, and C's effort

### Requirement: Profile schema (validation portion)
The runner SHALL validate field values when loading profiles.

#### Scenario: Invalid effort value
- **WHEN** a profile specifies an effort value not in (low, medium, high)
- **THEN** config loading SHALL fail with a validation error indicating the invalid effort value

#### Scenario: Invalid default_mode value
- **WHEN** a profile specifies a default_mode value not in (interactive, headless)
- **THEN** config loading SHALL fail with a validation error indicating the invalid default_mode value

### Requirement: Adapter arg construction (effort portion)
When effort is provided, the adapter SHALL include the appropriate CLI flag for the effort level. When effort is empty, no effort flag is emitted.

#### Scenario: Effort level specified
- **WHEN** the runner provides effort level "high" to an adapter
- **THEN** the adapter includes the CLI-appropriate effort flag in the args

#### Scenario: Effort level not specified
- **WHEN** the runner provides no effort level (empty string)
- **THEN** the adapter does not include any effort flag in the args

## Done When

- `internal/config` package exists with `LoadOrGenerate`, `Resolve`, cycle detection, and validation — all tested.
- Auto-generated config file contains the four default profiles.
- `BuildArgsInput.Effort` is wired through both Claude and Codex adapters — tested.
- All existing tests still pass (`make test` or `go test ./...`).
