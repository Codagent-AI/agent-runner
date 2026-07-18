//go:build darwin

package interactive

import "golang.org/x/sys/unix"

func classifyWaitStatus(status unix.WaitStatus) (event waitEvent, signal unix.Signal) {
	raw := uint32(status)
	if raw&0xff == 0x7f {
		signal := unix.Signal((raw >> 8) & 0xff)
		if signal == unix.SIGCONT {
			return waitContinued, 0
		}
		return waitStopped, signal
	}
	if status.Exited() || status.Signaled() {
		return waitExited, 0
	}
	return waitUnknown, 0
}
