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
)

const (
	defaultTerm = "xterm-256color"
	hintDelay   = 800 * time.Millisecond
	killTimeout = 3 * time.Second
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

	size, sizeErr := gopty.GetsizeFull(os.Stdin)
	var ptmx *os.File
	if sizeErr == nil {
		ptmx, err = gopty.StartWithSize(cmd, size)
	} else {
		ptmx, err = gopty.Start(cmd)
	}
	if err != nil {
		return Result{}, fmt.Errorf("pty: failed to start process: %w", err)
	}
	defer ptmx.Close()

	var (
		mu                sync.Mutex
		continueTriggered bool
		done              bool
	)

	proc := &inputProcessor{}
	hint := newIdleHint(hintDelay)

	// Propagate terminal resize events to the PTY.
	resizeCh := make(chan os.Signal, 1)
	signal.Notify(resizeCh, syscall.SIGWINCH)
	defer signal.Stop(resizeCh)
	go func() {
		for range resizeCh {
			if s, err := gopty.GetsizeFull(os.Stdin); err == nil {
				gopty.Setsize(ptmx, s) // #nosec G104 -- best-effort resize
			}
		}
	}()

	// Forward PTY output to stdout, managing the idle hint.
	exitCh := make(chan struct{})
	outputDone := make(chan struct{})
	go func() {
		defer close(outputDone)
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				hint.clearIfShown()
				os.Stdout.Write(buf[:n]) // #nosec G104 -- best-effort stdout write
				hint.reset()
			}
			if err != nil {
				hint.cancel()
				break
			}
		}
	}()

	// Read stdin, process input, and forward to PTY.
	go func() {
		buf := make([]byte, 1024)
		for {
			n, readErr := os.Stdin.Read(buf)
			if readErr != nil {
				break
			}

			mu.Lock()
			if done {
				mu.Unlock()
				break
			}
			mu.Unlock()

			result := proc.process(buf[:n])
			if len(result.forward) > 0 {
				mu.Lock()
				if !done {
					ptmx.Write(result.forward) // #nosec G104 -- best-effort PTY write
				}
				mu.Unlock()
			}
			if result.triggered {
				mu.Lock()
				continueTriggered = true
				done = true
				mu.Unlock()

				hint.cancel()
				cmd.Process.Signal(syscall.SIGTERM) // #nosec G104 -- best-effort SIGTERM

				// SIGKILL after timeout if the process hasn't exited.
				go func() {
					select {
					case <-time.After(killTimeout):
						cmd.Process.Kill() // #nosec G104 -- best-effort SIGKILL
					case <-exitCh:
					}
				}()
				break
			}
		}
	}()

	// Wait for the process to exit.
	waitErr := cmd.Wait()
	close(exitCh)

	mu.Lock()
	done = true
	triggered := continueTriggered
	mu.Unlock()

	hint.cancel()
	<-outputDone

	exitCode := 0
	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}

	return Result{
		ExitCode:          exitCode,
		ContinueTriggered: triggered,
	}, nil
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
