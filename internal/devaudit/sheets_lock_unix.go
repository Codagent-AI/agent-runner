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
		if err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil { // #nosec G115 -- a live OS file descriptor is representable as int on supported Unix targets.
			return func() { _ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN); _ = file.Close() }, nil // #nosec G115 -- same live descriptor acquired above.
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
