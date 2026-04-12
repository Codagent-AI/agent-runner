// Package runlock manages PID lock files for tracking active workflow runs.
package runlock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

const lockFileName = "lock"

// LockStatus represents the state of a session's lock file.
type LockStatus int

const (
	LockNone   LockStatus = iota // no lock file
	LockActive                   // lock file present, PID is alive
	LockStale                    // lock file present, PID is dead
)

// Write creates a lock file in sessionDir containing the current PID.
// Returns nil on success. Non-fatal: callers MUST proceed even if this fails.
func Write(sessionDir string) error {
	content := fmt.Sprintf("%d\n", os.Getpid())
	return os.WriteFile(filepath.Join(sessionDir, lockFileName), []byte(content), 0o600)
}

// Delete removes the lock file from sessionDir. Best-effort: ignores errors.
func Delete(sessionDir string) {
	_ = os.Remove(filepath.Join(sessionDir, lockFileName))
}

// Check returns the lock status for the given session directory.
func Check(sessionDir string) LockStatus {
	data, err := os.ReadFile(filepath.Join(sessionDir, lockFileName)) // #nosec G304 -- session dir is from internal state tracking
	if err != nil {
		if os.IsNotExist(err) {
			return LockNone
		}
		return LockStale
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return LockStale
	}

	if isProcessAlive(pid) {
		return LockActive
	}
	return LockStale
}

// isProcessAlive checks whether a process with the given PID is alive.
func isProcessAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}
