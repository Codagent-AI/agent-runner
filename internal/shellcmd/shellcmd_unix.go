//go:build linux || darwin

// Package shellcmd provides a cross-platform constructor for "run this string
// through the system shell" commands. On Linux and macOS the command is run
// via `sh -c <string>`. On Windows it is run via `cmd.exe /C <string>`.
//
// Workflow YAMLs are expected to use POSIX shell syntax. Workflows that rely
// on bash builtins or shell features that are not present in cmd.exe (here
// strings, process substitution, exported function names, etc.) will fail on
// Windows; that is a workflow portability issue, not a shellcmd issue.
package shellcmd

import "os/exec"

// New builds an *exec.Cmd that executes the given command string through the
// platform shell.
func New(command string) *exec.Cmd {
	return exec.Command("sh", "-c", command) // #nosec G204 -- caller-controlled workflow command
}
