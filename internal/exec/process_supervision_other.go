//go:build !darwin && !linux

package exec

import stdexec "os/exec"

// ConfigureAgentCommand is a no-op on platforms without Unix process groups.
func ConfigureAgentCommand(_ *stdexec.Cmd, _ AgentProcessSupervision) {}
