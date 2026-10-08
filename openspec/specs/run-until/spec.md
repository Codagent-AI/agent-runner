# run-until Specification

## Purpose

Define the per-invocation `--until` cap for command-form workflow runs.
## Requirements
### Requirement: Inclusive top-level step cap

The CLI SHALL accept `agent-runner run <workflow> --until <step-id> [key=value ...]`. The runner SHALL execute the resolved workflow's top-level `steps` entries in order and stop successfully immediately after reaching the top-level step whose `id` equals `<step-id>`. The target is inclusive: a target that executes SHALL finish before the runner stops. No later top-level step SHALL be dispatched.

#### Scenario: Stop after an executed target

- **WHEN** a workflow has top-level steps `A`, `B`, and `C` and is run with `--until B`
- **THEN** steps `A` and `B` execute, step `C` is not dispatched, the process exits 0, and output reports `stopped after step "B" (--until).`

#### Scenario: Target step fails normally

- **WHEN** the `--until` target fails without `continue_on_failure: true`
- **THEN** the workflow retains its normal failed outcome rather than converting the failure into a successful cap

### Requirement: Skipped target counts as reached

If runtime flow control skips the named top-level step, the runner SHALL treat the target's position as reached and stop successfully without dispatching later top-level steps. The saved execution pointer SHALL record the skipped target as reached so a later resume advances beyond it.

#### Scenario: Target skipped by skip_if

- **WHEN** top-level step `B` is the `--until` target and its `skip_if` condition skips it
- **THEN** the runner records `B` as reached, does not dispatch any later top-level step, exits 0, and reports the `--until` stop

### Requirement: Target validation before execution

The runner SHALL validate the `--until` value against IDs in the resolved workflow's top-level `steps` list before preparing the run or dispatching any step. Nested IDs inside loops, groups, or sub-workflows SHALL NOT match. An invalid target SHALL produce a clear error and a non-zero exit without creating run state, an audit log, or a run lock.

#### Scenario: Unknown target fails before execution

- **WHEN** a workflow has top-level steps `A`, `B`, and `C` and is run with `--until missing`
- **THEN** the command fails before step `A` runs and reports that `missing` was not found in the top-level workflow steps

#### Scenario: Nested target is rejected

- **WHEN** `child` is the ID of a step nested inside a top-level loop, group, or sub-workflow step but is not itself a top-level step ID
- **THEN** `--until child` fails validation before any step runs

### Requirement: Invocation-only state and resume

The `--until` value SHALL apply only to the current invocation and SHALL NOT be persisted in `state.json`. A successful capped run SHALL persist the normal state and audit entries for steps that executed or were reached, but SHALL leave the run unfinished when later top-level steps remain. Resuming that run without another cap SHALL continue normally from the next step. Passing `--until` to resume is not required.

`agent-runner --resume <run-id> --until <step-id>` SHALL apply an inclusive cap to the resumed invocation, with the same stop and skip semantics as a capped `run`. The Runner SHALL validate the target against the resumed workflow's top-level step IDs before dispatching any step. An invalid target SHALL fail with a clear error and a non-zero exit, without dispatching a step or changing the run's state. A target that the run has already passed SHALL be rejected the same way. The cap SHALL NOT be persisted.

#### Scenario: Resume after capped run

- **WHEN** a run stops successfully after top-level step `B` while step `C` remains
- **THEN** `state.json` records completed step `B` without storing the `--until` value, and a later uncapped resume continues with step `C`

#### Scenario: Cap reaches the final step

- **WHEN** the `--until` target is the workflow's final top-level step
- **THEN** the runner marks `state.json` completed because no later workflow work remains

#### Scenario: Audit log retained for capped run

- **WHEN** a run stops because it reaches its `--until` target
- **THEN** the run's `audit.log` remains on disk with the usual events for the portion of the workflow that ran

#### Scenario: Capped resume stops at the target

- **WHEN** a run with top-level steps `A`, `B`, and `C` was interrupted during `B` and is resumed with `--until B`
- **THEN** `B` finishes, `C` is not dispatched, the process exits 0, output reports `stopped after step "B" (--until).`, and `state.json` does not store the cap

#### Scenario: Invalid resume target

- **WHEN** a run is resumed with `--until missing` and `missing` is not a top-level step ID
- **THEN** the command fails before dispatching any step and leaves `state.json` unchanged

#### Scenario: Resume target already passed

- **WHEN** a run that already completed top-level step `A` and is positioned at `B` is resumed with `--until A`
- **THEN** the command fails before dispatching any step and reports that `A` is already past the resume point

