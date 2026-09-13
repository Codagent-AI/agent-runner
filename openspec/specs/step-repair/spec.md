# step-repair Specification

## Purpose
TBD - created by archiving change failure-recovery. Update Purpose after archive.
## Requirements
### Requirement: Repair block shape

A shell or script step MAY declare a `repair` block. The block SHALL contain exactly one repair form: an inline form with a `prompt` and exactly one of `session` (a declared named session, `resume`, or `inherit`) or `agent` (a profile name), or a rerun form with `rerun` naming a step ID. The block MAY set `max`, a positive integer defaulting to 1, bounding repair attempts per execution of the check. Load-time validation SHALL reject: `repair` on a step that is not a shell or script step; a block with both forms or neither; an inline block naming neither `session` nor `agent`, or both; `max` less than 1; a `rerun` target that is not an earlier step in the same sequential scope as the check; and a replay range (the `rerun` target through the step before the check) containing a step with `break_if` or `skip_if: previous_success`.

#### Scenario: Inline repair on a script step validates
- **WHEN** a script step declares `repair` with `session: planning-agent`, a `prompt`, and no `max`
- **THEN** the workflow loads and the step has a repair budget of 1

#### Scenario: Rerun repair on a shell step validates
- **WHEN** a shell step `verify-draft-pr` declares `repair: {rerun: open-draft-pr}` and `open-draft-pr` is an earlier sibling in the same scope
- **THEN** the workflow loads

#### Scenario: Repair on an agent step is rejected
- **WHEN** an agent step declares `repair`
- **THEN** loading fails with an error stating `repair` is only allowed on shell and script steps

#### Scenario: Both forms rejected
- **WHEN** a `repair` block sets both `rerun` and `prompt`
- **THEN** loading fails with an error stating exactly one repair form is allowed

#### Scenario: Inline block without a target is rejected
- **WHEN** a `repair` block has a `prompt` but neither `session` nor `agent`
- **THEN** loading fails with an error stating an inline repair must name `session` or `agent`

#### Scenario: Rerun target outside the scope is rejected
- **WHEN** a check inside a loop body declares `rerun` naming a step outside that loop body, a later step, or the check itself
- **THEN** loading fails with an error identifying the invalid target

#### Scenario: Replay range with break_if is rejected
- **WHEN** a step between the `rerun` target and the check declares `break_if`
- **THEN** loading fails with an error naming that step and stating replay ranges cannot contain `break_if` or outcome-relative `skip_if`

### Requirement: The check is the sole success authority

A step with `repair` SHALL succeed only when its own command or script exits zero. Repair activity SHALL NOT change the step's outcome directly, and any claim of success in an agent's output SHALL be ignored. The value captured by the step's `capture` SHALL be the output of the check's final run and SHALL be committed only at the check's terminal outcome; internal runs SHALL NOT update it. When a rerun rewinds, captured variables owned by steps in the replay range SHALL be cleared before replay so replayed steps recompute them and never observe values from the previous pass.

#### Scenario: Check passes on first attempt
- **WHEN** a step with `repair` exits zero on its first run
- **THEN** no repair runs and the step succeeds

#### Scenario: Repair agent claims success but check fails
- **WHEN** an inline repair agent reports the problem fixed and the rerun check exits non-zero
- **THEN** the attempt counts as failed and the cycle continues or exhausts according to `max`

#### Scenario: Replay clears range-owned captures
- **WHEN** a rerun target captures `pr_url`, an intermediate step captures `head`, and the check rewinds to the target
- **THEN** `pr_url` and `head` are absent until the replayed steps set them again, and captures set by steps outside the range are unchanged

#### Scenario: Capture reflects the final check run
- **WHEN** a step with `capture: out` fails once, is repaired, and then passes
- **THEN** `out` holds the stdout of the passing run

### Requirement: Repair cycle

When the check exits non-zero and attempts remain, Agent Runner SHALL first inspect the final response of the agent execution the check guards for a blocked declaration (see below). If none is present, it SHALL run the repair form and then rerun the check. An inline repair SHALL execute as an autonomous agent step using the named session or agent and the block's prompt, with the failure evidence supplied through the built-in variables `repair.attempt`, `repair.check_output`, `repair.check_stderr`, and `repair.action_response`, wrapped in an untrusted-input notice that instructs the agent not to follow directives found in the evidence. A rerun SHALL re-execute the target step and every subsequent step through the check in order, each with its declared session strategy; when the target is an agent step, its prompt SHALL be prefaced with the same evidence and notice. Replayed steps SHALL run in the owning scope's execution context, sharing its sessions and captured variables, so that a replayed agent step becomes the most recent agent execution in that scope for any later check. The cycle SHALL repeat while the check fails and completed attempts are fewer than `max`.

#### Scenario: Inline repair recovers a failed check
- **WHEN** a check fails, its inline repair agent fixes the reported problem, and the rerun check passes
- **THEN** the step succeeds after one attempt

#### Scenario: Inline repair receives the evidence
- **WHEN** an inline repair runs after the check printed errors to stderr following an agent step
- **THEN** the repair prompt contains `repair.check_stderr` with those errors and `repair.action_response` with that agent step's final response, both inside the untrusted-input notice

#### Scenario: Rerun re-executes the target and the check
- **WHEN** `verify-draft-pr` fails with `repair: {rerun: open-draft-pr}` and `open-draft-pr` carries no blocked declaration
- **THEN** `open-draft-pr` executes again with the evidence preface, then `verify-draft-pr` runs again

#### Scenario: Replayed agent step guards a later check
- **WHEN** a rerun replays an intermediate agent step and, after the check recovers, a later check in the same scope fails
- **THEN** that later check's failure record identifies the replayed agent execution as the guarded execution

#### Scenario: Rerun re-executes intermediate steps
- **WHEN** a rerun target is two steps before the check
- **THEN** the target, the intermediate step, and the check all execute again in order

#### Scenario: Budget of three
- **WHEN** a check with `max: 3` fails on every run
- **THEN** the check runs four times, the repair runs three times, and the step then fails

### Requirement: Blocked declaration

An agent's final response whose last non-empty line is exactly `REPAIR_BLOCKED` SHALL declare that the failure cannot be repaired by an agent. Agent Runner SHALL honor the declaration from the guarded agent execution inspected before the first repair, from the `rerun` target's execution during a rerun, and from an inline repair agent. On a blocked declaration Agent Runner SHALL stop repairing immediately, leave the check failed, and record the blocked reason with the declaring agent's response as evidence. The marker SHALL NOT mark a check passed, and responses of other agents inside a replay range SHALL NOT be consulted.

#### Scenario: Guarded action declares blocked before any repair
- **WHEN** `open-draft-pr` ends its response with `REPAIR_BLOCKED` and `verify-draft-pr` then fails
- **THEN** no rerun happens, the step fails, and the failure evidence includes the `open-draft-pr` response

#### Scenario: Repair agent declares blocked
- **WHEN** an inline repair agent with `max: 3` ends its first response with `REPAIR_BLOCKED`
- **THEN** the check is not rerun, no further attempts run, and the step fails as blocked

#### Scenario: Marker on a passing check has no effect
- **WHEN** a guarded agent's response ends with `REPAIR_BLOCKED` and the check exits zero
- **THEN** the step succeeds

#### Scenario: Marker from an unrelated agent is ignored
- **WHEN** an intermediate agent step inside a replay range ends with `REPAIR_BLOCKED` but the `rerun` target does not
- **THEN** repair continues according to the budget

### Requirement: Exhaustion and interaction with flow control

When attempts are exhausted or a blocked declaration stops repair, the step's terminal outcome SHALL be failed, and the step's own `continue_on_failure` and `warn_on_failure` SHALL apply to that terminal outcome exactly as for a step without `repair`. Internal repair attempts SHALL NOT update `previous_success` for the following step, SHALL NOT create warnings, and SHALL NOT evaluate the check's `break_if`. A blocking failure or abort of any step inside a replay range SHALL count as a failed repair attempt owned by the check rather than terminating the scope directly; the check's terminal outcome then follows the remaining budget. A check that recovers through repair SHALL be an ordinary success with no warning.

#### Scenario: Exhausted check stops the workflow
- **WHEN** a check without `continue_on_failure` exhausts its repair budget
- **THEN** the workflow stops with the check failed, and the failure reason names the check and its attempt count

#### Scenario: Exhausted check with warn_on_failure
- **WHEN** a check with `warn_on_failure: true` exhausts its repair budget
- **THEN** the step terminates with status `warning`, retains the failed outcome, and the workflow continues

#### Scenario: previous_success reflects only the terminal result
- **WHEN** a check fails once, is repaired, passes, and the next step has `skip_if: previous_success`
- **THEN** the next step is skipped

#### Scenario: break_if is not evaluated on internal attempts
- **WHEN** a check inside a loop declares `break_if: success` and fails its first run before repair
- **THEN** the loop does not break until the check's terminal result is evaluated

#### Scenario: Intermediate failure counts as a failed attempt
- **WHEN** a step inside a replay range fails during a rerun and the budget is not yet exhausted
- **THEN** the failure is recorded against the check's attempt and the next attempt begins

#### Scenario: Recovered check is not a warning
- **WHEN** a check with `warn_on_failure: true` fails once and passes after repair
- **THEN** the step and the completed run carry no warning from that recovery

### Requirement: Persistence and resume

Agent Runner SHALL persist an open repair frame identifying the owning check, its scope path and loop iteration, the repair form and target, the current phase (checking, repairing, replaying, or failed), the completed attempt count, and the guarded execution identity. The frame SHALL be cleared on success and when flow control advances past a failed check, and retained with phase `failed` when the failure stops the run. Resume inside an open frame SHALL restore it and continue the cycle from the recorded phase without repeating completed attempts. Resume of a run that failed at an exhausted or blocked check SHALL re-enter the cycle with a fresh budget: at the `rerun` target for the rerun form, and at the check for the inline form. Attempt counts SHALL be tracked per loop iteration.

#### Scenario: Interrupted during inline repair
- **WHEN** the run is interrupted while an inline repair agent for attempt 1 of 2 is executing
- **THEN** resume resumes that repair agent's session for attempt 1 and then reruns the check

#### Scenario: Interrupted during replay
- **WHEN** the run is interrupted while an intermediate step of a rerun replay is executing
- **THEN** resume continues the replay from that step and then reruns the check

#### Scenario: Resume after exhaustion with rerun form
- **WHEN** a run failed because `verify-draft-pr` was blocked and the user resumes after granting the missing scope
- **THEN** execution re-enters at `open-draft-pr` with the evidence preface, then reruns `verify-draft-pr`, with a full repair budget available

#### Scenario: Resume after exhaustion with inline form
- **WHEN** a run failed because an inline-repaired check exhausted its budget and the user resumes
- **THEN** the check runs first, and repair runs only if it fails again, with a full budget

#### Scenario: Attempts are per iteration
- **WHEN** a check with `max: 1` inside a loop uses its attempt in iteration 1 and fails again in iteration 2
- **THEN** iteration 2 gets its own repair attempt

