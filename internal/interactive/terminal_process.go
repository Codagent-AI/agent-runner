//go:build darwin || linux

package interactive

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/codagent/agent-runner/internal/audit"
)

// TerminalOptions configures a child process that inherits the user's real
// terminal. Agent Runner supervises the process group and job control but does
// not proxy, capture, inspect, or rewrite terminal bytes.
type TerminalOptions struct {
	Args    []string
	Env     []string
	DropEnv []string
	Workdir string

	Before func() error
	After  func() error

	Stdin      io.Reader
	Stdout     io.Writer
	Stderr     io.Writer
	TTY        *os.File
	Foreground bool

	TerminationGrace   time.Duration
	WatchdogExecutable string
	Persist            func(*ProcessMetadata)
	Logger             audit.EventLogger
	Prefix             string
}

type TerminalResult struct {
	ExitCode int
}

// RunTerminal starts one directly attached terminal process and waits for its
// natural exit. It shares the same foreground-process-group and Ctrl-Z/fg/bg
// supervision used by interactive agent steps.
func RunTerminal(ctx context.Context, options *TerminalOptions) (result TerminalResult, err error) {
	if options == nil {
		return result, errors.New("direct terminal runner: options are required")
	}
	if len(options.Args) == 0 {
		return result, errors.New("direct terminal runner: no command specified")
	}
	if options.Before != nil {
		if err := options.Before(); err != nil {
			return result, err
		}
	}
	if options.After != nil {
		defer func() {
			if restoreErr := options.After(); restoreErr != nil && err == nil {
				err = restoreErr
			}
		}()
	}

	cmd, tty, runnerModes, startErr := startTerminalChild(&childLaunchOptions{
		Args: options.Args, Env: options.Env, DropEnv: options.DropEnv,
		Workdir: options.Workdir, Stdin: options.Stdin, Stdout: options.Stdout,
		Stderr: options.Stderr, TTY: options.TTY, Foreground: options.Foreground,
	}, nil)
	if startErr != nil {
		return result, startErr
	}

	identity, identityErr := ReadProcessIdentity(cmd.Process.Pid)
	if identityErr != nil {
		return result, errors.Join(fmt.Errorf("identify direct terminal child: %w", identityErr), teardownSpawnedChild(cmd, tty, runnerModes))
	}
	metadata := ProcessMetadata{ChildPID: cmd.Process.Pid, PGID: cmd.Process.Pid, StartTime: identity}
	if options.Persist != nil {
		options.Persist(&metadata)
		defer options.Persist(nil)
	}

	closeWatchdog, watchdogErr := startWatchdog(options.WatchdogExecutable, metadata, options.TerminationGrace)
	if watchdogErr != nil {
		return result, errors.Join(watchdogErr, teardownSpawnedChild(cmd, tty, runnerModes))
	}
	if closeWatchdog != nil {
		defer closeWatchdog()
	}

	supervisor := newSupervisor(cmd, tty, runnerModes, options.Logger, options.Prefix)
	supervisor.Start()
	select {
	case <-supervisor.Done():
		childResult := supervisor.Result()
		return TerminalResult{ExitCode: waitStatusExitCode(childResult.status)}, childResult.err
	case <-ctx.Done():
		terminateErr := supervisor.Terminate(options.TerminationGrace)
		return result, errors.Join(ctx.Err(), terminateErr)
	}
}
