//go:build windows

package main

import (
	"os"
	"os/exec"
)

func runInOwnProcessGroup(*exec.Cmd) {}

func signalProcessGroup(cmd *exec.Cmd, sig os.Signal) { _ = cmd.Process.Signal(sig) }

func killProcessGroup(cmd *exec.Cmd) { _ = cmd.Process.Kill() }
