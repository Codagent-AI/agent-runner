## ADDED Requirements

### Requirement: last_step_failure_kind built-in variable

The runner SHALL expose `{{last_step_failure_kind}}` in every step. Its value SHALL be the failure kind of the preceding step that `skip_if: previous_success` evaluates in the same scope: `infrastructure`, `step`, or the empty string when that step did not fail or exhaust, or when no step has yet run in the scope. The variable SHALL always be present, so interpolating it never fails as unresolved. It SHALL follow the existing built-in precedence rules.

#### Scenario: After a crashed agent step
- **WHEN** an agent step with `continue_on_failure: true` crashes and the next step's command is `echo {{last_step_failure_kind}}`
- **THEN** the command prints `infrastructure`

#### Scenario: After an ordinary failure
- **WHEN** a shell step with `continue_on_failure: true` exits non-zero and the next step references `{{last_step_failure_kind}}`
- **THEN** it resolves to `step`

#### Scenario: After a success
- **WHEN** the preceding step succeeded
- **THEN** `{{last_step_failure_kind}}` resolves to the empty string

#### Scenario: First step in scope
- **WHEN** the first step of a workflow or loop body references `{{last_step_failure_kind}}`
- **THEN** it resolves to the empty string without an interpolation error

### Requirement: last_step_crash_observed built-in variable

The runner SHALL expose `{{last_step_crash_observed}}` in every step. Its value SHALL be `true` when the preceding step that `skip_if: previous_success` evaluates in the same scope reported a crash-observed signal, and `false` otherwise, including when no step has yet run in the scope. The variable SHALL always be present and SHALL follow the existing built-in precedence rules.

#### Scenario: After a recovered sub-workflow
- **WHEN** the preceding sub-workflow step succeeded after absorbing an agent crash
- **THEN** `{{last_step_crash_observed}}` resolves to `true`

#### Scenario: After a clean step
- **WHEN** the preceding step involved no agent crash
- **THEN** `{{last_step_crash_observed}}` resolves to `false`

#### Scenario: Param shadows the variable
- **WHEN** a workflow declares a param named `last_step_crash_observed` and the caller passes `custom`
- **THEN** `{{last_step_crash_observed}}` resolves to `custom`

### Requirement: Last-step variables follow each scope's skip tracking

The last-step built-in variables SHALL change exactly when the outcome that `skip_if: previous_success` evaluates changes in that scope, and SHALL NOT change at any other time. Each scope keeps its existing skip behavior:

- In a group or a loop body, a skipped step becomes the preceding step, so the variables resolve to the empty string and `false`.
- At the top level of a workflow and inside a sub-workflow, a skipped step does not replace the preceding step, so the variables keep describing the last step that executed.

#### Scenario: Skip at top level keeps the crash visible
- **WHEN** a top-level agent step with `continue_on_failure: true` crashes, the next top-level step is skipped, and the step after that references both variables
- **THEN** they resolve to `infrastructure` and `true`

#### Scenario: Skip inside a sub-workflow keeps the crash visible
- **WHEN** inside a sub-workflow an agent step with `continue_on_failure: true` crashes, the next step is skipped, and the step after that references both variables
- **THEN** they resolve to `infrastructure` and `true`

#### Scenario: Skip inside a group resets the variables
- **WHEN** inside a group an agent step with `continue_on_failure: true` crashes, the next group member is skipped, and the member after that references both variables
- **THEN** they resolve to the empty string and `false`

#### Scenario: Skip inside a loop body resets the variables
- **WHEN** inside a loop body an agent step with `continue_on_failure: true` crashes, the next body step is skipped, and the body step after that references both variables
- **THEN** they resolve to the empty string and `false`
