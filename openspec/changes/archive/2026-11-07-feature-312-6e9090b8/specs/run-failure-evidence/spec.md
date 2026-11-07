## MODIFIED Requirements

### Requirement: Classified failure reason

The run's root failure reason SHALL be derived from the evidence of the failure that ended the run. When the run ended with failure kind `infrastructure`, the reason SHALL be derived from the agent crash failure origin: the step ID, then ` failed (infrastructure): `, then the first non-empty line of its error or stderr, cut to 300 characters ending in `…` when longer. When the error and stderr are both empty, it SHALL use the exit code, or `agent session did not finish` when no exit code exists. Otherwise the reason SHALL be derived from the failure record of the failing check: the step ID, then the first non-empty line of stderr (or the exit code when stderr is empty), then `blocked: <first line of the declaring response's explanation>` when a blocked declaration was recorded, then `after N repair attempts` when at least one repair attempt ran. When the record's stdout and stderr come from a check run that preceded the repair attempt that ended the cycle, the reason SHALL mark that output as `before repair` and SHALL NOT present it as the check's current state. This happens when a repair agent or a `rerun` target declared `REPAIR_BLOCKED`, or when the last repair agent failed without an infrastructure failure (failure kind `step`) and the check was not run again. The label applies only to a reason derived from the failing check's failure record. When an infrastructure failure ends the cycle, the infrastructure reason above SHALL take precedence and SHALL carry no `before repair` label. The stderr line SHALL then be preceded by `before repair: `, or, when stderr is empty, the exit code SHALL be followed by ` before repair`. The failure record SHALL persist whether its output predates the last repair attempt, so every surface below renders the same reason for a historical run. A record persisted before this distinction existed SHALL render without the label. A blocked declaration from the guarded action before any repair, and an exhausted cycle whose last check run followed its final repair, SHALL keep the unlabeled wording. When the crashed agent is an inline repair agent, the step ID in the reason SHALL be the owning check's ID followed by ` repair`. When an infrastructure failure ends a check's repair cycle, the reason SHALL also end with `after N repair attempts`. The same reason SHALL appear in the failure surface of the run view, the run list row, the debug workflow's failure reason, and the resume hint printed when a run fails.

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

#### Scenario: Repair agent blocks after a failed check
- **WHEN** `check-evidence` fails with stderr `row I-21 has no status`, its inline repair agent declares `REPAIR_BLOCKED` with a first line of `needs tier 2 tests` on attempt 1, and the check is not run again
- **THEN** the failure reason is `check-evidence failed: before repair: row I-21 has no status; blocked: needs tier 2 tests after 1 repair attempts`

#### Scenario: Rerun target blocks
- **WHEN** a check's `rerun` target declares `REPAIR_BLOCKED` during its second attempt, after the check's first rerun had failed with stderr `3 rows unverified`
- **THEN** the failure reason contains `failed: before repair: 3 rows unverified; blocked: ` followed by the target's explanation

#### Scenario: Last repair agent failed without crashing
- **WHEN** a check's final inline repair agent finishes but is judged failed with failure kind `step` (for example, it attempted a disallowed `AskUserQuestion`), so the budget ends without the check running again
- **THEN** the failure reason labels the check's last stderr line `before repair: `

#### Scenario: Last repair agent crash keeps the infrastructure reason
- **WHEN** the final inline repair agent of check `check-plan` exits non-zero with stderr `Selected model is at capacity`, ending its only attempt
- **THEN** the failure reason is `check-plan repair failed (infrastructure): Selected model is at capacity after 1 repair attempts`, with no `before repair` label

#### Scenario: Pre-repair exit code without stderr
- **WHEN** a check with empty stderr exits 2, and its repair agent then declares `REPAIR_BLOCKED`
- **THEN** the failure reason begins `<step> failed with exit code 2 before repair`

#### Scenario: Exhausted after a final check run is unlabeled
- **WHEN** a check fails again on the run that follows its last repair attempt, ending its budget
- **THEN** the failure reason quotes that run's stderr without a `before repair` label

#### Scenario: Labeled reason is the same on every surface
- **WHEN** a run ends with a `before repair` failure reason and is later opened in the run view, listed in the run list, and inspected by the debug workflow
- **THEN** each surface shows the same labeled reason that the console printed above the `to resume:` hint
