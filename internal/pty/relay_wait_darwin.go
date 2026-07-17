//go:build darwin

package pty

import (
	"errors"
	"syscall"
	"unsafe"
)

const waitIDProcess = 1

func waitForProcessExit(pid int) error {
	// Darwin's Go syscall packages expose SYS_WAITID but not a waitid wrapper.
	// Keep the siginfo storage word-aligned and larger than Darwin's siginfo_t.
	var info [16]uintptr
	for {
		_, _, errno := syscall.Syscall6( // #nosec G103 -- waitid requires a siginfo_t pointer.
			syscall.SYS_WAITID,
			waitIDProcess,
			uintptr(pid),
			uintptr(unsafe.Pointer(&info[0])),
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
