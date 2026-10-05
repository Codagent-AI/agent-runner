# infrastructure-failure-classification Specification

## Purpose
TBD - created by archiving change feature-211-1543081d. Update Purpose after archive.
## Requirements
### Requirement: Agent crash classification

Agent Runner SHALL classify a failed agent execution as an **infrastructure failure** when the agent session did not finish. An agent execution has not finished when any of the following holds:

- the agent CLI process could not be launched;
- a headless agent CLI exited with a non-zero exit code after the adapter's existing headless result filtering;
- a headless agent CLI was terminated by a signal that Agent Runner did not send;
- Agent Runner terminated the agent CLI because the adapter detected that the session had stalled without producing its result;
- the agent CLI's process ended abnormally in a way that left no exit status, for example output pipes held open past the process wait limit without completed output;
- Agent Runner's own invocation machinery failed once the step had started the invocation, including failure to establish the runner control channel, failure to hand the terminal to an interactive session, or a session-durability failure.

An invocation that Agent Runner cancelled because the run itself was being stopped SHALL NOT be an infrastructure failure.

This applies to workflow agent steps in every mode, to inline repair agents, and to agent steps re-executed by a `rerun` repair. The step's outcome SHALL remain `failed`.

#### Scenario: Model at capacity
- **WHEN** a headless agent step's CLI exits with code 1 and stderr `Selected model is at capacity`
- **THEN** the step's outcome is `failed` and its failure kind is `infrastructure`

#### Scenario: CLI cannot be launched
- **WHEN** the configured agent CLI binary cannot be started for an agent step
- **THEN** the step's outcome is `failed` and its failure kind is `infrastructure`

#### Scenario: CLI killed externally
- **WHEN** a headless agent CLI is killed by a signal that Agent Runner did not send
- **THEN** the step's outcome is `failed` and its failure kind is `infrastructure`

#### Scenario: Adapter filtering still applies
- **WHEN** a headless CLI exits non-zero but the adapter's headless result filter maps the result to exit code 0
- **THEN** the step is not a failure and has no failure kind

#### Scenario: Stalled session terminated by Agent Runner
- **WHEN** a headless agent adapter detects a result stall and Agent Runner terminates the CLI
- **THEN** the step's outcome is `failed` and its failure kind is `infrastructure`

#### Scenario: Run stop cancels the agent
- **WHEN** Agent Runner cancels a running agent invocation because the run is being stopped
- **THEN** the step is not classified as an infrastructure failure and no crash is recorded

#### Scenario: Runner control channel failure
- **WHEN** an agent step cannot bind the runner control channel it needs before releasing the session
- **THEN** the step's outcome is `failed` and its failure kind is `infrastructure`

### Requirement: Ordinary agent failures

An agent step failure that is not an infrastructure failure SHALL have failure kind `step`. This SHALL include errors detected before the agent invocation starts (profile resolution, prompt interpolation, unsupported mode for the adapter, invalid invocation arguments) and sessions that finished but that Agent Runner judged failed: an autonomous session that attempted a disallowed `AskUserQuestion`, and a parent session that ended with agent calls still uncollected. Every failed or exhausted shell, script, check, UI, or other non-agent step SHALL have failure kind `step`. Failures inside `call_agent` child agents SHALL NOT make the calling agent step an infrastructure failure on their own.

#### Scenario: Prompt interpolation error
- **WHEN** an agent step fails because its prompt references an undefined variable
- **THEN** the step's failure kind is `step`

#### Scenario: Disallowed interactive prompt
- **WHEN** an autonomous agent session finishes with exit code 0 but its stderr shows a disallowed `AskUserQuestion` tool call
- **THEN** the step's failure kind is `step`

#### Scenario: Shell step failure
- **WHEN** a shell step exits with code 2
- **THEN** the step's failure kind is `step`

#### Scenario: Child agent call crashes
- **WHEN** a `call_agent` child's CLI crashes and the parent agent session then finishes with exit code 0
- **THEN** the parent step succeeds, has no failure kind, and its crash-observed signal is false

### Requirement: Failure kind is independent of outcome

The failure kind SHALL be reported only for steps whose terminal outcome is `failed` or `exhausted`, and it SHALL be `infrastructure` or `step`. Successful, skipped, and aborted steps SHALL have no failure kind. Classification SHALL NOT change any outcome value. Nor SHALL it change how `continue_on_failure`, `skip_if: previous_success`, `break_if`, `warn_on_failure`, warnings, run exit status, or resume treat a failed or exhausted step. A step that terminates with `warning` status SHALL keep the failure kind of its underlying outcome. User aborts and interrupts, including Ctrl-C and a user ending an interactive session without a continue trigger, SHALL remain `aborted` and are not infrastructure failures.

#### Scenario: Crash with continue_on_failure proceeds
- **WHEN** an agent step with `continue_on_failure: true` crashes
- **THEN** the workflow continues to the next step exactly as for any failed step, and the crashed step's failure kind is `infrastructure`

#### Scenario: Crash without continue_on_failure stops the workflow
- **WHEN** an agent step without `continue_on_failure` crashes
- **THEN** the workflow stops failed with the same exit status as any other failed run

#### Scenario: Crash on a warning step
- **WHEN** an agent step with `warn_on_failure: true` crashes
- **THEN** the step terminates with status `warning`, outcome `failed`, and failure kind `infrastructure`

#### Scenario: User interrupt
- **WHEN** the user interrupts a run while a headless agent step is executing
- **THEN** the step is `aborted` and has no failure kind

### Requirement: Container failure kind

A group, loop, or sub-workflow step that terminates `failed` or `exhausted` SHALL take the failure kind of the failure that ended it. A group or sub-workflow that stopped because a child failed takes that child's failure kind. A loop that failed because an iteration failed takes that iteration's terminating failure kind. A loop that exhausted its iterations takes the failure kind of the last iteration's terminating failure, or `step` when its last iteration ended without a failure. A failure that a child step absorbed through `continue_on_failure` or `warn_on_failure` SHALL NOT set the container's failure kind. This applies at every nesting depth, and the top-level run's failure kind is that of the step that ended it. A container or run stopped by an Agent Runner orchestration error rather than by a child step's failure SHALL have failure kind `step` and no failure origin. Such errors include a `skip_if` evaluation error and a resume-resolution or replay-priming error. This holds even when an earlier crash was absorbed in that scope; that crash still sets the crash-observed signal.

#### Scenario: Orchestration error after an absorbed crash
- **WHEN** a top-level agent step with `continue_on_failure: true` crashes, and the next top-level step's `skip_if` expression fails to evaluate and stops the run
- **THEN** the run's failure kind is `step` with no failure origin, and its crash-observed signal is true

#### Scenario: Crash ends a group
- **WHEN** an agent step inside a group crashes and the group stops because of it
- **THEN** the group's outcome is `failed` with failure kind `infrastructure`

#### Scenario: Absorbed crash then ordinary failure
- **WHEN** a group contains an agent step with `continue_on_failure: true` that crashes, followed by a shell check that exits non-zero and stops the group
- **THEN** the group's failure kind is `step`

#### Scenario: Crash ends a loop
- **WHEN** an agent step without `continue_on_failure` crashes in iteration 2 of a loop
- **THEN** the loop's outcome is `failed` with failure kind `infrastructure`

#### Scenario: Nested propagation
- **WHEN** an agent step crashes inside a group inside a sub-workflow inside the top-level workflow, and no level absorbs the failure
- **THEN** the group, the sub-workflow step, and the run all report failure kind `infrastructure`

### Requirement: Crash-observed signal

Every step SHALL report a crash-observed signal that is true when any infrastructure failure occurred within that step's execution. For a container, this covers its own execution and every descendant. The signal SHALL count crashes regardless of whether they were absorbed by `continue_on_failure` or `warn_on_failure`, occurred during a repair attempt or a rerun replay, or were followed by recovery. The signal SHALL be reported for steps whose terminal outcome is success, failed, exhausted, or warning. For an aborted step, it SHALL reflect crashes that occurred before the abort. For a skipped step, it SHALL be false. Once true for a step execution, nothing later in that execution SHALL clear it, and a container's signal SHALL be true whenever any descendant's signal is true. The top-level run SHALL report the signal for the whole run.

#### Scenario: Recovered sub-workflow still reports the crash
- **WHEN** an agent step with `continue_on_failure: true` crashes inside a sub-workflow, and the sub-workflow then completes successfully
- **THEN** the sub-workflow step's outcome is `success` and its crash-observed signal is true

#### Scenario: Loop recovers in a later iteration
- **WHEN** a loop body's agent step with `continue_on_failure: true` crashes in iteration 1 and iteration 2 meets `break_if: success`
- **THEN** the loop's outcome is `success` and its crash-observed signal is true

#### Scenario: No crash
- **WHEN** a sub-workflow completes in which no agent session crashed
- **THEN** its crash-observed signal is false

#### Scenario: Run-level signal
- **WHEN** a run completes successfully after an absorbed agent crash anywhere in its step tree
- **THEN** the run's crash-observed signal is true

### Requirement: Classification survives resume

Failure kinds, crash-observed signals, and failure origins SHALL be persisted with run state, so an interrupted or failed run that is resumed reports the same values for every step that completed before the interruption. When resume re-executes a step, including a step inside a container that resume re-enters, that step's failure kind and crash-observed signal SHALL be determined by its new execution. A container re-entered by resume SHALL combine the persisted signals of its children that completed before the interruption with the signals of its re-executed children. Each resumable scope's preceding-step record SHALL also be persisted and restored before the first step that resume runs in that scope. The record is the outcome, failure kind, and crash-observed signal that `skip_if: previous_success` and the last-step built-in variables read.

#### Scenario: Resume right after an absorbed crash
- **WHEN** a sub-workflow step with `continue_on_failure: true` fails because an agent inside it crashed, the run is interrupted before the next step starts, and the user resumes
- **THEN** the next step sees `{{last_step_failure_kind}}` = `infrastructure` and `{{last_step_crash_observed}}` = `true`, and `skip_if: previous_success` treats the preceding step as failed

#### Scenario: Resume after a crash that stopped the run
- **WHEN** a run stopped because agent step `generate-code` crashed, and the user resumes and `generate-code` then finishes successfully
- **THEN** `generate-code` has no failure kind and a false crash-observed signal for its new execution

#### Scenario: Resume keeps earlier absorbed crash
- **WHEN** a sub-workflow absorbed a crash in a step that completed, a later child step in the same sub-workflow was interrupted, and the run is resumed and completes
- **THEN** the sub-workflow step's crash-observed signal is true

#### Scenario: Historical run without classification
- **WHEN** a run directory written before this change is inspected or resumed
- **THEN** its failed steps are treated as failure kind `step` and its crash-observed signals as false

