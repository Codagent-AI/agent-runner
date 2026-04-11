package cli

import (
	"strings"
	"testing"
)

func TestRegistry(t *testing.T) {
	t.Run("resolves known CLI claude", func(t *testing.T) {
		adapter, err := Get("claude")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if adapter == nil {
			t.Fatal("expected non-nil adapter")
		}
	})

	t.Run("resolves known CLI codex", func(t *testing.T) {
		adapter, err := Get("codex")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if adapter == nil {
			t.Fatal("expected non-nil adapter")
		}
	})

	t.Run("returns error for unknown CLI", func(t *testing.T) {
		_, err := Get("unknown")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "unknown CLI adapter") {
			t.Fatalf("expected 'unknown CLI adapter' error, got: %v", err)
		}
	})

	t.Run("KnownCLIs returns all registered names", func(t *testing.T) {
		names := KnownCLIs()
		if len(names) < 2 {
			t.Fatalf("expected at least 2 known CLIs, got %d", len(names))
		}
		found := map[string]bool{}
		for _, name := range names {
			found[name] = true
		}
		if !found["claude"] {
			t.Fatal("expected 'claude' in known CLIs")
		}
		if !found["codex"] {
			t.Fatal("expected 'codex' in known CLIs")
		}
	})
}

func TestClaudeAdapter(t *testing.T) {
	adapter := &ClaudeAdapter{}

	t.Run("fresh headless with session-id", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:    "do something",
			SessionID: "uuid-123",
			Headless:  true,
		})
		expected := []string{"claude", "--session-id", "uuid-123", "-p", "do something"}
		assertArgs(t, expected, args)
	})

	t.Run("fresh interactive with session-id", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:    "review code",
			SessionID: "uuid-456",
			Headless:  false,
		})
		expected := []string{"claude", "--session-id", "uuid-456", "review code"}
		assertArgs(t, expected, args)
	})

	t.Run("fresh headless without session-id", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:   "do something",
			Headless: true,
		})
		expected := []string{"claude", "-p", "do something"}
		assertArgs(t, expected, args)
	})

	t.Run("resume headless uses session-id not resume", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:    "continue",
			SessionID: "session-abc",
			Resume:    true,
			Headless:  true,
		})
		// Headless resume uses --session-id because --resume requires a deferred
		// tool marker which may not exist after a normal session completion.
		expected := []string{"claude", "--session-id", "session-abc", "-p", "continue"}
		assertArgs(t, expected, args)
	})

	t.Run("resume interactive", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:    "continue review",
			SessionID: "session-abc",
			Resume:    true,
			Headless:  false,
		})
		expected := []string{"claude", "--resume", "session-abc", "continue review"}
		assertArgs(t, expected, args)
	})

	t.Run("model override", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:   "do something",
			Model:    "opus",
			Headless: true,
		})
		expected := []string{"claude", "--model", "opus", "-p", "do something"}
		assertArgs(t, expected, args)
	})

	t.Run("resume headless with model override", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:    "continue",
			SessionID: "session-abc",
			Resume:    true,
			Model:     "sonnet",
			Headless:  true,
		})
		expected := []string{"claude", "--session-id", "session-abc", "--model", "sonnet", "-p", "continue"}
		assertArgs(t, expected, args)
	})

	t.Run("effort level specified", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:   "do something",
			Effort:   "high",
			Headless: true,
		})
		expected := []string{"claude", "--effort", "high", "-p", "do something"}
		assertArgs(t, expected, args)
	})

	t.Run("effort level not specified", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:   "do something",
			Headless: true,
		})
		for _, a := range args {
			if a == "--effort" {
				t.Fatalf("did not expect --effort when Effort is empty, got %v", args)
			}
		}
	})

	t.Run("effort with model", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:   "do something",
			Model:    "opus",
			Effort:   "low",
			Headless: true,
		})
		expected := []string{"claude", "--model", "opus", "--effort", "low", "-p", "do something"}
		assertArgs(t, expected, args)
	})

	t.Run("supports system prompt", func(t *testing.T) {
		if !adapter.SupportsSystemPrompt() {
			t.Fatal("expected Claude adapter to support system prompt")
		}
	})

	t.Run("interactive with system prompt emits --append-system-prompt", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			SystemPrompt: "you are helpful",
			SessionID:    "uuid-789",
			Headless:     false,
		})
		expected := []string{"claude", "--session-id", "uuid-789", "--append-system-prompt", "you are helpful"}
		assertArgs(t, expected, args)
	})

	t.Run("system prompt with headless prompt emits both", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:       "do something",
			SystemPrompt: "extra context",
			SessionID:    "uuid-abc",
			Headless:     true,
		})
		expected := []string{"claude", "--session-id", "uuid-abc", "-p", "--append-system-prompt", "extra context", "do something"}
		assertArgs(t, expected, args)
	})

	t.Run("disallowed tools emits --disallowedTools flags", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:          "do something",
			Headless:        true,
			DisallowedTools: []string{"AskUserQuestion"},
		})
		expected := []string{"claude", "-p", "--disallowedTools", "AskUserQuestion", "do something"}
		assertArgs(t, expected, args)
	})

	t.Run("multiple disallowed tools", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:          "do something",
			Headless:        true,
			DisallowedTools: []string{"AskUserQuestion", "WebSearch"},
		})
		expected := []string{"claude", "-p", "--disallowedTools", "AskUserQuestion", "--disallowedTools", "WebSearch", "do something"}
		assertArgs(t, expected, args)
	})

	t.Run("no disallowed tools omits flag", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:   "do something",
			Headless: true,
		})
		for _, a := range args {
			if a == "--disallowedTools" {
				t.Fatalf("did not expect --disallowedTools when DisallowedTools is empty, got %v", args)
			}
		}
	})

	t.Run("no system prompt omits flag", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:   "do something",
			Headless: true,
		})
		for _, a := range args {
			if a == "--append-system-prompt" {
				t.Fatalf("did not expect --append-system-prompt when SystemPrompt is empty, got %v", args)
			}
		}
	})

	t.Run("discover session ID returns preset", func(t *testing.T) {
		id := adapter.DiscoverSessionID(DiscoverOptions{
			PresetID: "preset-123",
		})
		if id != "preset-123" {
			t.Fatalf("expected 'preset-123', got %q", id)
		}
	})

	t.Run("discover session ID returns empty when no preset", func(t *testing.T) {
		id := adapter.DiscoverSessionID(DiscoverOptions{})
		if id != "" {
			t.Fatalf("expected empty string, got %q", id)
		}
	})
}

func TestCodexAdapter(t *testing.T) {
	adapter := &CodexAdapter{}

	t.Run("fresh headless", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:   "do something",
			Headless: true,
		})
		expected := []string{"codex", "exec", "--json", "-a", "never", "do something"}
		assertArgs(t, expected, args)
	})

	t.Run("fresh interactive", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:   "review code",
			Headless: false,
		})
		expected := []string{"codex", "--no-alt-screen", "review code"}
		assertArgs(t, expected, args)
	})

	t.Run("resume headless", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:    "continue",
			SessionID: "thread-abc",
			Headless:  true,
		})
		expected := []string{"codex", "exec", "resume", "thread-abc", "-a", "never", "continue"}
		assertArgs(t, expected, args)
	})

	t.Run("resume interactive", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:    "continue review",
			SessionID: "thread-abc",
			Headless:  false,
		})
		expected := []string{"codex", "resume", "--no-alt-screen", "thread-abc", "continue review"}
		assertArgs(t, expected, args)
	})

	t.Run("model override headless", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:   "do something",
			Model:    "o3",
			Headless: true,
		})
		expected := []string{"codex", "exec", "--json", "-a", "never", "-m", "o3", "do something"}
		assertArgs(t, expected, args)
	})

	t.Run("model override interactive", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:   "review",
			Model:    "o3",
			Headless: false,
		})
		expected := []string{"codex", "--no-alt-screen", "-m", "o3", "review"}
		assertArgs(t, expected, args)
	})

	t.Run("effort level specified headless", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:   "do something",
			Effort:   "medium",
			Headless: true,
		})
		expected := []string{"codex", "exec", "--json", "-a", "never", "--effort", "medium", "do something"}
		assertArgs(t, expected, args)
	})

	t.Run("effort level specified interactive", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt: "review",
			Effort: "high",
		})
		expected := []string{"codex", "--no-alt-screen", "--effort", "high", "review"}
		assertArgs(t, expected, args)
	})

	t.Run("effort level not specified", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:   "do something",
			Headless: true,
		})
		for _, a := range args {
			if a == "--effort" {
				t.Fatalf("did not expect --effort when Effort is empty, got %v", args)
			}
		}
	})

	t.Run("does not support system prompt", func(t *testing.T) {
		if adapter.SupportsSystemPrompt() {
			t.Fatal("expected Codex adapter to not support system prompt")
		}
	})

	t.Run("ignores system prompt field", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:       "do something",
			SystemPrompt: "should be ignored",
			Headless:     true,
		})
		expected := []string{"codex", "exec", "--json", "-a", "never", "do something"}
		assertArgs(t, expected, args)
	})

	t.Run("interactive always includes --no-alt-screen", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:   "review",
			Headless: false,
		})
		found := false
		for _, a := range args {
			if a == "--no-alt-screen" {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected --no-alt-screen for interactive mode, got %v", args)
		}
	})

	t.Run("headless does not include --no-alt-screen", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:   "do something",
			Headless: true,
		})
		for _, a := range args {
			if a == "--no-alt-screen" {
				t.Fatalf("did not expect --no-alt-screen for headless mode, got %v", args)
			}
		}
	})

	t.Run("interactive does not include -a never", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:   "review",
			Headless: false,
		})
		for i, a := range args {
			if a == "-a" && i+1 < len(args) && args[i+1] == "never" {
				t.Fatalf("did not expect -a never for interactive mode, got %v", args)
			}
		}
	})

	t.Run("codex ignores DisallowedTools", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:          "do something",
			Headless:        true,
			DisallowedTools: []string{"AskUserQuestion"},
		})
		for _, a := range args {
			if a == "--disallowedTools" {
				t.Fatalf("did not expect --disallowedTools for codex, got %v", args)
			}
		}
	})

	t.Run("discover headless session from JSONL", func(t *testing.T) {
		output := `{"type":"thread.started","thread_id":"thread-xyz-123"}
{"type":"message","content":"hello"}`
		id := adapter.DiscoverSessionID(DiscoverOptions{
			ProcessOutput: output,
			Headless:      true,
		})
		if id != "thread-xyz-123" {
			t.Fatalf("expected 'thread-xyz-123', got %q", id)
		}
	})

	t.Run("discover headless session returns empty for no thread.started", func(t *testing.T) {
		output := `{"type":"message","content":"hello"}`
		id := adapter.DiscoverSessionID(DiscoverOptions{
			ProcessOutput: output,
			Headless:      true,
		})
		if id != "" {
			t.Fatalf("expected empty string, got %q", id)
		}
	})

	t.Run("discover headless session returns empty for empty output", func(t *testing.T) {
		id := adapter.DiscoverSessionID(DiscoverOptions{
			ProcessOutput: "",
			Headless:      true,
		})
		if id != "" {
			t.Fatalf("expected empty string, got %q", id)
		}
	})
}

func assertArgs(t *testing.T, expected, actual []string) {
	t.Helper()
	if len(expected) != len(actual) {
		t.Fatalf("expected args %v, got %v", expected, actual)
	}
	for i := range expected {
		if expected[i] != actual[i] {
			t.Fatalf("arg[%d]: expected %q, got %q (full: %v)", i, expected[i], actual[i], actual)
		}
	}
}
