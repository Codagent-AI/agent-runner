## ADDED Requirements

### Requirement: intake_handoff built-in variable

The runner SHALL expose `{{intake_handoff}}` as a built-in template variable in every run, without exception. Its value SHALL be the **contents** of the sealed handoff when the run was launched from intake, and the empty string otherwise. Supplying the contents rather than a location is what makes the handoff actually reach the agent: a path reaches it only if the agent chooses to read the file, which no workflow can guarantee.

The runner SHALL bound the inlined contents by a limit separate from, and much smaller than, the limit bounding the handoff file itself. When the sealed handoff exceeds that limit, the runner SHALL supply the leading portion cut at a line boundary followed by a marker naming the full handoff path, so that a verbose handoff degrades the prompt rather than failing the run.

Unlike other built-ins, it SHALL be present even when its value is empty, so that a workflow referencing it never fails interpolation on a directly invoked run. Its value SHALL be preserved across resume, so a resumed run sees the same value its original invocation had.

#### Scenario: Intake-launched run resolves the handoff contents
- **WHEN** a run launched from intake interpolates `{{intake_handoff}}`
- **THEN** the runner replaces it with the contents of that run's sealed handoff

#### Scenario: Oversized handoff is truncated rather than rejected
- **WHEN** a sealed handoff longer than the inline limit is interpolated
- **THEN** the value is its leading portion cut at a line boundary, followed by a marker naming the full handoff path
- **AND** the step runs

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

### Requirement: intake_handoff_path built-in variable

The runner SHALL expose `{{intake_handoff_path}}` as a built-in template variable in every run. Its value SHALL be the absolute path of the sealed handoff when the run was launched from intake, and the empty string otherwise. It exists for workflows that need to address the handoff file itself rather than read its contents. It SHALL follow the same rules as `{{intake_handoff}}`: always present even when empty, preserved across resume, and reserved against shadowing.

#### Scenario: Intake-launched run resolves the sealed path
- **WHEN** a run launched from intake interpolates `{{intake_handoff_path}}`
- **THEN** the runner replaces it with the absolute path of that run's sealed handoff

#### Scenario: Path addresses the same handoff that was inlined
- **WHEN** a step interpolates both `{{intake_handoff}}` and `{{intake_handoff_path}}`
- **THEN** the path names the file whose contents produced the inlined value

#### Scenario: Direct run resolves to empty
- **WHEN** a directly invoked run interpolates `{{intake_handoff_path}}`
- **THEN** the runner replaces it with the empty string and interpolation succeeds

#### Scenario: Resumed intake-launched run keeps its handoff path
- **WHEN** a run launched from intake is resumed and a step interpolates `{{intake_handoff_path}}`
- **THEN** it resolves to the same sealed handoff path it had before the interruption

## MODIFIED Requirements

### Requirement: Built-in precedence

Built-in variables have the **lowest** interpolation precedence. A workflow `params` entry or a captured variable with the same name as a built-in SHALL shadow the built-in.

`intake_handoff` and `intake_handoff_path` are exempt: both are reserved names. A workflow SHALL NOT declare a parameter with either name, and a step SHALL NOT capture into either name through **any** capture sink, including both ordinary output capture and UI-step outcome capture. Such a workflow SHALL be rejected, so the sealed handoff can never be shadowed by workflow-supplied data.

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

#### Scenario: The handoff path name is reserved the same way
- **WHEN** a workflow declares a parameter named `intake_handoff_path`, or a step captures into that name through any capture sink
- **THEN** the workflow is rejected with an error stating the name is reserved
