# run-failure-evidence Specification

## Purpose
TBD - created by archiving change failure-recovery. Update Purpose after archive.
## Requirements
### Requirement: Failure record for every failed check

When a shell or script step exits non-zero, Agent Runner SHALL record a failure record for that execution containing the step ID, exit code, stdout, stderr, and attempt number. When the same sequential scope contains an earlier agent step execution, the record SHALL also identify the most recent such execution (the guarded execution), regardless of how many shell, script, sub-workflow, or skipped steps lie between it and the check, and SHALL contain its identity (audit prefix and attempt), its final response, and the final responses of its recorded agent calls. Skipped steps and steps in other scopes are never the guarded execution. The guarded execution SHALL be identified by execution identity, never by position, and that identity SHALL be persisted with the check's state so the record can be rebuilt from audit evidence after an interruption between the guarded execution and the check. Responses of repair agents and rerun targets SHALL be recorded separately from the guarded execution and SHALL NOT replace it. The record SHALL be written whether or not the step declares `repair`.

#### Scenario: Failed check after an agent step
- **WHEN** `verify-draft-pr` exits 1 immediately after the agent step `open-draft-pr`
- **THEN** the failure record contains the check's stderr and exit code and `open-draft-pr`'s final response identified by its audit prefix

#### Scenario: Failed check several steps after the agent
- **WHEN** `verify-task-commit` exits 1 after `generate-code` (agent), `run-validator` (sub-workflow), `check-clean` (shell), and a skipped `commit-leftovers-if-needed`
- **THEN** the failure record identifies `generate-code` as the guarded execution and contains its final response

#### Scenario: Group boundary
- **WHEN** an agent step runs, then a group containing another agent step and a failing check runs, then a check after the group fails
- **THEN** the check inside the group identifies the group's agent as guarded, and the check after the group identifies the agent that ran before the group

#### Scenario: No agent step in scope
- **WHEN** a check exits non-zero and no agent step executed earlier in the same scope
- **THEN** the failure record contains the check's output and exit code and no guarded agent response

#### Scenario: Interrupted between action and check
- **WHEN** the run is interrupted after `open-draft-pr` completes and before `verify-draft-pr` runs, then resumed, and the check fails
- **THEN** the failure record contains `open-draft-pr`'s final response rebuilt from audit by its persisted identity

#### Scenario: Guarded agent inside a sub-workflow
- **WHEN** a check inside a sub-workflow fails after the preceding sibling agent step in that sub-workflow
- **THEN** the failure record identifies that agent execution by its full nested prefix

#### Scenario: Guarded agent used agent calls
- **WHEN** the preceding agent step made two `call_agent` calls and the check then fails
- **THEN** the failure record contains the agent step's final response and both children's final responses, each labeled with its call identity

### Requirement: Classified failure reason

The run's root failure reason SHALL be derived from the failure record of the failing check: the step ID, then the first non-empty line of stderr (or the exit code when stderr is empty), then `blocked: <first line of the declaring response's explanation>` when a blocked declaration was recorded, then `after N repair attempts` when at least one repair attempt ran. The same reason SHALL appear in the failure surface of the run view, the run list row, the debug workflow's failure reason, and the resume hint printed when a run fails.

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

### Requirement: Evidence persistence and reconstruction

Failure records SHALL be persisted in the run directory and in audit evidence so that the run view, run list, and debug workflow can reconstruct the failure reason and evidence for a historical run without the workflow definition. Resume SHALL retain earlier failure records.

#### Scenario: Historical run shows evidence
- **WHEN** a failed run is opened in the run view after the workflow file has changed
- **THEN** the failing check's failure evidence, including the guarded agent's response, is displayed

#### Scenario: Evidence survives resume
- **WHEN** a run fails at a check, is resumed, and later completes
- **THEN** the earlier failure record remains inspectable for that attempt

