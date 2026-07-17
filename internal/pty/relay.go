package pty

import (
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"time"
)

const relayStopTimeout = 250 * time.Millisecond

var signalProcessGroup = syscall.Kill

type relayProcess interface {
	waitForExit() error
	reap() error
	signalGroup(syscall.Signal) error
}

type commandRelayProcess struct {
	cmd *exec.Cmd
}

func (p commandRelayProcess) waitForExit() error {
	if p.cmd == nil || p.cmd.Process == nil {
		return errors.New("pty: command process is unavailable")
	}
	return waitForProcessExit(p.cmd.Process.Pid)
}

func (p commandRelayProcess) reap() error {
	if p.cmd == nil {
		return errors.New("pty: command is unavailable")
	}
	return p.cmd.Wait()
}

func (p commandRelayProcess) signalGroup(sig syscall.Signal) error {
	if p.cmd == nil || p.cmd.Process == nil {
		return nil
	}
	return signalProcessGroup(-p.cmd.Process.Pid, sig)
}

type relayConfig struct {
	process          relayProcess
	outputDone       <-chan error
	inputDone        <-chan error
	stopInput        func()
	closePTY         func() error
	drainTimeout     time.Duration
	terminationGrace time.Duration
}

type relayOutcome struct {
	waitErr  error
	relayErr error
	warning  string
}

func superviseRelay(config relayConfig) relayOutcome {
	exitDone := make(chan error, 1)
	go func() { exitDone <- config.process.waitForExit() }()

	outputDone := config.outputDone
	inputDone := config.inputDone
	for {
		select {
		case err := <-exitDone:
			if err != nil {
				return failExitObservation(config, err)
			}
			return finishRelay(config, outputDone, inputDone)
		case err := <-outputDone:
			outputDone = nil
			if err != nil {
				return failRelay(config, exitDone, "output", err)
			}
		case err := <-inputDone:
			inputDone = nil
			if err != nil {
				return failRelay(config, exitDone, "input", err)
			}
		}
	}
}

func finishRelay(config relayConfig, outputDone, inputDone <-chan error) relayOutcome {
	if config.stopInput != nil {
		config.stopInput()
	}

	outcome := relayOutcome{}
	drainTimedOut := false
	ptyClosed := false
	if outputDone != nil {
		timer := time.NewTimer(config.drainTimeout)
		select {
		case err := <-outputDone:
			if err != nil {
				outcome.relayErr = fmt.Errorf("pty: relay output failed: %w", err)
			}
		case <-timer.C:
			drainTimedOut = true
			outcome.warning = fmt.Sprintf("WARNING: PTY output drain timeout after %s; possible output truncation", config.drainTimeout)
			_ = config.process.signalGroup(syscall.SIGTERM)
			if config.closePTY != nil {
				ptyClosed = true
				if err := config.closePTY(); err != nil {
					outcome.warning += fmt.Sprintf("; PTY close error: %v", err)
				}
			}
			escalateProcessGroup(config)
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	}

	if config.closePTY != nil && !ptyClosed {
		if err := config.closePTY(); err != nil && outcome.relayErr == nil {
			if drainTimedOut {
				outcome.warning += fmt.Sprintf("; PTY close error: %v", err)
			} else {
				outcome.relayErr = fmt.Errorf("pty: close relay: %w", err)
			}
		}
	}
	if inputDone != nil {
		select {
		case err := <-inputDone:
			if err != nil && outcome.relayErr == nil {
				outcome.relayErr = fmt.Errorf("pty: relay input failed: %w", err)
			}
		case <-time.After(relayStopTimeout):
		}
	}
	outcome.waitErr = config.process.reap()
	return outcome
}

func escalateProcessGroup(config relayConfig) {
	grace := config.terminationGrace
	if grace <= 0 {
		grace = killTimeout
	}
	timer := time.NewTimer(grace)
	defer timer.Stop()
	<-timer.C
	// waitForExit deliberately leaves the group leader unreaped, so its PID
	// cannot be reused by an unrelated process group during this grace period.
	_ = config.process.signalGroup(syscall.SIGKILL)
}

func failRelay(config relayConfig, exitDone <-chan error, direction string, relayErr error) relayOutcome {
	if config.stopInput != nil {
		config.stopInput()
	}
	_ = config.process.signalGroup(syscall.SIGTERM)
	if config.closePTY != nil {
		_ = config.closePTY()
	}

	grace := config.terminationGrace
	if grace <= 0 {
		grace = killTimeout
	}
	timer := time.NewTimer(grace)
	defer timer.Stop()
	var waitErr error
	exited := false
	select {
	case err := <-exitDone:
		exited = true
		if err != nil {
			relayErr = fmt.Errorf("%w (observe process exit: %v)", relayErr, err)
		}
	case <-timer.C:
		_ = config.process.signalGroup(syscall.SIGKILL)
		killTimer := time.NewTimer(grace)
		select {
		case err := <-exitDone:
			exited = true
			if err != nil {
				relayErr = fmt.Errorf("%w (observe process exit: %v)", relayErr, err)
			}
		case <-killTimer.C:
			relayErr = fmt.Errorf("%w (process did not exit after SIGKILL)", relayErr)
		}
		if !killTimer.Stop() {
			select {
			case <-killTimer.C:
			default:
			}
		}
	}
	if exited {
		waitErr = config.process.reap()
	}
	return relayOutcome{
		waitErr:  waitErr,
		relayErr: fmt.Errorf("pty: relay %s failed while command was running: %w", direction, relayErr),
	}
}

func failExitObservation(config relayConfig, observeErr error) relayOutcome {
	if config.stopInput != nil {
		config.stopInput()
	}
	if config.closePTY != nil {
		_ = config.closePTY()
	}
	// Without a successful non-reaping observation, the group leader cannot
	// serve as a stable identity anchor. Do not signal a numeric PGID here.
	return relayOutcome{
		waitErr:  config.process.reap(),
		relayErr: fmt.Errorf("pty: observe command exit without reaping: %w", observeErr),
	}
}
