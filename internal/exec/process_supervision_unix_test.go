//go:build darwin || linux

package exec

import (
	stdexec "os/exec"
	"testing"
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
