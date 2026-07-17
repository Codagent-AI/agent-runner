//go:build linux

package interactive

import "golang.org/x/sys/unix"

func classifyWaitStatus(status unix.WaitStatus) (event waitEvent, signal unix.Signal) {
	switch {
	case status.Stopped():
		return waitStopped, status.StopSignal()
	case status.Continued():
		return waitContinued, 0
	case status.Exited(), status.Signaled():
		return waitExited, 0
	default:
		return waitUnknown, 0
	}
}
