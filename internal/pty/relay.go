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
	wait() error
	signalGroup(syscall.Signal) error
	isGroupAlive() bool
}

type commandRelayProcess struct {
	cmd *exec.Cmd
}

func (p commandRelayProcess) wait() error {
	return p.cmd.Wait()
}

func (p commandRelayProcess) signalGroup(sig syscall.Signal) error {
	if p.cmd == nil || p.cmd.Process == nil {
		return nil
	}
	return signalProcessGroup(-p.cmd.Process.Pid, sig)
}

func (p commandRelayProcess) isGroupAlive() bool {
	if p.cmd == nil || p.cmd.Process == nil {
		return false
	}
	err := signalProcessGroup(-p.cmd.Process.Pid, syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
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
	waitDone := make(chan error, 1)
	go func() { waitDone <- config.process.wait() }()

	outputDone := config.outputDone
	inputDone := config.inputDone
	for {
		select {
		case waitErr := <-waitDone:
			return finishRelay(config, waitErr, outputDone, inputDone)
		case err := <-outputDone:
			outputDone = nil
			if err != nil {
				return failRelay(config, waitDone, "output", err)
			}
		case err := <-inputDone:
			inputDone = nil
			if err != nil {
				return failRelay(config, waitDone, "input", err)
			}
		}
	}
}

func finishRelay(config relayConfig, waitErr error, outputDone, inputDone <-chan error) relayOutcome {
	if config.stopInput != nil {
		config.stopInput()
	}

	outcome := relayOutcome{waitErr: waitErr}
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
	if config.process.isGroupAlive() {
		_ = config.process.signalGroup(syscall.SIGKILL)
	}
}

func failRelay(config relayConfig, waitDone <-chan error, direction string, relayErr error) relayOutcome {
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
	select {
	case waitErr = <-waitDone:
	case <-timer.C:
		_ = config.process.signalGroup(syscall.SIGKILL)
		killTimer := time.NewTimer(grace)
		select {
		case waitErr = <-waitDone:
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
	return relayOutcome{
		waitErr:  waitErr,
		relayErr: fmt.Errorf("pty: relay %s failed while command was running: %w", direction, relayErr),
	}
}
