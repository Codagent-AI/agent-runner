## ADDED Requirements

### Requirement: Per-step autonomous backend

An agent step MAY include an `autonomous_backend` field. Its allowed values are `headless`,
`interactive`, and `interactive-claude`, the same values and meanings as the user setting of the same
name. When the step resolves to autonomous mode, the field SHALL decide that step's autonomous backend
in place of the user's `autonomous_backend` setting. When the field is absent, the step SHALL use the
user setting as before. The field SHALL apply only to the step that declares it; it SHALL NOT be
inherited by steps nested inside a group, loop, or sub-workflow.

Validation SHALL fail at workflow-load time when the field has any other value. The error SHALL name the
invalid value and list the allowed values.

#### Scenario: Step backend overrides the user setting
- **WHEN** the user's `autonomous_backend` setting is `headless` and an autonomous agent step sets `autonomous_backend: interactive`
- **THEN** the runner resolves that step's backend as `interactive`

#### Scenario: Step can force headless
- **WHEN** the user's `autonomous_backend` setting is `interactive` and an autonomous agent step sets `autonomous_backend: headless`
- **THEN** the runner invokes that step as autonomous-headless

#### Scenario: Absent field uses the user setting
- **WHEN** an autonomous agent step has no `autonomous_backend` field and the user's setting is `interactive`
- **THEN** the runner resolves that step's backend from the user setting

#### Scenario: Override is per step
- **WHEN** a workflow has two autonomous agent steps, only the first sets `autonomous_backend: interactive`, and the user's setting is `headless`
- **THEN** the first step resolves to `interactive` and the second step resolves to `headless`

#### Scenario: Not inherited by nested steps
- **WHEN** a sub-workflow contains an autonomous agent step without `autonomous_backend`
- **THEN** that nested step resolves its backend from the user setting, regardless of fields on the parent workflow's steps

#### Scenario: Invalid value rejected
- **WHEN** an agent step sets `autonomous_backend: tty`
- **THEN** validation fails at workflow-load time with an error naming `tty` and listing `headless`, `interactive`, and `interactive-claude`

### Requirement: `autonomous_backend` field limited to autonomous agent steps

The `autonomous_backend` field SHALL be valid only on agent steps. Validation SHALL fail at
workflow-load time when it is set on a shell step, a `script:` step, a `mode: ui` step, a sub-workflow
step, or a group or loop step. It SHALL also fail when it is set on an agent step that declares
`mode: interactive`.

When an agent step omits `mode`, its mode may resolve from the agent profile's `default_mode` at
execution time. If such a step sets `autonomous_backend` and its mode resolves to interactive, the step
SHALL fail before the agent is launched. The error SHALL say that `autonomous_backend` requires
autonomous mode.

#### Scenario: Shell step with autonomous_backend
- **WHEN** a shell step sets `autonomous_backend: interactive`
- **THEN** validation fails with an error indicating that `autonomous_backend` is only valid on agent steps

#### Scenario: UI step with autonomous_backend
- **WHEN** a step has `mode: ui` and sets `autonomous_backend: headless`
- **THEN** validation fails with an error indicating that `autonomous_backend` is only valid on agent steps

#### Scenario: Script step with autonomous_backend
- **WHEN** a step declares `script: detect.sh` and sets `autonomous_backend: interactive`
- **THEN** validation fails with an error indicating that `autonomous_backend` is only valid on agent steps

#### Scenario: Sub-workflow step with autonomous_backend
- **WHEN** a step declares `workflow: implement-task` and sets `autonomous_backend: interactive`
- **THEN** validation fails with an error indicating that `autonomous_backend` is only valid on agent steps

#### Scenario: Explicit interactive mode with autonomous_backend
- **WHEN** an agent step has `mode: interactive` and sets `autonomous_backend: interactive`
- **THEN** validation fails with an error indicating that `autonomous_backend` requires autonomous mode

#### Scenario: Profile resolves to interactive at execution time
- **WHEN** an agent step omits `mode`, sets `autonomous_backend: interactive`, and its agent profile's `default_mode` is `interactive`
- **THEN** the step fails before the agent is launched with an error indicating that `autonomous_backend` requires autonomous mode

#### Scenario: Profile resolves to autonomous at execution time
- **WHEN** an agent step omits `mode`, sets `autonomous_backend: interactive`, and its agent profile's `default_mode` is `autonomous`
- **THEN** the step runs as autonomous and its backend is resolved from the step field

### Requirement: Interactive `autonomous_backend` rejected with capture

An agent step SHALL NOT combine `capture` with `autonomous_backend: interactive` or
`autonomous_backend: interactive-claude`. Captured output needs the headless backend, so the
combination contradicts itself. Validation SHALL fail at workflow-load time instead of silently running
the step headless. `capture` combined with `autonomous_backend: headless` SHALL be valid.

#### Scenario: Capture with interactive step backend
- **WHEN** an autonomous agent step sets `capture: result` and `autonomous_backend: interactive`
- **THEN** validation fails with an error indicating that `capture` cannot be combined with an interactive `autonomous_backend`

#### Scenario: Capture with interactive-claude step backend
- **WHEN** an autonomous agent step sets `capture: result` and `autonomous_backend: interactive-claude`
- **THEN** validation fails with an error indicating that `capture` cannot be combined with an interactive `autonomous_backend`

#### Scenario: Capture with headless step backend
- **WHEN** an autonomous agent step sets `capture: result` and `autonomous_backend: headless`
- **THEN** validation succeeds and the step runs as autonomous-headless with its output captured
