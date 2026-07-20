package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	testAgentCallExecutable       = "agent-runner-test-bin"
	expectedAgentCallServer       = "agent-runner"
	expectedAgentCallTool         = "call_agent"
	expectedAgentCallTimeoutSec   = int64(2_592_000)
	expectedAgentCallTimeoutMilli = int64(2_147_483_647)
)

type agentCallPreparedInvocation struct {
	args []string
	env  []string
}

func TestRegisteredAdaptersProvisionAgentCallProcessLocally(t *testing.T) {
	for _, adapterName := range []string{"claude", "codex", "copilot", "cursor", "opencode"} {
		adapterName := adapterName
		for _, invocationContext := range []InvocationContext{ContextInteractive, ContextAutonomousHeadless} {
			invocationContext := invocationContext
			t.Run(adapterName+"/"+string(invocationContext), func(t *testing.T) {
				adapter, input := agentCallTestInput(t, adapterName, invocationContext)
				prepared, err := prepareAgentCallTestInvocation(adapter, input)
				if err != nil {
					t.Fatalf("prepare invocation: %v", err)
				}

				registration := agentCallRegistration(t, adapterName, prepared)
				if registration.command != input.RunnerIntegration.AgentCall.Executable {
					t.Fatalf("MCP command = %q, want %q", registration.command, input.RunnerIntegration.AgentCall.Executable)
				}
				if got := strings.Join(registration.args, " "); got != "internal call-agent-mcp" {
					t.Fatalf("MCP args = %q, want fixed internal call-agent-mcp command", got)
				}
				if registration.serverName != expectedAgentCallServer {
					t.Fatalf("MCP server name = %q, want %q", registration.serverName, expectedAgentCallServer)
				}
				if len(registration.tools) != 1 || registration.tools[0] != expectedAgentCallTool {
					t.Fatalf("MCP tools = %v, want only %q", registration.tools, expectedAgentCallTool)
				}

				assertAgentCallApproval(t, adapterName, invocationContext, prepared)
				assertAgentCallTimeout(t, adapterName, &registration)
			})
		}
	}
}

func TestAdaptersDoNotInferAgentCallEligibilityFromPrompt(t *testing.T) {
	for _, adapterName := range []string{"claude", "codex", "copilot", "cursor", "opencode"} {
		adapterName := adapterName
		for _, prompt := range []string{"ordinary prompt", "called child mentions call_agent"} {
			prompt := prompt
			t.Run(adapterName+"/"+prompt, func(t *testing.T) {
				adapter, input := agentCallTestInput(t, adapterName, ContextAutonomousHeadless)
				input.Prompt = prompt
				input.RunnerIntegration = nil
				prepared, err := prepareAgentCallTestInvocation(adapter, input)
				if err != nil {
					t.Fatalf("prepare invocation: %v", err)
				}
				assertNoAgentCallIntegration(t, adapterName, prepared)
			})
		}
	}
}

func TestAdaptersRejectMalformedOrUnusableAgentCallDescriptorsBeforeSpawn(t *testing.T) {
	for _, adapterName := range []string{"claude", "codex", "copilot", "cursor", "opencode"} {
		adapterName := adapterName
		for _, descriptor := range []MCPServerCommand{
			{Executable: "agent-runner", Args: []string{"internal", "call-agent-mcp"}},
			{Executable: "/missing/agent-runner", Args: []string{"internal", "call-agent-mcp"}},
			{Executable: "/opt/agent-runner", Args: []string{"wrong"}},
		} {
			descriptor := descriptor
			t.Run(adapterName+"/"+strings.ReplaceAll(descriptor.Executable, "/", "_"), func(t *testing.T) {
				adapter, input := agentCallTestInput(t, adapterName, ContextAutonomousHeadless)
				input.RunnerIntegration = &RunnerIntegration{AgentCall: &descriptor}
				if _, err := prepareAgentCallTestInvocation(adapter, input); err == nil {
					t.Fatalf("prepare invocation accepted descriptor %#v", descriptor)
				}
			})
		}
	}
}

func TestAgentCallProvisioningDoesNotModifyUserOrProjectConfig(t *testing.T) {
	for _, adapterName := range []string{"claude", "codex", "copilot", "cursor", "opencode"} {
		adapterName := adapterName
		for _, fail := range []bool{false, true} {
			name := "success"
			if fail {
				name = "failure"
			}
			t.Run(adapterName+"/"+name, func(t *testing.T) {
				adapter, input := agentCallTestInput(t, adapterName, ContextAutonomousHeadless)
				configPaths := adapterConfigSentinels(t, adapterName, input.Workdir)
				before := make(map[string]configSentinel, len(configPaths))
				for _, path := range configPaths {
					before[path] = readConfigSentinel(t, path)
				}
				if fail {
					input.RunnerIntegration.AgentCall.Executable = "/missing/agent-runner"
				}

				_, err := prepareAgentCallTestInvocation(adapter, input)
				if fail && err == nil {
					t.Fatal("preparation succeeded with a missing executable")
				}
				if !fail && err != nil {
					t.Fatalf("preparation failed: %v", err)
				}
				for _, path := range configPaths {
					assertConfigSentinelUnchanged(t, path, before[path])
				}
			})
		}
	}
}

func TestAgentCallGenerationFailuresPreventSpawnAndPreservePersistentConfig(t *testing.T) {
	for _, adapterName := range []string{"claude", "cursor", "codex"} {
		t.Run(adapterName, func(t *testing.T) {
			adapter, input := agentCallTestInput(t, adapterName, ContextAutonomousHeadless)
			configPaths := adapterConfigSentinels(t, adapterName, input.Workdir)
			before := make(map[string]configSentinel, len(configPaths))
			for _, path := range configPaths {
				before[path] = readConfigSentinel(t, path)
			}

			cacheDir, err := os.UserCacheDir()
			if err != nil {
				t.Fatalf("locate test cache: %v", err)
			}
			if err := os.MkdirAll(filepath.Dir(cacheDir), 0o700); err != nil {
				t.Fatalf("create test cache parent: %v", err)
			}
			if err := os.WriteFile(cacheDir, []byte("blocks cache directory creation"), 0o600); err != nil {
				t.Fatalf("block test cache directory: %v", err)
			}

			if _, err := prepareAgentCallTestInvocation(adapter, input); err == nil || !strings.Contains(err.Error(), cacheDir) {
				t.Fatalf("prepare invocation error = %v, want filesystem generation cause containing %q", err, cacheDir)
			}
			for _, path := range configPaths {
				assertConfigSentinelUnchanged(t, path, before[path])
			}
		})
	}
}

func TestOpenCodeAgentCallBuilderIsModeNeutral(t *testing.T) {
	adapter := &OpenCodeAdapter{}
	for _, invocationContext := range []InvocationContext{ContextInteractive, ContextAutonomousHeadless} {
		_, input := agentCallTestInput(t, "opencode", invocationContext)
		prepared, err := prepareAgentCallTestInvocation(adapter, input)
		if err != nil {
			t.Fatalf("prepare %s OpenCode integration: %v", invocationContext, err)
		}
		registration := agentCallRegistration(t, "opencode", prepared)
		if registration.serverName != expectedAgentCallServer {
			t.Fatalf("%s OpenCode server = %q", invocationContext, registration.serverName)
		}
	}
}

func TestOpenCodeAgentCallUsesNativeSpawnEnvironment(t *testing.T) {
	adapter, input := agentCallTestInput(t, "opencode", ContextAutonomousHeadless)
	prepared, err := prepareAgentCallTestInvocation(adapter, input)
	if err != nil {
		t.Fatalf("prepare invocation: %v", err)
	}
	if len(prepared.args) == 0 || prepared.args[0] != "opencode" {
		t.Fatalf("OpenCode args = %v, want native opencode executable without an env wrapper", prepared.args)
	}
	if got := envValue(t, prepared.env, "OPENCODE_CONFIG_CONTENT"); !strings.Contains(got, `"agent-runner"`) {
		t.Fatalf("OPENCODE_CONFIG_CONTENT = %q, want process-local agent-runner MCP config", got)
	}
	if got := envValue(t, prepared.env, "OPENCODE_DISABLE_AUTOUPDATE"); got != "1" {
		t.Fatalf("OPENCODE_DISABLE_AUTOUPDATE = %q, want 1", got)
	}
	for _, name := range []string{"OPENCODE_CONFIG_CONTENT", "OPENCODE_PERMISSION", "OPENCODE_DISABLE_AUTOUPDATE"} {
		if !containsString(DropSpawnEnvVars(adapter), name) {
			t.Fatalf("OpenCode nested-child environment does not drop %s", name)
		}
	}
}

func TestCodexAgentCallRejectsEquivalentReservedServerSpellings(t *testing.T) {
	for _, table := range []string{
		"[mcp_servers.'agent-runner']\ncommand = 'other'\n",
		"[mcp_servers . agent-runner]\ncommand = 'other'\n",
	} {
		t.Run(strings.ReplaceAll(strings.TrimSpace(table), " ", "_"), func(t *testing.T) {
			adapter, input := agentCallTestInput(t, "codex", ContextAutonomousHeadless)
			codexHome := os.Getenv("CODEX_HOME")
			if err := os.WriteFile(filepath.Join(codexHome, "config.toml"), []byte(table), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := prepareAgentCallTestInvocation(adapter, input); err == nil || !strings.Contains(err.Error(), "already defines agent-runner") {
				t.Fatalf("prepare invocation error = %v, want reserved-server conflict", err)
			}
		})
	}
}

func TestCursorAutonomousAgentCallRejectsProjectDenyConflict(t *testing.T) {
	adapter, input := agentCallTestInput(t, "cursor", ContextAutonomousHeadless)
	projectConfig := filepath.Join(input.Workdir, ".cursor", "cli.json")
	if err := os.MkdirAll(filepath.Dir(projectConfig), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(projectConfig, []byte(`{"permissions":{"deny":["Mcp(agent-runner:call_agent)"]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareAgentCallTestInvocation(adapter, input); err == nil || !strings.Contains(err.Error(), "project-level Cursor deny rule") {
		t.Fatalf("prepare invocation error = %v, want project-level MCP deny conflict", err)
	}
}

type agentCallRegistrationView struct {
	serverName string
	command    string
	args       []string
	tools      []string
	timeout    int64
}

func agentCallTestInput(t *testing.T, adapterName string, invocationContext InvocationContext) (Adapter, *BuildArgsInput) {
	t.Helper()
	home := t.TempDir()
	workdir := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	runner := filepath.Join(t.TempDir(), testAgentCallExecutable)
	if err := os.WriteFile(runner, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatalf("write test agent-runner executable: %v", err)
	}
	if adapterName == "codex" {
		codexHome := filepath.Join(home, ".codex")
		if err := os.MkdirAll(codexHome, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(codexHome, "config.toml"), []byte("model = \"test\"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("CODEX_HOME", codexHome)
	}
	if adapterName == "cursor" {
		cursorHome := filepath.Join(home, ".cursor")
		if err := os.MkdirAll(cursorHome, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cursorHome, "cli-config.json"), []byte(`{"version":1,"permissions":{"allow":[],"deny":[]}}`), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("CURSOR_CONFIG_DIR", cursorHome)
	}
	adapter, err := Get(adapterName)
	if err != nil {
		t.Fatalf("get %s adapter: %v", adapterName, err)
	}
	return adapter, &BuildArgsInput{
		Prompt:  "use call_agent",
		Context: invocationContext,
		RunID:   "agent-call-test-run",
		Workdir: workdir,
		RunnerIntegration: &RunnerIntegration{AgentCall: &MCPServerCommand{
			Executable: runner,
			Args:       []string{"internal", "call-agent-mcp"},
		}},
	}
}

func prepareAgentCallTestInvocation(adapter Adapter, input *BuildArgsInput) (agentCallPreparedInvocation, error) {
	args, err := BuildInvocationArgs(adapter, input)
	if err != nil {
		return agentCallPreparedInvocation{}, err
	}
	env, err := SpawnEnvForInvocation(adapter, input)
	if err != nil {
		return agentCallPreparedInvocation{}, err
	}
	return agentCallPreparedInvocation{args: args, env: env}, nil
}

func agentCallRegistration(t *testing.T, adapterName string, prepared agentCallPreparedInvocation) agentCallRegistrationView {
	t.Helper()
	switch adapterName {
	case "claude", "cursor":
		for i, arg := range prepared.args {
			if arg != "--plugin-dir" || i+1 >= len(prepared.args) {
				continue
			}
			path := filepath.Join(prepared.args[i+1], ".mcp.json")
			if _, err := os.Stat(path); err == nil {
				return decodeStandardMCPConfig(t, path)
			}
		}
		t.Fatalf("%s args have no process-local MCP plugin: %v", adapterName, prepared.args)
	case "codex":
		home := envValue(t, prepared.env, "CODEX_HOME")
		data, err := os.ReadFile(filepath.Join(home, "config.toml"))
		if err != nil {
			t.Fatalf("read private Codex config: %v", err)
		}
		text := string(data)
		return agentCallRegistrationView{
			serverName: expectedAgentCallServer,
			command:    tomlStringValue(t, text, "command"),
			args:       []string{"internal", "call-agent-mcp"},
			tools:      []string{expectedAgentCallTool},
			timeout:    tomlIntValue(t, text, "tool_timeout_sec"),
		}
	case "copilot":
		for _, arg := range prepared.args {
			value, ok := strings.CutPrefix(arg, "--additional-mcp-config=")
			if !ok {
				continue
			}
			return decodeStandardMCPJSON(t, []byte(value))
		}
		t.Fatalf("Copilot args have no additional MCP config: %v", prepared.args)
	case "opencode":
		value := envValue(t, prepared.env, "OPENCODE_CONFIG_CONTENT")
		var config struct {
			MCP map[string]struct {
				Command []string `json:"command"`
			} `json:"mcp"`
		}
		if err := json.Unmarshal([]byte(value), &config); err != nil {
			t.Fatalf("decode OpenCode config: %v", err)
		}
		server, ok := config.MCP[expectedAgentCallServer]
		if !ok || len(server.Command) == 0 {
			t.Fatalf("OpenCode MCP config = %#v", config.MCP)
		}
		return agentCallRegistrationView{serverName: expectedAgentCallServer, command: server.Command[0], args: server.Command[1:], tools: []string{expectedAgentCallTool}}
	}
	t.Fatalf("unsupported adapter %q", adapterName)
	return agentCallRegistrationView{}
}

func decodeStandardMCPConfig(t *testing.T, path string) agentCallRegistrationView {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read MCP config: %v", err)
	}
	return decodeStandardMCPJSON(t, data)
}

func decodeStandardMCPJSON(t *testing.T, data []byte) agentCallRegistrationView {
	t.Helper()
	var config struct {
		MCPServers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
			Tools   []string `json:"tools"`
			Timeout int64    `json:"timeout"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("decode MCP config: %v", err)
	}
	if len(config.MCPServers) != 1 {
		t.Fatalf("MCP servers = %#v, want only agent-runner", config.MCPServers)
	}
	server, ok := config.MCPServers[expectedAgentCallServer]
	if !ok {
		t.Fatalf("MCP config has no %q server: %#v", expectedAgentCallServer, config.MCPServers)
	}
	tools := server.Tools
	if len(tools) == 0 {
		tools = []string{expectedAgentCallTool}
	}
	return agentCallRegistrationView{serverName: expectedAgentCallServer, command: server.Command, args: server.Args, tools: tools, timeout: server.Timeout}
}

func assertAgentCallApproval(t *testing.T, adapterName string, invocationContext InvocationContext, prepared agentCallPreparedInvocation) {
	t.Helper()
	wantApproval := invocationContext.IsAutonomous()
	found := false
	switch adapterName {
	case "claude":
		found = hasFlagValue(prepared.args, "--allowedTools", "mcp__agent-runner__call_agent")
	case "codex":
		home := envValue(t, prepared.env, "CODEX_HOME")
		data, err := os.ReadFile(filepath.Join(home, "config.toml"))
		if err != nil {
			t.Fatal(err)
		}
		found = strings.Contains(string(data), "approval_mode = \"approve\"")
	case "copilot":
		found = containsString(prepared.args, "--allow-tool=agent-runner(call_agent)")
	case "cursor":
		configDir := envValue(t, prepared.env, "CURSOR_CONFIG_DIR")
		data, err := os.ReadFile(filepath.Join(configDir, "cli-config.json"))
		if err != nil {
			t.Fatal(err)
		}
		found = strings.Contains(string(data), `"Mcp(agent-runner:call_agent)"`)
	case "opencode":
		config := envValue(t, prepared.env, "OPENCODE_CONFIG_CONTENT")
		found = strings.Contains(config, `"agent-runner_call_agent":"allow"`)
	}
	if found != wantApproval {
		t.Fatalf("%s %s narrow approval present = %v, want %v; args=%v env=%v", adapterName, invocationContext, found, wantApproval, prepared.args, prepared.env)
	}
}

func assertAgentCallTimeout(t *testing.T, adapterName string, registration *agentCallRegistrationView) {
	t.Helper()
	switch adapterName {
	case "codex":
		if registration.timeout != expectedAgentCallTimeoutSec {
			t.Fatalf("Codex tool timeout = %d, want %d", registration.timeout, expectedAgentCallTimeoutSec)
		}
	case "copilot":
		if registration.timeout != expectedAgentCallTimeoutMilli {
			t.Fatalf("Copilot tool timeout = %d, want %d", registration.timeout, expectedAgentCallTimeoutMilli)
		}
	default:
		if registration.timeout != 0 {
			t.Fatalf("%s unexpectedly sets a Runner tool timeout: %d", adapterName, registration.timeout)
		}
	}
}

func assertNoAgentCallIntegration(t *testing.T, adapterName string, prepared agentCallPreparedInvocation) {
	t.Helper()
	joined := strings.Join(append(append([]string{}, prepared.args...), prepared.env...), "\n")
	for _, forbidden := range []string{"call-agent-mcp", "agent-runner(call_agent)", "agent-runner_call_agent", "mcp__agent-runner__call_agent"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("%s emitted agent-call integration %q: args=%v env=%v", adapterName, forbidden, prepared.args, prepared.env)
		}
	}
	for i, arg := range prepared.args {
		if arg == "--plugin-dir" && i+1 < len(prepared.args) {
			if _, err := os.Stat(filepath.Join(prepared.args[i+1], ".mcp.json")); err == nil {
				t.Fatalf("%s emitted an MCP plugin without a Runner descriptor", adapterName)
			}
		}
	}
}

func envValue(t *testing.T, env []string, key string) string {
	t.Helper()
	prefix := key + "="
	for _, entry := range env {
		if value, ok := strings.CutPrefix(entry, prefix); ok {
			return value
		}
	}
	t.Fatalf("environment has no %s: %v", key, env)
	return ""
}

func tomlStringValue(t *testing.T, text, key string) string {
	t.Helper()
	prefix := key + " = \""
	for _, line := range strings.Split(text, "\n") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), prefix); ok {
			return strings.TrimSuffix(value, "\"")
		}
	}
	t.Fatalf("TOML has no %s: %s", key, text)
	return ""
}

func tomlIntValue(t *testing.T, text, key string) int64 {
	t.Helper()
	prefix := key + " = "
	for _, line := range strings.Split(text, "\n") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), prefix); ok {
			result, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				t.Fatalf("parse TOML %s value %q: %v", key, value, err)
			}
			return result
		}
	}
	t.Fatalf("TOML has no %s: %s", key, text)
	return 0
}

type configSentinel struct {
	data  string
	mode  os.FileMode
	mtime time.Time
}

func writeConfigSentinel(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
		t.Fatal(err)
	}
	mtime := time.Unix(1_700_000_000, 0)
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

func adapterConfigSentinels(t *testing.T, adapterName, workdir string) []string {
	t.Helper()
	home := os.Getenv("HOME")
	globalPath := map[string]string{
		"claude":   filepath.Join(home, ".claude.json"),
		"codex":    filepath.Join(home, ".codex", "config.toml"),
		"copilot":  filepath.Join(home, ".copilot", "mcp-config.json"),
		"cursor":   filepath.Join(home, ".cursor", "cli-config.json"),
		"opencode": filepath.Join(home, ".config", "opencode", "opencode.json"),
	}[adapterName]
	globalContent := `{"sentinel":"user config"}`
	if adapterName == "codex" {
		globalContent = `model = "test"`
	}
	if adapterName == "cursor" {
		globalContent = `{"version":1,"permissions":{"allow":[],"deny":[]},"sentinel":"user config"}`
	}
	projectPath := filepath.Join(workdir, ".mcp.json")
	if adapterName == "cursor" {
		projectPath = filepath.Join(workdir, ".cursor", "cli.json")
	}
	writeConfigSentinel(t, globalPath, globalContent)
	writeConfigSentinel(t, projectPath, `{"sentinel":"project config"}`)
	return []string{globalPath, projectPath}
}

func readConfigSentinel(t *testing.T, path string) configSentinel {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return configSentinel{data: string(data), mode: info.Mode().Perm(), mtime: info.ModTime()}
}

func assertConfigSentinelUnchanged(t *testing.T, path string, before configSentinel) {
	t.Helper()
	after := readConfigSentinel(t, path)
	if after != before {
		t.Fatalf("config %s changed: before=%+v after=%+v", path, before, after)
	}
}
