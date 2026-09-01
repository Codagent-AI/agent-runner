//go:build !windows && dev_audit

package devaudit

import (
	"context"
	"os"
	"syscall"
	"time"
)

func acquireDeliveryLock(ctx context.Context, path string) (func(), error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0o600) // #nosec G304 -- hashed private destination lock path.
	if err != nil {
		return nil, err
	}
	for {
		if err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
			return func() { _ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN); _ = file.Close() }, nil
		}
		if err != syscall.EWOULDBLOCK && err != syscall.EAGAIN {
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
