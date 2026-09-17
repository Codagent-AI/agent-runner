package interactive

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestRunTerminalJoinsChildAndRestoreFailures(t *testing.T) {
	restoreErr := errors.New("restore terminal: boom")
	_, err := RunTerminal(context.Background(), &TerminalOptions{
		Args:  []string{filepath.Join(t.TempDir(), "missing-command")},
		After: func() error { return restoreErr },
	})
	if err == nil || !errors.Is(err, restoreErr) {
		t.Fatalf("RunTerminal() error = %v, want joined restore failure", err)
	}
	for _, want := range []string{"missing-command", "restore terminal after direct child"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("RunTerminal() error = %v, want %q", err, want)
		}
	}
}

func TestReclaimForegroundSurfacesTerminalErrors(t *testing.T) {
	t.Parallel()
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("open %s: %v", os.DevNull, err)
	}
	defer devNull.Close()
	supervisor := &Supervisor{tty: devNull, runnerPGID: unix.Getpgrp()}

	if err := supervisor.reclaimForeground(); err == nil {
		t.Fatal("reclaimForeground on a non-terminal succeeded, want surfaced error")
	} else if !strings.Contains(err.Error(), "reclaim terminal foreground") {
		t.Fatalf("reclaimForeground error = %v, want reclaim terminal foreground failure", err)
	}
}

func TestRestoreRunnerTerminalWithoutTTYIsNoOp(t *testing.T) {
	t.Parallel()
	if err := restoreRunnerTerminal(nil, unix.Getpgrp(), nil); err != nil {
		t.Fatalf("restoreRunnerTerminal(nil tty) = %v, want nil", err)
	}
}

func TestSupervisorRestoresTerminalAfterWaitFailure(t *testing.T) {
	t.Parallel()
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("open %s: %v", os.DevNull, err)
	}
	defer devNull.Close()

	supervisor := &Supervisor{
		pid:        1 << 30,
		tty:        devNull,
		runnerPGID: unix.Getpgrp(),
		done:       make(chan struct{}),
	}
	supervisor.Start()
	result := supervisor.Result()
	if result.err == nil {
		t.Fatal("supervisor wait failure returned nil error")
	}
	for _, want := range []string{"wait for child", "reclaim terminal foreground"} {
		if !strings.Contains(result.err.Error(), want) {
			t.Fatalf("supervisor error = %v, want joined %q error", result.err, want)
		}
	}
}

func TestProcessIdentityRejectsDifferentStartTime(t *testing.T) {
	identity, err := ReadProcessIdentity(os.Getpid())
	if err != nil {
		t.Fatalf("ReadProcessIdentity: %v", err)
	}
	if ProcessIdentityMatches(os.Getpid(), identity+"-reused") {
		t.Fatal("different process start identity matched")
	}
	if !ProcessIdentityMatches(os.Getpid(), identity) {
		t.Fatal("current process identity did not match")
	}
}

// A terminal ownership failure must end supervision rather than repeatedly
// continuing a child that will immediately stop on terminal input again.
func TestSupervisorSurfacesJobControlRecoveryFailure(t *testing.T) {
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer devNull.Close()
	cmd := exec.Command("sleep", "30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = unix.Kill(-cmd.Process.Pid, unix.SIGKILL); _ = cmd.Process.Release() }()
	logger := &recordingEventLogger{}
	s := newSupervisor(cmd, devNull, nil, logger, "test")
	if err := unix.Kill(-cmd.Process.Pid, unix.SIGTTIN); err != nil {
		t.Fatal(err)
	}
	s.Start()
	select {
	case <-s.Done():
		if err := s.Result().err; err == nil || !strings.Contains(err.Error(), "recover child terminal foreground") {
			t.Fatalf("result = %v, want recovery failure", err)
		}
	case <-time.After(time.Second):
		t.Fatal("supervisor ignored terminal recovery failure")
	}
}

func TestSupervisorLogsTerminalOwnership(t *testing.T) {
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer devNull.Close()
	cmd := exec.Command("true")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Release()
	logger := &recordingEventLogger{}
	s := newSupervisor(cmd, devNull, nil, logger, "test")
	s.Start()
	_ = s.Result()
	logger.mu.Lock()
	defer logger.mu.Unlock()
	phases := map[string]bool{}
	for _, event := range logger.events {
		if string(event.Type) != "terminal_ownership" {
			continue
		}
		phases[event.Data["phase"].(string)] = true
		if event.Data["runner_pid"] != os.Getpid() || event.Data["runner_pgid"] != unix.Getpgrp() || event.Data["child_pgid"] != cmd.Process.Pid || event.Data["foreground_error"] == nil {
			t.Fatalf("incomplete diagnostic: %#v", event)
		}
	}
	for _, phase := range []string{"child_started", "before_reclaim", "after_reclaim"} {
		if !phases[phase] {
			t.Errorf("missing terminal diagnostic %s", phase)
		}
	}
}

func TestSupervisorLogsRunnerContinue(t *testing.T) {
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer devNull.Close()
	cmd := exec.Command("sleep", "30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	logger := &recordingEventLogger{}
	s := newSupervisor(cmd, devNull, nil, logger, "test")
	s.Start()
	defer func() { _ = unix.Kill(-cmd.Process.Pid, unix.SIGKILL); <-s.Done(); _ = cmd.Process.Release() }()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if err := unix.Kill(os.Getpid(), unix.SIGCONT); err != nil {
			t.Fatal(err)
		}
		for _, event := range logger.snapshot() {
			if string(event.Type) == "terminal_ownership" && event.Data["phase"] == "runner_continued" {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("missing runner continuation ownership diagnostic")
}

func TestSupervisorReapsChildAfterRecoveryKillFails(t *testing.T) {
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer devNull.Close()
	cmd := exec.Command("sleep", "30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = unix.Kill(cmd.Process.Pid, unix.SIGKILL); _ = cmd.Process.Release() }()
	s := newSupervisor(cmd, devNull, nil, nil, "test")
	// Simulate an unavailable process group while the direct child still has
	// a terminal wait status to collect. Group-directed SIGKILL returns ESRCH.
	s.pgid = 1 << 30
	if err := unix.Kill(cmd.Process.Pid, unix.SIGTTIN); err != nil {
		t.Fatal(err)
	}
	s.Start()
	select {
	case <-s.Done():
		t.Fatal("supervision finished without reaping its direct child")
	case <-time.After(50 * time.Millisecond):
	}
	if err := unix.Kill(cmd.Process.Pid, unix.SIGKILL); err != nil {
		t.Fatal(err)
	}
	select {
	case <-s.Done():
		result := s.Result()
		if !result.status.Signaled() || result.status.Signal() != unix.SIGKILL {
			t.Fatalf("child status = %v, want SIGKILL", result.status)
		}
		if result.err == nil || !strings.Contains(result.err.Error(), "recover child terminal foreground") {
			t.Fatalf("lost recovery error: %v", result.err)
		}
	case <-time.After(time.Second):
		t.Fatal("supervisor did not reap the exited child")
	}
}
