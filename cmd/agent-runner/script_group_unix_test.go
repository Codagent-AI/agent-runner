//go:build !windows

package main

import (
	"os"
	"os/exec"
	"syscall"
)

// The worker leads its own process group so signals and the final cleanup reach
// the commands testscript starts beneath it, not just the worker itself.
func runInOwnProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func signalProcessGroup(cmd *exec.Cmd, sig os.Signal) {
	if s, ok := sig.(syscall.Signal); ok && syscall.Kill(-cmd.Process.Pid, s) == nil {
		return
	}
	_ = cmd.Process.Signal(sig)
}

func killProcessGroup(cmd *exec.Cmd) {
	if syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) != nil {
		_ = cmd.Process.Kill()
	}
}
