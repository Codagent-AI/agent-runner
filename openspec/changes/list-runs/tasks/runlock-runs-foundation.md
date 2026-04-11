# Task: Run lock file, runner integration, and run discovery

## Goal

Create the data foundation for the run list TUI: a PID lock file package that tracks whether a run is actively executing, integrate it into the runner lifecycle, persist the original project CWD for display, and build the run discovery package that assembles `[]RunInfo` from session directories on disk.

## Background

**Why this exists:** The TUI needs to know each run's status (active / inactive / completed) and display fields (workflow name, current step, start time). That requires two new capabilities: (1) a lock file written on run start so active runs can be detected by PID liveness, and (2) a discovery layer that reads session directories and assembles structured run info.

**State storage layout:**
```
~/.agent-runner/projects/
  <encoded-cwd>/                        ← project directory (/ . _ → -)
    meta.json                           ← NEW: {"path": "/original/cwd"}
    runs/
      <workflow-name>-<RFC3339>/        ← session directory
        lock                            ← NEW: plain text file containing PID integer
        state.json                      ← exists while run is resumable; deleted on success
        audit.log                       ← always persists
```

The encoded-cwd is produced by `audit.EncodePath(cwd)` in `internal/audit/logger.go`. This encoding is lossy (replaces `/`, `.`, `_` with `-`), which is why `meta.json` is needed to recover the original path.

**Session ID format:** `<sanitized-workflow-name>-<RFC3339-timestamp>` (e.g., `plan-change-2026-04-11T09-14-00Z`). The timestamp is always the last hyphen-separated segment group and is parseable for `StartTime`.

**Key files to read before starting:**
- `internal/runner/runner.go` — `initRunState` and `finalizeRun` are the two integration points
- `internal/stateio/stateio.go` — pattern for writing/deleting files in the session directory
- `internal/audit/logger.go` — `EncodePath` and `SanitizeWorkflowName`
- `internal/model/state.go` — `RunState` struct (WorkflowName, CurrentStep, etc.)
- `cmd/agent-runner/main.go` — `resolveResumeStatePath` for the resume hint message to update

**`internal/runlock` package — what to build:**

Three functions:
```go
// Write creates a lock file in sessionDir containing the current PID.
// Returns nil on success. Non-fatal: callers MUST proceed even if this fails.
func Write(sessionDir string) error

// Delete removes the lock file from sessionDir. Best-effort: ignores errors.
func Delete(sessionDir string)

// Check returns the lock status for the given session directory.
func Check(sessionDir string) LockStatus

type LockStatus int
const (
    LockNone   LockStatus = iota // no lock file
    LockActive                   // lock file present, PID is alive
    LockStale                    // lock file present, PID is dead
)
```

Lock file name: `lock` (plain text, contains the decimal PID integer, newline-terminated).

PID liveness: use `os.FindProcess(pid)` followed by `process.Signal(syscall.Signal(0))`. A zero signal doesn't kill the process — it just checks if it's alive. An error from `Signal` means the process is dead.

**Runner integration — `internal/runner/runner.go`:**

In `initRunState`, after `os.MkdirAll(sessionDir, ...)` succeeds:
1. Call `runlock.Write(sessionDir)` — ignore the error (run must proceed).
2. Write `meta.json` to the project directory (parent of the `runs/` directory) if it does not already exist:
   ```json
   {"path": "/the/original/cwd"}
   ```
   Use `os.Stat` to check existence before writing. Non-fatal if it fails.

In `finalizeRun`, call `runlock.Delete(rs.sessionDir)` as the first action, before the switch on result.

Also update the resume hint printed on failure (currently in `finalizeRun`'s `ResultFailed` case):
- Old: `agent-runner --resume --session %s`
- New: `agent-runner --resume %s`

**`internal/runs` package — what to build:**

```go
type Status int
const (
    StatusActive    Status = iota
    StatusInactive
    StatusCompleted
)

type RunInfo struct {
    SessionID    string
    SessionDir   string
    WorkflowName string
    CurrentStep  string    // empty string when status is Completed
    Status       Status
    StartTime    time.Time
}

// ListForDir reads all session directories under projectDir/runs/ and returns
// RunInfo for each, sorted most recent first.
func ListForDir(projectDir string) ([]RunInfo, error)
```

Status determination per session directory:
- `runlock.LockActive` → `StatusActive`
- `runlock.LockStale` → `StatusInactive`
- `runlock.LockNone` + `state.json` present → `StatusInactive`
- `runlock.LockNone` + no `state.json` → `StatusCompleted`

`WorkflowName` and `CurrentStep`: read from `state.json` via `stateio.ReadState`. For completed runs (no state.json), `WorkflowName` can be parsed from the session ID (everything before the timestamp suffix) or left as the session ID basename — implementer's call on best effort.

`StartTime`: parse from the session directory name. The session ID format is `<sanitized-name>-<timestamp>` where the timestamp is RFC3339Nano with colons and dots replaced by hyphens (e.g., `plan-change-2026-04-11T09-14-00-000000000Z`). To extract the timestamp: use a regex like `\d{4}-\d{2}-\d{2}T\d{2}-\d{2}-\d{2}` to find the start of the timestamp portion, then restore colons in the time part and parse with `time.Parse`. The workflow name is everything before that match.

`ProjectPath` (needed by the TUI for display): also expose a helper:
```go
// ReadProjectPath returns the stored path from meta.json, or the encoded
// directory name if meta.json does not exist.
func ReadProjectPath(projectDir string) string
```

## Spec

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

### Requirement: Run status determination

The TUI SHALL determine each run's status from the run-lock and state file: active (live lock), inactive (stale lock or state file present without lock), or completed (no lock and no state file).

#### Scenario: Status from live lock
- **WHEN** a session has a lock file with an alive PID
- **THEN** the run is shown as active

#### Scenario: Status from stale lock
- **WHEN** a session has a lock file with a dead PID
- **THEN** the run is shown as inactive

#### Scenario: Status from state file only
- **WHEN** a session has a state file but no lock file
- **THEN** the run is shown as inactive

#### Scenario: Status as completed
- **WHEN** a session has no lock file and no state file
- **THEN** the run is shown as completed

### Requirement: Run fields

Each run entry in the TUI SHALL display: workflow name, current step, status, and start time.

#### Scenario: Active run fields
- **WHEN** a run is active (lock file present with alive PID)
- **THEN** its entry shows workflow name, current step, status=active, and start time

#### Scenario: Inactive run fields
- **WHEN** a run is inactive (interrupted before completion)
- **THEN** its entry shows workflow name, last recorded step, status=inactive, and start time

#### Scenario: Completed run fields
- **WHEN** a run completed successfully (no lock file and no state file)
- **THEN** its entry shows workflow name, status=completed, and start time (current step is omitted)

## Done When

- `internal/runlock` package exists with `Write`, `Delete`, and `Check` functions, covered by unit tests.
- `internal/runs` package exists with `ListForDir` and `ReadProjectPath`, covered by unit tests using a temp directory with fixture session directories.
- Running a workflow creates a `lock` file in the session directory; the file is deleted when the run ends normally.
- Running a workflow writes `meta.json` to the project directory if it does not exist.
- The resume hint in the failure output reads `agent-runner --resume <session-id>` (no `--session` flag).
- All spec scenarios above are addressed by the implementation and tests.
