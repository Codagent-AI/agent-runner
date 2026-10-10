## MODIFIED Requirements

### Requirement: Run-view entry points
The CLI SHALL provide three entry points to the run view: a `--inspect <run-id>` flag for direct entry, an Enter action from the list TUI (covered by the `list-runs` delta), and a new `FromDefinition` entry from the list TUI's "new" tab or directly via workflow name. The `FromDefinition` entry SHALL load the workflow definition file and render all steps as `pending` with no run instance attached. Drill-in, step list rendering, detail pane, keyboard focus, scrolling, legend overlay, and exit behavior SHALL apply unchanged. Live refresh, auto-follow, resume-run (`r`), and cross-step auto-scroll SHALL NOT apply in `FromDefinition` mode (there is no active run). The `r` keybinding in `FromDefinition` mode is owned by the `workflow-definition-view` capability (start run) and SHALL NOT conflict with the resume-run `r` defined for inactive runs.

The `--inspect` flag behavior and all its constraints are **unchanged** by this delta — the scenarios below are carried verbatim from the prior spec as required by delta spec convention.

#### Scenario: --inspect launches run view
- **WHEN** `agent-runner --inspect <run-id>` is invoked and the run exists and is not locked by another live process
- **THEN** the run-view TUI launches for that run

#### Scenario: --inspect with unknown run ID
- **WHEN** `agent-runner --inspect <run-id>` is invoked and the run does not exist
- **THEN** agent-runner prints an error message naming the missing run ID and exits with a non-zero status

#### Scenario: --inspect requires full run ID
- **WHEN** `agent-runner --inspect <prefix>` is invoked with a prefix that is not a complete run ID
- **THEN** agent-runner treats it as "not found" and exits non-zero

#### Scenario: --inspect is mutually exclusive with --list and --resume
- **WHEN** `agent-runner --inspect <run-id>` is invoked together with `--list` or `--resume`
- **THEN** agent-runner prints an error indicating the flags are mutually exclusive and exits non-zero

#### Scenario: --inspect rejects a run locked by another process
- **WHEN** `agent-runner --inspect <run-id>` is invoked and the target run's run-lock belongs to another live process
- **THEN** agent-runner prints an error to stderr identifying the run as active in another process and exits non-zero; no TUI is launched

#### Scenario: --inspect proceeds past a stale lock
- **WHEN** `agent-runner --inspect <run-id>` is invoked and the target run's run-lock PID is dead
- **THEN** the lock is treated as stale and the run-view TUI launches normally

#### Scenario: FromDefinition entry renders all steps as pending
- **WHEN** the user opens a workflow via the definition view entry point
- **THEN** the run-view TUI launches with all steps populated from the workflow definition file in `pending` status, with no run ID or audit log

#### Scenario: FromDefinition mode has no live refresh
- **WHEN** the run view is in `FromDefinition` mode
- **THEN** no polling occurs; the view is static

#### Scenario: Escape from FromDefinition returns to list
- **WHEN** the user presses Escape at the top level in `FromDefinition` mode
- **THEN** the view returns to the list TUI

## ADDED Requirements

### Requirement: Post-run Escape navigates to list
When a run has reached a terminal state (completed or failed) and the user presses Escape at the top level, the run view SHALL exit and exec `agent-runner --resume` (no arg) — opening the list TUI on the current-dir tab. This applies to both `FromLiveRun` mode (newly started runs) and runs resumed via `r` from the run list. The just-completed run SHALL be visible in the current-dir run list. This replaces the previous behavior where Escape at top level after `FromLiveRun` completion exited the program.

Runs opened via `--inspect` are unaffected — Escape at top level still exits the program.

#### Scenario: Escape after live run completion opens list
- **WHEN** a run started via `FromLiveRun` has completed and the user presses Escape at the top level
- **THEN** the process execs `agent-runner --resume` (no arg), opening the list TUI on the current-dir tab

#### Scenario: Escape after live run failure opens list
- **WHEN** a run started via `FromLiveRun` has failed and the user presses Escape at the top level
- **THEN** the process execs `agent-runner --resume` (no arg), opening the list TUI on the current-dir tab

#### Scenario: Escape after resumed run completion opens list
- **WHEN** a run resumed via `r` from the run list has completed and the user presses Escape at the top level
- **THEN** the process execs `agent-runner --resume` (no arg), opening the list TUI on the current-dir tab

#### Scenario: Escape after --inspect still exits
- **WHEN** a run opened via `--inspect` is at the top level and the user presses Escape
- **THEN** the program exits (unchanged behavior)
