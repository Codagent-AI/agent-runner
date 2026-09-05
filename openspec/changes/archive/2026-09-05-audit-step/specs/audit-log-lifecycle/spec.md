## ADDED Requirements

### Requirement: Execution sessions have durable identities

Every run invocation that starts or resumes workflow execution SHALL have a durable execution-session identity. Run-level and step-level lifecycle events SHALL identify the execution session that emitted them so evidence from separate resume sessions can be distinguished without changing the stable source-run identity.

#### Scenario: New run starts
- **WHEN** Agent Runner starts a new source run
- **THEN** its lifecycle events identify both the run and its first execution session

#### Scenario: Run resumes
- **WHEN** Agent Runner resumes an existing source run
- **THEN** newly emitted lifecycle events retain the run identity and use a new execution-session identity

### Requirement: Step Git checkpoints are recorded durably

For each executable source step attempt, Agent Runner SHALL record a local repository checkpoint at step start and step end. When Git evidence is available, the checkpoint SHALL include the current revision plus index, working-tree, and untracked-file state sufficient to derive aggregate file/addition/deletion deltas and identify commits appearing across the boundary. When unavailable, unsupported, or interrupted before the closing checkpoint, the event SHALL state that limitation rather than inventing a revision or zero change.

#### Scenario: Step starts and completes in Git repository
- **WHEN** a step executes normally in a supported Git worktree
- **THEN** its lifecycle evidence contains starting and ending Git revisions and the local checkpoint data needed for conservative change attribution

#### Scenario: Step does not change HEAD
- **WHEN** a step completes without creating a commit
- **THEN** both checkpoints retain enough repository state to distinguish no change from uncommitted work

#### Scenario: Step is interrupted before end checkpoint
- **WHEN** the process is terminated before Agent Runner can capture the step's ending repository checkpoint
- **THEN** the lifecycle evidence marks the ending checkpoint unavailable

#### Scenario: Run is outside a Git repository
- **WHEN** a step executes where supported Git evidence is unavailable
- **THEN** the checkpoint records Git evidence as unavailable and the step continues normally

### Requirement: Source and audit lifecycle events are linked

Agent Runner SHALL durably record audit launch requested, audit launched, audit launch failed, audit completed, and reporting warning events as applicable. A successful launch SHALL link the source run and execution session to the audit run, and the audit run SHALL contain the reciprocal source identifiers.

Audit launch SHALL occur only after the source terminal outcome, metrics, step checkpoints, and other required durable evidence have been flushed.

#### Scenario: Audit launch succeeds
- **WHEN** an eligible execution session is finalized and its linked audit starts
- **THEN** durable lifecycle evidence links the source run and execution session to the audit run in both directions

#### Scenario: Audit launch fails
- **WHEN** Agent Runner cannot start the audit
- **THEN** the source audit log records the failed launch and reason without changing the source outcome

#### Scenario: Audit completes with reporting warning
- **WHEN** model auditing completes but Google Sheets reporting fails
- **THEN** the audit lifecycle records its completion and reporting warning separately

#### Scenario: Source evidence is not yet durable
- **WHEN** the source reaches a terminal outcome but required evidence has not finished flushing
- **THEN** Agent Runner does not launch the audit until finalization succeeds or records a non-blocking launch warning if it cannot be finalized
