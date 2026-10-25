package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestCodexSpawnEnvSharesThreadState(t *testing.T) {
	for _, tc := range []struct {
		name          string
		sourceEnv     bool
		sqliteEnv     string
		config        string
		existingIndex string
	}{
		{name: "fresh explicit home", sourceEnv: true},
		{name: "default home"},
		{name: "user sqlite environment", sourceEnv: true, sqliteEnv: "/custom/sqlite"},
		{name: "user sqlite config", sourceEnv: true, config: "sqlite_home = \"/configured/sqlite\"\n"},
		{name: "config takes precedence", sourceEnv: true, sqliteEnv: "/custom/sqlite", config: "sqlite_home = \"/configured/sqlite\"\n"},
		{name: "existing index preserved", sourceEnv: true, existingIndex: "existing thread\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			sourceHome := filepath.Join(home, ".codex")
			t.Setenv("CODEX_HOME", "")
			if tc.sourceEnv {
				sourceHome = filepath.Join(home, "codex-source")
				t.Setenv("CODEX_HOME", sourceHome)
			}
			t.Setenv("CODEX_SQLITE_HOME", tc.sqliteEnv)
			if tc.config != "" || tc.existingIndex != "" {
				if err := os.MkdirAll(sourceHome, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			if tc.config != "" {
				if err := os.WriteFile(filepath.Join(sourceHome, "config.toml"), []byte(tc.config), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.existingIndex != "" {
				if err := os.WriteFile(filepath.Join(sourceHome, "session_index.jsonl"), []byte(tc.existingIndex), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			executable := filepath.Join(t.TempDir(), "agent-runner")
			if err := os.WriteFile(executable, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			env, err := (&CodexAdapter{}).SpawnEnv(&BuildArgsInput{
				Context: ContextAutonomousHeadless, RunID: "thread-state-test",
				RunnerIntegration: &RunnerIntegration{AgentCall: &MCPServerCommand{Executable: executable, Args: []string{"internal", "call-agent-mcp"}}},
			})
			if err != nil {
				t.Fatal(err)
			}
			wantSQLiteEnv := sourceHome
			if tc.sqliteEnv != "" || tc.config != "" {
				wantSQLiteEnv = ""
			}
			if diff := cmp.Diff(wantSQLiteEnv, envEntry(t, env, "CODEX_SQLITE_HOME=")); diff != "" {
				t.Errorf("SQLite environment (-want +got):\n%s", diff)
			}
			privateHome := envEntry(t, env, "CODEX_HOME=")
			target, err := os.Readlink(filepath.Join(privateHome, "session_index.jsonl"))
			if err != nil {
				t.Fatalf("private session index must be a symlink: %v", err)
			}
			if diff := cmp.Diff(filepath.Join(sourceHome, "session_index.jsonl"), target); diff != "" {
				t.Errorf("index target (-want +got):\n%s", diff)
			}
			info, err := os.Stat(target)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0o600 {
				t.Errorf("index permissions = %o, want 600", info.Mode().Perm())
			}
			data, err := os.ReadFile(target)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tc.existingIndex, string(data)); diff != "" {
				t.Errorf("index contents (-want +got):\n%s", diff)
			}
			if tc.config != "" {
				data, err := os.ReadFile(filepath.Join(privateHome, "config.toml"))
				if err != nil {
					t.Fatal(err)
				}
				if got := tomlStringValue(t, string(data), "sqlite_home"); got != "/configured/sqlite" {
					t.Errorf("sqlite_home = %q", got)
				}
			}
		})
	}
}

// SQLite detection must not become an extra config validation gate. Codex
// validates its own config, whose sqlite_home takes precedence over our default.
func TestCodexSpawnEnvDefersSQLiteConfigValidation(t *testing.T) {
	for _, tc := range []struct {
		name      string
		config    string
		agentCall bool
	}{
		{name: "completion with default state"},
		{name: "completion with unexpected sqlite type", config: "sqlite_home = 42\n"},
		{name: "completion with undecodable TOML", config: "future_syntax = [\n"},
		{name: "agent call with unexpected sqlite type", config: "sqlite_home = 42\n", agentCall: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sourceHome := t.TempDir()
			t.Setenv("HOME", t.TempDir())
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			t.Setenv("CODEX_HOME", sourceHome)
			t.Setenv("CODEX_SQLITE_HOME", "")
			if err := os.WriteFile(filepath.Join(sourceHome, "config.toml"), []byte(tc.config), 0o600); err != nil {
				t.Fatal(err)
			}
			input := &BuildArgsInput{
				Context: ContextInteractive, RunID: "config-validation-test",
				CompletionCommand: &CompletionCommand{Executable: "/opt/codagent/bin/agent-runner", Args: []string{"step", "complete"}},
			}
			if tc.agentCall {
				executable := filepath.Join(t.TempDir(), "agent-runner")
				if err := os.WriteFile(executable, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
					t.Fatal(err)
				}
				input.RunnerIntegration = &RunnerIntegration{AgentCall: &MCPServerCommand{Executable: executable, Args: []string{"internal", "call-agent-mcp"}}}
			}
			env, err := (&CodexAdapter{}).SpawnEnv(input)
			if err != nil {
				t.Fatalf("SQLite detection must defer config validation to Codex: %v", err)
			}
			if diff := cmp.Diff(sourceHome, envEntry(t, env, "CODEX_SQLITE_HOME=")); diff != "" {
				t.Errorf("SQLite fallback (-want +got):\n%s", diff)
			}
			privateHome := envEntry(t, env, "CODEX_HOME=")
			config, err := os.ReadFile(filepath.Join(privateHome, "config.toml"))
			if err != nil {
				t.Fatal(err)
			}
			if !tc.agentCall {
				if diff := cmp.Diff(tc.config, string(config)); diff != "" {
					t.Errorf("config must pass through unchanged (-want +got):\n%s", diff)
				}
			}
		})
	}
}
