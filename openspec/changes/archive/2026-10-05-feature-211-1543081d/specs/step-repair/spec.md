## ADDED Requirements

### Requirement: Crashed repair agents

A repair agent crash SHALL be classified as an infrastructure failure under `infrastructure-failure-classification`. This covers an inline repair agent and any agent step re-executed by a `rerun` replay. The crash SHALL set the owning check's crash-observed signal even when the check later recovers. The repair cycle's handling of the attempt is unchanged: a crashed inline repair agent still counts as a failed repair attempt and the check is not rerun on that attempt. When the check's terminal outcome is failed and the attempt that ended the cycle failed because of a crash, the check's failure kind SHALL be `infrastructure`. When the cycle ended because the check itself exited non-zero after a repair agent that finished, or because of a blocked declaration, the failure kind SHALL be `step`.

#### Scenario: Repair agent crashes on the last attempt
- **WHEN** a check with `repair: {max: 1}` fails and its inline repair agent's CLI exits non-zero with a provider capacity error
- **THEN** the check's outcome is `failed`, its failure kind is `infrastructure`, and its crash-observed signal is true

#### Scenario: Crash then recovery
- **WHEN** a check with `max: 2` fails, its first repair agent crashes, its second repair agent finishes, and the rerun check passes
- **THEN** the check succeeds and its crash-observed signal is true

#### Scenario: Repair finishes but check still red
- **WHEN** a check with `max: 1` fails, its repair agent finishes, and the rerun check exits non-zero
- **THEN** the check's failure kind is `step`

#### Scenario: Crash in a rerun replay
- **WHEN** a check's `rerun` target agent step crashes during the last allowed replay
- **THEN** the check's outcome is `failed` and its failure kind is `infrastructure`
