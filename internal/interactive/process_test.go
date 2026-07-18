package interactive

import (
	"os"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

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
