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
	defaultTerm        = "xterm-256color"
	killTimeout        = 3 * time.Second
	drainTimeout       = time.Second
	stdinPollMs        = 100
	transcriptMaxBytes = 1 << 20
)

// Result holds the outcome of an interactive shell PTY session.
type Result struct {
	ExitCode int
	// Stdout is a plain-text transcript of the bytes emitted by the PTY.
	Stdout string
	// Warning records a non-fatal relay lifecycle warning for console and audit output.
	Warning string
}

// Options configures an interactive shell PTY session.
type Options struct {
	Env     []string
	Workdir string
}

type inputState struct {
	mu   sync.Mutex
	done bool
}

func (s *inputState) isDone() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.done
}

func (s *inputState) markDone() {
	s.mu.Lock()
	s.done = true
	s.mu.Unlock()
}

// RunShellInteractive executes a shell command in a pseudo-terminal with the
// user's terminal attached through an opaque byte relay.
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

	state := &inputState{}
	transcript := newTailBuffer(transcriptMaxBytes)
	resizeCh := startResizeHandler(ptmx)
	outputDone := make(chan error, 1)
	go func() { outputDone <- forwardOutputRaw(ptmx, os.Stdout, transcript) }()
	inputDone := make(chan error, 1)
	go func() { inputDone <- processStdinRaw(ptmx, state) }()

	relayResult := superviseRelay(relayConfig{
		process:          commandRelayProcess{cmd: cmd},
		outputDone:       outputDone,
		inputDone:        inputDone,
		stopInput:        state.markDone,
		closePTY:         ptmx.Close,
		drainTimeout:     drainTimeout,
		terminationGrace: killTimeout,
	})
	signal.Stop(resizeCh)
	close(resizeCh)
	if relayResult.relayErr != nil {
		return Result{}, relayResult.relayErr
	}

	result, err := shellResultFromWait(relayResult.waitErr)
	result.Stdout = stripTranscript(transcript.String())
	result.Warning = relayResult.warning
	return result, err
}

// startInPTY opens a PTY pair, attaches the command to the slave, starts it in
// its own session/process group, closes the parent's slave fd, and returns the
// PTY master.
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
		Ctty:    0,
	}
	if err := cmd.Start(); err != nil {
		_ = ptmx.Close()
		_ = tty.Close()
		return nil, fmt.Errorf("pty: failed to start process: %w", err)
	}
	_ = tty.Close()
	return ptmx, nil
}

func startResizeHandler(ptmx *os.File) chan os.Signal {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	go func() {
		for range ch {
			if size, err := gopty.GetsizeFull(os.Stdin); err == nil {
				_ = gopty.Setsize(ptmx, size)
			}
		}
	}()
	return ch
}

// forwardOutputRaw copies each PTY byte unchanged to stdout and the transcript.
func forwardOutputRaw(src io.Reader, stdout, transcript io.Writer) error {
	buf := make([]byte, 4096)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			if writeErr := writeFull(stdout, buf[:n]); writeErr != nil {
				return writeErr
			}
			if transcript != nil {
				if writeErr := writeFull(transcript, buf[:n]); writeErr != nil {
					return writeErr
				}
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, syscall.EIO) {
				return nil
			}
			return err
		}
	}
}

func writeFull(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}

type tailBuffer struct {
	buf       []byte
	maxBytes  int
	truncated bool
}

func newTailBuffer(maxBytes int) *tailBuffer { return &tailBuffer{maxBytes: maxBytes} }

func (t *tailBuffer) Write(data []byte) (int, error) {
	if len(data) >= t.maxBytes {
		t.truncated = true
		t.buf = append(t.buf[:0], data[len(data)-t.maxBytes:]...)
		return len(data), nil
	}
	t.buf = append(t.buf, data...)
	if len(t.buf) > t.maxBytes {
		t.truncated = true
		t.buf = t.buf[len(t.buf)-t.maxBytes:]
	}
	return len(data), nil
}

func (t *tailBuffer) String() string {
	if t.truncated {
		return "[...transcript truncated, showing tail...]\n" + string(t.buf)
	}
	return string(t.buf)
}

func stripTranscript(transcript string) string { return ansi.Strip(transcript) }

func processStdinRaw(ptmx *os.File, state *inputState) error {
	fd := os.Stdin.Fd()
	pollFDs := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}} // #nosec G115 -- fd fits int32
	buf := make([]byte, 1024)
	for {
		if state.isDone() {
			return nil
		}
		ready, err := unix.Poll(pollFDs, stdinPollMs)
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			return err
		}
		if ready == 0 {
			continue
		}
		n, err := unix.Read(int(fd), buf) // #nosec G115 -- fd fits int
		if err != nil {
			return err
		}
		if n <= 0 {
			return io.EOF
		}
		if state.isDone() {
			return nil
		}
		if err := writeFull(ptmx, buf[:n]); err != nil {
			return err
		}
	}
}

func buildEnv(extra []string) []string {
	env := append(os.Environ(), extra...)
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
