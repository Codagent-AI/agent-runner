//go:build windows

// Package shellcmd provides a cross-platform constructor for "run this string
// through the system shell" commands. See shellcmd_unix.go for the Unix
// implementation and the portability caveat.
package shellcmd

import "os/exec"

// New builds an *exec.Cmd that executes the given command string through
// cmd.exe. The /C flag tells cmd.exe to terminate after the command runs.
func New(command string) *exec.Cmd {
	return exec.Command("cmd.exe", "/C", command) // #nosec G204 -- caller-controlled workflow command
}
