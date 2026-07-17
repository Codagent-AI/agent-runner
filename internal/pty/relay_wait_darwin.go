//go:build darwin

package pty

import (
	"errors"
	"math"
	"syscall"
	"unsafe"
)

const waitIDProcess = 1

func waitForProcessExit(pid int) error {
	if pid <= 0 || int64(pid) > math.MaxUint32 {
		return syscall.EINVAL
	}
	// Darwin's Go syscall packages expose SYS_WAITID but not a waitid wrapper.
	// Keep the siginfo storage word-aligned and larger than Darwin's siginfo_t.
	var info [16]uintptr
	for {
		_, _, errno := syscall.Syscall6(
			syscall.SYS_WAITID,
			waitIDProcess,
			uintptr(pid),                      // #nosec G115 -- pid is validated against id_t's uint32 range above.
			uintptr(unsafe.Pointer(&info[0])), // #nosec G103 -- waitid requires a word-aligned siginfo_t pointer.
			uintptr(syscall.WEXITED|syscall.WNOWAIT),
			0,
			0,
		)
		if !errors.Is(errno, syscall.EINTR) {
			if errno != 0 {
				return errno
			}
			return nil
		}
	}
}
