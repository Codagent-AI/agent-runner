# Task: Canonicalize Standard Roles

## Goal

Establish `lead`, `crosscheck`, `implementor`, and `tester` as the canonical built-in roles while keeping `planner` and `reviewer` as safe, warning-producing compatibility aliases. Cut built-in workflows over to the canonical names and route acceptance-flow testing through Tester without changing retries, evidence handling, or session behavior.

## Background

Agent Runner currently defines its default agents and layer-merging behavior in `internal/config/config.go`; profile consumers resolve names through `Config.Resolve` from workflow execution, prevalidation, and runtime agent calls. The implementation must canonicalize each global or project layer before ordinary precedence is applied so a legacy override can replace a canonical built-in and a higher-layer synonym still wins. Canonicalization must also cover agent-level `extends` targets and names passed by workflow steps, named-session declarations, and runtime `call_agent` targets.

Do not rewrite legacy configuration on load. Reject a canonical and legacy synonym declared together in one profile layer with an actionable error. Return structured deprecation metadata from configuration/resolution code and emit each distinct alias warning at most once at the command or run boundary; non-TUI paths use stderr, while workflow/TUI paths must retain visible diagnostic or audit evidence.

Replace the six-agent defaults in `internal/config/config.go` with the approved seven-agent shape. Update focused coverage in `internal/config/config_test.go`, `internal/config/autonomous_profiles_test.go`, resolution consumers such as `internal/exec/agent.go`, `internal/exec/agent_call.go`, and `internal/prevalidate/pipeline.go`, plus command/run warning tests.

Update canonical role references throughout embedded workflow YAML and its tests. Planning leadership uses `lead`; independent planning-artifact challenge uses `crosscheck`. In particular, `workflows/openspec/implement-change2.yaml` and `workflows/openspec/accept-change.yaml` must call `tester` for initial and targeted acceptance testing. Preserve acceptance-call freshness, retry limits, verification scopes, evidence paths, and handoff behavior. Update other built-in workflow role references and user-facing workflow prompt terminology consistently, including `workflows/core/`, `workflows/openspec/`, `workflows/spec-driven/`, and `workflows/onboarding/`, with corresponding assertions in `workflows/embed_test.go` and onboarding workflow tests.

## Spec

### Requirement: Legacy planning-role aliases

The runner SHALL treat `planner` as a deprecated alias for `lead` and `reviewer` as a deprecated alias for `crosscheck` wherever agent profile names are accepted, including profile entries, `extends` targets, workflow agent references, and runtime agent-call targets. Each global or project configuration layer SHALL be canonicalized before normal layer precedence is applied, so a legacy override remains effective for a canonical built-in workflow and a higher-layer alias overrides a lower-layer synonym.

Using either legacy alias SHALL surface a non-fatal, user-visible deprecation warning that names its canonical replacement and SHALL remain supported without a scheduled removal date. Each distinct alias warning SHALL appear at most once per command or workflow run. Loading legacy configuration SHALL NOT rewrite its source file.

A single profile layer that declares both `lead` and `planner`, or both `crosscheck` and `reviewer`, SHALL fail validation with an actionable ambiguity error. Native setup migration after explicit overwrite confirmation is governed by the agent-profile-editor capability.

#### Scenario: Legacy config override serves canonical workflow
- **WHEN** a user config layer defines `planner` with a custom CLI and a workflow references `agent: lead`
- **THEN** the runner resolves Lead from the legacy override
- **AND** emits a deprecation warning directing the user to `lead`

#### Scenario: Legacy workflow reference serves canonical config
- **WHEN** a config layer defines `lead` and a workflow or agent call references `agent: planner`
- **THEN** the runner resolves the canonical Lead profile
- **AND** emits a deprecation warning directing the user to `lead`

#### Scenario: Legacy extends target is canonicalized
- **WHEN** an agent declares `extends: reviewer` and the profile layer provides canonical `crosscheck`
- **THEN** the runner resolves the inheritance through Crosscheck
- **AND** emits a deprecation warning directing the user to `crosscheck`

#### Scenario: Higher-layer synonym wins
- **WHEN** global config defines `planner` and project config defines `lead`
- **THEN** the project Lead replaces the canonicalized global Lead under normal layer precedence

#### Scenario: Alias warning is deduplicated
- **WHEN** one command or workflow run resolves `planner` multiple times
- **THEN** it emits one `planner`-to-`lead` deprecation warning

#### Scenario: Same-layer alias pair is rejected
- **WHEN** one profile layer defines both `crosscheck` and `reviewer`
- **THEN** config validation fails with an error naming both conflicting aliases and directing the user to keep `crosscheck`

#### Scenario: Legacy file is not rewritten on load
- **WHEN** the runner loads a valid config file containing only legacy planning-role aliases
- **THEN** it leaves the file contents unchanged

### Requirement: Built-in default profile set

The runner SHALL provide an in-memory default profile set named `default` as the bottom layer of config resolution. The default set SHALL contain seven agents:

- `interactive_base`: default_mode=interactive, cli=claude, model=opus, effort=high
- `autonomous_base`: default_mode=autonomous, cli=claude, model=opus, effort=high
- `lead`: extends interactive_base with no overrides
- `crosscheck`: extends autonomous_base with no overrides
- `implementor`: extends autonomous_base with no overrides
- `tester`: default_mode=autonomous, cli=claude, model=sonnet, effort=high
- `summarizer`: default_mode=autonomous, cli=claude, model=haiku, effort=low

The runner SHALL NOT create `.agent-runner/config.yaml` or any other config file automatically. The defaults SHALL exist only as an in-memory layer beneath global and project configuration that the user has chosen to create.

#### Scenario: Project config missing on startup
- **WHEN** the runner starts and `.agent-runner/config.yaml` does not exist
- **THEN** the runner uses all seven built-in agents in memory and SHALL NOT create the file or its parent directory

#### Scenario: Project config already exists
- **WHEN** the runner starts and `.agent-runner/config.yaml` exists
- **THEN** the runner loads and uses it without modifying it

#### Scenario: Crosscheck resolves to autonomous flagship fallback
- **WHEN** a workflow step references `agent: crosscheck` with no global or project override and no step mode override
- **THEN** the resolved crosscheck has default_mode=autonomous, cli=claude, model=opus, and effort=high

#### Scenario: Legacy reviewer resolves to Crosscheck fallback
- **WHEN** a workflow step references `agent: reviewer` with no global or project override and no step mode override
- **THEN** the resolved agent has default_mode=autonomous, cli=claude, model=opus, and effort=high
- **AND** the runner emits the `reviewer`-to-`crosscheck` deprecation warning

#### Scenario: Explicit crosscheck mode override still wins
- **WHEN** a workflow step references the built-in crosscheck and specifies `mode: interactive`
- **THEN** the runner executes that step interactively while retaining the crosscheck's resolved CLI, model, and effort

#### Scenario: Tester resolves to autonomous balanced fallback
- **WHEN** a workflow step or agent call references `agent: tester` with no global or project override
- **THEN** the resolved tester has default_mode=autonomous, cli=claude, model=sonnet, and effort=high

#### Scenario: Summarizer agent resolves to Claude Haiku
- **WHEN** a workflow step references `agent: summarizer` with no global or project override
- **THEN** the resolved summarizer has default_mode=autonomous, cli=claude, model=haiku, and effort=low

## Done When

- Focused configuration, resolver, runtime agent-call, prevalidation, warning, and embedded-workflow tests cover every scenario above and pass.
- Canonicalization occurs before layer precedence and handles profile entries, `extends`, workflow/session agent references, and runtime agent calls without mutating source files.
- Same-layer synonym conflicts fail clearly, and each used legacy alias produces one undated warning per command or run with retained workflow diagnostics.
- The in-memory default profile contains exactly the approved seven agents and creates no files.
- Built-in workflows use canonical role names; planning review uses Crosscheck, and acceptance passes in `implement-change2` and `accept-change` use Tester while preserving their existing behavioral controls.
- Targeted package tests pass, followed by `make fmt`, `make test`, and `make lint`.
