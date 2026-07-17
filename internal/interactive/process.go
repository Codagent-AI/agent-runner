package interactive

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/codagent/agent-runner/internal/audit"
)

const DefaultTerminationGrace = 3 * time.Second

type waitEvent uint8

const (
	waitUnknown waitEvent = iota
	waitStopped
	waitContinued
	waitExited
)

// ProcessMetadata is enough to identify and clean up a child attempt without
// ever signaling a process that merely reused the same PID.
type ProcessMetadata struct {
	ChildPID  int    `json:"child_pid"`
	PGID      int    `json:"pgid"`
	StartTime string `json:"start_time"`
	Socket    string `json:"socket_path"`
}

type processResult struct {
	status unix.WaitStatus
	err    error
}

// Supervisor is the sole owner of Wait4 for one direct child. cmd.Wait must
// never be called for a command handed to Supervisor.
type Supervisor struct {
	cmd         *exec.Cmd
	pid         int
	pgid        int
	tty         *os.File
	runnerPGID  int
	runnerModes *unix.Termios
	logger      audit.EventLogger
	prefix      string
	now         func() time.Time

	mu        sync.Mutex
	suspended bool
	timers    map[*ActiveRuntimeTimer]struct{}
	done      chan struct{}
	result    processResult
	doneOnce  sync.Once
}

func newSupervisor(cmd *exec.Cmd, tty *os.File, runnerModes *unix.Termios, logger audit.EventLogger, prefix string) *Supervisor {
	now := time.Now
	return &Supervisor{
		cmd: cmd, pid: cmd.Process.Pid, pgid: cmd.Process.Pid, tty: tty,
		runnerPGID: unix.Getpgrp(), runnerModes: runnerModes, logger: logger,
		prefix: prefix, now: now, timers: make(map[*ActiveRuntimeTimer]struct{}),
		done: make(chan struct{}),
	}
}

func (s *Supervisor) Start() { go s.waitLoop() }

func (s *Supervisor) Done() <-chan struct{} { return s.done }

func (s *Supervisor) Result() processResult {
	<-s.done
	return s.result
}

func (s *Supervisor) TrackTimer(timer *ActiveRuntimeTimer) func() {
	s.mu.Lock()
	s.timers[timer] = struct{}{}
	if s.suspended {
		timer.Pause()
	}
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		delete(s.timers, timer)
		s.mu.Unlock()
	}
}

func (s *Supervisor) waitLoop() {
	for {
		var status unix.WaitStatus
		pid, err := unix.Wait4(s.pid, &status, unix.WUNTRACED|unix.WCONTINUED, nil)
		if err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			s.finish(processResult{err: fmt.Errorf("wait for child %d: %w", s.pid, err)})
			return
		}
		if pid != s.pid {
			continue
		}
		event, stopSignal := classifyWaitStatus(status)
		switch event {
		case waitStopped:
			if stopSignal == unix.SIGTTIN && s.tty != nil {
				fd, fdErr := checkedTerminalFD(s.tty.Fd())
				if fdErr != nil {
					s.finish(processResult{err: fdErr})
					return
				}
				_ = setForegroundProcessGroup(fd, s.pgid)
				_ = unix.Kill(-s.pgid, unix.SIGCONT)
				continue
			}
			if err := s.forwardStop(stopSignal); err != nil {
				s.finish(processResult{err: err})
				return
			}
		case waitContinued:
			// Continuation is audited by forwardStop after foreground ownership
			// has been restored. This event can also arrive for the SIGTTIN fix.
		case waitExited:
			// The restore error is surfaced through the result without
			// altering the recorded exit status.
			s.finish(processResult{status: status, err: s.reclaimForeground()})
			return
		}
	}
}

func (s *Supervisor) forwardStop(stopSignal unix.Signal) error {
	if s.tty == nil {
		return fmt.Errorf("child %d stopped with %s without a controlling terminal", s.pid, stopSignal)
	}
	fd, err := checkedTerminalFD(s.tty.Fd())
	if err != nil {
		return err
	}
	childModes, err := readTerminalModes(fd)
	if err != nil {
		return fmt.Errorf("save child terminal modes: %w", err)
	}
	s.pauseTimers()
	if err := setForegroundProcessGroup(fd, s.runnerPGID); err != nil {
		return fmt.Errorf("reclaim terminal after child stop: %w", err)
	}
	if s.runnerModes != nil {
		if err := writeTerminalModes(fd, s.runnerModes); err != nil {
			return fmt.Errorf("restore runner terminal modes after child stop: %w", err)
		}
	}
	s.emit(audit.EventChildStopped, map[string]any{"pid": s.pid, "pgid": s.pgid, "signal": stopSignal.String()})

	for {
		if err := unix.Kill(-s.runnerPGID, unix.SIGSTOP); err != nil {
			return fmt.Errorf("stop runner process group: %w", err)
		}
		foreground, err := foregroundProcessGroup(fd)
		if err != nil {
			return fmt.Errorf("inspect terminal foreground after continue: %w", err)
		}
		if foreground != s.runnerPGID {
			// A background `bg` must not wake the child or touch its terminal.
			continue
		}
		break
	}
	if err := writeTerminalModes(fd, childModes); err != nil {
		return fmt.Errorf("restore child terminal modes: %w", err)
	}
	if err := setForegroundProcessGroup(fd, s.pgid); err != nil {
		return fmt.Errorf("return terminal foreground to child: %w", err)
	}
	if err := unix.Kill(-s.pgid, unix.SIGCONT); err != nil && !errors.Is(err, unix.ESRCH) {
		return fmt.Errorf("continue child process group: %w", err)
	}
	s.resumeTimers()
	s.emit(audit.EventChildContinued, map[string]any{"pid": s.pid, "pgid": s.pgid})
	return nil
}

func (s *Supervisor) Terminate(grace time.Duration) error {
	if grace <= 0 {
		grace = DefaultTerminationGrace
	}
	select {
	case <-s.done:
		return s.result.err
	default:
	}
	if err := unix.Kill(-s.pgid, unix.SIGTERM); err != nil && !errors.Is(err, unix.ESRCH) {
		return fmt.Errorf("terminate child process group: %w", err)
	}
	timer := NewActiveRuntimeTimer(grace)
	defer timer.Stop()
	untrack := s.TrackTimer(timer)
	defer untrack()
	select {
	case <-s.done:
		return s.result.err
	case <-timer.Done():
		if err := unix.Kill(-s.pgid, unix.SIGKILL); err != nil && !errors.Is(err, unix.ESRCH) {
			return fmt.Errorf("kill child process group: %w", err)
		}
		<-s.done
		return s.result.err
	}
}

func (s *Supervisor) pauseTimers() {
	s.mu.Lock()
	s.suspended = true
	for timer := range s.timers {
		timer.Pause()
	}
	s.mu.Unlock()
}

func (s *Supervisor) resumeTimers() {
	s.mu.Lock()
	s.suspended = false
	for timer := range s.timers {
		timer.Resume()
	}
	s.mu.Unlock()
}

func (s *Supervisor) reclaimForeground() error {
	return restoreRunnerTerminal(s.tty, s.runnerPGID, s.runnerModes)
}

// restoreRunnerTerminal returns terminal foreground to the runner's process
// group and restores the runner's saved modes. Failures are surfaced to the
// caller instead of being silently discarded.
func restoreRunnerTerminal(tty *os.File, runnerPGID int, runnerModes *unix.Termios) error {
	if tty == nil {
		return nil
	}
	fd, err := checkedTerminalFD(tty.Fd())
	if err != nil {
		return err
	}
	var errs []error
	if err := setForegroundProcessGroup(fd, runnerPGID); err != nil {
		errs = append(errs, fmt.Errorf("reclaim terminal foreground: %w", err))
	}
	if runnerModes != nil {
		if err := writeTerminalModes(fd, runnerModes); err != nil {
			errs = append(errs, fmt.Errorf("restore runner terminal modes: %w", err))
		}
	}
	return errors.Join(errs...)
}

func (s *Supervisor) finish(result processResult) {
	s.doneOnce.Do(func() {
		s.result = result
		close(s.done)
	})
}

func (s *Supervisor) emit(eventType audit.EventType, data map[string]any) {
	if s.logger == nil {
		return
	}
	s.logger.Emit(audit.Event{Timestamp: s.now().UTC().Format(time.RFC3339Nano), Prefix: s.prefix, Type: eventType, Data: data})
}

func foregroundProcessGroup(fd int) (int, error) {
	return unix.IoctlGetInt(fd, unix.TIOCGPGRP)
}

func setForegroundProcessGroup(fd, pgid int) error {
	signal.Ignore(syscall.SIGTTOU)
	defer signal.Reset(syscall.SIGTTOU)
	return unix.IoctlSetPointerInt(fd, unix.TIOCSPGRP, pgid)
}

func waitStatusExitCode(status unix.WaitStatus) int {
	if status.Exited() {
		return status.ExitStatus()
	}
	if status.Signaled() {
		return 128 + int(status.Signal())
	}
	return -1
}

func watchdogArgs(metadata ProcessMetadata, grace time.Duration) []string {
	return []string{"internal", "watchdog", "--pid", strconv.Itoa(metadata.ChildPID), "--pgid", strconv.Itoa(metadata.PGID), "--start-time", metadata.StartTime, "--grace", grace.String()}
}
