package pty

import (
	"errors"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

type fakeRelayProcess struct {
	waitErr     error
	observeErr  error
	waitRelease chan struct{}

	mu              sync.Mutex
	signals         []syscall.Signal
	processSignals  []syscall.Signal
	reapBlock       chan struct{}
	reapReleaseSig  syscall.Signal
	reaped          bool
	killedAfterReap bool
}

type stubbornRelayProcess struct {
	signals chan syscall.Signal
}

func (p *stubbornRelayProcess) waitForExit() error {
	select {}
}

func (p *stubbornRelayProcess) reap() error {
	select {}
}

func (p *stubbornRelayProcess) signalGroup(sig syscall.Signal) error {
	p.signals <- sig
	return nil
}

func (p *stubbornRelayProcess) signalProcess(syscall.Signal) error {
	return nil
}

func (p *fakeRelayProcess) waitForExit() error {
	p.mu.Lock()
	release := p.waitRelease
	p.mu.Unlock()
	if release != nil {
		<-release
	}
	return p.observeErr
}

func (p *fakeRelayProcess) reap() error {
	p.mu.Lock()
	block := p.reapBlock
	p.mu.Unlock()
	if block != nil {
		<-block
	}
	p.mu.Lock()
	p.reaped = true
	p.mu.Unlock()
	return p.waitErr
}

func (p *fakeRelayProcess) signalGroup(sig syscall.Signal) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.signals = append(p.signals, sig)
	if sig == syscall.SIGKILL && p.reaped {
		p.killedAfterReap = true
	}
	if p.waitRelease != nil && sig == syscall.SIGTERM {
		close(p.waitRelease)
		p.waitRelease = nil
	}
	return nil
}

func (p *fakeRelayProcess) signalProcess(sig syscall.Signal) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.processSignals = append(p.processSignals, sig)
	if p.reapBlock != nil && sig == p.reapReleaseSig {
		close(p.reapBlock)
		p.reapBlock = nil
	}
	return nil
}

func (p *fakeRelayProcess) gotSignals() []syscall.Signal {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]syscall.Signal(nil), p.signals...)
}

func (p *fakeRelayProcess) gotProcessSignals() []syscall.Signal {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]syscall.Signal(nil), p.processSignals...)
}

func (p *fakeRelayProcess) wasKilledAfterReap() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.killedAfterReap
}

func TestSuperviseRelayDrainsOutputAfterCommandExit(t *testing.T) {
	waitErr := errors.New("exit status 7")
	process := &fakeRelayProcess{waitErr: waitErr}
	outputDone := make(chan error, 1)
	closed := make(chan struct{})

	go func() {
		time.Sleep(10 * time.Millisecond)
		outputDone <- nil
	}()

	outcome := superviseRelay(relayConfig{
		process:      process,
		outputDone:   outputDone,
		closePTY:     func() error { close(closed); return nil },
		drainTimeout: 100 * time.Millisecond,
	})

	if !errors.Is(outcome.waitErr, waitErr) {
		t.Fatalf("wait error = %v, want %v", outcome.waitErr, waitErr)
	}
	if outcome.relayErr != nil {
		t.Fatalf("unexpected relay error: %v", outcome.relayErr)
	}
	if outcome.warning != "" {
		t.Fatalf("unexpected warning: %q", outcome.warning)
	}
	select {
	case <-closed:
	default:
		t.Fatal("PTY was not closed after output drained")
	}
}

func TestSuperviseRelayBoundsDrainAndPreservesCommandOutcome(t *testing.T) {
	waitErr := errors.New("exit status 9")
	process := &fakeRelayProcess{waitErr: waitErr}
	outputDone := make(chan error)
	closed := make(chan struct{})

	start := time.Now()
	outcome := superviseRelay(relayConfig{
		process:          process,
		outputDone:       outputDone,
		closePTY:         func() error { close(closed); return errors.New("close after timeout") },
		drainTimeout:     15 * time.Millisecond,
		terminationGrace: 10 * time.Millisecond,
	})

	if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
		t.Fatalf("bounded drain took %s", elapsed)
	}
	if !errors.Is(outcome.waitErr, waitErr) {
		t.Fatalf("wait error = %v, want preserved %v", outcome.waitErr, waitErr)
	}
	if outcome.relayErr != nil {
		t.Fatalf("drain timeout must not replace command outcome: %v", outcome.relayErr)
	}
	if !strings.Contains(strings.ToLower(outcome.warning), "drain timeout") ||
		!strings.Contains(strings.ToLower(outcome.warning), "output truncation") {
		t.Fatalf("warning = %q, want prominent drain-timeout/output-truncation warning", outcome.warning)
	}
	if diff := cmp.Diff([]syscall.Signal{syscall.SIGTERM, syscall.SIGKILL}, process.gotSignals()); diff != "" {
		t.Fatalf("signals mismatch (-want +got):\n%s", diff)
	}
	if process.wasKilledAfterReap() {
		t.Fatal("SIGKILL was sent after the process-group leader was reaped")
	}
	select {
	case <-closed:
	default:
		t.Fatal("PTY was not closed on drain timeout")
	}
}

func TestSuperviseRelayTerminatesProcessGroupOnOutputError(t *testing.T) {
	process := &fakeRelayProcess{waitRelease: make(chan struct{})}
	outputDone := make(chan error, 1)
	outputDone <- errors.New("stdout broke")
	closed := make(chan struct{})

	outcome := superviseRelay(relayConfig{
		process:          process,
		outputDone:       outputDone,
		closePTY:         func() error { close(closed); return nil },
		drainTimeout:     50 * time.Millisecond,
		terminationGrace: 50 * time.Millisecond,
	})

	if outcome.relayErr == nil || !strings.Contains(outcome.relayErr.Error(), "relay output") ||
		!strings.Contains(outcome.relayErr.Error(), "stdout broke") {
		t.Fatalf("relay error = %v, want descriptive output error", outcome.relayErr)
	}
	if diff := cmp.Diff([]syscall.Signal{syscall.SIGTERM}, process.gotSignals()); diff != "" {
		t.Fatalf("signals mismatch (-want +got):\n%s", diff)
	}
	select {
	case <-closed:
	default:
		t.Fatal("PTY was not closed after relay failure")
	}
}

func TestSuperviseRelayDoesNotSignalUnverifiedGroupAfterExitObservationFailure(t *testing.T) {
	process := &fakeRelayProcess{observeErr: errors.New("waitid failed")}

	outcome := superviseRelay(relayConfig{
		process:  process,
		closePTY: func() error { return nil },
	})

	if outcome.relayErr == nil || !strings.Contains(outcome.relayErr.Error(), "observe command exit") {
		t.Fatalf("relay error = %v, want exit-observation failure", outcome.relayErr)
	}
	if diff := cmp.Diff([]syscall.Signal(nil), process.gotSignals()); diff != "" {
		t.Fatalf("signals mismatch (-want +got):\n%s", diff)
	}
}

func TestSuperviseRelayBoundsReapAfterExitObservationFailure(t *testing.T) {
	waitErr := errors.New("signal: terminated")
	process := &fakeRelayProcess{
		waitErr:        waitErr,
		observeErr:     errors.New("waitid failed"),
		reapBlock:      make(chan struct{}),
		reapReleaseSig: syscall.SIGTERM,
	}
	resultDone := make(chan relayOutcome, 1)

	go func() {
		resultDone <- superviseRelay(relayConfig{
			process:          process,
			closePTY:         func() error { return nil },
			terminationGrace: 20 * time.Millisecond,
		})
	}()

	var outcome relayOutcome
	select {
	case outcome = <-resultDone:
	case <-time.After(2 * time.Second):
		t.Fatal("relay hung reaping a live child after exit observation failure")
	}
	if outcome.relayErr == nil || !strings.Contains(outcome.relayErr.Error(), "observe command exit") {
		t.Fatalf("relay error = %v, want exit-observation failure", outcome.relayErr)
	}
	if !errors.Is(outcome.waitErr, waitErr) {
		t.Fatalf("wait error = %v, want preserved %v", outcome.waitErr, waitErr)
	}
	if diff := cmp.Diff([]syscall.Signal(nil), process.gotSignals()); diff != "" {
		t.Fatalf("group signals mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]syscall.Signal{syscall.SIGTERM}, process.gotProcessSignals()); diff != "" {
		t.Fatalf("process signals mismatch (-want +got):\n%s", diff)
	}
}

func TestSuperviseRelayEscalatesProcessSignalAfterExitObservationFailure(t *testing.T) {
	process := &fakeRelayProcess{
		observeErr:     errors.New("waitid failed"),
		reapBlock:      make(chan struct{}),
		reapReleaseSig: syscall.SIGKILL,
	}
	resultDone := make(chan relayOutcome, 1)

	go func() {
		resultDone <- superviseRelay(relayConfig{
			process:          process,
			closePTY:         func() error { return nil },
			terminationGrace: 20 * time.Millisecond,
		})
	}()

	var outcome relayOutcome
	select {
	case outcome = <-resultDone:
	case <-time.After(2 * time.Second):
		t.Fatal("relay hung reaping a SIGTERM-resistant child after exit observation failure")
	}
	if outcome.relayErr == nil || !strings.Contains(outcome.relayErr.Error(), "observe command exit") {
		t.Fatalf("relay error = %v, want exit-observation failure", outcome.relayErr)
	}
	if diff := cmp.Diff([]syscall.Signal(nil), process.gotSignals()); diff != "" {
		t.Fatalf("group signals mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]syscall.Signal{syscall.SIGTERM, syscall.SIGKILL}, process.gotProcessSignals()); diff != "" {
		t.Fatalf("process signals mismatch (-want +got):\n%s", diff)
	}
}

func TestSuperviseRelayGivesUpOnUnreapableProcessAfterExitObservationFailure(t *testing.T) {
	process := &fakeRelayProcess{
		waitErr:    errors.New("never seen"),
		observeErr: errors.New("waitid failed"),
		reapBlock:  make(chan struct{}),
		// reapReleaseSig is zero: no signal ever unblocks reap.
	}
	resultDone := make(chan relayOutcome, 1)

	go func() {
		resultDone <- superviseRelay(relayConfig{
			process:          process,
			closePTY:         func() error { return nil },
			terminationGrace: 20 * time.Millisecond,
		})
	}()

	var outcome relayOutcome
	select {
	case outcome = <-resultDone:
	case <-time.After(2 * time.Second):
		t.Fatal("relay hung on an unreapable child after exit observation failure")
	}
	if outcome.relayErr == nil || !strings.Contains(outcome.relayErr.Error(), "observe command exit") ||
		!strings.Contains(outcome.relayErr.Error(), "SIGKILL") {
		t.Fatalf("relay error = %v, want exit-observation failure noting SIGKILL escalation", outcome.relayErr)
	}
	if outcome.waitErr != nil {
		t.Fatalf("wait error = %v, want nil for unreaped process", outcome.waitErr)
	}
	if diff := cmp.Diff([]syscall.Signal(nil), process.gotSignals()); diff != "" {
		t.Fatalf("group signals mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]syscall.Signal{syscall.SIGTERM, syscall.SIGKILL}, process.gotProcessSignals()); diff != "" {
		t.Fatalf("process signals mismatch (-want +got):\n%s", diff)
	}
}

func TestSuperviseRelayFailureCannotDeadlockOnUnreapedProcess(t *testing.T) {
	process := &stubbornRelayProcess{signals: make(chan syscall.Signal, 2)}
	outputDone := make(chan error, 1)
	outputDone <- errors.New("stdout broke")
	resultDone := make(chan relayOutcome, 1)

	go func() {
		resultDone <- superviseRelay(relayConfig{
			process:          process,
			outputDone:       outputDone,
			closePTY:         func() error { return nil },
			drainTimeout:     10 * time.Millisecond,
			terminationGrace: 10 * time.Millisecond,
		})
	}()

	select {
	case outcome := <-resultDone:
		if outcome.relayErr == nil {
			t.Fatal("expected relay error")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("relay failure deadlocked after SIGKILL")
	}
	var got []syscall.Signal
	for len(process.signals) > 0 {
		got = append(got, <-process.signals)
	}
	if diff := cmp.Diff([]syscall.Signal{syscall.SIGTERM, syscall.SIGKILL}, got); diff != "" {
		t.Fatalf("signals mismatch (-want +got):\n%s", diff)
	}
}

func TestCommandRelayProcessSignalsNegativeProcessGroupID(t *testing.T) {
	oldKill := signalProcessGroup
	defer func() { signalProcessGroup = oldKill }()

	var gotPID int
	var gotSignal syscall.Signal
	signalProcessGroup = func(pid int, sig syscall.Signal) error {
		gotPID = pid
		gotSignal = sig
		return nil
	}

	process := commandRelayProcess{cmd: &exec.Cmd{Process: &os.Process{Pid: 4321}}}
	if err := process.signalGroup(syscall.SIGTERM); err != nil {
		t.Fatalf("signal process group: %v", err)
	}
	if gotPID != -4321 || gotSignal != syscall.SIGTERM {
		t.Fatalf("signal target = (%d, %v), want (-4321, %v)", gotPID, gotSignal, syscall.SIGTERM)
	}
}

func TestCommandRelayProcessSignalsProcessHandle(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start command: %v", err)
	}
	process := commandRelayProcess{cmd: cmd}
	if err := process.signalProcess(syscall.SIGKILL); err != nil {
		t.Fatalf("signal process: %v", err)
	}
	waitErr := cmd.Wait()
	var exitErr *exec.ExitError
	if !errors.As(waitErr, &exitErr) {
		t.Fatalf("wait error = %v, want exit error from SIGKILL", waitErr)
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
		t.Fatalf("wait status = %+v, want killed by SIGKILL", exitErr.Sys())
	}
}

func TestCommandRelayProcessSignalProcessWithoutHandle(t *testing.T) {
	if err := (commandRelayProcess{}).signalProcess(syscall.SIGTERM); err == nil {
		t.Fatal("signalProcess without a process handle must return an error, not panic or signal")
	}
}

func TestWaitForProcessExitLeavesCommandReapable(t *testing.T) {
	cmd := exec.Command("sh", "-c", "exit 7")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start command: %v", err)
	}
	if err := waitForProcessExit(cmd.Process.Pid); err != nil {
		t.Fatalf("observe command exit: %v", err)
	}
	waitErr := cmd.Wait()
	var exitErr *exec.ExitError
	if !errors.As(waitErr, &exitErr) || exitErr.ExitCode() != 7 {
		t.Fatalf("wait error = %v, want exit status 7", waitErr)
	}
}

func TestWaitForProcessExitRejectsOutOfRangePID(t *testing.T) {
	if strconv.IntSize < 64 {
		t.Skip("int cannot represent a PID above uint32")
	}
	pid := int(int64(math.MaxUint32) + 1)
	if err := waitForProcessExit(pid); !errors.Is(err, syscall.EINVAL) {
		t.Fatalf("waitForProcessExit(%d) error = %v, want EINVAL", pid, err)
	}
}
