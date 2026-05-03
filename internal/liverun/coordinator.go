package liverun

import (
	"fmt"
	"os"
	"sync"

	tea "github.com/charmbracelet/bubbletea"

	iexec "github.com/codagent/agent-runner/internal/exec"
	"github.com/codagent/agent-runner/internal/model"
)

// terminalProgram is the subset of *tea.Program used by Coordinator. Exists
// so tests can supply a mock instead of a real bubbletea program.
type terminalProgram interface {
	ReleaseTerminal() error
	RestoreTerminal() error
	Send(msg tea.Msg)
}

// Coordinator holds the shared terminal-program handle and session directory
// used by the live-run machinery. The runner goroutine calls its methods to
// send progress messages to the TUI. All methods are safe to call from any
// goroutine.
type Coordinator struct {
	program    terminalProgram
	sessionDir string

	mu                 sync.Mutex
	suspended          bool // terminal has been released
	pendingResume      bool // interactive step finished; restore deferred
	altScreenRequested bool // ShowTUIMsg has been sent at least once
}

// NewCoordinator creates a Coordinator for a live workflow run.
func NewCoordinator(program terminalProgram, sessionDir string) *Coordinator {
	return &Coordinator{program: program, sessionDir: sessionDir}
}

// BeforeInteractive releases the terminal for an interactive agent step.
// Idempotent: a second call while already suspended is a no-op, which
// eliminates the restore-then-release flicker between consecutive
// interactive steps.
func (c *Coordinator) BeforeInteractive() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.suspended {
		c.pendingResume = false
		return
	}
	if err := c.program.ReleaseTerminal(); err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: release terminal failed: %v\n", err)
	}
	c.suspended = true
	c.send(SuspendedMsg{})
}

// AfterInteractive marks that an interactive step has finished. The terminal
// is NOT restored here — that decision is deferred to PrepareForStep or
// NotifyDone so we can avoid the flicker when the next step is also
// interactive.
func (c *Coordinator) AfterInteractive() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pendingResume = true
}

// PrepareForStep is called before each leaf step begins. It decides whether
// to restore the terminal based on whether the upcoming step is interactive.
func (c *Coordinator) PrepareForStep(interactive bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if interactive {
		if c.pendingResume {
			c.pendingResume = false
		}
		return
	}
	// Non-interactive step: restore if we were suspended.
	if c.pendingResume {
		if err := c.program.RestoreTerminal(); err != nil {
			fmt.Fprintf(os.Stderr, "agent-runner: restore terminal failed: %v\n", err)
		}
		c.suspended = false
		c.pendingResume = false
		c.send(ResumedMsg{})
	}
	if !c.altScreenRequested {
		c.altScreenRequested = true
		c.send(ShowTUIMsg{})
	}
}

// NotifyDone signals the TUI that the runner goroutine has finished. If the
// terminal is still suspended (e.g. the last step was interactive), it is
// restored so the TUI can show results.
func (c *Coordinator) NotifyDone(result string, err error) {
	c.mu.Lock()
	if c.suspended || c.pendingResume {
		if restoreErr := c.program.RestoreTerminal(); restoreErr != nil {
			fmt.Fprintf(os.Stderr, "agent-runner: restore terminal failed: %v\n", restoreErr)
		}
		c.suspended = false
		c.pendingResume = false
		c.send(ResumedMsg{})
	}
	if !c.altScreenRequested {
		c.altScreenRequested = true
		c.send(ShowTUIMsg{})
	}
	c.mu.Unlock()
	c.send(ExecDoneMsg{Result: result, Err: err})
}

// NotifyStepChange signals the TUI that a new step has become active.
func (c *Coordinator) NotifyStepChange(auditPrefix string) {
	c.send(StepStateMsg{ActiveStepPrefix: auditPrefix})
}

// HandleUIStep renders a UI step through the live-run TUI and waits for the
// selected outcome.
func (c *Coordinator) HandleUIStep(req *model.UIStepRequest) (model.UIStepResult, error) {
	if req == nil {
		return model.UIStepResult{}, fmt.Errorf("ui step request is nil")
	}
	reply := make(chan model.UIStepResult, 1)
	c.send(&UIRequestMsg{Request: *req, Reply: reply})
	return <-reply, nil
}

// send delivers msg to the TUI program. p.Send is non-blocking (channel with
// a large buffer), so this call is safe from a runner goroutine.
// Silently drops the message if program is nil (used in tests).
func (c *Coordinator) send(msg tea.Msg) {
	if c.program != nil {
		c.program.Send(msg)
	}
}

// TUIProcessRunner wraps a base exec.ProcessRunner and tees all subprocess
// output through the TUI (via OutputChunkMsg) and to per-step output files.
// For non-captured steps the ProcessResult.Stdout/Stderr still populate from
// the in-process buffer so the Go-level contract is unchanged.
func (c *Coordinator) TUIProcessRunner(base iexec.ProcessRunner) iexec.ProcessRunner {
	return &tuiProcessRunner{base: base, coord: c}
}
