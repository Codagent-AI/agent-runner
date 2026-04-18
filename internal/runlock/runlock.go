// Package runlock manages PID lock files for tracking active workflow runs.
package runlock

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
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
//
// Write is non-atomic relative to other writers — use Acquire when mutual
// exclusion against concurrent runners matters.
func Write(sessionDir string) error {
	content := fmt.Sprintf("%d\n", os.Getpid())
	return os.WriteFile(filepath.Join(sessionDir, lockFileName), []byte(content), 0o600)
}

// Acquire atomically creates the lock file with the current PID. Returns:
//   - activePID == 0 and err == nil: the lock was acquired for this process.
//   - activePID > 0 and err == nil: an existing active lock is held by that
//     PID; the caller must NOT proceed.
//   - activePID == 0 and err != nil: an I/O error prevented a decision; the
//     caller must NOT proceed and must surface the error.
//
// Atomicity is provided by O_CREATE|O_EXCL — two concurrent callers cannot
// both succeed. If the existing lock is stale (dead PID or unparseable), it
// is removed and acquisition is retried exactly once.
func Acquire(sessionDir string) (activePID int, err error) {
	lockPath := filepath.Join(sessionDir, lockFileName)

	if err := tryCreateLock(lockPath); err == nil {
		return 0, nil
	} else if !errors.Is(err, fs.ErrExist) {
		return 0, fmt.Errorf("create lock: %w", err)
	}

	status, pid, checkErr := checkPID(sessionDir)
	if checkErr != nil {
		return 0, fmt.Errorf("inspect existing lock: %w", checkErr)
	}
	if status == LockActive {
		return pid, nil
	}

	// Stale: remove and retry once. A concurrent acquirer may win the retry;
	// in that case the second create returns ErrExist and we re-check.
	if rmErr := os.Remove(lockPath); rmErr != nil && !errors.Is(rmErr, fs.ErrNotExist) {
		return 0, fmt.Errorf("remove stale lock: %w", rmErr)
	}
	if err := tryCreateLock(lockPath); err == nil {
		return 0, nil
	} else if !errors.Is(err, fs.ErrExist) {
		return 0, fmt.Errorf("create lock after stale: %w", err)
	}
	status, pid, checkErr = checkPID(sessionDir)
	if checkErr != nil {
		return 0, fmt.Errorf("inspect existing lock after race: %w", checkErr)
	}
	if status == LockActive {
		return pid, nil
	}
	return 0, errors.New("stale lock reappeared after retry; giving up to avoid a loop")
}

// tryCreateLock atomically creates lockPath with the current PID as contents.
// Uses write-temp-then-hardlink so concurrent readers never observe a
// partially-written file: the target path either exists with full content or
// does not exist at all. Returns an ErrExist-wrapping error when another
// process has already created the target.
//
// On filesystems that do not support hard links (FAT/exFAT, some SMB/NFS
// mounts), os.Link returns EPERM/ENOTSUP; in that case we fall back to
// O_CREATE|O_EXCL. The fallback is weaker — a concurrent reader could see an
// empty file between create and write — but it matches the behavior of the
// pre-atomic path and is only used on filesystems that never supported the
// stronger guarantee anyway.
func tryCreateLock(lockPath string) error {
	dir := filepath.Dir(lockPath)

	// Best-effort sweep of stale temp files left by prior SIGKILL'd runners.
	// Uses a conservative age threshold so we never race a live sibling
	// invocation that has just created its own temp file.
	sweepStaleTempFiles(dir)

	tmp, err := os.CreateTemp(dir, "lock-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := fmt.Fprintf(tmp, "%d\n", os.Getpid()); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	linkErr := os.Link(tmpPath, lockPath)
	if linkErr == nil {
		return nil
	}
	if errors.Is(linkErr, fs.ErrExist) {
		return linkErr
	}

	// Link may be unsupported on this filesystem. Fall back to O_EXCL so the
	// lock still works (with a weaker atomicity guarantee).
	var lerr *os.LinkError
	if errors.As(linkErr, &lerr) &&
		(errors.Is(lerr.Err, syscall.EPERM) || errors.Is(lerr.Err, syscall.ENOTSUP)) {
		return createLockExcl(lockPath)
	}
	return linkErr
}

// createLockExcl is the no-link fallback: O_CREATE|O_EXCL followed by write.
// A concurrent checkPID can observe the file between create and write, which
// is why this is only used when os.Link is unavailable.
func createLockExcl(lockPath string) error {
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) // #nosec G304 -- session dir from internal state tracking
	if err != nil {
		return err
	}
	_, writeErr := fmt.Fprintf(f, "%d\n", os.Getpid())
	closeErr := f.Close()
	if writeErr != nil {
		_ = os.Remove(lockPath)
		return writeErr
	}
	if closeErr != nil {
		_ = os.Remove(lockPath)
		return closeErr
	}
	return nil
}

const tempLockMaxAge = 5 * time.Minute

func sweepStaleTempFiles(dir string) {
	entries, err := filepath.Glob(filepath.Join(dir, "lock-*.tmp"))
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-tempLockMaxAge)
	for _, e := range entries {
		info, statErr := os.Stat(e)
		if statErr != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(e)
		}
	}
}

// Delete removes the lock file from sessionDir. Best-effort: ignores errors.
func Delete(sessionDir string) {
	_ = os.Remove(filepath.Join(sessionDir, lockFileName))
}

// Check returns the lock status for the given session directory.
// Read errors are collapsed to LockStale for backward compatibility with
// status-display callers that cannot act on an error. Callers that need to
// distinguish a corrupt / unreadable lock from a stale one SHOULD use the
// internal checkPID helper (exposed via Acquire) which returns the error.
func Check(sessionDir string) LockStatus {
	status, _, err := checkPID(sessionDir)
	if err != nil {
		return LockStale
	}
	return status
}

// CheckPID returns the lock status and the PID recorded in the lock file.
// Errors reading the lock file are surfaced as LockStale for backward
// compatibility; see Check for details.
func CheckPID(sessionDir string) (status LockStatus, pid int) {
	status, pid, err := checkPID(sessionDir)
	if err != nil {
		return LockStale, 0
	}
	return status, pid
}

// checkPID is the error-returning variant used by Acquire to distinguish
// genuine I/O errors (permission, transient I/O, corrupt mount) from a
// legitimately stale lock.
func checkPID(sessionDir string) (LockStatus, int, error) {
	data, err := os.ReadFile(filepath.Join(sessionDir, lockFileName)) // #nosec G304 -- session dir is from internal state tracking
	if err != nil {
		if os.IsNotExist(err) {
			return LockNone, 0, nil
		}
		return LockStale, 0, err
	}

	parsed, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || parsed <= 0 {
		return LockStale, 0, nil
	}

	if isProcessAlive(parsed) {
		return LockActive, parsed, nil
	}
	return LockStale, parsed, nil
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
