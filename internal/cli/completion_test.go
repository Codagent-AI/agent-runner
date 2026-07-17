package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompletionCommandValidation(t *testing.T) {
	tests := []struct {
		name    string
		command CompletionCommand
		want    bool
	}{
		{
			name: "absolute fixed completion command",
			command: CompletionCommand{
				Executable: "/opt/codagent/bin/agent-runner",
				Args:       []string{"step", "complete"},
			},
			want: true,
		},
		{
			name:    "relative executable",
			command: CompletionCommand{Executable: "agent-runner", Args: []string{"step", "complete"}},
			want:    false,
		},
		{
			name:    "additional argument",
			command: CompletionCommand{Executable: "/opt/codagent/bin/agent-runner", Args: []string{"step", "complete", "extra"}},
			want:    false,
		},
		{
			name:    "different subcommand",
			command: CompletionCommand{Executable: "/opt/codagent/bin/agent-runner", Args: []string{"internal", "turn-committed"}},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.command.Valid(); got != tt.want {
				t.Fatalf("Valid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCompletionCommandShellCommandQuotesExecutablePath(t *testing.T) {
	command := CompletionCommand{
		Executable: "/Applications/Agent Runner/bin/agent-runner",
		Args:       []string{"step", "complete"},
	}

	if got, want := command.ShellCommand(), "'/Applications/Agent Runner/bin/agent-runner' step complete"; got != want {
		t.Fatalf("ShellCommand() = %q, want %q", got, want)
	}
}

func TestAdaptersEmitCompletionIntegrations(t *testing.T) {
	command := &CompletionCommand{
		Executable: "/Applications/Agent Runner/bin/agent-runner",
		Args:       []string{"step", "complete"},
	}

	t.Run("claude preapproves only the quoted exact command and adds stop hook", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		args := (&ClaudeAdapter{}).BuildArgs(&BuildArgsInput{
			Prompt:            "work",
			Context:           ContextInteractive,
			CompletionCommand: command,
		})
		wantCommand := "'/Applications/Agent Runner/bin/agent-runner' step complete"
		if !hasFlagValue(args, "--allowedTools", "Bash("+wantCommand+")") {
			t.Fatalf("Claude args do not narrowly allow completion command: %v", args)
		}
		settings := flagValue(t, args, "--settings")
		var decoded struct {
			Hooks map[string][]struct {
				Hooks []struct {
					Type    string `json:"type"`
					Command string `json:"command"`
				} `json:"hooks"`
			} `json:"hooks"`
		}
		if err := json.Unmarshal([]byte(settings), &decoded); err != nil {
			t.Fatalf("decode Claude settings: %v", err)
		}
		got := decoded.Hooks["Stop"][0].Hooks[0]
		if got.Type != "command" || got.Command != "'/Applications/Agent Runner/bin/agent-runner' internal turn-committed" {
			t.Fatalf("Claude Stop hook = %#v", got)
		}
		assertNextCommandPlugin(t, args, "--plugin-dir", "'/Applications/Agent Runner/bin/agent-runner' step complete")
	})

	t.Run("codex injects additive turn complete notification without broad approval", func(t *testing.T) {
		realCodexHome := t.TempDir()
		t.Setenv("HOME", t.TempDir())
		t.Setenv("CODEX_HOME", realCodexHome)
		if err := os.WriteFile(filepath.Join(realCodexHome, "config.toml"), []byte("model = \"gpt-5.6-sol\"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		adapter := &CodexAdapter{}
		input := &BuildArgsInput{
			Prompt:            "work",
			Context:           ContextInteractive,
			RunID:             "test-run",
			CompletionCommand: command,
		}
		args, err := BuildInvocationArgs(adapter, input)
		if err != nil {
			t.Fatalf("build Codex invocation: %v", err)
		}
		if !hasFlagValue(args, "-c", `notify=["/Applications/Agent Runner/bin/agent-runner","internal","turn-committed"]`) {
			t.Fatalf("Codex args do not include notify integration: %v", args)
		}
		for _, arg := range args {
			if strings.Contains(arg, "marketplaces.agent-runner") || strings.Contains(arg, `plugins."agent-runner`) {
				t.Fatalf("Codex invocation leaks plugin configuration through ineffective -c overrides: %v", args)
			}
		}
		env, err := SpawnEnvForInvocation(adapter, input)
		if err != nil {
			t.Fatalf("build Codex spawn env: %v", err)
		}
		if len(env) != 1 || !strings.HasPrefix(env[0], "CODEX_HOME=") || env[0] == "CODEX_HOME="+realCodexHome {
			t.Fatalf("Codex spawn env = %v, want a private CODEX_HOME distinct from %s", env, realCodexHome)
		}
		privateHome := strings.TrimPrefix(env[0], "CODEX_HOME=")
		assertCodexNextSkill(t, privateHome, "'/Applications/Agent Runner/bin/agent-runner' step complete")
		for _, broad := range []string{"approval_policy=\"never\"", "danger-full-access", "--full-auto"} {
			if strings.Contains(strings.Join(args, " "), broad) {
				t.Fatalf("Codex args contain broad approval %q: %v", broad, args)
			}
		}
	})

	t.Run("copilot keeps the completion command under supervised approval", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		args := (&CopilotAdapter{}).BuildArgs(&BuildArgsInput{
			Prompt:            "work",
			Context:           ContextInteractive,
			CompletionCommand: command,
		})
		for _, arg := range args {
			if strings.HasPrefix(arg, "--allow-tool=shell(") {
				t.Fatalf("Copilot cannot safely preapprove fixed arguments and must not emit %q: %v", arg, args)
			}
		}
		assertNextCommandPlugin(t, args, "--plugin-dir", "'/Applications/Agent Runner/bin/agent-runner' step complete")
	})

	t.Run("cursor does not emit its command-base allowlist", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		args := (&CursorAdapter{}).BuildArgs(&BuildArgsInput{
			Prompt:            "work",
			Context:           ContextInteractive,
			CompletionCommand: command,
		})
		if strings.Contains(strings.Join(args, " "), "Shell(") {
			t.Fatalf("Cursor command-base allowlist would cover extra arguments: %v", args)
		}
		if len(args) < 4 || args[0] != "agent" || args[1] != "--plugin-dir" {
			t.Fatalf("Cursor args do not load the isolated completion plugin: %v", args)
		}
		manifest, err := os.ReadFile(filepath.Join(args[2], ".cursor-plugin", "plugin.json"))
		if err != nil {
			t.Fatalf("read Cursor completion manifest: %v", err)
		}
		if !strings.Contains(string(manifest), `"name": "agent-runner"`) ||
			!strings.Contains(string(manifest), `"commands": "./commands/"`) {
			t.Fatalf("Cursor-compatible plugin manifest is incomplete: %s", manifest)
		}
		if _, err := os.Stat(filepath.Join(args[2], "hooks", "hooks.json")); !os.IsNotExist(err) {
			t.Fatalf("Cursor plugin must rely on native-store durability, hook stat error = %v", err)
		}
		next, err := os.ReadFile(filepath.Join(args[2], "commands", "next.md"))
		if err != nil {
			t.Fatalf("read Cursor /agent-runner:next command: %v", err)
		}
		if !strings.Contains(string(next), `'/Applications/Agent Runner/bin/agent-runner' step complete`) {
			t.Fatalf("Cursor /agent-runner:next command does not invoke the completion client: %s", next)
		}
	})

	t.Run("opencode injects exact inline bash permission", func(t *testing.T) {
		args := (&OpenCodeAdapter{}).BuildArgs(&BuildArgsInput{
			Prompt:            "work",
			Context:           ContextInteractive,
			CompletionCommand: command,
		})
		if !containsString(args, "OPENCODE_DISABLE_AUTOUPDATE=1") {
			t.Fatalf("OpenCode interactive completion must suppress update prompts: %v", args)
		}
		if len(args) < 5 || args[0] != "env" || args[4] != "opencode" {
			t.Fatalf("OpenCode args do not use an ephemeral environment override: %v", args)
		}
		const prefix = "OPENCODE_PERMISSION="
		if !strings.HasPrefix(args[1], prefix) {
			t.Fatalf("OpenCode args do not include OPENCODE_PERMISSION: %v", args)
		}
		var permission map[string]map[string]string
		if err := json.Unmarshal([]byte(strings.TrimPrefix(args[1], prefix)), &permission); err != nil {
			t.Fatalf("decode OpenCode permission: %v", err)
		}
		want := "'/Applications/Agent Runner/bin/agent-runner' step complete"
		if len(permission) != 1 || len(permission["bash"]) != 1 || permission["bash"][want] != "allow" {
			t.Fatalf("OpenCode permission = %#v, want only exact bash command", permission)
		}
		const configPrefix = "OPENCODE_CONFIG_CONTENT="
		if !strings.HasPrefix(args[2], configPrefix) {
			t.Fatalf("OpenCode args do not include /agent-runner:next command config: %v", args)
		}
		var config struct {
			Command map[string]struct {
				Template string `json:"template"`
			} `json:"command"`
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(args[2], configPrefix)), &config); err != nil {
			t.Fatalf("decode OpenCode config: %v", err)
		}
		if got := config.Command["agent-runner:next"].Template; got != "!`"+want+"`" {
			t.Fatalf("OpenCode /agent-runner:next template = %q", got)
		}
	})
}

func TestCodexCompletionHomePreservesRuntimeTrustState(t *testing.T) {
	realCodexHome := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEX_HOME", realCodexHome)
	if err := os.WriteFile(filepath.Join(realCodexHome, "config.toml"), []byte("model = \"gpt-5.6-sol\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	command := CompletionCommand{
		Executable: "/opt/codagent/bin/agent-runner",
		Args:       []string{"step", "complete"},
	}

	privateHome, err := prepareCodexCompletionHome(command, "run-a")
	if err != nil {
		t.Fatalf("prepare Codex completion home: %v", err)
	}
	privateConfig := filepath.Join(privateHome, "config.toml")
	f, err := os.OpenFile(privateConfig, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open private Codex config: %v", err)
	}
	if _, err := f.WriteString("\n[hooks.state.test]\ntrusted_hash = \"sha256:runtime-state\"\n"); err != nil {
		_ = f.Close()
		t.Fatalf("append simulated Codex trust state: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close private Codex config: %v", err)
	}

	secondHome, err := prepareCodexCompletionHome(command, "run-a")
	if err != nil {
		t.Fatalf("prepare Codex completion home again: %v", err)
	}
	if secondHome != privateHome {
		t.Fatalf("second private Codex home = %q, want %q", secondHome, privateHome)
	}
	config, err := os.ReadFile(privateConfig)
	if err != nil {
		t.Fatalf("read private Codex config: %v", err)
	}
	if !strings.Contains(string(config), `trusted_hash = "sha256:runtime-state"`) {
		t.Fatalf("second preparation discarded Codex runtime trust state:\n%s", config)
	}
}

func TestCodexCompletionHomeSharesSessionsCreatedAfterPreparation(t *testing.T) {
	realCodexHome := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEX_HOME", realCodexHome)
	if err := os.WriteFile(filepath.Join(realCodexHome, "config.toml"), []byte("model = \"gpt-5.6-sol\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	command := CompletionCommand{
		Executable: "/opt/codagent/bin/agent-runner",
		Args:       []string{"step", "complete"},
	}

	privateHome, err := prepareCodexCompletionHome(command, "run-a")
	if err != nil {
		t.Fatalf("prepare Codex completion home: %v", err)
	}
	sessionPath := filepath.Join(privateHome, "sessions", "new-session.jsonl")
	if err := os.WriteFile(sessionPath, []byte("session state"), 0o600); err != nil {
		t.Fatalf("write session through private Codex home: %v", err)
	}
	shared, err := os.ReadFile(filepath.Join(realCodexHome, "sessions", "new-session.jsonl"))
	if err != nil {
		t.Fatalf("read session through real Codex home: %v", err)
	}
	if string(shared) != "session state" {
		t.Fatalf("shared session content = %q", shared)
	}
}

func TestCodexCompletionHomesAreIsolatedByRun(t *testing.T) {
	realCodexHome := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEX_HOME", realCodexHome)
	if err := os.WriteFile(filepath.Join(realCodexHome, "config.toml"), []byte("model = \"gpt-5.6-sol\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	command := &CompletionCommand{
		Executable: "/opt/codagent/bin/agent-runner",
		Args:       []string{"step", "complete"},
	}
	adapter := &CodexAdapter{}

	runA := codexCompletionHomeForRun(t, adapter, command, "run-a")
	runAAgain := codexCompletionHomeForRun(t, adapter, command, "run-a")
	runB := codexCompletionHomeForRun(t, adapter, command, "run-b")
	if runAAgain != runA {
		t.Fatalf("same run got different private Codex homes: %q and %q", runA, runAAgain)
	}
	if runB == runA {
		t.Fatalf("concurrent runs share private Codex home %q", runA)
	}
}

func codexCompletionHomeForRun(t *testing.T, adapter *CodexAdapter, command *CompletionCommand, runID string) string {
	t.Helper()
	input := &BuildArgsInput{Context: ContextInteractive, CompletionCommand: command, RunID: runID}
	env, err := adapter.SpawnEnv(input)
	if err != nil {
		t.Fatalf("prepare Codex spawn environment for %s: %v", runID, err)
	}
	return strings.TrimPrefix(env[0], "CODEX_HOME=")
}

func TestCursorInvocationFailsWhenCompletionPluginCreationFails(t *testing.T) {
	adapter := &CursorAdapter{prepareCompletionPlugin: func(CompletionCommand) (string, error) {
		return "", errors.New("cache dir unavailable")
	}}
	args, err := BuildInvocationArgs(adapter, &BuildArgsInput{
		Prompt:  "work",
		Context: ContextInteractive,
		CompletionCommand: &CompletionCommand{
			Executable: "/opt/codagent/bin/agent-runner",
			Args:       []string{"step", "complete"},
		},
	})
	if err == nil {
		t.Fatalf("BuildInvocationArgs = %v, want error when completion plugin creation fails", args)
	}
	if !strings.Contains(err.Error(), "completion plugin") || !strings.Contains(err.Error(), "cache dir unavailable") {
		t.Fatalf("BuildInvocationArgs error = %v, want descriptive completion plugin error", err)
	}
}

func TestCommandPluginAdaptersFailWhenCompletionPluginCreationFails(t *testing.T) {
	command := &CompletionCommand{
		Executable: "/opt/codagent/bin/agent-runner",
		Args:       []string{"step", "complete"},
	}
	for _, test := range []struct {
		name    string
		adapter Adapter
	}{
		{name: "claude", adapter: &ClaudeAdapter{prepareCompletionPlugin: func(CompletionCommand) (string, error) {
			return "", errors.New("cache dir unavailable")
		}}},
		{name: "copilot", adapter: &CopilotAdapter{prepareCompletionPlugin: func(CompletionCommand) (string, error) {
			return "", errors.New("cache dir unavailable")
		}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			args, err := BuildInvocationArgs(test.adapter, &BuildArgsInput{
				Prompt:            "work",
				Context:           ContextInteractive,
				CompletionCommand: command,
			})
			if err == nil {
				t.Fatalf("BuildInvocationArgs = %v, want error when completion plugin creation fails", args)
			}
			if !strings.Contains(err.Error(), "completion plugin") || !strings.Contains(err.Error(), "cache dir unavailable") {
				t.Fatalf("BuildInvocationArgs error = %v, want descriptive completion plugin error", err)
			}
		})
	}
}

func TestInvalidCompletionDescriptorDoesNotLoosenPermissions(t *testing.T) {
	invalid := &CompletionCommand{
		Executable: "/opt/codagent/bin/agent-runner",
		Args:       []string{"step", "complete", "; rm -rf /"},
	}
	adapters := []Adapter{
		&ClaudeAdapter{},
		&CodexAdapter{},
		&CopilotAdapter{},
		&CursorAdapter{},
		&OpenCodeAdapter{},
	}
	for _, adapter := range adapters {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:            "work",
			Context:           ContextInteractive,
			CompletionCommand: invalid,
		})
		joined := strings.Join(args, " ")
		for _, forbidden := range []string{"--allowedTools", "--allow-tool=shell", "OPENCODE_PERMISSION=", "notify=["} {
			if strings.Contains(joined, forbidden) {
				t.Fatalf("%T emitted completion integration for invalid descriptor: %v", adapter, args)
			}
		}
	}
}

func TestHeadlessInvocationDoesNotEmitInteractiveCompletionIntegration(t *testing.T) {
	command := &CompletionCommand{
		Executable: "/opt/codagent/bin/agent-runner",
		Args:       []string{"step", "complete"},
	}
	adapters := []Adapter{
		&ClaudeAdapter{},
		&CodexAdapter{},
		&CopilotAdapter{},
		&CursorAdapter{},
		&OpenCodeAdapter{},
	}
	for _, adapter := range adapters {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:            "work",
			Context:           ContextAutonomousHeadless,
			CompletionCommand: command,
		})
		joined := strings.Join(args, " ")
		for _, forbidden := range []string{"--allowedTools", "--allow-tool=shell", "OPENCODE_PERMISSION=", "notify=[", "--plugin-dir"} {
			if strings.Contains(joined, forbidden) {
				t.Fatalf("%T emitted interactive completion integration in headless context: %v", adapter, args)
			}
		}
	}
}

func flagValue(t *testing.T, args []string, flag string) string {
	t.Helper()
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag {
			return args[i+1]
		}
	}
	t.Fatalf("flag %q not found in %v", flag, args)
	return ""
}

func assertNextCommandPlugin(t *testing.T, args []string, flag, command string) {
	t.Helper()
	pluginDir := flagValue(t, args, flag)
	next, err := os.ReadFile(filepath.Join(pluginDir, "commands", "next.md"))
	if err != nil {
		t.Fatalf("read /agent-runner:next command: %v", err)
	}
	if !strings.Contains(string(next), command) {
		t.Fatalf("/agent-runner:next command does not route through %q: %s", command, next)
	}
	manifest, err := os.ReadFile(filepath.Join(pluginDir, "plugin.json"))
	if err != nil {
		t.Fatalf("read completion plugin manifest: %v", err)
	}
	var decoded struct {
		Name     string `json:"name"`
		Commands string `json:"commands"`
	}
	if err := json.Unmarshal(manifest, &decoded); err != nil {
		t.Fatalf("decode completion plugin manifest: %v", err)
	}
	if decoded.Name != "agent-runner" || decoded.Commands != "./commands/" {
		t.Fatalf("completion plugin identity = %#v, want name agent-runner and ./commands/", decoded)
	}
}

func assertCodexNextSkill(t *testing.T, codexHome, command string) {
	t.Helper()
	config, err := os.ReadFile(filepath.Join(codexHome, "config.toml"))
	if err != nil {
		t.Fatalf("read private Codex config: %v", err)
	}
	if strings.Contains(string(config), `[plugins."agent-runner@agent-runner"]`) {
		t.Fatalf("private Codex config unnecessarily installs a plugin: %s", config)
	}
	next, err := os.ReadFile(filepath.Join(codexHome, "skills", "agent-runner-next", "SKILL.md"))
	if err != nil {
		t.Fatalf("read Codex $agent-runner-next skill: %v", err)
	}
	if !strings.Contains(string(next), command) {
		t.Fatalf("Codex $agent-runner-next skill does not route through %q: %s", command, next)
	}
}
