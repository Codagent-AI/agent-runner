//go:build windows

package pty

import "errors"

// errInteractiveUnsupported is returned by RunInteractive and
// RunShellInteractive on Windows builds. The Windows port of agent-runner
// currently supports only autonomous and command steps; interactive PTY
// steps require ConPTY plumbing that is not yet implemented.
//
// Workflow authors targeting Windows should set `mode: autonomous` on
// agent steps and rely on plain shell steps for shell commands. The
// upstream Unix builds retain full interactive support.
var errInteractiveUnsupported = errors.New(
	"pty: interactive mode is not yet supported on Windows; " +
		"use mode: autonomous on agent steps or run a non-interactive shell step",
)

// RunInteractive is a Windows stub that returns errInteractiveUnsupported.
// See pty_unix.go for the Linux/macOS implementation.
func RunInteractive(args []string, opts Options) (Result, error) {
	_ = args
	_ = opts
	return Result{}, errInteractiveUnsupported
}

// RunShellInteractive is a Windows stub that returns errInteractiveUnsupported.
// See pty_unix.go for the Linux/macOS implementation.
func RunShellInteractive(command string, opts Options) (Result, error) {
	_ = command
	_ = opts
	return Result{}, errInteractiveUnsupported
}
