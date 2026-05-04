package liverun

import "github.com/codagent/agent-runner/internal/model"

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

// ShowTUIMsg asks the TUI to enter alt-screen for the first time. Sent when
// transitioning from suspended (interactive) to active (non-interactive) and
// alt-screen has not yet been requested.
type ShowTUIMsg struct{}

// UIRequestMsg asks the live run view to render a mode: ui step inside the
// existing run-view chrome and reply when the user chooses an outcome.
type UIRequestMsg struct {
	Request model.UIStepRequest
	Reply   chan<- model.UIStepResult
}

// ExecDoneMsg is sent when the runner goroutine finishes (success, failure, or panic).
type ExecDoneMsg struct {
	Result string // "success", "failed", or "stopped"
	Err    error
}
