package interactive

import (
	"errors"
	"fmt"
	"io"
	"time"

	"golang.org/x/sys/unix"
)

// RunWatchdog waits for EOF from the runner-only pipe and then cleans up a
// still-matching child. A mismatched start identity is treated as PID reuse
// and is never signaled.
func RunWatchdog(parent io.Reader, metadata ProcessMetadata, grace time.Duration) error {
	if metadata.ChildPID <= 0 || metadata.PGID <= 0 || metadata.StartTime == "" {
		return errors.New("watchdog requires pid, pgid, and start time")
	}
	if grace <= 0 {
		grace = DefaultTerminationGrace
	}
	if _, err := io.Copy(io.Discard, parent); err != nil {
		return fmt.Errorf("watchdog parent pipe: %w", err)
	}
	return CleanupProcess(metadata, grace)
}

// CleanupProcess terminates metadata's process group only while pid and start
// identity still match. It is shared by the watchdog and resume cleanup.
func CleanupProcess(metadata ProcessMetadata, grace time.Duration) error {
	if !ProcessIdentityMatches(metadata.ChildPID, metadata.StartTime) {
		return nil
	}
	if grace <= 0 {
		grace = DefaultTerminationGrace
	}
	if err := unix.Kill(-metadata.PGID, unix.SIGTERM); err != nil && !errors.Is(err, unix.ESRCH) {
		return fmt.Errorf("terminate stale child process group: %w", err)
	}
	deadline := time.NewTimer(grace)
	defer deadline.Stop()
	poll := time.NewTicker(25 * time.Millisecond)
	defer poll.Stop()
	for {
		select {
		case <-poll.C:
			if !ProcessIdentityMatches(metadata.ChildPID, metadata.StartTime) {
				return nil
			}
		case <-deadline.C:
			if !ProcessIdentityMatches(metadata.ChildPID, metadata.StartTime) {
				return nil
			}
			if err := unix.Kill(-metadata.PGID, unix.SIGKILL); err != nil && !errors.Is(err, unix.ESRCH) {
				return fmt.Errorf("kill stale child process group: %w", err)
			}
			return nil
		}
	}
}
