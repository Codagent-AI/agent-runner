## ADDED Requirements

### Requirement: Live agent-call visibility

When an autonomous-headless parent invokes `call_agent`, the live run view SHALL insert the accepted call beneath its parent, update the call's status independently, and stream its stdout and stderr through the call's detail pane using ordinary headless-agent behavior. Parent and called-child output MUST remain separate.

When auto-follow is engaged, the cursor SHALL move to an active call and SHALL return to the next active execution point after the call finishes. Existing manual navigation SHALL pause auto-follow. When an expanded parent has an active call child, the child SHALL carry the sole running indicator using the existing active-child status-suppression behavior.

When an interactive parent owns the terminal, Agent Runner MUST NOT interrupt it to display the run view. Calls completed during that interval SHALL appear when the parent returns terminal ownership and the TUI resumes.

#### Scenario: Autonomous call appears live
- **WHEN** an autonomous-headless parent starts an accepted agent call
- **THEN** the live run view inserts an in-progress call row beneath that parent

#### Scenario: Called-child output streams separately
- **WHEN** an active called child produces stdout or stderr
- **THEN** the bytes stream into the call's detail pane without being attributed to the parent row

#### Scenario: Auto-follow enters and leaves call
- **WHEN** auto-follow is engaged as an agent call starts and later finishes
- **THEN** the cursor moves to the active call and then returns to the next active execution point

#### Scenario: Manual navigation pauses call auto-follow
- **WHEN** the user navigates manually before or during an active call
- **THEN** the cursor remains where the user placed it instead of moving automatically to the call

#### Scenario: Active call carries running indicator
- **WHEN** an expanded parent has an in-progress call child
- **THEN** the child displays the running indicator and the parent suppresses its duplicate running indicator

#### Scenario: Call completion is independent of parent
- **WHEN** a called child succeeds or fails while its parent remains active
- **THEN** the call row displays its terminal status without assigning that status to the parent row

#### Scenario: Interactive parent retains terminal ownership
- **WHEN** an interactive parent invokes `call_agent` while its CLI owns the terminal
- **THEN** Agent Runner does not display or resume the run-view TUI during the call

#### Scenario: Interactive-parent calls appear after return
- **WHEN** an interactive parent returns terminal ownership after completing one or more calls
- **THEN** the resumed run view displays those calls beneath the parent from persisted run evidence
