package liverun

import (
	tea "github.com/charmbracelet/bubbletea"

	iexec "github.com/codagent/agent-runner/internal/exec"
)

// Coordinator holds the shared *tea.Program handle and session directory used
// by the live-run machinery. The runner goroutine calls its methods to send
// progress messages to the TUI. All methods are safe to call from any goroutine.
type Coordinator struct {
	program    *tea.Program
	sessionDir string
}

// NewCoordinator creates a Coordinator for a live workflow run.
func NewCoordinator(program *tea.Program, sessionDir string) *Coordinator {
	return &Coordinator{program: program, sessionDir: sessionDir}
}

// BeforeInteractive releases the terminal for an interactive agent step.
// Call this as SuspendHook just before launching the interactive subprocess.
func (c *Coordinator) BeforeInteractive() {
	c.program.ReleaseTerminal()
	c.send(SuspendedMsg{})
}

// AfterInteractive reclaims the terminal after an interactive agent step returns.
// Call this as ResumeHook immediately after the interactive subprocess exits.
func (c *Coordinator) AfterInteractive() {
	if err := c.program.RestoreTerminal(); err != nil {
		// Best-effort: if restore fails, the user sees a garbled terminal.
		// Nothing we can do here; the TUI will still respond to keystrokes.
		_ = err
	}
	c.send(ResumedMsg{})
}

// NotifyDone signals the TUI that the runner goroutine has finished.
// result is one of "success", "failed", or "stopped"; err is non-nil on panic.
func (c *Coordinator) NotifyDone(result string, err error) {
	c.send(ExecDoneMsg{Result: result, Err: err})
}

// NotifyStepChange signals the TUI that a new step has become active.
func (c *Coordinator) NotifyStepChange(auditPrefix string) {
	c.send(StepStateMsg{ActiveStepPrefix: auditPrefix})
}

// send delivers msg to the TUI program. p.Send is non-blocking (channel with
// a large buffer), so this call is safe from a runner goroutine.
func (c *Coordinator) send(msg tea.Msg) {
	c.program.Send(msg)
}

// TUIProcessRunner wraps a base exec.ProcessRunner and tees all subprocess
// output through the TUI (via OutputChunkMsg) and to per-step output files.
// For non-captured steps the ProcessResult.Stdout/Stderr still populate from
// the in-process buffer so the Go-level contract is unchanged.
func (c *Coordinator) TUIProcessRunner(base iexec.ProcessRunner) iexec.ProcessRunner {
	return &tuiProcessRunner{base: base, coord: c}
}
