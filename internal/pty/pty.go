package pty

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/x/ansi"
	gopty "github.com/creack/pty"
	"golang.org/x/sys/unix"
)

const (
	defaultTerm = "xterm-256color"
	hintDelay   = 3 * time.Second
	killTimeout = 3 * time.Second
	stdinPollMs = 100 // poll timeout in ms for cancellable stdin reads
)

// Result holds the outcome of an interactive PTY session.
type Result struct {
	ExitCode          int
	ContinueTriggered bool
	// Stdout is a transcript of bytes the PTY emitted during the session, with
	// ANSI escape sequences stripped. Populated for shell interactive sessions
	// so callers can surface what scrolled past after the session ends.
	Stdout string
}

// Options configures an interactive PTY session.
type Options struct {
	Env     []string // additional environment variables
	Workdir string   // working directory for the child process
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

// tryTriggerContinue atomically transitions to the continue-triggered state
// only if the session is not already done. Returns false if the process has
// already exited, preventing a late trigger from flipping the outcome.
func (s *ptyState) tryTriggerContinue() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return false
	}
	s.continueTriggered = true
	s.done = true
	return true
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
	ptmx, err := startInPTY(cmd, opts)
	if err != nil {
		return Result{}, err
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
		forwardOutput(ptmx, hint, cmd, state, exitCh)
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

// RunShellInteractive executes a shell command in a pseudo-terminal with the
// user's terminal attached. Unlike RunInteractive, it does not install
// continue-trigger detection, sentinel scanning, or idle hints.
//
// The shell runs inside its own alt-screen buffer so the prompt renders on a
// clean canvas, independent of whatever the suspended TUI (or any enclosing
// parent process like a host CLI) last drew on the normal screen. Without
// this, stale content from the parent terminal can interleave with the shell
// prompt, making it appear as though input is broken.
func RunShellInteractive(command string, opts Options) (Result, error) {
	if command == "" {
		return Result{}, fmt.Errorf("pty: no command specified")
	}

	if !isTerminal(os.Stdin) {
		return Result{}, fmt.Errorf("pty: stdin is not a terminal")
	}

	oldState, err := makeRaw(os.Stdin.Fd())
	if err != nil {
		return Result{}, fmt.Errorf("pty: failed to set raw mode: %w", err)
	}
	enterShellAltScreen()
	defer func() {
		exitShellAltScreen()
		restoreTerminal(os.Stdin.Fd(), oldState)
		restoreTerminalModes()
	}()

	cmd := exec.Command("sh", "-c", command) // #nosec G204 -- workflow shell command by design
	ptmx, err := startInPTY(cmd, opts)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = ptmx.Close() }()

	state := &ptyState{}
	transcript := newTailBuffer(transcriptMaxBytes)

	resizeCh := startResizeHandler(ptmx)
	outputDone := make(chan struct{})
	go func() {
		defer close(outputDone)
		forwardOutputRaw(ptmx, os.Stdout, transcript)
	}()

	inputDone := make(chan struct{})
	go func() {
		defer close(inputDone)
		processStdinRaw(ptmx, state)
	}()

	waitErr := cmd.Wait()
	state.markDone()

	signal.Stop(resizeCh)
	close(resizeCh)
	<-outputDone
	<-inputDone

	result, err := shellResultFromWait(waitErr)
	result.Stdout = stripTranscript(transcript.String())
	return result, err
}

// startInPTY opens a PTY pair, wires the command's stdio to the slave device,
// sets AGENT_RUNNER_TTY in the environment so child processes can write directly
// to the terminal, and starts the command. Returns the PTY master fd.
func startInPTY(cmd *exec.Cmd, opts Options) (*os.File, error) {
	ptmx, tty, err := gopty.Open()
	if err != nil {
		return nil, fmt.Errorf("pty: failed to open PTY: %w", err)
	}
	ttyPath := tty.Name()
	if ttyPath == "" {
		_ = ptmx.Close()
		_ = tty.Close()
		return nil, fmt.Errorf("pty: slave device has no path")
	}
	if size, sizeErr := gopty.GetsizeFull(os.Stdin); sizeErr == nil {
		_ = gopty.Setsize(ptmx, size)
	}

	cmd.Stdin = tty
	cmd.Stdout = tty
	cmd.Stderr = tty
	cmd.Env = buildEnv(append(opts.Env, "AGENT_RUNNER_TTY="+ttyPath))
	if opts.Workdir != "" {
		cmd.Dir = opts.Workdir
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid:  true,
		Setctty: true,
		Ctty:    0, // stdin fd in the child process
	}

	if err := cmd.Start(); err != nil {
		_ = ptmx.Close()
		_ = tty.Close()
		return nil, fmt.Errorf("pty: failed to start process: %w", err)
	}
	_ = tty.Close()
	return ptmx, nil
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

// writeStdout writes data to stdout, returning any write error.
func writeStdout(data []byte) error {
	_, err := os.Stdout.Write(data)
	return err
}

// beginTermination triggers the continue protocol and schedules SIGTERM/SIGKILL.
// Returns false if the continue trigger was already consumed elsewhere.
func beginTermination(cmd *exec.Cmd, state *ptyState, hint *idleHint, exitCh chan struct{}) bool {
	if !state.tryTriggerContinue() {
		return false
	}
	hint.cancel()
	_ = cmd.Process.Signal(syscall.SIGTERM)
	go func() {
		select {
		case <-time.After(killTimeout):
			_ = cmd.Process.Kill()
		case <-exitCh:
		}
	}()
	return true
}

// forwardOutput reads from the PTY master and writes to stdout, managing the
// idle hint timer. It detects continuation sentinels in the output stream; on
// detection, the sentinel is stripped and the continue + termination protocol
// is triggered (SIGTERM then SIGKILL after killTimeout), mirroring the logic
// in processStdin. Returns when the PTY master is closed or errors.
// flushToStdout drains any buffered bytes from the output processor to stdout,
// returning any write error so callers can handle it.
func flushToStdout(proc *outputProcessor) error {
	flushed := proc.flush()
	if len(flushed) == 0 {
		return nil
	}
	return writeStdout(flushed)
}

// forwardChunk processes a single chunk of PTY output, forwarding clean bytes
// to stdout and detecting the sentinel trigger. Returns true if the sentinel
// was triggered in this chunk (transitioning sentinelTriggered from false to true).
// Returns false with a non-nil error if a write to stdout fails.
func forwardChunk(result outputResult, proc *outputProcessor, hint *idleHint, sentinelTriggered bool) (triggered bool, err error) {
	if !sentinelTriggered {
		hint.clearIfShown()
		if len(result.forward) > 0 {
			if werr := writeStdout(result.forward); werr != nil {
				return false, werr
			}
		}
		hint.reset()
	}
	if result.triggered && !sentinelTriggered {
		if ferr := flushToStdout(proc); ferr != nil {
			return false, ferr
		}
		return true, nil
	}
	return false, nil
}

func forwardOutput(ptmx *os.File, hint *idleHint, cmd *exec.Cmd, state *ptyState, exitCh chan struct{}) {
	proc := &outputProcessor{}
	buf := make([]byte, 4096)
	sentinelTriggered := false
	for {
		n, err := ptmx.Read(buf)
		if n > 0 {
			result := proc.process(buf[:n])
			triggered, werr := forwardChunk(result, proc, hint, sentinelTriggered)
			if werr != nil {
				hint.cancel()
				return
			}
			if triggered {
				sentinelTriggered = true
				if !beginTermination(cmd, state, hint, exitCh) {
					return
				}
			}
		}
		if err != nil {
			if !sentinelTriggered {
				result := proc.finish()
				if len(result.forward) > 0 {
					if werr := writeStdout(result.forward); werr != nil {
						hint.cancel()
						return
					}
				}
				if result.triggered {
					_ = beginTermination(cmd, state, hint, exitCh)
				}
				if ferr := flushToStdout(proc); ferr != nil {
					hint.cancel()
					return
				}
				hint.cancel()
			}
			return
		}
	}
}

// forwardOutputRaw copies PTY output to the user's stdout and, if provided,
// tees a copy into transcript so the session's visible output can be replayed
// after the shell exits. transcript may be nil when no recording is wanted.
// Both sinks are written in full, retrying on short writes per the io.Writer
// contract so no PTY bytes are silently dropped.
func forwardOutputRaw(src io.Reader, stdout, transcript io.Writer) {
	buf := make([]byte, 4096)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			if werr := writeFull(stdout, buf[:n]); werr != nil {
				return
			}
			if transcript != nil {
				_ = writeFull(transcript, buf[:n])
			}
		}
		if err != nil {
			return
		}
	}
}

// writeFull writes all of p to w, looping on short writes. Returns the first
// error encountered.
func writeFull(w io.Writer, p []byte) error {
	for len(p) > 0 {
		n, err := w.Write(p)
		if err != nil {
			return err
		}
		p = p[n:]
	}
	return nil
}

// transcriptMaxBytes bounds the in-memory PTY transcript so long-running or
// very noisy commands (`tail -f`, `yes`, verbose build logs) cannot exhaust
// runner memory. When a session exceeds this, the tail is kept.
const transcriptMaxBytes = 1 << 20 // 1 MiB

// tailBuffer is a bounded writer that retains only the final cap bytes written
// to it. A leading marker is prepended to the output when truncation has
// occurred so readers can tell the transcript was trimmed.
type tailBuffer struct {
	buf       []byte
	maxBytes  int
	truncated bool
}

func newTailBuffer(maxBytes int) *tailBuffer {
	return &tailBuffer{maxBytes: maxBytes}
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	if len(p) >= t.maxBytes {
		t.truncated = true
		t.buf = append(t.buf[:0], p[len(p)-t.maxBytes:]...)
		return len(p), nil
	}
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.maxBytes {
		t.truncated = true
		t.buf = t.buf[len(t.buf)-t.maxBytes:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	if t.truncated {
		return "[...transcript truncated, showing tail...]\n" + string(t.buf)
	}
	return string(t.buf)
}

// stripTranscript removes ANSI escape sequences from raw PTY output so the
// transcript is readable when rendered as plain text.
func stripTranscript(s string) string {
	return ansi.Strip(s)
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
		// Flush forwarded bytes before handling triggers so user input
		// preceding a control sequence is not silently dropped.
		if len(result.forward) > 0 && !state.isDone() {
			_, _ = ptmx.Write(result.forward)
		}
		if result.triggered {
			if !state.tryTriggerContinue() {
				return
			}
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
	}
}

func processStdinRaw(ptmx *os.File, state *ptyState) {
	fd := os.Stdin.Fd()
	pollFds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}} // #nosec G115 -- fd fits int32
	buf := make([]byte, 1024)

	for {
		if state.isDone() {
			return
		}

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
		if state.isDone() {
			return
		}
		_, _ = ptmx.Write(buf[:n])
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

func shellResultFromWait(waitErr error) (Result, error) {
	if waitErr == nil {
		return Result{ExitCode: 0}, nil
	}

	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) {
		return Result{ExitCode: exitErr.ExitCode()}, nil
	}

	return Result{}, fmt.Errorf("pty: wait failed: %w", waitErr)
}
