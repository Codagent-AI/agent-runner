# Task: Rename headless to autonomous + adapter permission alignment

## Goal

Rename the "headless" mode concept to "autonomous" throughout the codebase and align all five CLI adapters to use least-permissive permission flags for autonomous steps. This also introduces the `InvocationContext` type that replaces the `Headless bool` on `BuildArgsInput`, establishing the three-context model (interactive, autonomous-headless, autonomous-interactive) that the routing task builds on.

## Background

The codebase currently uses "headless" to mean autonomous agent execution. This is a naming mismatch — "headless" describes an invocation mechanism (no TTY/print mode), not the intent (autonomous operation). The project is pre-release; `mode: headless` is a hard break, no deprecation aliases.

**Key files and what to change:**

- `internal/model/step.go` — Rename `ModeHeadless` constant to `ModeAutonomous` (value changes from `"headless"` to `"autonomous"`). Update the validation at line ~388 that checks valid mode values. Update the `capture` validation messages (lines ~314-323) to say "autonomous" instead of "headless".

- `internal/config/config.go` — Update `validDefaultMode` map from `{"interactive": true, "headless": true}` to `{"interactive": true, "autonomous": true}`. Rename built-in profile `headless_base` to `autonomous_base` with `default_mode: "autonomous"`. Update `implementor` to extend `autonomous_base`. Update `summarizer` to `default_mode: "autonomous"`.

- `internal/cli/adapter.go` — Define the `InvocationContext` named string type with three constants and helper methods:
  ```go
  type InvocationContext string

  const (
      ContextInteractive           InvocationContext = "interactive"
      ContextAutonomousHeadless    InvocationContext = "autonomous-headless"
      ContextAutonomousInteractive InvocationContext = "autonomous-interactive"
  )

  func (c InvocationContext) IsInteractive() bool { return c == ContextInteractive }
  func (c InvocationContext) IsAutonomous() bool  { return c != ContextInteractive }
  func (c InvocationContext) IsHeadless() bool    { return c == ContextAutonomousHeadless }
  ```
  Replace `Headless bool` in `BuildArgsInput` with `Context InvocationContext`. Also update the `HeadlessResultFilter` optional interface name if referenced — keep the interface name as-is since it describes behavior specific to headless output filtering, not the mode concept.

- `internal/cli/claude.go` — Switch on `input.Context` instead of `input.Headless`. Emit `-p` only when `input.Context.IsHeadless()`. When `input.Context.IsAutonomous()`, add `--permission-mode`, `acceptEdits` (two separate args). Currently passes nothing beyond `-p` for headless, which fails on fresh installs.

- `internal/cli/codex.go` — Replace `--dangerously-bypass-approvals-and-sandbox` with `--sandbox`, `workspace-write` when `input.Context.IsAutonomous()`. Emit the `exec` subcommand only when `input.Context.IsHeadless()`.

- `internal/cli/opencode.go` — Remove `--dangerously-skip-permissions` from autonomous args entirely. Default behavior already allows workspace edits without prompting.

- `internal/cli/copilot.go` — Replace `--allow-all` with `--allow-tool='write'` when `input.Context.IsAutonomous()`. When `input.Context == ContextAutonomousInteractive`, also add `--autopilot`. Emit `-p` and `-s` only when `input.Context.IsHeadless()`; emit `-i` otherwise.

- `internal/cli/cursor.go` — Remove `--force` from autonomous args. Keep `--trust` (required for non-interactive Cursor). Emit print-mode flag only when `input.Context.IsHeadless()`.

- `internal/exec/agent.go` — Rename `headlessPreamble` to `autonomyPreamble`. Replace `headless := mode == model.ModeHeadless` with computing an `InvocationContext`. For this task, all autonomous steps produce `ContextAutonomousHeadless` (the routing logic for interactive backend comes in the next task). Replace all `headless` bool uses with context method calls. Update `buildAdapterInput` to set `Context` instead of `Headless`. Update `continueMarkerForMode` and `runAgentProcess` to use the context. Update `AskUserQuestion` blocking to check `context.IsAutonomous()`.

- `internal/exec/dispatch.go` — Update the `PrepareStepHook(interactive bool)` call to derive the boolean from the context.

- `workflows/*.yaml` — Replace all `mode: headless` with `mode: autonomous` (46 occurrences across workflow YAML files).

- `docs/` — Update all references to "headless" mode in: `USER-GUIDE.md`, `WHY-AGENT-RUNNER.md`, `LOOPS-AND-SUBWORKFLOWS.md`, `workflow-diagram-notes.md`, `agent_runner_onboarding_workflow_spec.md`. Replace "headless" with "autonomous" where it refers to the mode concept. Keep "headless" where it refers to the invocation mechanism (e.g., "headless backend").

- `internal/cli/adapter_test.go` — Update all test cases that check headless arg construction. Each adapter test should verify autonomous-headless args (with the new permission flags) and interactive args (no permission flags). Add test cases for autonomous-interactive context (no print-mode flag, permission flags present, adapter-specific autonomy flags where applicable).

**Conventions:**
- This project uses `google/go-cmp` for structured comparisons in tests.
- Run `make fmt` (goimports) after changes.
- Tests live next to source packages.

## Spec

### Requirement: Profile schema
Each agent profile SHALL have a name (the YAML key) and MAY include: `default_mode` (interactive|autonomous), `cli` (claude|codex), `model` (string), `effort` (low|medium|high), `system_prompt` (string), and `extends` (string referencing another profile name).

#### Scenario: Invalid default_mode value
- **WHEN** a profile specifies a default_mode value not in (interactive, autonomous)
- **THEN** config loading SHALL fail with a validation error indicating the invalid default_mode value

### Requirement: Built-in default profile set
The runner SHALL provide an in-memory default profile set named `default` as the bottom layer of config resolution. The default set contains five agents:
- `interactive_base`: default_mode=interactive, cli=claude, model=opus, effort=high
- `autonomous_base`: default_mode=autonomous, cli=claude, model=opus, effort=high
- `planner`: extends interactive_base (no overrides)
- `implementor`: extends autonomous_base (no overrides)
- `summarizer`: default_mode=autonomous, cli=claude, model=haiku, effort=low

#### Scenario: Summarizer agent resolves to claude + haiku
- **WHEN** a workflow step references `agent: summarizer` with no project or global overrides (so the active profile is `default`)
- **THEN** the resolved agent has default_mode=autonomous, cli=claude, model=haiku, effort=low

### Requirement: Step mode override
An agent step MAY include a `mode` field (interactive|autonomous) to override the resolved profile's `default_mode` for that step. When omitted, the runner SHALL use the profile's `default_mode`.

#### Scenario: Mode override on resume step
- **WHEN** an agent step has `session: resume` and `mode: autonomous`, and the inherited profile has `default_mode: interactive`
- **THEN** the runner executes the step in autonomous mode

#### Scenario: Mode override on new session step
- **WHEN** an agent step has `session: new`, `agent: interactive_base`, and `mode: autonomous`
- **THEN** the runner executes the step in autonomous mode, overriding the profile's default

### Requirement: Adapter arg construction
Each adapter SHALL construct the CLI invocation args for three invocation contexts: interactive, autonomous-headless, and autonomous-interactive. In autonomous-interactive context, adapters SHALL include adapter-specific autonomy flags where the backing CLI supports them (e.g., `--autopilot` for Copilot).

#### Scenario: Autonomous-headless invocation with model override
- **WHEN** the runner executes an autonomous-headless step with `model: sonnet` and a session ID from state
- **THEN** the adapter returns args that include the prompt, model flag, session resume flag, and headless/print-mode flag appropriate to that CLI

#### Scenario: Interactive invocation with no session
- **WHEN** the runner executes an interactive step with session strategy `new`
- **THEN** the adapter returns args for a fresh interactive session (no resume flag, no headless flag)

#### Scenario: Autonomous-interactive invocation
- **WHEN** the runner executes an autonomous-interactive step
- **THEN** the adapter returns args for an interactive session (no headless/print-mode flag) and includes any adapter-specific autonomy flags

#### Scenario: Autonomous-interactive with autonomy flag (Copilot)
- **WHEN** the runner executes an autonomous-interactive step using the Copilot adapter
- **THEN** the adapter includes `--autopilot` in the args alongside the interactive session flags

### Requirement: Adapter mode coverage
Every registered CLI adapter SHALL support all three invocation contexts: interactive, autonomous-headless, and autonomous-interactive.

#### Scenario: Autonomous-headless step succeeds for any registered CLI
- **WHEN** an agent step runs in autonomous-headless context with any registered `cli`
- **THEN** the runner spawns the CLI without emitting an unsupported-context error

#### Scenario: Autonomous-interactive step succeeds for any registered CLI
- **WHEN** an agent step runs in autonomous-interactive context with any registered `cli`
- **THEN** the runner spawns the CLI without emitting an unsupported-context error

### Requirement: No permission loosening in interactive mode
In interactive context, no adapter SHALL emit a flag that bypasses or pre-approves the underlying CLI's permission/approval prompts. Autonomous invocations (both headless and interactive backend) MAY emit such flags.

#### Scenario: Adapter omits permission-grant flags in interactive context
- **WHEN** any adapter constructs args for an interactive step
- **THEN** the args do not include any flag that auto-approves tools, paths, URLs, or commands (e.g., `--allow-all`, `--force`, `--yolo`, `--dangerously-skip-permissions`)

#### Scenario: Autonomous-headless adapter MAY include permission-grant flags
- **WHEN** any adapter constructs args for an autonomous-headless step
- **THEN** the adapter MAY include CLI-specific permission-grant flags as needed for unattended autonomous operation

#### Scenario: Autonomous-interactive adapter MAY include permission-grant flags
- **WHEN** any adapter constructs args for an autonomous-interactive step
- **THEN** the adapter MAY include CLI-specific permission-grant flags as needed for unattended autonomous operation

### Requirement: AskUserQuestion blocked in headless, allowed in interactive
The runner SHALL include `AskUserQuestion` in the disallowed-tools list for every autonomous agent step — regardless of whether the backend is headless or interactive. The runner SHALL leave the disallowed-tools list empty for every interactive agent step.

#### Scenario: Autonomous-headless step blocks AskUserQuestion
- **WHEN** the runner builds adapter input for an autonomous-headless agent step
- **THEN** the disallowed-tools list includes `"AskUserQuestion"`

#### Scenario: Autonomous-interactive step blocks AskUserQuestion
- **WHEN** the runner builds adapter input for an autonomous-interactive agent step
- **THEN** the disallowed-tools list includes `"AskUserQuestion"`

#### Scenario: Interactive step does not block AskUserQuestion
- **WHEN** the runner builds adapter input for an interactive agent step
- **THEN** the disallowed-tools list is empty

## Done When

- `ModeHeadless` is renamed to `ModeAutonomous` throughout, `headless_base` to `autonomous_base`
- `InvocationContext` type exists with three constants and helper methods
- `BuildArgsInput.Headless` is replaced with `BuildArgsInput.Context`
- All five adapters switch on `input.Context` with updated permission flags
- All workflow YAML uses `mode: autonomous`
- All docs references updated
- Tests covering the above scenarios pass, including adapter arg tests for all three contexts
- `make test` and `make lint` pass
