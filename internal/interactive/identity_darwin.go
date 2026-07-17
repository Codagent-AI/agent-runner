//go:build darwin

package interactive

import (
	"fmt"

	"golang.org/x/sys/unix"
)

func ReadProcessIdentity(pid int) (string, error) {
	proc, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return "", err
	}
	if proc == nil || int(proc.Proc.P_pid) != pid {
		return "", fmt.Errorf("process %d not found", pid)
	}
	started := proc.Proc.P_starttime
	return fmt.Sprintf("%d.%06d", started.Sec, started.Usec), nil
}
