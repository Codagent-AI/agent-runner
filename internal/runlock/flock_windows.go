//go:build windows

package runlock

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// lockRange returns the byte range used for the run lock. Windows byte-range
// locks are mandatory, so the range sits far beyond any file content to keep
// the recorded PID readable by other processes.
func lockRange() *windows.Overlapped {
	return &windows.Overlapped{Offset: 0xFFFFFFFF, OffsetHigh: 0x7FFFFFFF}
}

func tryLock(f *os.File) (bool, error) {
	err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, lockRange())
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return false, nil
	}
	return err == nil, err
}

func unlock(f *os.File) {
	_ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, lockRange())
}
