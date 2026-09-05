//go:build windows && dev_audit

package devaudit

import (
	"context"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

func acquireDeliveryLock(ctx context.Context, path string) (func(), error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0o600) // #nosec G304 -- hashed private destination lock path.
	if err != nil {
		return nil, err
	}
	overlapped := &windows.Overlapped{}
	for {
		err = windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, overlapped)
		if err == nil {
			return func() { _ = windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, overlapped); _ = file.Close() }, nil
		}
		if err != windows.ERROR_LOCK_VIOLATION {
			_ = file.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			_ = file.Close()
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}
