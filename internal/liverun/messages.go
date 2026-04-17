package liverun

// OutputChunkMsg delivers a batch of subprocess output bytes to the TUI.
// Bytes have had ANSI escape sequences stripped for clean rendering.
// StepPrefix matches the audit-log event prefix for the step.
// Stream is "stdout" or "stderr".
type OutputChunkMsg struct {
	StepPrefix string
	Stream     string
	Bytes      []byte
}

// StepStateMsg notifies the TUI that a new step has become active.
type StepStateMsg struct {
	ActiveStepPrefix string
}

// SuspendedMsg is sent when the TUI releases the terminal for an interactive step.
type SuspendedMsg struct{}

// ResumedMsg is sent when the TUI reclaims the terminal after an interactive step.
type ResumedMsg struct{}

// ExecDoneMsg is sent when the runner goroutine finishes (success, failure, or panic).
type ExecDoneMsg struct {
	Result string // "success", "failed", or "stopped"
	Err    error
}
