package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

const completionPluginName = "agent-runner"

var codexSharedStateDirectories = []string{
	"archived_sessions",
	"memories",
	"sessions",
	"shell_snapshots",
}

var codexAgentCallControlEnvironment = []string{
	"AGENT_RUNNER_CONTROL_SOCKET",
	"AGENT_RUNNER_RUN_ID",
	"AGENT_RUNNER_STEP_ID",
	"AGENT_RUNNER_ATTEMPT_ID",
	"AGENT_RUNNER_CONTROL_TOKEN",
}

func prepareNextCommandPlugin(command CompletionCommand) (string, error) {
	if !command.Valid() {
		return "", fmt.Errorf("invalid completion command")
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("locate user cache: %w", err)
	}
	digest := sha256.Sum256([]byte(command.ShellCommand()))
	pluginDir := filepath.Join(cacheDir, "agent-runner", "completion-plugins", "next-"+hex.EncodeToString(digest[:6]))
	if err := writeNextCommandPlugin(pluginDir, command, "1.0.0"); err != nil {
		return "", err
	}
	return pluginDir, nil
}

func writeNextCommandPlugin(pluginDir string, command CompletionCommand, version string) error {
	manifest, err := json.MarshalIndent(map[string]any{
		"name":        completionPluginName,
		"version":     version,
		"description": "Agent Runner control-channel completion",
		"commands":    "./commands/",
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode completion command plugin: %w", err)
	}
	manifest = append(manifest, '\n')
	next := `---
description: Complete the current Agent Runner workflow step
---

Run this exact command now, then finish the response:

` + "`" + command.ShellCommand() + "`" + `
`
	files := map[string]string{
		"plugin.json": string(manifest),
		filepath.Join(".claude-plugin", "plugin.json"): string(manifest),
		filepath.Join(".cursor-plugin", "plugin.json"): string(manifest),
		filepath.Join(".codex-plugin", "plugin.json"):  string(manifest),
		filepath.Join("commands", "next.md"):           next,
	}
	for relativePath, content := range files {
		path := filepath.Join(pluginDir, relativePath)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return fmt.Errorf("create completion command plugin: %w", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			return fmt.Errorf("write completion command plugin: %w", err)
		}
	}
	return nil
}

func prepareCodexCompletionHome(command CompletionCommand, runID string) (string, error) {
	return prepareCodexRunnerHome(&command, nil, ContextInteractive, runID)
}

func prepareCodexRunnerHome(completion *CompletionCommand, integration *RunnerIntegration, context InvocationContext, runID string) (string, error) {
	if completion != nil && !completion.Valid() {
		return "", fmt.Errorf("invalid completion command")
	}
	if integration != nil && !integration.Valid() {
		return "", fmt.Errorf("invalid Runner agent-call integration descriptor")
	}
	if completion == nil && integration == nil {
		return "", fmt.Errorf("missing Runner integration")
	}
	if runID == "" {
		return "", fmt.Errorf("missing run identity")
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("locate user cache: %w", err)
	}
	sourceHome := os.Getenv("CODEX_HOME")
	if sourceHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("locate Codex home: %w", err)
		}
		sourceHome = filepath.Join(home, ".codex")
	}
	config, err := os.ReadFile(filepath.Join(sourceHome, "config.toml")) // #nosec G703,G304 -- CODEX_HOME is the user's documented Codex root and the joined name is fixed
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("read Codex config: %w", err)
	}
	completionCommand := ""
	if completion != nil {
		completionCommand = completion.ShellCommand()
	}
	if integration != nil {
		conflict, err := codexConfigHasAgentCallConflict(config)
		if err != nil {
			return "", err
		}
		if conflict {
			return "", fmt.Errorf("codex config already defines %s; cannot safely install the Runner-owned server", agentCallMCPServerName)
		}
		config = appendCodexAgentCallConfig(config, *integration.AgentCall, context.IsAutonomous())
	}
	digest := sha256.Sum256([]byte("codex-v6\x00" + runID + "\x00" + sourceHome + "\x00" + string(config) + "\x00" + completionCommand + "\x00" + string(context)))
	hash := hex.EncodeToString(digest[:6])

	privateHome := filepath.Join(cacheDir, "agent-runner", "codex-homes", hash)
	if err := os.MkdirAll(privateHome, 0o700); err != nil { // #nosec G703 -- path is the local cache root plus fixed components and a hex digest
		return "", fmt.Errorf("create private Codex home: %w", err)
	}
	if err := ensureCodexSharedStateDirectories(sourceHome); err != nil {
		return "", err
	}
	if err := linkCodexHomeEntries(sourceHome, privateHome); err != nil {
		return "", err
	}
	config = inheritCodexHookTrust(config, sourceHome, privateHome)
	privateConfigPath := filepath.Join(privateHome, "config.toml")
	configFile, err := os.OpenFile(privateConfigPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) // #nosec G703,G304 -- privateHome is confined to the cache digest directory and the filename is fixed
	if err == nil {
		if _, err := configFile.Write(config); err != nil {
			_ = configFile.Close()
			return "", fmt.Errorf("write private Codex config: %w", err)
		}
		if err := configFile.Close(); err != nil {
			return "", fmt.Errorf("close private Codex config: %w", err)
		}
	} else if !os.IsExist(err) {
		return "", fmt.Errorf("create private Codex config: %w", err)
	}

	privateSkills := filepath.Join(privateHome, "skills")
	if err := linkCodexSkillEntries(filepath.Join(sourceHome, "skills"), privateSkills); err != nil {
		return "", err
	}
	if completion == nil {
		return privateHome, nil
	}
	next := `---
name: agent-runner-next
description: Complete the current Agent Runner workflow step
---

Run this exact command now, then finish the response:

` + "`" + completion.ShellCommand() + "`" + `
`
	nextSkillDir := filepath.Join(privateSkills, "agent-runner-next")
	if err := os.MkdirAll(nextSkillDir, 0o700); err != nil { // #nosec G703 -- fixed skill path beneath the cache digest directory
		return "", fmt.Errorf("create Codex completion skill: %w", err)
	}
	if err := os.WriteFile(filepath.Join(nextSkillDir, "SKILL.md"), []byte(next), 0o600); err != nil { // #nosec G703 -- fixed filename beneath the cache digest directory
		return "", fmt.Errorf("write Codex completion skill: %w", err)
	}
	return privateHome, nil
}

func codexConfigHasAgentCallConflict(config []byte) (bool, error) {
	if strings.TrimSpace(string(config)) == "" {
		return false, nil
	}
	var decoded map[string]any
	if err := toml.Unmarshal(config, &decoded); err != nil {
		return false, fmt.Errorf("parse Codex config before installing Runner integration: %w", err)
	}
	mcpServers, ok := decoded["mcp_servers"].(map[string]any)
	if !ok {
		return false, nil
	}
	_, conflict := mcpServers[agentCallMCPServerName]
	return conflict, nil
}

func appendCodexAgentCallConfig(config []byte, command MCPServerCommand, autonomous bool) []byte {
	var result strings.Builder
	result.Write(config)
	if len(config) > 0 && config[len(config)-1] != '\n' {
		result.WriteByte('\n')
	}
	result.WriteString("\n[mcp_servers.agent-runner]\n")
	result.WriteString("command = ")
	result.WriteString(strconv.Quote(command.Executable))
	result.WriteByte('\n')
	args, _ := json.Marshal(command.Args)
	result.WriteString("args = ")
	result.Write(args)
	result.WriteByte('\n')
	result.WriteString(`enabled_tools = ["call_agent"]`)
	result.WriteByte('\n')
	envVars, _ := json.Marshal(codexAgentCallControlEnvironment)
	result.WriteString("env_vars = ")
	result.Write(envVars)
	result.WriteByte('\n')
	fmt.Fprintf(&result, "tool_timeout_sec = %d\n", agentCallTimeoutSeconds)
	if autonomous {
		result.WriteString("\n[mcp_servers.agent-runner.tools.call_agent]\n")
		result.WriteString(`approval_mode = "approve"`)
		result.WriteByte('\n')
	}
	return []byte(result.String())
}

func inheritCodexHookTrust(config []byte, sourceHome, privateHome string) []byte {
	sourcePrefix := `[hooks.state."` + filepath.Join(sourceHome, "hooks.json") + `:`
	privatePrefix := `[hooks.state."` + filepath.Join(privateHome, "hooks.json") + `:`
	lines := strings.SplitAfter(string(config), "\n")
	var inherited []string
	for i := 0; i < len(lines); {
		if !strings.HasPrefix(lines[i], sourcePrefix) {
			i++
			continue
		}
		end := i + 1
		for end < len(lines) && !strings.HasPrefix(lines[end], "[") {
			end++
		}
		section := strings.Join(lines[i:end], "")
		privateSection := strings.Replace(section, sourcePrefix, privatePrefix, 1)
		privateHeader := strings.TrimSuffix(strings.SplitN(privateSection, "\n", 2)[0], "\r")
		if !strings.Contains(string(config), privateHeader) {
			inherited = append(inherited, privateSection)
		}
		i = end
	}
	if len(inherited) == 0 {
		return config
	}
	var result strings.Builder
	result.Write(config)
	if len(config) > 0 && config[len(config)-1] != '\n' {
		result.WriteByte('\n')
	}
	for _, section := range inherited {
		result.WriteByte('\n')
		result.WriteString(section)
	}
	return []byte(result.String())
}

func ensureCodexSharedStateDirectories(sourceHome string) error {
	for _, name := range codexSharedStateDirectories {
		if err := os.MkdirAll(filepath.Join(sourceHome, name), 0o700); err != nil { // #nosec G703 -- CODEX_HOME is the selected root and name comes from the fixed shared-state allowlist
			return fmt.Errorf("create shared Codex state directory %s: %w", name, err)
		}
	}
	return nil
}

func linkCodexHomeEntries(sourceHome, privateHome string) error {
	entries, err := os.ReadDir(sourceHome)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read Codex home: %w", err)
	}
	for _, entry := range entries {
		if entry.Name() == "config.toml" || entry.Name() == "skills" {
			continue
		}
		if err := ensureSymlink(filepath.Join(sourceHome, entry.Name()), filepath.Join(privateHome, entry.Name())); err != nil {
			return fmt.Errorf("link Codex home entry %s: %w", entry.Name(), err)
		}
	}
	return nil
}

func linkCodexSkillEntries(sourceSkills, privateSkills string) error {
	if err := os.MkdirAll(privateSkills, 0o700); err != nil { // #nosec G703 -- fixed skills path beneath the cache digest directory
		return fmt.Errorf("create private Codex skills: %w", err)
	}
	entries, err := os.ReadDir(sourceSkills)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read Codex skills: %w", err)
	}
	for _, entry := range entries {
		if entry.Name() == "agent-runner-next" {
			continue
		}
		if err := ensureSymlink(filepath.Join(sourceSkills, entry.Name()), filepath.Join(privateSkills, entry.Name())); err != nil {
			return fmt.Errorf("link Codex skill %s: %w", entry.Name(), err)
		}
	}
	return nil
}

func ensureSymlink(target, link string) error {
	if _, err := os.Lstat(link); err == nil { // #nosec G703 -- callers construct link from a confined root plus a single os.ReadDir entry name
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.Symlink(target, link)
}
