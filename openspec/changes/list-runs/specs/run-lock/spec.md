## ADDED Requirements

### Requirement: Lock file created on run start

The runner SHALL create a lock file in the session directory at run start, before the first step executes. The lock file SHALL contain the PID of the running agent-runner process.

#### Scenario: Lock file created
- **WHEN** a workflow run begins (after validation succeeds, before the first step executes)
- **THEN** a lock file is created in the session directory containing the current process PID

#### Scenario: Lock file creation fails
- **WHEN** the lock file cannot be written (permissions, disk full, etc.)
- **THEN** the run proceeds without a lock file and is not aborted

### Requirement: Lock file deleted on run end

The runner SHALL delete the lock file when the run exits, regardless of outcome (success, failure, or user-initiated stop). If the process is killed or crashes without executing cleanup, the lock file remains on disk as a stale lock.

#### Scenario: Normal run end
- **WHEN** a run completes with any outcome (success, failure, or stop)
- **THEN** the lock file is deleted from the session directory

#### Scenario: Process crash leaves stale lock
- **WHEN** the runner process is killed or crashes without executing cleanup
- **THEN** the lock file remains on disk containing the now-dead PID

### Requirement: Stale lock detection

A lock file whose recorded PID is no longer alive SHALL be treated as stale. Stale locks SHALL NOT prevent new runs from starting. The run-lock subsystem SHALL expose a check that returns whether a session's lock is active or stale.

#### Scenario: Stale lock detected
- **WHEN** a lock file exists in a session directory and the PID it contains is not a live process
- **THEN** the lock is treated as stale and the session is considered inactive

#### Scenario: Active lock detected
- **WHEN** a lock file exists in a session directory and the PID it contains is a live process
- **THEN** the session is considered active
