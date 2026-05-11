# Task: Advanced, help agent, and onboarding chain

## Goal

Create the `advanced.yaml` (Phase 6), `help.yaml` (permanent help agent), update the top-level `onboarding.yaml` to chain all sub-workflows, and add a `?` keybinding in the list view TUI to launch the help agent.

## Background

### Existing patterns

The existing `workflows/onboarding/step-types-demo.yaml` and `workflows/onboarding/onboarding.yaml` are the models for onboarding sub-workflows and the top-level chain. Follow the same conventions.

The list view key handling is in `internal/listview/model.go` in the `Update` method's `tea.KeyMsg` switch (around line 354). Keys like `"n"`, `"c"`, `"r"` are handled there. The `?` key should follow the same pattern.

### advanced.yaml (Phase 6)

Two steps:

1. **concepts-ui** — `mode: ui`. Advanced AR concepts: workflows, sessions (new/resume/inherit), loops, validator loops, sub-workflows. This is an informational screen covering the concepts the user has just experienced.
2. **help** — `workflow: help.yaml`. Runs the help agent as a sub-workflow.

### help.yaml (permanent help agent)

Declares one named session:
- `help-session` (agent: planner)

One step:

1. **help-agent** — `session: help-session`, `mode: interactive`. Prompt references bundled docs at `{{session_dir}}/bundled/onboarding/docs/` for general AR Q&A. The agent should answer questions about Agent Runner concepts, workflows, step types, sessions, validation, and anything else covered in the bundled documentation.

This workflow is used in two ways:
- As a sub-workflow from `advanced.yaml` during onboarding
- Launched directly via the `?` keybinding as `onboarding:help`

### onboarding.yaml update

The current `onboarding.yaml` chains `step-types-demo` → `set-completed`. Update it to:

```yaml
steps:
  - id: step-types-demo
    workflow: step-types-demo.yaml
  - id: guided-workflow
    workflow: guided-workflow.yaml
  - id: validator
    workflow: validator.yaml
  - id: advanced
    workflow: advanced.yaml
  - id: set-completed
    command: agent-runner internal write-setting onboarding.completed_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
```

### `?` keybinding

In `internal/listview/model.go`, add a `case "?"` in the key-handling switch (non-search-focused state, around line 354). When pressed:

1. Resolve the workflow via `builtinworkflows.Resolve("onboarding:help")`.
2. If resolution succeeds, emit a `discovery.StartRunMsg` with a `discovery.WorkflowEntry` populated from the resolved ref.
3. If resolution fails (e.g., the builtin is missing), set `m.errMsg` and return.

Look at how `handleEnter()` and `newTabStartRunCmd()` emit `StartRunMsg` for the pattern. The `?` handler needs to construct a `WorkflowEntry` from the builtin ref rather than from the cursor selection.

Relevant imports: `builtinworkflows "github.com/codagent/agent-runner/workflows"` is already imported in `internal/listview/model.go` (check the existing imports). `internal/discovery` provides `StartRunMsg` and `WorkflowEntry`.

### Key files

- `workflows/onboarding/onboarding.yaml` — update the chain
- `workflows/onboarding/step-types-demo.yaml` — reference for conventions
- `workflows/onboarding/docs/agent-runner-basics.md` — bundled docs the help agent references
- `workflows/embed_test.go` — add resolution tests for new sub-workflows
- `internal/listview/model.go` — add `?` keybinding (around line 354 in the key switch)
- `internal/listview/model_test.go` — add test for `?` keybinding
- `internal/discovery/` — provides `StartRunMsg`, `WorkflowEntry`, `ViewDefinitionMsg`

### Bundled docs

The help agent references `{{session_dir}}/bundled/onboarding/docs/`. Source files are under `workflows/onboarding/docs/` and are automatically embedded and materialized at runtime. Do not duplicate docs.

### Testing

- **Embedding resolution**: verify `Resolve("onboarding:advanced")` and `Resolve("onboarding:help")` return the correct builtin refs.
- **Workflow shape tests**: verify `advanced.yaml` has the concepts-ui and help sub-workflow steps. Verify `help.yaml` has the help-agent step with the correct session and mode.
- **Onboarding chain test**: verify `onboarding.yaml` step IDs are `["step-types-demo", "guided-workflow", "validator", "advanced", "set-completed"]` in order.
- **`?` keybinding test**: in `internal/listview/model_test.go`, verify that sending a `?` key event produces a `StartRunMsg` for `onboarding:help`. Follow the existing test patterns in that file.

## Spec

### Requirement: Advanced concepts screen

A `mode: ui` screen SHALL present advanced Agent Runner concepts: workflows, sessions (new/resume/inherit), loops, validator loops, and sub-workflows.

#### Scenario: Concepts screen renders
- **WHEN** the advanced sub-workflow starts
- **THEN** the concepts screen renders

#### Scenario: Concepts screen covers core topics
- **WHEN** the concepts screen renders
- **THEN** it covers workflows, sessions, loops, and sub-workflows

### Requirement: Help agent Q&A in onboarding

An interactive step using the `planner` agent in a named `help-session` SHALL provide general Agent Runner Q&A with access to bundled documentation. This is the final interactive step in onboarding.

#### Scenario: Help agent runs interactively
- **WHEN** the user reaches the help agent step in onboarding
- **THEN** it runs as an interactive session with bundled docs access

#### Scenario: Help agent answers AR questions
- **WHEN** the user asks Agent Runner questions during the help step
- **THEN** the agent answers using bundled documentation

### Requirement: Help agent accessible from main menu

The help agent SHALL be accessible as a permanent entry in the main menu, independent of onboarding completion state. It SHALL use the same agent configuration as the onboarding help agent step.

#### Scenario: Help agent available from main menu
- **WHEN** the user selects the help agent from the main menu
- **THEN** an interactive help session starts

#### Scenario: Help agent available before onboarding completion
- **WHEN** onboarding has not been completed
- **THEN** the help agent is still accessible from the main menu

#### Scenario: Help agent available after onboarding completion
- **WHEN** onboarding has been completed
- **THEN** the help agent remains accessible from the main menu

### Requirement: Help agent uses bundled documentation

The help agent SHALL have access to bundled Agent Runner documentation and SHALL answer general Agent Runner questions about workflows, sessions, step types, validation, and other AR concepts.

#### Scenario: Help agent has docs access from main menu
- **WHEN** the help agent starts from the main menu
- **THEN** bundled Agent Runner documentation is available to it

#### Scenario: Help agent answers concept questions
- **WHEN** the user asks about Agent Runner concepts
- **THEN** the agent answers using bundled documentation

### Requirement: Onboarding workflow step sequence

The top-level `onboarding:onboarding` workflow SHALL chain sub-workflows in this order: `step-types-demo` → `guided-workflow` → `validator` → `advanced` → `set-completed`. Onboarding completion requires all sub-workflows to complete successfully. Cancellation (Esc) at any point aborts the workflow without writing `settings.onboarding.completed_at`.

#### Scenario: Guided workflow runs after step-types-demo
- **WHEN** `step-types-demo` completes successfully
- **THEN** `guided-workflow` runs as the next sub-workflow

#### Scenario: Validator runs after guided workflow
- **WHEN** `guided-workflow` completes successfully
- **THEN** `validator` runs as the next sub-workflow

#### Scenario: Advanced runs after validator
- **WHEN** `validator` completes successfully
- **THEN** `advanced` runs as the next sub-workflow

#### Scenario: Completion recorded after all sub-workflows
- **WHEN** `advanced` completes successfully
- **THEN** `set-completed` records `settings.onboarding.completed_at`

### Requirement: Embedded onboarding namespace contents

The `onboarding` builtin workflow namespace SHALL contain at minimum: `onboarding` (top-level), `step-types-demo`, `guided-workflow`, `validator`, `advanced`, `help`, and bundled documentation referenced by these workflows.

#### Scenario: Guided workflow resolves from namespace
- **WHEN** `onboarding:onboarding` references `workflow: guided-workflow.yaml`
- **THEN** the sub-workflow loads from the embedded namespace

#### Scenario: Validator resolves from namespace
- **WHEN** `onboarding:onboarding` references `workflow: validator.yaml`
- **THEN** the sub-workflow loads from the embedded namespace

#### Scenario: Advanced resolves from namespace
- **WHEN** `onboarding:onboarding` references `workflow: advanced.yaml`
- **THEN** the sub-workflow loads from the embedded namespace

## Done When

- `workflows/onboarding/advanced.yaml` exists with the concepts-ui and help sub-workflow steps.
- `workflows/onboarding/help.yaml` exists with the help-agent interactive step, correct session declaration, and docs reference.
- `workflows/onboarding/onboarding.yaml` chains `step-types-demo` → `guided-workflow` → `validator` → `advanced` → `set-completed`.
- `?` keybinding in `internal/listview/model.go` launches `onboarding:help`.
- Embedding resolution tests pass for `advanced` and `help`.
- Workflow shape tests verify step IDs and types for `advanced.yaml`, `help.yaml`, and the updated `onboarding.yaml`.
- Keybinding test verifies `?` produces a `StartRunMsg` for `onboarding:help`.
