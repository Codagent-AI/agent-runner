# run-lock Specification

## Purpose
Define run-lock files that let Agent Runner distinguish active runs from inspectable inactive runs.
## Requirements
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

### Requirement: Active lock refuses concurrent run

When a run is started (fresh or resume) and the target session directory already has an active lock, the runner SHALL refuse to start and SHALL exit with an error that names the PID holding the lock. Stale locks SHALL be overwritten and the run SHALL proceed. A lock held by a process that is removing the run under run retention SHALL be treated as an active lock.

#### Scenario: Resume refused while original run is active
- **WHEN** `--resume <id>` is invoked against a session directory whose lock file contains a live PID
- **THEN** the runner exits with an error identifying the PID (e.g., "run already in progress (PID 41247)") without executing any steps, and the existing lock file is preserved unchanged

#### Scenario: Resume proceeds after stale lock
- **WHEN** `--resume <id>` is invoked against a session directory whose lock file contains a dead PID
- **THEN** the runner overwrites the stale lock with its own PID and resumes the workflow

#### Scenario: Resume refused while retention holds the lock
- **WHEN** `--resume <id>` is invoked while a retention sweep in another process holds that run's lock
- **THEN** the runner exits with an error identifying the PID holding the lock and does not execute any steps

### Requirement: Lock acquisition is exclusive

Acquiring a run lock SHALL be mutually exclusive across processes, including when the existing lock is stale. At most one process SHALL hold a run's lock at any time, and a process SHALL NOT remove or overwrite a lock that another live process acquired. A lock SHALL be released automatically when its holder process exits, including by crash or kill, so a crashed run never blocks a later resume. The lock file SHALL continue to record the holder's PID for display and error messages.

#### Scenario: Two processes take over the same stale lock
- **WHEN** two processes simultaneously try to acquire a run lock whose recorded PID is dead
- **THEN** exactly one acquires it and the other is refused with an error naming the winner's PID

#### Scenario: Crashed holder
- **WHEN** the process holding a run lock is killed with SIGKILL and another process then resumes the run
- **THEN** the resume acquires the lock and proceeds

### Requirement: Resume requires the run to still exist under its lock

Resuming a run SHALL NOT create the run's session directory; only a fresh run creates a session directory. After acquiring the run lock, resume SHALL re-read the run's saved state and SHALL proceed only if the session directory and its saved state still exist and still describe an unfinished run. If the session directory no longer exists, or its saved state is missing or no longer unfinished, resume SHALL exit with an error without executing any steps, releasing any lock it acquired, and SHALL leave no newly created session directory behind.

#### Scenario: Run removed between state read and lock
- **WHEN** resume has read a run's saved state and, before it acquires the lock, run retention removes that run
- **THEN** resume exits with a session-not-found error, executes no steps, and no directory exists at the run's original path

#### Scenario: Run finished between state read and lock
- **WHEN** resume has read a run's saved state as unfinished and, before it acquires the lock, the saved state is updated to record completion
- **THEN** resume does not execute any steps and handles the run as completed

#### Scenario: Normal resume unaffected
- **WHEN** `--resume <id>` targets an unfinished run whose directory and state are unchanged
- **THEN** resume proceeds exactly as before

