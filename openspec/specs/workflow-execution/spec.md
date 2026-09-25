# Capability: workflow-execution

## Purpose

Defines how the runner dispatches agent step execution to CLI adapters through direct terminal handoff or headless process execution.
## Requirements
### Requirement: Agent step execution dispatch
The runner's agent step executor SHALL resolve the agent profile before delegating CLI invocation. For `session: new` steps, the profile is resolved from the step's `agent` field. For `session: resume` or `session: inherit` steps, the profile is inherited from the session-originating step. The step's optional `mode` override is applied on top of the resolved profile's `default_mode`. Per-step `model` and `cli` overrides, if present, take precedence over the profile's values. Interactive steps and autonomous steps routed to the interactive backend SHALL execute via direct terminal handoff. Autonomous-headless steps SHALL execute via direct process execution with piped output. Both paths use the adapter for argument construction.

#### Scenario: New session step dispatched
- **WHEN** the runner executes an agent step with `session: new` and `agent: interactive_base`
- **THEN** the runner resolves the `interactive_base` profile, determines mode from the profile's `default_mode` (or the step's `mode` override), and dispatches via direct terminal handoff for interactive or direct headless exec for autonomous

#### Scenario: Resume step with mode override
- **WHEN** the runner executes an agent step with `session: resume` and `mode: autonomous`, and the inherited profile has `default_mode: interactive`
- **THEN** the runner inherits the profile from the session-originating step, overrides mode to autonomous, and dispatches via direct exec

#### Scenario: Resume step with no overrides
- **WHEN** the runner executes an agent step with `session: resume` and no `mode`, `model`, or `cli` overrides
- **THEN** the runner inherits the profile from the session-originating step and uses all profile values as-is

#### Scenario: Resume step with per-step model override
- **WHEN** the runner executes an agent step with `session: resume` and `model: sonnet`, and the inherited profile has model=opus
- **THEN** the runner uses sonnet for that step's CLI invocation, not the profile's opus

#### Scenario: Inherit step resolves profile from session origin
- **WHEN** the runner executes an agent step with `session: inherit` and no overrides
- **THEN** the runner inherits the profile from the session-originating step and uses all profile values as-is

### Requirement: Resume prompt messaging

When a workflow is resumed via `--resume`, the **first agent step** that executes SHALL receive resume-specific messaging. All subsequent agent steps in that run SHALL receive normal (non-resume) messaging. Non-agent steps (shell, group, loop, sub-workflow containers) do not consume or emit resume messaging — only agent-step execution consults `WorkflowResumed`. The resume messaging is distinct from `session: resume` session reuse — a step that reuses a CLI session during normal (non-resumed) workflow execution is NOT a workflow resume.

For interactive steps on adapters that support system prompts, the runner uses a short positional prompt only when starting a fresh CLI session. On a resumed CLI session, `input.Prompt` contains the full current step instructions, with `buildStepPrefix` before the step text and the completion instruction at the end; `input.SystemPrompt` is empty. Enrichment or a profile system prompt causes the positional prompt to be wrapped in `<system>` tags. The messaging rules below apply to agent steps only:

| Condition | `input.Prompt` | `buildStepPrefix` delivery |
|---|---|---|
| Workflow resumed, first agent step, existing CLI session | Full step prompt containing `Resuming step: "{step}". If you already started on this step, resume from where you left off.` | Positional user prompt |
| Workflow resumed, first agent step, fresh CLI session | `"Resume the {step} step."` | System prompt, with resume messaging |
| Session reuse (`session: resume`), normal flow | Full step prompt containing `Continuing to step "{step}".` before the step text | Positional user prompt |
| New CLI session, normal flow | `"Let's start the {step} step"` | System prompt, with workflow description when available |

The `WorkflowResumed` flag SHALL be set on `ExecutionContext` when `opts.From` is non-empty (indicating a `--resume` invocation). The flag is consumed by agent-step execution (see `internal/exec/agent.go`) and cleared after the first agent step uses it, so only that first agent step receives resume messaging. Non-agent steps executed before the first agent step SHALL NOT consume the flag.

#### Scenario: Workflow resumed — first agent step gets resume messaging
- **WHEN** a workflow is resumed via `--resume` and the first agent step executes
- **THEN** the step prefix includes "If you already started on this step, resume from where you left off."; it appears before the step text in the full positional prompt when reusing a CLI session, or goes in the system prompt with `"Resume the {step} step."` as the positional prompt when starting a fresh CLI session

#### Scenario: Workflow resumed — second agent step gets normal messaging
- **WHEN** a workflow is resumed via `--resume` and the second agent step executes (after the first resumed agent step completes)
- **THEN** there is no resume prefix; a reused CLI session gets the full step prompt with `Continuing to step "{step}".` before the step text in the user message, while a fresh CLI session gets `"Let's start the {step} step"` as its short user prompt

#### Scenario: Session reuse without workflow resume
- **WHEN** a step has `session: resume` during a normal (non-resumed) workflow run
- **THEN** the user prompt contains the full step instructions with `Continuing to step "{step}".` before the step text, with no workflow resume messaging and no adapter system prompt
