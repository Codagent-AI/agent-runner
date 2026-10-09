## MODIFIED Requirements

### Requirement: Other step types in external-user mode

In external-user mode:
- Autonomous agent steps without a step-level `autonomous_backend` field SHALL run in the
  autonomous-headless context, regardless of the user's autonomous backend setting.
- An autonomous agent step whose step-level `autonomous_backend` requires the autonomous-interactive
  backend SHALL fail before the agent is launched. That is the case for `interactive`, and for
  `interactive-claude` when the step's resolved CLI is Claude. The error SHALL name the step and the
  requested backend and state that the autonomous-interactive backend is not supported in
  external-user mode. The step SHALL NOT run headless and SHALL NOT wait for a terminal.
- An autonomous agent step with step-level `autonomous_backend: headless`, or with
  `autonomous_backend: interactive-claude` whose resolved CLI is not Claude, SHALL run in the
  autonomous-headless context.
- Agents started by `call_agent` SHALL behave as they do outside the mode.
- Interactive shell steps and UI steps SHALL fail with an error stating that they are not supported in external-user mode. They SHALL NOT wait for a terminal.

#### Scenario: Interactive autonomous backend overridden
- **WHEN** the user's autonomous backend is `interactive-claude` and an autonomous step runs in external-user mode
- **THEN** the step runs headless

#### Scenario: Step-level interactive backend rejected
- **WHEN** an autonomous agent step sets `autonomous_backend: interactive` and runs in external-user mode
- **THEN** the step fails before the agent is launched with an error stating that the autonomous-interactive backend is not supported in external-user mode

#### Scenario: Step-level interactive-claude backend rejected for Claude
- **WHEN** an autonomous agent step sets `autonomous_backend: interactive-claude`, its resolved CLI is Claude, and it runs in external-user mode
- **THEN** the step fails before the agent is launched with an error stating that the autonomous-interactive backend is not supported in external-user mode

#### Scenario: Step-level interactive-claude backend with non-Claude CLI runs headless
- **WHEN** an autonomous agent step sets `autonomous_backend: interactive-claude`, its resolved CLI is Codex, and it runs in external-user mode
- **THEN** the step runs headless

#### Scenario: Step-level headless backend runs headless
- **WHEN** an autonomous agent step sets `autonomous_backend: headless` and runs in external-user mode
- **THEN** the step runs headless

#### Scenario: UI step rejected
- **WHEN** a workflow reaches a UI step in external-user mode
- **THEN** the step fails with an error stating that UI steps are not supported in external-user mode
