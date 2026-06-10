//go:build windows

package main

import (
	"errors"
	"os"
	"os/exec"
)

// execProcessImpl on Windows emulates the Unix exec(2) semantics by spawning
// the named program as a child process, waiting for it to exit, and calling
// os.Exit with its exit code. Windows has no in-place process replacement
// equivalent to syscall.Exec.
//
// argv0 is the absolute path to the executable. argv[0] is the program name
// the caller wants the child to see (typically filepath.Base(argv0)) and
// argv[1:] are the arguments. envv is the full environment to apply.
//
// Note: this never returns under normal conditions — it terminates the
// current process via os.Exit. It only returns an error when the child
// could not be started, mirroring the Unix syscall.Exec contract where a
// non-nil return means the exec syscall itself failed.
func execProcessImpl(argv0 string, argv []string, envv []string) error {
	if len(argv) == 0 {
		return errors.New("execProcess: argv must contain at least the program name")
	}
	cmd := exec.Command(argv0, argv[1:]...) // #nosec G204 -- argv0 is our own executable
	cmd.Env = envv
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	waitErr := cmd.Wait()
	exitCode := 0
	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}
	os.Exit(exitCode)
	return nil // unreachable
}
