package pty

// Result holds the outcome of an interactive PTY session.
//
// Stdout is a transcript of bytes the PTY emitted during the session, with
// ANSI escape sequences stripped. Populated for shell interactive sessions
// so callers can surface what scrolled past after the session ends.
type Result struct {
	ExitCode          int
	ContinueTriggered bool
	Stdout            string
}

// Options configures an interactive PTY session.
type Options struct {
	Env            []string // additional environment variables
	Workdir        string   // working directory for the child process
	DebugLabel     string   // optional human-readable context for PTY debug logs
	ContinueMarker string   // optional plain-text continuation marker accepted from PTY output
}
