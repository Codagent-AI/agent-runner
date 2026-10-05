## ADDED Requirements

### Requirement: Agent crash failure origin

When an agent execution is classified as an infrastructure failure, Agent Runner SHALL record a failure origin for it. The origin SHALL contain:

- the agent step ID;
- its full audit prefix, identifying its scope and nesting;
- its attempt number;
- the exit code, when one exists;
- the invocation error message, when one exists;
- a stderr excerpt whose total size, including any truncation marker, never exceeds 4096 bytes. Stderr of 4096 bytes or fewer is kept whole. Longer stderr keeps a head and a tail around a fixed truncation marker. The bytes remaining after the marker are split so the head gets half, rounded down, and the tail gets the rest. Each cut moves inward to a UTF-8 character boundary, so the excerpt is always valid UTF-8.

It SHALL be kept separately from the check failure record, and it SHALL NOT replace or merge with that record's guarded-execution evidence.

The origin follows this lifecycle:

- The run's terminating failure origin SHALL be the origin of the infrastructure failure that ended the run. It is selected only when the run's failure kind is `infrastructure`, so a crash that was absorbed, or that was followed by a step that succeeded, never supplies the evidence or reason for a later failure.
- A later failure that terminates the same scope replaces the origin as that scope's terminating failure evidence.
- Every origin SHALL remain inspectable on the end event of the execution that recorded it, including after the step later succeeds in a repair replay or on resume.

Origins SHALL be persisted in the run directory and in audit evidence, so a historical run's crash can be reconstructed without the workflow definition.

#### Scenario: Origin for a direct agent crash
- **WHEN** agent step `generate-code` crashes on attempt 1 with stderr `Selected model is at capacity`
- **THEN** a failure origin records `generate-code`, its full prefix, attempt 1, its exit code, and that stderr line

#### Scenario: Long stderr is bounded
- **WHEN** a crashed agent wrote 10,000 bytes of stderr
- **THEN** the origin's stderr excerpt is at most 4096 bytes in total, is valid UTF-8, begins with the start of stderr, ends with the end of stderr, and contains the truncation marker between them

#### Scenario: Stale origin does not leak into a later reason
- **WHEN** an agent step with `continue_on_failure: true` crashes, a later shell check in the same scope fails and stops the run
- **THEN** the run's terminating failure evidence is the check's failure record, and the earlier crash origin remains inspectable on the crashed step

#### Scenario: Origin survives resume
- **WHEN** a run stops on an agent crash and is resumed, and the step then succeeds
- **THEN** the earlier origin remains inspectable on that earlier attempt's end event, and the completed run has no terminating failure origin

## MODIFIED Requirements

### Requirement: Classified failure reason

The run's root failure reason SHALL be derived from the evidence of the failure that ended the run. When the run ended with failure kind `infrastructure`, the reason SHALL be derived from the agent crash failure origin: the step ID, then ` failed (infrastructure): `, then the first non-empty line of its error or stderr. When the error and stderr are both empty, it SHALL use the exit code, or `agent session did not finish` when no exit code exists. Otherwise the reason SHALL be derived from the failure record of the failing check: the step ID, then the first non-empty line of stderr (or the exit code when stderr is empty), then `blocked: <first line of the declaring response's explanation>` when a blocked declaration was recorded, then `after N repair attempts` when at least one repair attempt ran. When the crashed agent is an inline repair agent, the step ID in the reason SHALL be the owning check's ID followed by ` repair`. When an infrastructure failure ends a check's repair cycle, the reason SHALL also end with `after N repair attempts`. The same reason SHALL appear in the failure surface of the run view, the run list row, the debug workflow's failure reason, and the resume hint printed when a run fails.

#### Scenario: Blocked failure reason
- **WHEN** `verify-draft-pr` fails after `open-draft-pr` declared `REPAIR_BLOCKED` with a first line of `push rejected: token lacks workflow scope`
- **THEN** the failure reason is `verify-draft-pr failed: expected exactly one open pull request for branch 'dev', found 0; blocked: push rejected: token lacks workflow scope`

#### Scenario: Exhausted failure reason
- **WHEN** `check-plan` fails after two repair attempts
- **THEN** the failure reason ends with `after 2 repair attempts`

#### Scenario: Plain failure reason unchanged
- **WHEN** a check without `repair` fails and no agent preceded it
- **THEN** the failure reason is the step ID and its first stderr line, as today

#### Scenario: Resume hint carries the reason
- **WHEN** a headless run fails at a check
- **THEN** the console prints the classified failure reason above the `to resume:` hint

#### Scenario: Agent crash reason
- **WHEN** a run stops because agent step `generate-code` exited non-zero with stderr `Selected model is at capacity`
- **THEN** the failure reason is `generate-code failed (infrastructure): Selected model is at capacity`

#### Scenario: Agent crash inside a sub-workflow
- **WHEN** a run stops because an agent step inside a builtin sub-workflow crashed, and an earlier check in the run had failed under `continue_on_failure`
- **THEN** the failure reason names the crashed agent step and its error, not the earlier check

#### Scenario: Repair agent crash reason
- **WHEN** a run stops because the inline repair agent of check `check-plan` crashed on its only attempt with stderr `Selected model is at capacity`
- **THEN** the failure reason is `check-plan repair failed (infrastructure): Selected model is at capacity after 1 repair attempts`

#### Scenario: Crash reason shown in run list and resume hint
- **WHEN** a headless run stops on an agent crash
- **THEN** the run list row and the console's resume hint show the same `failed (infrastructure)` reason
