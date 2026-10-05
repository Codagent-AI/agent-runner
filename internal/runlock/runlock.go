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
	"sync"
	"syscall"
	"time"
)

const lockFileName = "lock"

var held sync.Map // cleaned session directory -> open, OS-locked descriptor

// LockStatus represents the state of a session's lock file.
type LockStatus int

const (
	LockNone   LockStatus = iota // no lock file
	LockActive                   // lock file present, PID is alive
	LockStale                    // lock file present, PID is dead
)

// HeldProof is an unforgeable-by-construction capability showing that this
// process owns a run directory's lock. Its zero value is invalid.
type HeldProof struct {
	sessionDir string
	pid        int
}

// ProveHeld returns a capability only when sessionDir is currently locked by
// this process.
func ProveHeld(sessionDir string) (HeldProof, error) {
	if _, ok := held.Load(filepath.Clean(sessionDir)); !ok {
		return HeldProof{}, errors.New("prove held run lock: current process does not own the lock")
	}
	status, pid, err := checkPID(sessionDir)
	if err != nil {
		return HeldProof{}, fmt.Errorf("prove held run lock: %w", err)
	}
	if status != LockActive || pid != os.Getpid() {
		return HeldProof{}, errors.New("prove held run lock: current process does not own the lock")
	}
	return HeldProof{sessionDir: filepath.Clean(sessionDir), pid: pid}, nil
}

// Validate confirms that the proof still names a lock currently owned by this
// process and that it belongs to sessionDir.
func (p HeldProof) Validate(sessionDir string) error {
	if p.pid != os.Getpid() || p.sessionDir == "" || p.sessionDir != filepath.Clean(sessionDir) {
		return errors.New("run lock proof does not match the run directory")
	}
	if _, ok := held.Load(p.sessionDir); !ok {
		return errors.New("run lock proof is no longer held by this process")
	}
	status, pid, err := checkPID(sessionDir)
	if err != nil {
		return fmt.Errorf("validate run lock proof: %w", err)
	}
	if status != LockActive || pid != p.pid {
		return errors.New("run lock proof is no longer held by this process")
	}
	return nil
}

// Write creates a lock file in sessionDir containing the current PID.
// Returns nil on success. Non-fatal: callers MUST proceed even if this fails.
//
// Write is non-atomic relative to other writers — use Acquire when mutual
// exclusion against concurrent runners matters.
func Write(sessionDir string) error {
	content := fmt.Sprintf("%d\n", os.Getpid())
	return os.WriteFile(filepath.Join(sessionDir, lockFileName), []byte(content), 0o600)
}

// Acquire locks the persistent lock file with an OS advisory lock. Returns:
//   - activePID == 0 and err == nil: the lock was acquired for this process.
//   - activePID > 0 and err == nil: an existing active lock is held by that
//     PID; the caller must NOT proceed.
//   - activePID == 0 and err != nil: an I/O error prevented a decision; the
//     caller must NOT proceed and must surface the error.
func Acquire(sessionDir string) (activePID int, err error) {
	lockPath := filepath.Join(sessionDir, lockFileName)
	key := filepath.Clean(sessionDir)
	for attempt := 0; attempt < 100; attempt++ {
		f, outcome, err := lockStablePath(lockPath)
		if err != nil {
			return 0, err
		}
		switch outcome {
		case lockReplaced:
			continue
		case lockBusy:
			_, pid, readErr := checkPID(sessionDir)
			if readErr != nil {
				return 0, fmt.Errorf("lock held by another process: %w", readErr)
			}
			if pid == 0 || !isProcessAlive(pid) {
				time.Sleep(2 * time.Millisecond)
				continue
			}
			return pid, nil
		}
		sweepStaleTempFiles(sessionDir)
		if _, loaded := held.LoadOrStore(key, f); loaded {
			release(f)
			return os.Getpid(), nil
		}
		if err := writePID(f); err != nil {
			held.Delete(key)
			release(f)
			return 0, err
		}
		return 0, nil
	}
	return 0, fmt.Errorf("lock changed during acquisition: %w", fs.ErrNotExist)
}

type lockOutcome int

const (
	lockAcquired lockOutcome = iota
	lockBusy
	lockReplaced
)

// lockStablePath opens path, takes a non-blocking exclusive OS lock, and
// confirms the path still names the locked file. On lockAcquired the caller
// owns the returned file and must release it.
func lockStablePath(path string) (*os.File, lockOutcome, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600) // #nosec G304 -- lock path is selected by the caller.
	if err != nil {
		return nil, 0, fmt.Errorf("open lock: %w", err)
	}
	locked, err := tryLock(f)
	if err != nil {
		_ = f.Close()
		return nil, 0, fmt.Errorf("lock: %w", err)
	}
	if !locked {
		_ = f.Close()
		return nil, lockBusy, nil
	}
	opened, statErr := f.Stat()
	current, pathErr := os.Lstat(path)
	if statErr != nil || pathErr != nil || !os.SameFile(opened, current) {
		release(f)
		if pathErr != nil && !errors.Is(pathErr, fs.ErrNotExist) {
			return nil, 0, pathErr
		}
		return nil, lockReplaced, nil
	}
	return f, lockAcquired, nil
}

func release(f *os.File) {
	unlock(f)
	_ = f.Close()
}

func writePID(f *os.File) error {
	if err := f.Truncate(0); err != nil {
		return err
	}
	if _, err := f.Seek(0, 0); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(f, "%d\n", os.Getpid()); err != nil {
		return err
	}
	return f.Sync()
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
	key := filepath.Clean(sessionDir)
	if value, ok := held.LoadAndDelete(key); ok {
		f := value.(*os.File)
		if opened, err := f.Stat(); err == nil {
			if current, err := os.Lstat(filepath.Join(sessionDir, lockFileName)); err == nil && os.SameFile(opened, current) {
				_ = os.Remove(filepath.Join(sessionDir, lockFileName))
			}
		}
		release(f)
		return
	}
	// Without a held descriptor, take the OS lock first so a lock another
	// process has just acquired (but not yet stamped) is never unlinked.
	lockPath := filepath.Join(sessionDir, lockFileName)
	if _, err := os.Lstat(lockPath); err != nil {
		return
	}
	f, outcome, err := lockStablePath(lockPath)
	if err != nil || outcome != lockAcquired {
		return
	}
	defer release(f)
	status, pid, err := checkPID(sessionDir)
	if err == nil && (status != LockActive || pid == os.Getpid()) {
		_ = os.Remove(lockPath)
	}
}

// Inspect preserves read errors for callers making destructive decisions.
func Inspect(sessionDir string) (LockStatus, int, error) { return checkPID(sessionDir) }

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

// CheckOwnedByOther returns true iff the lock file exists, contains a live
// PID, and that PID differs from selfPID. Absent, stale, unreadable, or
// same-process locks all return false.
func CheckOwnedByOther(sessionDir string, selfPID int) bool {
	data, err := os.ReadFile(filepath.Join(sessionDir, lockFileName)) // #nosec G304
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return false
	}
	if pid == selfPID {
		return false
	}
	return isProcessAlive(pid)
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
