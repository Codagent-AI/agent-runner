package cli

import (
	"encoding/json"
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
		args := (&CodexAdapter{}).BuildArgs(&BuildArgsInput{
			Prompt:            "work",
			Context:           ContextInteractive,
			CompletionCommand: command,
		})
		if !hasFlagValue(args, "-c", `notify=["/Applications/Agent Runner/bin/agent-runner","internal","turn-committed"]`) {
			t.Fatalf("Codex args do not include notify integration: %v", args)
		}
		for _, broad := range []string{"approval_policy=\"never\"", "danger-full-access", "--full-auto"} {
			if strings.Contains(strings.Join(args, " "), broad) {
				t.Fatalf("Codex args contain broad approval %q: %v", broad, args)
			}
		}
	})

	t.Run("copilot preapproves exact shell command", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		args := (&CopilotAdapter{}).BuildArgs(&BuildArgsInput{
			Prompt:            "work",
			Context:           ContextInteractive,
			CompletionCommand: command,
		})
		want := "--allow-tool=shell('/Applications/Agent Runner/bin/agent-runner' step complete)"
		if !containsString(args, want) {
			t.Fatalf("Copilot args do not include %q: %v", want, args)
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
		if len(args) < 6 || args[0] != "env" || args[1] != "AGENT_RUNNER_COMPLETION_CLIENT=/Applications/Agent Runner/bin/agent-runner" || args[2] != "agent" || args[3] != "--plugin-dir" {
			t.Fatalf("Cursor args do not load the isolated completion plugin: %v", args)
		}
		hooks, err := os.ReadFile(filepath.Join(args[4], "hooks", "hooks.json"))
		if err != nil {
			t.Fatalf("read Cursor completion hooks: %v", err)
		}
		if !strings.Contains(string(hooks), `"stop"`) || !strings.Contains(string(hooks), `step complete`) || !strings.Contains(string(hooks), `internal turn-committed`) {
			t.Fatalf("Cursor completion hook is incomplete: %s", hooks)
		}
		next, err := os.ReadFile(filepath.Join(args[4], "commands", "next.md"))
		if err != nil {
			t.Fatalf("read Cursor /next command: %v", err)
		}
		if !strings.Contains(string(next), "completion is automatic") {
			t.Fatalf("Cursor /next command does not route through Stop hook: %s", next)
		}
	})

	t.Run("opencode injects exact inline bash permission", func(t *testing.T) {
		args := (&OpenCodeAdapter{}).BuildArgs(&BuildArgsInput{
			Prompt:            "work",
			Context:           ContextInteractive,
			CompletionCommand: command,
		})
		if len(args) < 4 || args[0] != "env" || args[3] != "opencode" {
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
			t.Fatalf("OpenCode args do not include /next command config: %v", args)
		}
		var config struct {
			Command map[string]struct {
				Template string `json:"template"`
			} `json:"command"`
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(args[2], configPrefix)), &config); err != nil {
			t.Fatalf("decode OpenCode config: %v", err)
		}
		if got := config.Command["next"].Template; got != "!`"+want+"`" {
			t.Fatalf("OpenCode /next template = %q", got)
		}
	})
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
		t.Fatalf("read /next command: %v", err)
	}
	if !strings.Contains(string(next), command) {
		t.Fatalf("/next command does not route through %q: %s", command, next)
	}
}
