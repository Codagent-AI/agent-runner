//go:build !darwin && !linux

package interactive

import "golang.org/x/sys/unix"

func classifyWaitStatus(status unix.WaitStatus) (event waitEvent, signal unix.Signal) {
	if status.Stopped() {
		return waitStopped, status.StopSignal()
	}
	if status.Continued() {
		return waitContinued, 0
	}
	if status.Exited() || status.Signaled() {
		return waitExited, 0
	}
	return waitUnknown, 0
}
