//go:build linux

package pty

import (
	"errors"
	"math"
	"syscall"

	"golang.org/x/sys/unix"
)

func waitForProcessExit(pid int) error {
	if pid <= 0 || int64(pid) > math.MaxUint32 {
		return syscall.EINVAL
	}
	for {
		var info unix.Siginfo
		err := unix.Waitid(unix.P_PID, pid, &info, unix.WEXITED|unix.WNOWAIT, nil)
		if !errors.Is(err, unix.EINTR) {
			return err
		}
	}
}
