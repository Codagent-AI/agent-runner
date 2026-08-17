## ADDED Requirements

### Requirement: intake_handoff built-in variable

The runner SHALL expose `{{intake_handoff}}` as a built-in template variable in every run, without exception. Its value SHALL be the **contents** of the sealed handoff when the run was launched from intake, and the empty string otherwise. The handoff SHALL be bounded to a prompt-safe size when the route is submitted.

For an intake-launched run, Agent Runner SHALL also prepend the handoff to the first agent prompt automatically. Workflow definitions SHALL NOT need to reference the built-in to receive it. The automatic prefix SHALL tell the agent that the context was already provided by the user and that the user should not be asked to repeat it.

Unlike other built-ins, it SHALL be present even when its value is empty, so that a workflow referencing it never fails interpolation on a directly invoked run. Its value SHALL be preserved across resume, so a resumed run sees the same value its original invocation had.

#### Scenario: Intake-launched run resolves the handoff contents
- **WHEN** a run launched from intake interpolates `{{intake_handoff}}`
- **THEN** the runner replaces it with the contents of that run's sealed handoff

#### Scenario: First agent receives intake context automatically
- **WHEN** a workflow launched from intake reaches its first agent step
- **THEN** the runner prepends the sealed handoff contents to that step's prompt
- **AND** the workflow need not reference `{{intake_handoff}}`

#### Scenario: Later agent prompts do not repeat intake context
- **WHEN** an intake-launched workflow reaches an agent step after an agent session has already started
- **THEN** the runner does not prepend the intake handoff again

#### Scenario: Direct run resolves to empty
- **WHEN** a run invoked directly from the CLI or the workflow browser interpolates `{{intake_handoff}}`
- **THEN** the runner replaces it with the empty string and interpolation succeeds

#### Scenario: Reference does not fail on a direct run
- **WHEN** a workflow prompt references `{{intake_handoff}}` and the workflow is invoked directly
- **THEN** the step runs without an unresolved-variable failure

#### Scenario: Resumed intake-launched run keeps its handoff
- **WHEN** a run launched from intake is interrupted and later resumed, and a step interpolates `{{intake_handoff}}`
- **THEN** it resolves to the same handoff contents it had before the interruption

#### Scenario: Resumed direct run stays empty
- **WHEN** a directly invoked run is resumed and a step interpolates `{{intake_handoff}}`
- **THEN** it resolves to the empty string

## MODIFIED Requirements

### Requirement: Built-in precedence

Built-in variables have the **lowest** interpolation precedence. A workflow `params` entry or a captured variable with the same name as a built-in SHALL shadow the built-in.

`intake_handoff` is exempt: it is a reserved name. A workflow SHALL NOT declare a parameter with that name, and a step SHALL NOT capture into it through **any** capture sink, including both ordinary output capture and UI-step outcome capture. Such a workflow SHALL be rejected, so the sealed handoff can never be shadowed by workflow-supplied data.

#### Scenario: Param shadows built-in
- **WHEN** a workflow declares `params: [step_id]` and a caller passes `step_id: custom-value`
- **THEN** `{{step_id}}` in that step resolves to `custom-value`, not the actual step ID

#### Scenario: Captured variable shadows built-in
- **WHEN** a prior step captures output into a variable named `session_dir`
- **THEN** `{{session_dir}}` in subsequent steps resolves to the captured value, not the session directory path

#### Scenario: Param named intake_handoff is rejected
- **WHEN** a workflow declares a parameter named `intake_handoff`
- **THEN** the workflow is rejected with an error stating the name is reserved

#### Scenario: Capture named intake_handoff is rejected
- **WHEN** a step captures its output into a variable named `intake_handoff`
- **THEN** the workflow is rejected with an error stating the name is reserved

#### Scenario: UI outcome capture named intake_handoff is rejected
- **WHEN** a UI step captures its outcome into a variable named `intake_handoff`
- **THEN** the workflow is rejected with an error stating the name is reserved
