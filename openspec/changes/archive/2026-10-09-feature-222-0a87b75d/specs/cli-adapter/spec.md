## ADDED Requirements

### Requirement: Effective autonomous backend resolution

For an autonomous agent step, the runner SHALL resolve the effective autonomous backend in this order.
If the step declares `autonomous_backend`, the step's value applies and the request is
**step-required**. Otherwise the user's `autonomous_backend` setting applies, and the request is
**inherited**. If neither is set, the default is `headless`. Invocation-context selection, the autonomy
system prompt enrichment, disallowed tools, and permission flags SHALL all follow the context produced
from this effective backend. `interactive-claude` SHALL request the autonomous-interactive backend only
when the step's resolved CLI is Claude. For any other CLI it SHALL resolve to autonomous-headless and the
step SHALL NOT fail.

#### Scenario: Step-required interactive with TTY
- **WHEN** an autonomous step sets `autonomous_backend: interactive`, the user's setting is `headless`, and a TTY is available
- **THEN** the runner invokes the step as autonomous-interactive with the autonomy instructions prepended to its system prompt and the autonomous permission flags applied

#### Scenario: Step-required interactive-claude with non-Claude CLI
- **WHEN** an autonomous step sets `autonomous_backend: interactive-claude` and its resolved CLI is Codex
- **THEN** the runner invokes the step as autonomous-headless without failing, regardless of TTY availability

#### Scenario: Step-required interactive-claude with Claude and TTY
- **WHEN** an autonomous step sets `autonomous_backend: interactive-claude`, its resolved CLI is Claude, and a TTY is available
- **THEN** the runner invokes the step as autonomous-interactive

### Requirement: Step-required interactive backend fails without a TTY

When an autonomous step's backend request is step-required and needs the autonomous-interactive context
(`interactive`, or `interactive-claude` with a Claude CLI), but no TTY is available, the runner SHALL fail
the step before launching the agent. It SHALL NOT fall back to autonomous-headless. The error SHALL name
the step and the requested backend and state that the autonomous-interactive backend needs a TTY. The
failure SHALL be an ordinary step failure, so the step's existing failure handling (such as
`continue_on_failure`) applies.

#### Scenario: Step-required interactive without TTY fails
- **WHEN** an autonomous step sets `autonomous_backend: interactive` and runs without a TTY (for example in CI or Docker)
- **THEN** the step fails before any agent process starts, with an error naming the step and stating that the autonomous-interactive backend requires a TTY

#### Scenario: Step-required interactive-claude with Claude and no TTY fails
- **WHEN** an autonomous step sets `autonomous_backend: interactive-claude`, its resolved CLI is Claude, and no TTY is available
- **THEN** the step fails before any agent process starts

#### Scenario: Step-required failure is per step
- **WHEN** a run without a TTY contains one autonomous step with `autonomous_backend: interactive` and another autonomous step without the field, and the user's setting is `interactive`
- **THEN** the step with the field fails, while the step without the field falls back to autonomous-headless with a warning

## MODIFIED Requirements

### Requirement: Capture forces autonomous-headless

When an autonomous step has a `capture` field and its backend request is inherited from the
`autonomous_backend` user setting, the runner SHALL force the invocation context to autonomous-headless
regardless of that setting or TTY availability. Capture requires a clean stdout pipe, which only the
headless execution path provides; the interactive backend attaches directly to the user's terminal and
does not expose its output for programmatic capture. This override is per-step — other autonomous steps
in the same run that do not use `capture` are unaffected by this rule. A step that combines `capture`
with a step-level interactive `autonomous_backend` is rejected at workflow-load time (see the
`step-model` capability), so it never reaches this override.

#### Scenario: Capture step with interactive backend forced to headless
- **WHEN** the `autonomous_backend` setting is `interactive` and an autonomous step has `capture: result`
- **THEN** the runner invokes the step as autonomous-headless and captures stdout into the variable

#### Scenario: Non-capture step unaffected
- **WHEN** the `autonomous_backend` setting is `interactive` and an autonomous step does not have `capture`
- **THEN** the runner routes the step per the normal backend and TTY rules

#### Scenario: Capture step with step-level headless backend
- **WHEN** an autonomous step has `capture: result` and `autonomous_backend: headless`
- **THEN** the runner invokes the step as autonomous-headless and captures stdout into the variable

### Requirement: TTY fallback for autonomous-interactive

When the runner determines that the invocation context should be autonomous-interactive because of an
inherited backend request (the step mode and the `autonomous_backend` user setting, with no step-level
`autonomous_backend`) but no TTY is available, the runner SHALL fall back to autonomous-headless for that
step and SHALL log a warning indicating the fallback occurred and the reason. The fallback SHALL be
per-step, not global — other steps in the same run that do have a TTY available (or that are already
autonomous-headless) are unaffected. A step-required backend request never falls back; it follows
"Step-required interactive backend fails without a TTY".

#### Scenario: No TTY triggers fallback to headless
- **WHEN** the `autonomous_backend` setting is `interactive` and the runner is executing an autonomous step without a TTY (e.g., in CI or Docker)
- **THEN** the runner invokes the step as autonomous-headless and logs a warning

#### Scenario: TTY available uses interactive backend as configured
- **WHEN** the `autonomous_backend` setting is `interactive` and the runner is executing an autonomous step with a TTY available
- **THEN** the runner invokes the step as autonomous-interactive

#### Scenario: Fallback is per-step
- **WHEN** a run contains two autonomous steps, one with TTY available and one without
- **THEN** the step without TTY falls back to autonomous-headless while the step with TTY uses autonomous-interactive

#### Scenario: Interactive-claude backend with non-Claude adapter
- **WHEN** the `autonomous_backend` setting is `interactive-claude` and the adapter is Codex (not Claude)
- **THEN** the runner invokes the step as autonomous-headless regardless of TTY availability

#### Scenario: Step-level backend does not fall back
- **WHEN** the `autonomous_backend` setting is `headless`, an autonomous step sets `autonomous_backend: interactive`, and no TTY is available
- **THEN** the runner does not fall back to autonomous-headless and the step fails
