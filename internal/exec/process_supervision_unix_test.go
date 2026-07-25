//go:build darwin || linux

package exec

import (
	"context"
	"errors"
	"os"
	stdexec "os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestConfigureAgentCommandCreatesInvocationProcessGroup(t *testing.T) {
	command := stdexec.Command("sh", "-c", "exit 0")
	ConfigureAgentCommand(command, AgentProcessSupervision{ProcessGroup: true})

	if command.SysProcAttr == nil || !command.SysProcAttr.Setpgid {
		t.Fatalf("agent command SysProcAttr = %#v, want invocation process group", command.SysProcAttr)
	}
	if command.Cancel == nil {
		t.Fatal("agent command cancellation was not configured")
	}
}

func TestConfigureAgentCommandCancellationKillsTermIgnoringDescendant(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	workdir := t.TempDir()
	command := stdexec.CommandContext(ctx, "sh", "-c", `trap '' TERM; sh -c 'trap "" TERM; echo $$ > child.pid; while :; do sleep 1; done' & wait`)
	command.Dir = workdir
	ConfigureAgentCommand(command, AgentProcessSupervision{ProcessGroup: true, TerminationGrace: 50 * time.Millisecond})
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL) }()

	childPID := waitForChildPID(t, filepath.Join(workdir, "child.pid"))
	cancel()
	if err := command.Wait(); err == nil {
		t.Fatal("canceled command returned no error")
	}

	deadline := time.Now().Add(time.Second)
	for {
		err := syscall.Kill(childPID, 0)
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("term-ignoring descendant %d survived process-group cancellation: %v", childPID, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitForChildPID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		data, err := os.ReadFile(path)
		if err == nil {
			pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
			if parseErr != nil {
				t.Fatalf("parse child PID: %v", parseErr)
			}
			return pid
		}
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for child PID")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
