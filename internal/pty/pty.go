package pty

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	gopty "github.com/creack/pty"
	"golang.org/x/sys/unix"
)

const (
	defaultTerm = "xterm-256color"
	hintDelay   = 800 * time.Millisecond
	killTimeout = 3 * time.Second
	stdinPollMs = 100 // poll timeout in ms for cancellable stdin reads
)

// Result holds the outcome of an interactive PTY session.
type Result struct {
	ExitCode          int
	ContinueTriggered bool
}

// Options configures the interactive PTY session.
type Options struct {
	Env []string // additional environment variables
}

// ptyState holds shared mutable state for a PTY session.
type ptyState struct {
	mu                sync.Mutex
	continueTriggered bool
	done              bool
}

func (s *ptyState) isDone() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.done
}

func (s *ptyState) markDone() {
	s.mu.Lock()
	s.done = true
	s.mu.Unlock()
}

func (s *ptyState) triggerContinue() {
	s.mu.Lock()
	s.continueTriggered = true
	s.done = true
	s.mu.Unlock()
}

// RunInteractive executes a command inside a pseudo-terminal with I/O proxying
// and continue-trigger detection (/next, Ctrl-], enhanced-keyboard Ctrl-]).
func RunInteractive(args []string, opts Options) (Result, error) {
	if len(args) == 0 {
		return Result{}, fmt.Errorf("pty: no command specified")
	}

	if !isTerminal(os.Stdin) {
		return Result{}, fmt.Errorf("pty: stdin is not a terminal")
	}

	// Set terminal to raw mode.
	oldState, err := makeRaw(os.Stdin.Fd())
	if err != nil {
		return Result{}, fmt.Errorf("pty: failed to set raw mode: %w", err)
	}
	defer func() {
		restoreTerminal(os.Stdin.Fd(), oldState)
		restoreTerminalModes()
	}()

	// Build and start the command inside a PTY.
	cmd := exec.Command(args[0], args[1:]...) // #nosec G204 -- interactive agent execution by design
	cmd.Env = buildEnv(opts.Env)

	ptmx, err := startWithTermSize(cmd)
	if err != nil {
		return Result{}, fmt.Errorf("pty: failed to start process: %w", err)
	}
	defer func() { _ = ptmx.Close() }()

	state := &ptyState{}
	hint := newIdleHint(hintDelay)
	exitCh := make(chan struct{})

	// Propagate terminal resize events to the PTY.
	resizeCh := startResizeHandler(ptmx)

	// Forward PTY output to stdout, managing the idle hint.
	outputDone := make(chan struct{})
	go func() {
		defer close(outputDone)
		forwardOutput(ptmx, hint)
	}()

	// Read stdin, process input, forward to PTY, detect continue triggers.
	inputDone := make(chan struct{})
	go func() {
		defer close(inputDone)
		processStdin(ptmx, cmd, hint, state, exitCh)
	}()

	// Wait for the process to exit.
	waitErr := cmd.Wait()
	close(exitCh)
	state.markDone()

	hint.cancel()
	signal.Stop(resizeCh)
	close(resizeCh)
	<-outputDone
	<-inputDone

	exitCode := 0
	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}

	state.mu.Lock()
	triggered := state.continueTriggered
	state.mu.Unlock()

	return Result{
		ExitCode:          exitCode,
		ContinueTriggered: triggered,
	}, nil
}

// startWithTermSize starts the command in a PTY, using the current terminal
// size if available.
func startWithTermSize(cmd *exec.Cmd) (*os.File, error) {
	size, sizeErr := gopty.GetsizeFull(os.Stdin)
	if sizeErr == nil {
		return gopty.StartWithSize(cmd, size)
	}
	return gopty.Start(cmd)
}

// startResizeHandler spawns a goroutine that propagates SIGWINCH events to the
// PTY. The caller must call signal.Stop on the returned channel and then close
// it to allow the goroutine to exit.
func startResizeHandler(ptmx *os.File) chan os.Signal {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	go func() {
		for range ch {
			if s, err := gopty.GetsizeFull(os.Stdin); err == nil {
				_ = gopty.Setsize(ptmx, s)
			}
		}
	}()
	return ch
}

// forwardOutput reads from the PTY master and writes to stdout, managing the
// idle hint timer. Returns when the PTY master is closed or errors.
func forwardOutput(ptmx *os.File, hint *idleHint) {
	buf := make([]byte, 4096)
	for {
		n, err := ptmx.Read(buf)
		if n > 0 {
			hint.clearIfShown()
			_, _ = os.Stdout.Write(buf[:n])
			hint.reset()
		}
		if err != nil {
			hint.cancel()
			return
		}
	}
}

// processStdin reads from stdin using poll-based I/O, processes input through
// the escape-sequence-aware input processor, forwards bytes to the PTY, and
// detects continue triggers. On trigger, it sends SIGTERM (then SIGKILL after
// timeout) to the child process.
func processStdin(ptmx *os.File, cmd *exec.Cmd, hint *idleHint, state *ptyState, exitCh chan struct{}) {
	proc := &inputProcessor{}
	fd := os.Stdin.Fd()
	pollFds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}} // #nosec G115 -- fd fits int32
	buf := make([]byte, 1024)

	for {
		if state.isDone() {
			return
		}

		// Poll stdin with timeout so we can check the done flag periodically.
		ready, pollErr := unix.Poll(pollFds, stdinPollMs)
		if pollErr != nil {
			if pollErr == unix.EINTR {
				continue
			}
			return
		}
		if ready == 0 {
			continue
		}

		n, readErr := unix.Read(int(fd), buf) // #nosec G115 -- fd fits int
		if readErr != nil || n <= 0 {
			return
		}

		result := proc.process(buf[:n])
		if result.triggered {
			state.triggerContinue()
			hint.cancel()
			_ = cmd.Process.Signal(syscall.SIGTERM)

			// SIGKILL after timeout if the process hasn't exited.
			go func() {
				select {
				case <-time.After(killTimeout):
					_ = cmd.Process.Kill()
				case <-exitCh:
				}
			}()
			return
		}
		if len(result.forward) > 0 && !state.isDone() {
			_, _ = ptmx.Write(result.forward)
		}
	}
}

// buildEnv combines os.Environ() with additional variables, ensuring TERM is set.
func buildEnv(extra []string) []string {
	env := os.Environ()
	env = append(env, extra...)

	for _, entry := range env {
		if strings.HasPrefix(entry, "TERM=") {
			return env
		}
	}
	return append(env, "TERM="+defaultTerm)
}
