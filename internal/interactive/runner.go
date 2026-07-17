package interactive

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/mattn/go-isatty"
	"golang.org/x/sys/unix"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/cli"
)

type DirectOptions struct {
	Args      []string
	Env       []string
	Workdir   string
	StepID    string
	SessionID string
	CLI       string
	Control   *ControlServer
	Probe     cli.TurnDurabilityProbe
	// ResolveSessionID discovers a CLI-assigned fresh session after spawn and
	// before the completion checkpoint is captured.
	ResolveSessionID func() string
	Before           func() error
	After            func() error
	Stdin            io.Reader
	Stdout           io.Writer
	Stderr           io.Writer
	TTY              *os.File
	Foreground       bool

	DurabilityTimeout  time.Duration
	TerminationGrace   time.Duration
	WatchdogExecutable string
	Persist            func(*ProcessMetadata)
	Logger             audit.EventLogger
	Prefix             string
}

type DirectResult struct {
	ExitCode         int
	Completed        bool
	DurabilityFailed bool
	DurabilityError  error
}

type DirectRunner struct{ options *DirectOptions }

func NewDirectRunner(options *DirectOptions) *DirectRunner { return &DirectRunner{options: options} }

const (
	freshSessionResolveTimeout  = 2 * time.Second
	freshSessionResolveInterval = 25 * time.Millisecond
)

func (r *DirectRunner) Run(ctx context.Context) (result DirectResult, err error) {
	options := r.options
	if options == nil {
		return result, errors.New("direct interactive runner: options are required")
	}
	if len(options.Args) == 0 {
		return result, errors.New("direct interactive runner: no command specified")
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
	if options.Control == nil {
		return result, errors.New("direct interactive runner: control server is required")
	}
	if options.Probe == nil {
		return result, errors.New("direct interactive runner: turn durability probe is required")
	}

	attempt := options.Control.ActivateWithCheckpoint(options.StepID, func() (cli.Checkpoint, error) {
		if options.SessionID == "" && options.ResolveSessionID != nil {
			options.SessionID = resolveFreshSessionID(ctx, options.ResolveSessionID)
		}
		return captureDurabilityCheckpoint(ctx, options.Probe, options.SessionID)
	})
	defer options.Control.Deactivate()

	cmd, tty, runnerModes, startErr := startDirectChild(options, &attempt)
	if startErr != nil {
		return result, startErr
	}

	identity, identityErr := ReadProcessIdentity(cmd.Process.Pid)
	if identityErr != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		return result, fmt.Errorf("identify direct interactive child: %w", identityErr)
	}
	metadata := ProcessMetadata{ChildPID: cmd.Process.Pid, PGID: cmd.Process.Pid, StartTime: identity, Socket: options.Control.SocketPath()}
	if options.Persist != nil {
		options.Persist(&metadata)
		defer options.Persist(nil)
	}

	closeWatchdog, watchdogErr := startWatchdog(options.WatchdogExecutable, metadata, options.TerminationGrace)
	if watchdogErr != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		return result, watchdogErr
	}
	if closeWatchdog != nil {
		defer closeWatchdog()
	}

	supervisor := newSupervisor(cmd, tty, runnerModes, options.Logger, options.Prefix)
	supervisor.Start()
	return awaitDirectResult(ctx, options, &attempt, supervisor)
}

func captureDurabilityCheckpoint(ctx context.Context, probe cli.TurnDurabilityProbe, sessionID string) (cli.Checkpoint, error) {
	deadline := time.NewTimer(freshSessionResolveTimeout)
	defer deadline.Stop()
	ticker := time.NewTicker(freshSessionResolveInterval)
	defer ticker.Stop()
	var lastErr error
	for {
		checkpoint, err := probe.Checkpoint(sessionID)
		if err == nil {
			return checkpoint, nil
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return cli.Checkpoint{}, ctx.Err()
		case <-deadline.C:
			if errors.Is(lastErr, os.ErrNotExist) {
				// The native store does not exist yet, so nothing was
				// persisted at accept time: the semantically correct baseline
				// is an empty store. The durability wait then runs its full
				// bound from that start-of-store baseline instead of failing
				// the completion at accept time.
				return cli.Checkpoint{}, nil
			}
			return cli.Checkpoint{}, lastErr
		case <-ticker.C:
		}
	}
}

func resolveFreshSessionID(ctx context.Context, resolve func() string) string {
	deadline := time.NewTimer(freshSessionResolveTimeout)
	defer deadline.Stop()
	ticker := time.NewTicker(freshSessionResolveInterval)
	defer ticker.Stop()
	for {
		if sessionID := resolve(); sessionID != "" {
			return sessionID
		}
		select {
		case <-ctx.Done():
			return ""
		case <-deadline.C:
			return ""
		case <-ticker.C:
		}
	}
}

func startDirectChild(options *DirectOptions, attempt *Attempt) (*exec.Cmd, *os.File, *unix.Termios, error) {
	cmd := exec.Command(options.Args[0], options.Args[1:]...) // #nosec G204 -- adapter-built interactive command
	cmd.Dir = options.Workdir
	cmd.Env = append(os.Environ(), options.Env...)
	cmd.Env = append(cmd.Env, attempt.Environment()...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = options.Stdin, options.Stdout, options.Stderr
	if cmd.Stdin == nil {
		cmd.Stdin = os.Stdin
	}
	if cmd.Stdout == nil {
		cmd.Stdout = os.Stdout
	}
	if cmd.Stderr == nil {
		cmd.Stderr = os.Stderr
	}
	tty := options.TTY
	if tty == nil && options.Foreground && isatty.IsTerminal(os.Stdin.Fd()) {
		tty = os.Stdin
	}
	var runnerModes *unix.Termios
	if options.Foreground {
		if tty == nil {
			return nil, nil, nil, errors.New("direct interactive runner: stdin is not a terminal")
		}
		fd, err := checkedTerminalFD(tty.Fd())
		if err != nil {
			return nil, nil, nil, err
		}
		modes, err := readTerminalModes(fd)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("read runner terminal modes: %w", err)
		}
		runnerModes = modes
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Foreground: true, Ctty: fd}
	} else {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, nil, fmt.Errorf("start direct interactive child: %w", err)
	}
	return cmd, tty, runnerModes, nil
}

func awaitDirectResult(ctx context.Context, options *DirectOptions, attempt *Attempt, supervisor *Supervisor) (DirectResult, error) {
	select {
	case completion := <-options.Control.Completions():
		return finishDirectCompletion(ctx, options, attempt, supervisor, &completion)
	case <-supervisor.Done():
		// Acceptance and exit can become ready together. Once accepted, the
		// durability state machine wins over natural-exit handling.
		select {
		case completion := <-options.Control.Completions():
			return finishDirectCompletion(ctx, options, attempt, supervisor, &completion)
		default:
		}
		childResult := supervisor.Result()
		return DirectResult{ExitCode: waitStatusExitCode(childResult.status)}, childResult.err
	case <-ctx.Done():
		_ = supervisor.Terminate(options.TerminationGrace)
		return DirectResult{}, ctx.Err()
	}
}

func finishDirectCompletion(ctx context.Context, options *DirectOptions, attempt *Attempt, supervisor *Supervisor, completion *CompletionRequest) (DirectResult, error) {
	if completion.AttemptID != attempt.ID {
		_ = supervisor.Terminate(options.TerminationGrace)
		return DirectResult{}, errors.New("direct interactive runner: received completion for a different attempt")
	}
	committed, unsubscribe := options.Control.SubscribeCommittedTurn(attempt.ID)
	defer unsubscribe()
	durabilityTimer := NewActiveRuntimeTimer(durationOrDefault(options.DurabilityTimeout, DefaultDurabilityTimeout))
	untrack := supervisor.TrackTimer(durabilityTimer)
	durability := AwaitTurnDurability(ctx, &DurabilityOptions{
		CLI: options.CLI, SessionID: options.SessionID, Probe: options.Probe,
		Checkpoint: completion.Checkpoint, CheckpointErr: completion.CheckpointErr,
		CommittedTurn: committed, ChildExited: supervisor.Done(),
		Timeout: options.DurabilityTimeout, Timer: durabilityTimer,
		Logger: options.Logger, Prefix: options.Prefix,
	})
	untrack()
	durabilityTimer.Stop()
	result := DirectResult{
		Completed:        durability.Outcome == CompletionSuccess,
		DurabilityFailed: durability.Outcome == CompletionFailed,
		DurabilityError:  durability.Err,
	}
	if err := supervisor.Terminate(options.TerminationGrace); err != nil {
		return result, err
	}
	result.ExitCode = waitStatusExitCode(supervisor.Result().status)
	return result, nil
}

func checkedTerminalFD(fd uintptr) (int, error) {
	if fd > uintptr(^uint(0)>>1) {
		return 0, fmt.Errorf("terminal file descriptor %d overflows int", fd)
	}
	return int(fd), nil
}

func durationOrDefault(value, fallback time.Duration) time.Duration {
	if value > 0 {
		return value
	}
	return fallback
}

func startWatchdog(executable string, metadata ProcessMetadata, grace time.Duration) (func(), error) {
	if executable == "" {
		return nil, nil
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("create watchdog pipe: %w", err)
	}
	command := exec.Command(executable, watchdogArgs(metadata, durationOrDefault(grace, DefaultTerminationGrace))...) // #nosec G204 -- current executable and internal args
	command.ExtraFiles = []*os.File{reader}
	command.Stdin = nil
	command.Stdout = nil
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		_ = reader.Close()
		_ = writer.Close()
		return nil, fmt.Errorf("start child watchdog: %w", err)
	}
	_ = reader.Close()
	return func() {
		_ = writer.Close()
		_ = command.Wait()
	}, nil
}
