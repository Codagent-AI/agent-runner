package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const (
	agentCallMCPServerName       = "agent-runner"
	agentCallMCPToolName         = "call_agent"
	agentCallTimeoutSeconds      = 30 * 24 * 60 * 60
	agentCallTimeoutMilliseconds = int64(2_147_483_647)
)

var agentCallControlEnvironmentVariables = []string{
	"AGENT_RUNNER_CONTROL_SOCKET",
	"AGENT_RUNNER_RUN_ID",
	"AGENT_RUNNER_STEP_ID",
	"AGENT_RUNNER_ATTEMPT_ID",
	"AGENT_RUNNER_CONTROL_TOKEN",
}

// validatedAgentCall returns the trusted process-local server command, if one
// was supplied. Adapters deliberately do not infer eligibility from prompts.
func validatedAgentCall(input *BuildArgsInput) (*MCPServerCommand, error) {
	if input == nil || input.RunnerIntegration == nil {
		return nil, nil
	}
	integration := input.RunnerIntegration
	if !integration.Valid() {
		return nil, fmt.Errorf("invalid Runner agent-call integration descriptor")
	}
	command := integration.AgentCall
	info, err := os.Stat(command.Executable)
	if err != nil {
		return nil, fmt.Errorf("inspect Runner agent-call executable %s: %w", command.Executable, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("runner agent-call executable %s is not a regular file", command.Executable)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		return nil, fmt.Errorf("runner agent-call executable %s is not executable", command.Executable)
	}
	return command, nil
}

func standardAgentCallMCPConfig(command MCPServerCommand, includeToolFilter bool, timeoutMilliseconds int64, inheritControlEnvironment bool) ([]byte, error) {
	server := map[string]any{
		"command": command.Executable,
		"args":    command.Args,
	}
	if inheritControlEnvironment {
		environment := make(map[string]string, len(agentCallControlEnvironmentVariables))
		for _, name := range agentCallControlEnvironmentVariables {
			environment[name] = "${" + name + "}"
		}
		server["env"] = environment
	}
	if includeToolFilter {
		server["tools"] = []string{agentCallMCPToolName}
	}
	if timeoutMilliseconds > 0 {
		server["timeout"] = timeoutMilliseconds
	}
	return json.Marshal(map[string]any{
		"mcpServers": map[string]any{agentCallMCPServerName: server},
	})
}

func prepareAgentCallPlugin(command MCPServerCommand) (string, error) {
	config, err := standardAgentCallMCPConfig(command, false, 0, true)
	if err != nil {
		return "", fmt.Errorf("encode agent-call MCP plugin: %w", err)
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("locate user cache: %w", err)
	}
	digest := sha256.Sum256(append([]byte("agent-call-plugin-v1\x00"), config...))
	pluginDir := filepath.Join(cacheDir, "agent-runner", "agent-call-plugins", hex.EncodeToString(digest[:6]))
	if err := os.MkdirAll(filepath.Join(pluginDir, ".claude-plugin"), 0o700); err != nil {
		return "", fmt.Errorf("create agent-call MCP plugin: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(pluginDir, ".cursor-plugin"), 0o700); err != nil {
		return "", fmt.Errorf("create agent-call MCP plugin: %w", err)
	}
	manifest, err := json.MarshalIndent(map[string]any{
		"name":        "agent-runner-call",
		"version":     "1.0.0",
		"description": "Agent Runner process-local agent-call integration",
	}, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode agent-call MCP plugin manifest: %w", err)
	}
	manifest = append(manifest, '\n')
	files := map[string][]byte{
		".mcp.json":   append(config, '\n'),
		"plugin.json": manifest,
		filepath.Join(".claude-plugin", "plugin.json"): manifest,
		filepath.Join(".cursor-plugin", "plugin.json"): manifest,
	}
	for relativePath, content := range files {
		path := filepath.Join(pluginDir, relativePath)
		if err := writeFileAtomic(path, content); err != nil {
			return "", fmt.Errorf("write agent-call MCP plugin: %w", err)
		}
	}
	return pluginDir, nil
}
