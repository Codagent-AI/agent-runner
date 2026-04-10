# Task: Step model, executor integration, and workflow updates

## Goal

Wire agent profiles into the step model, execution pipeline, and state persistence, then update all workflow files to use the new schema. This is the breaking change that replaces `mode` as a step-type discriminator with profile-based resolution.

## Background

The `internal/config` package (with `LoadOrGenerate`, `Resolve`, Profile/ResolvedProfile types) and CLI adapter effort support already exist. This task integrates them into the step model, executor, and runner.

### Step model changes (`internal/model/step.go`)

The `Step` struct needs:
- **New field**: `Agent string` (`yaml:"agent,omitempty"`) — names an agent profile from `.agent-runner/config.yaml`.
- **Mode repurposed**: `Mode` stays on the struct but is now an optional override (interactive|headless), not a type discriminator. Remove the `ModeShell` constant — shell steps are identified by the `command` field alone.
- **`StepType()`** must change: agent steps are identified by `step.Prompt != "" || step.Agent != ""` instead of `step.Mode == ModeInteractive || step.Mode == ModeHeadless`. Same for `isAgentContext()` and `hasExactlyOneStepType()`.
- **`ApplyDefaults()`** must change for session strategy: the workflow-level `ApplyDefaults()` must track whether the first agentic step (one with a `prompt` field) has been seen. The first gets `session: new`, all subsequent get `session: resume`. Explicit `session` values are never overwritten.
- **Validation** must enforce:
  - `agent` is required when `session` is `new` on agent steps.
  - `agent` is forbidden when `session` is `resume` or `inherit`.
  - `agent` is forbidden on shell steps (steps with `command`).
  - `mode` values restricted to `interactive` and `headless` only (remove `shell` as a valid value).
  - Remove validation that required `mode: shell` steps to have `command` — mode no longer identifies shell steps.

**Key files:**
- `internal/model/step.go` — Step struct, StepType(), isAgentContext(), hasExactlyOneStepType(), ApplyDefaults(), Validate(), validateFieldConstraints()
- `internal/model/step_test.go` — existing step tests
- `internal/validate/` — additional workflow validation constraints
- `internal/validate/workflow_test.go` — existing validation tests

### ExecutionContext changes (`internal/model/context.go`)

- **Add** `ProfileStore interface{}` — holds `*config.Config` at runtime (stored as `interface{}` to avoid circular imports, following the `EngineRef` pattern). Must be propagated to child contexts (loop iteration, sub-workflow).
- **Add** `SessionProfiles map[string]string` — maps session-originating step ID to profile name. When a new-session step stores its session ID, it also stores its profile name here. Resume/inherit steps read from this map using `ctx.LastSessionStepID` as the key.
- Initialize `SessionProfiles` in `NewRootContext` (empty map). Propagate in `NewLoopIterationContext` and `NewSubWorkflowContext` (same pattern as `SessionIDs`).

**Key files:**
- `internal/model/context.go` — ExecutionContext, NewRootContext, NewLoopIterationContext, NewSubWorkflowContext, RootContextOptions
- `internal/model/context_test.go`

### State persistence (`internal/model/state.go`)

- **Add** `SessionProfiles map[string]string` to `NestedStepState` (JSON key: `sessionProfiles`). This is persisted alongside `SessionIDs` so that resume-after-restart can recover which profile a session uses.

**Key files:**
- `internal/model/state.go` — NestedStepState struct
- `internal/model/state_test.go`
- `internal/runner/runner.go` — `writeStepState()` must populate `SessionProfiles` from the context

### Executor changes (`internal/exec/agent.go`)

The `ExecuteAgentStep` function currently reads `step.Mode`, `step.Model`, and `step.CLI` directly. It needs to:

1. **Resolve the profile**: New `resolveStepProfile()` function:
   - For `session: new` steps: read `step.Agent`, type-assert `ctx.ProfileStore` to `*config.Config`, call `Resolve(step.Agent)`.
   - For `session: resume/inherit` steps: look up the profile name from `ctx.SessionProfiles[ctx.LastSessionStepID]`, then resolve it.
   - Apply step-level overrides: `step.Mode` overrides `resolved.DefaultMode`, `step.Model` overrides `resolved.Model`, `step.CLI` overrides `resolved.CLI`.

2. **Update `resolveMode()`**: currently defaults to `ModeInteractive` when `step.Mode` is empty. Must now use the resolved profile's `DefaultMode` as the default, with `step.Mode` as override.

3. **Prepend system_prompt**: if the resolved profile has `SystemPrompt`, prepend it to the `fullPrompt`. The order is: `[profile system_prompt]\n\n[step prompt]\n\n[engine enrichment]`.

4. **Pass effort**: populate `BuildArgsInput.Effort` from the resolved profile's effort.

5. **Store session profile**: when a new-session step stores its session ID (`ctx.SessionIDs[step.ID] = ...`), also store `ctx.SessionProfiles[step.ID] = step.Agent`.

6. **Update `resolveAdapterAndSession()`**: currently defaults CLI to "claude" when `step.CLI` is empty. Must now use the resolved profile's CLI as the default, with `step.CLI` as override.

**Key files:**
- `internal/exec/agent.go` — ExecuteAgentStep, resolveMode, resolveAdapterAndSession, buildAgentPrompt
- `internal/exec/agent_test.go`

### Dispatch changes (`internal/exec/dispatch.go`)

Agent detection changes from:
```go
step.Mode == model.ModeInteractive || step.Mode == model.ModeHeadless
```
to:
```go
step.Agent != "" || step.Prompt != ""
```

**Key files:**
- `internal/exec/dispatch.go` — DispatchStep, hasExactlyOneStepType (if referenced)
- `internal/exec/dispatch_test.go`

### Runner changes (`internal/runner/runner.go`)

- In `initRunState()`: load the config via `config.LoadOrGenerate(".agent-runner/config.yaml")` before creating the ExecutionContext. Pass the loaded config as `ProfileStore` in `RootContextOptions`.
- Add `ProfileStore` to `RootContextOptions`.

**Key files:**
- `internal/runner/runner.go` — initRunState, RunWorkflow, Options
- `internal/runner/runner_test.go`

### Workflow file updates

All 5 workflow files must be updated:
- Remove `mode: shell` from all shell steps (it was redundant — `command` identifies them).
- On the first agentic step in each workflow: add `agent: <profile_name>`, remove `session: resume` if present (defaults to `new`).
- On subsequent agentic steps: remove `mode: interactive` or `mode: headless` and `session: resume` (both defaulted). Add `mode: headless` only where the step needs to override the profile's `default_mode`.
- The profile to use for each workflow:
  - `plan-change.yaml`: first agentic step (`proposal`) gets `agent: interactive_base`. Steps `specs`, `design`, `tasks` are resume (no agent). Steps `pre-review-validate`, `review`, `post-review-validate` need `mode: headless`.
  - `implement-task.yaml`: first agentic step (`implement`) gets `agent: headless_base` and `session: new`. Steps `self-review`, `commit-leftovers-if-needed`, `session-report` are resume (no agent). No mode overrides needed (profile default is headless).
  - `implement-change.yaml`: `finalize` is the only direct agentic step, so it gets `agent: headless_base` and session defaults to `new` (no explicit `session` needed).
  - `run-validator.yaml`: `fix-violations` step uses `session: inherit`. Since it inherits, no `agent` needed. No mode override needed.
  - `smoke-test.yaml`: first agentic step (`greet`) gets `agent: interactive_base`. Step `how-are-you` is resume (no agent).

**Key files:**
- `workflows/plan-change.yaml`
- `workflows/implement-task.yaml`
- `workflows/implement-change.yaml`
- `workflows/run-validator.yaml`
- `workflows/smoke-test.yaml`

## Spec

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
An agent step MAY include a `mode` field (interactive|headless) to override the resolved profile's `default_mode` for that step. When omitted, the profile's `default_mode` is used.

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

### Requirement: Step mode field (REMOVED)
**Reason**: The `mode` field as a step-type discriminator is removed. Shell steps are identified by the `command` field. Agent steps are identified by the `prompt` and/or `agent` field. The execution mode (interactive/headless) is determined by the resolved agent profile's `default_mode`, with an optional per-step `mode` override.
**Migration**: Replace `mode: interactive` or `mode: headless` with an `agent` profile reference on the first agentic step. For subsequent steps that resume the session, remove the `mode` field entirely or use the optional `mode` override to switch between interactive and headless.

### Requirement: Per-step model override
A step MAY include a `model` field specifying which model the agent should use. When present, the runner SHALL pass the model to the CLI adapter, overriding the model from the resolved agent profile. When absent, the profile's model is used (which may itself be unset, in which case no model is passed to the CLI). The `model` field is only valid on agent steps, not shell steps.

#### Scenario: Model specified overrides profile
- **WHEN** an agent step has `agent: headless_base` (profile model=opus) and `model: sonnet`
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
A step MAY include a `cli` field specifying which CLI backend to use. When present, it overrides the cli from the resolved agent profile. When absent, the profile's cli is used. The `cli` field is only valid on agent steps, not shell steps.

#### Scenario: CLI specified overrides profile
- **WHEN** an agent step has `agent: headless_base` (profile cli=claude) and `cli: codex`
- **THEN** the runner uses the Codex adapter for that step

#### Scenario: CLI not specified, uses profile
- **WHEN** an agent step has no `cli` field and the resolved profile has cli=claude
- **THEN** the runner uses the Claude adapter

#### Scenario: CLI on shell step
- **WHEN** a shell step has a `cli` field
- **THEN** the runner fails with a validation error

### Requirement: Agent step execution dispatch
The runner's agent step executor SHALL resolve the agent profile before delegating CLI invocation. For `session: new` steps, the profile is resolved from the step's `agent` field. For `session: resume` or `session: inherit` steps, the profile is inherited from the session-originating step. The step's optional `mode` override is applied on top of the resolved profile's `default_mode`. Per-step `model` and `cli` overrides, if present, take precedence over the profile's values. Interactive steps SHALL execute via the PTY layer. Headless steps SHALL execute via direct process execution. Both paths use the adapter for arg construction.

#### Scenario: New session step dispatched
- **WHEN** the runner executes an agent step with `session: new` and `agent: interactive_base`
- **THEN** the runner resolves the `interactive_base` profile, determines mode from the profile's `default_mode` (or the step's `mode` override), and dispatches via PTY for interactive or direct exec for headless

#### Scenario: Resume step with mode override
- **WHEN** the runner executes an agent step with `session: resume` and `mode: headless`, and the inherited profile has `default_mode: interactive`
- **THEN** the runner inherits the profile from the session-originating step, overrides mode to headless, and dispatches via direct exec

#### Scenario: Resume step with no overrides
- **WHEN** the runner executes an agent step with `session: resume` and no `mode`, `model`, or `cli` overrides
- **THEN** the runner inherits the profile from the session-originating step and uses all profile values as-is

#### Scenario: Resume step with per-step model override
- **WHEN** the runner executes an agent step with `session: resume` and `model: sonnet`, and the inherited profile has model=opus
- **THEN** the runner uses sonnet for that step's CLI invocation, not the profile's opus

### Requirement: Profile resolution (system_prompt portion)

#### Scenario: System prompt set in resolved profile
- **WHEN** a profile is resolved and `system_prompt` is set
- **THEN** the runner prepends it to the fullPrompt string (before the step prompt and engine enrichment), which is then routed through the existing delivery mechanism unchanged

#### Scenario: System prompt combined with engine enrichment
- **WHEN** a profile has `system_prompt` set and the engine provides enrichment for the step
- **THEN** the full prompt is ordered as: [profile system_prompt] [step prompt] [engine enrichment]

#### Scenario: Profile lookup failure on resume
- **WHEN** a resume or inherit step attempts to resolve its inherited profile and no session-originating profile is found (e.g., no prior agentic step in the session chain)
- **THEN** the runner SHALL treat the step as failed with an error indicating no profile could be resolved

### Requirement: Agent step execution dispatch (inherit scenario)

#### Scenario: Inherit step resolves profile from session origin
- **WHEN** the runner executes an agent step with `session: inherit` and no overrides
- **THEN** the runner inherits the profile from the session-originating step and uses all profile values as-is

## Done When

- Step model correctly identifies agent steps by `prompt`/`agent` fields, not by `mode`.
- Session defaults work: first agentic step defaults to `new`, subsequent to `resume`.
- Validation enforces `agent` required/forbidden rules and rejects `mode: shell`.
- `ExecuteAgentStep` resolves profiles, prepends system_prompt, passes effort, stores session profiles.
- `SessionProfiles` persisted in state.json and restored on resume.
- All 5 workflow files updated and loadable.
- All tests pass (`make test` or `go test ./...`).
