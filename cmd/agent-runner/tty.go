package main

import (
	"errors"
	"os"

	"github.com/mattn/go-isatty"
)

// requireTTY returns an error if stdout is not an interactive terminal.
// Called at the top of any entry point that launches the TUI (handleRun,
// handleResume, handleList, handleInspect). Not called for --validate, --version, or -v.
// AGENT_RUNNER_NO_TUI=1 bypasses the check (used in automated tests).
func requireTTY() error {
	if os.Getenv("AGENT_RUNNER_NO_TUI") == "1" {
		return nil
	}
	if !isatty.IsTerminal(os.Stdout.Fd()) {
		return errors.New("agent-runner: an interactive terminal is required; stdout is not a TTY")
	}
	if !isatty.IsTerminal(os.Stdin.Fd()) {
		return errors.New("agent-runner: an interactive terminal is required; stdin is not a TTY")
	}
	return nil
}
