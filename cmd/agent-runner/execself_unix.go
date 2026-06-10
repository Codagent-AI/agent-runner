//go:build linux || darwin

package main

import "syscall"

// execProcessImpl on Unix is syscall.Exec — the current process is replaced
// in place with the named program. Used by execSelf to re-launch agent-runner
// (e.g. after onboarding or theme changes) without leaving a stranded parent.
func execProcessImpl(argv0 string, argv []string, envv []string) error {
	return syscall.Exec(argv0, argv, envv)
}
