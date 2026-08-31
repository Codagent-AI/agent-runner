## MODIFIED Requirements

### Requirement: Script step shape and field rules

A workflow step MAY declare `script: <path>` to invoke a script bundled alongside the workflow YAML. A script step SHALL NOT also set any other step-type field (`command`, `prompt`, `agent`, `workflow`, `loop`, or nested `steps`). A script step SHALL NOT set `cli`, `model`, `mode`, or `session`. A script step MAY set `workdir`, `capture`, `capture_stderr`, `skip_if`, `break_if`, `continue_on_failure`, `warn_on_failure`, `script_inputs`, and `capture_format`.

#### Scenario: Minimal script step validates

- **WHEN** a step declares `script: detect-adapters.sh` and no other step-type fields
- **THEN** validation succeeds and the step is loaded as a script step

#### Scenario: Script step combined with command rejected

- **WHEN** a step declares both `script: x.sh` and `command: echo hi`
- **THEN** validation fails with an error indicating exactly one step type is allowed

#### Scenario: Script step combined with agent rejected

- **WHEN** a step declares both `script: x.sh` and `agent: planner`
- **THEN** validation fails with an error indicating exactly one step type is allowed

#### Scenario: Agent-only fields rejected on script step

- **WHEN** a script step sets `cli: claude`, `model: opus`, `mode: autonomous`, or `session: new`
- **THEN** validation fails with an error indicating that field is not valid on script steps

#### Scenario: Warning modifier allowed on script step

- **WHEN** a script step declares `warn_on_failure: true`
- **THEN** validation succeeds and a failed script execution follows the generic non-blocking warning behavior
