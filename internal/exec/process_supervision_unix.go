//go:build darwin || linux

package exec

import (
	"errors"
	"os"
	stdexec "os/exec"
	"syscall"
	"time"
)

// ConfigureAgentCommand gives one invocation its own process group and makes
// context cancellation signal that group instead of only the direct child.
func ConfigureAgentCommand(command *stdexec.Cmd, supervision AgentProcessSupervision) {
	if command == nil || !supervision.ProcessGroup {
		return
	}
	if command.SysProcAttr == nil {
		command.SysProcAttr = &syscall.SysProcAttr{}
	}
	command.SysProcAttr.Setpgid = true
	grace := supervision.TerminationGrace
	if grace <= 0 {
		grace = 3 * time.Second
	}
	command.WaitDelay = grace
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		processGroupID := command.Process.Pid
		err := syscall.Kill(-processGroupID, syscall.SIGTERM)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		if err == nil {
			time.AfterFunc(grace, func() {
				_ = syscall.Kill(-processGroupID, syscall.SIGKILL)
			})
		}
		return err
	}
}
