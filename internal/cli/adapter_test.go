package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

	t.Run("resolves known CLI copilot", func(t *testing.T) {
		adapter, err := Get("copilot")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if adapter == nil {
			t.Fatal("expected non-nil adapter")
		}
	})

	t.Run("resolves known CLI cursor", func(t *testing.T) {
		adapter, err := Get("cursor")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if adapter == nil {
			t.Fatal("expected non-nil adapter")
		}
	})

	t.Run("KnownCLIs returns all registered names", func(t *testing.T) {
		names := KnownCLIs()
		if len(names) < 4 {
			t.Fatalf("expected at least 4 known CLIs, got %d", len(names))
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
		if !found["copilot"] {
			t.Fatal("expected 'copilot' in known CLIs")
		}
		if !found["cursor"] {
			t.Fatal("expected 'cursor' in known CLIs")
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
		expected := []string{"claude", "--session-id", "uuid-123", "-p", "--", "do something"}
		assertArgs(t, expected, args)
	})

	t.Run("fresh interactive with session-id", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:    "review code",
			SessionID: "uuid-456",
			Headless:  false,
		})
		expected := []string{"claude", "--session-id", "uuid-456", "--", "review code"}
		assertArgs(t, expected, args)
	})

	t.Run("fresh headless without session-id", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:   "do something",
			Headless: true,
		})
		expected := []string{"claude", "-p", "--", "do something"}
		assertArgs(t, expected, args)
	})

	t.Run("resume headless uses --resume", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:    "continue",
			SessionID: "session-abc",
			Resume:    true,
			Headless:  true,
		})
		// --session-id is reserved for fresh sessions — Claude CLI rejects it
		// for existing session IDs. Use --resume for headless continuations too.
		expected := []string{"claude", "--resume", "session-abc", "-p", "--", "continue"}
		assertArgs(t, expected, args)
	})

	t.Run("resume interactive", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:    "continue review",
			SessionID: "session-abc",
			Resume:    true,
			Headless:  false,
		})
		expected := []string{"claude", "--resume", "session-abc", "--", "continue review"}
		assertArgs(t, expected, args)
	})

	t.Run("model override", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:   "do something",
			Model:    "opus",
			Headless: true,
		})
		expected := []string{"claude", "--model", "opus", "-p", "--", "do something"}
		assertArgs(t, expected, args)
	})

	t.Run("resume drops model flag", func(t *testing.T) {
		// A Claude session keeps the model it was started with; passing
		// --model on resume would be at best ignored and at worst rejected,
		// so the adapter must omit it even when Model is set on the input.
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:    "continue",
			SessionID: "session-abc",
			Resume:    true,
			Model:     "sonnet",
			Headless:  true,
		})
		expected := []string{"claude", "--resume", "session-abc", "-p", "--", "continue"}
		assertArgs(t, expected, args)
	})

	t.Run("resume headless with disallowed tools", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:          "continue",
			SessionID:       "session-abc",
			Resume:          true,
			Headless:        true,
			DisallowedTools: []string{"AskUserQuestion"},
		})
		// --disallowedTools is compatible with --resume; both flags coexist on
		// headless resume steps.
		expected := []string{"claude", "--resume", "session-abc", "-p", "--disallowedTools", "AskUserQuestion", "--", "continue"}
		assertArgs(t, expected, args)
	})

	t.Run("effort level specified", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:   "do something",
			Effort:   "high",
			Headless: true,
		})
		expected := []string{"claude", "--effort", "high", "-p", "--", "do something"}
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
		expected := []string{"claude", "--model", "opus", "--effort", "low", "-p", "--", "do something"}
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
		expected := []string{"claude", "--session-id", "uuid-abc", "-p", "--append-system-prompt", "extra context", "--", "do something"}
		assertArgs(t, expected, args)
	})

	t.Run("disallowed tools emits --disallowedTools flags", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:          "do something",
			Headless:        true,
			DisallowedTools: []string{"AskUserQuestion"},
		})
		expected := []string{"claude", "-p", "--disallowedTools", "AskUserQuestion", "--", "do something"}
		assertArgs(t, expected, args)
	})

	t.Run("multiple disallowed tools", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:          "do something",
			Headless:        true,
			DisallowedTools: []string{"AskUserQuestion", "WebSearch"},
		})
		expected := []string{"claude", "-p", "--disallowedTools", "AskUserQuestion", "--disallowedTools", "WebSearch", "--", "do something"}
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
		id := adapter.DiscoverSessionID(&DiscoverOptions{
			PresetID: "preset-123",
		})
		if id != "preset-123" {
			t.Fatalf("expected 'preset-123', got %q", id)
		}
	})

	t.Run("discover session ID returns empty when no preset", func(t *testing.T) {
		id := adapter.DiscoverSessionID(&DiscoverOptions{})
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

	t.Run("resume drops model flag", func(t *testing.T) {
		// A Codex thread keeps the model it was started with, so -m is
		// omitted when resuming even if Model is set on the input.
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:    "continue",
			SessionID: "thread-abc",
			Model:     "o3",
			Headless:  true,
		})
		expected := []string{"codex", "exec", "resume", "thread-abc", "-a", "never", "continue"}
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
		id := adapter.DiscoverSessionID(&DiscoverOptions{
			ProcessOutput: output,
			Headless:      true,
		})
		if id != "thread-xyz-123" {
			t.Fatalf("expected 'thread-xyz-123', got %q", id)
		}
	})

	t.Run("discover headless session returns empty for no thread.started", func(t *testing.T) {
		output := `{"type":"message","content":"hello"}`
		id := adapter.DiscoverSessionID(&DiscoverOptions{
			ProcessOutput: output,
			Headless:      true,
		})
		if id != "" {
			t.Fatalf("expected empty string, got %q", id)
		}
	})

	t.Run("discover headless session returns empty for empty output", func(t *testing.T) {
		id := adapter.DiscoverSessionID(&DiscoverOptions{
			ProcessOutput: "",
			Headless:      true,
		})
		if id != "" {
			t.Fatalf("expected empty string, got %q", id)
		}
	})
}

func TestCopilotAdapter(t *testing.T) {
	adapter := &CopilotAdapter{}

	t.Run("fresh headless copilot step", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:   "do something",
			Headless: true,
		})
		expected := []string{"copilot", "-p", "do something", "--allow-all", "--autopilot", "-s"}
		assertArgs(t, expected, args)
	})

	t.Run("headless always includes --allow-all", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:   "do something",
			Headless: true,
		})
		found := false
		for _, a := range args {
			if a == "--allow-all" {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected --allow-all in args, got %v", args)
		}
	})

	t.Run("fresh headless does not include --resume", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:   "do something",
			Headless: true,
		})
		for _, a := range args {
			if strings.HasPrefix(a, "--resume") {
				t.Fatalf("did not expect --resume for fresh session, got %v", args)
			}
		}
	})

	t.Run("resume headless includes --resume flag", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:    "continue",
			SessionID: "session-abc",
			Resume:    true,
			Headless:  true,
		})
		expected := []string{"copilot", "-p", "continue", "--allow-all", "--autopilot", "-s", "--resume=session-abc"}
		assertArgs(t, expected, args)
	})

	t.Run("resume drops model flag", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:    "continue",
			SessionID: "session-abc",
			Resume:    true,
			Model:     "gpt-5.2",
			Headless:  true,
		})
		for _, a := range args {
			if a == "--model" {
				t.Fatalf("did not expect --model on resume, got %v", args)
			}
		}
	})

	t.Run("model specified on fresh step", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:   "do something",
			Model:    "gpt-5.2",
			Headless: true,
		})
		expected := []string{"copilot", "-p", "do something", "--allow-all", "--autopilot", "-s", "--model", "gpt-5.2"}
		assertArgs(t, expected, args)
	})

	t.Run("model specified on resumed step is omitted", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:    "continue",
			SessionID: "session-abc",
			Resume:    true,
			Model:     "gpt-5.2",
			Headless:  true,
		})
		for _, a := range args {
			if a == "--model" {
				t.Fatalf("did not expect --model on resumed step, got %v", args)
			}
		}
	})

	t.Run("effort specified on fresh step", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:   "do something",
			Effort:   "high",
			Headless: true,
		})
		expected := []string{"copilot", "-p", "do something", "--allow-all", "--autopilot", "-s", "--reasoning-effort", "high"}
		assertArgs(t, expected, args)
	})

	t.Run("effort not specified omits flag", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:   "do something",
			Headless: true,
		})
		for _, a := range args {
			if a == "--reasoning-effort" {
				t.Fatalf("did not expect --reasoning-effort when Effort is empty, got %v", args)
			}
		}
	})

	t.Run("resume drops reasoning-effort flag", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:    "continue",
			SessionID: "session-abc",
			Resume:    true,
			Effort:    "high",
			Headless:  true,
		})
		for _, a := range args {
			if a == "--reasoning-effort" {
				t.Fatalf("did not expect --reasoning-effort on resume, got %v", args)
			}
		}
	})

	t.Run("AskUserQuestion disallowed emits --no-ask-user", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:          "do something",
			Headless:        true,
			DisallowedTools: []string{"AskUserQuestion"},
		})
		expected := []string{"copilot", "-p", "do something", "--allow-all", "--autopilot", "-s", "--no-ask-user"}
		assertArgs(t, expected, args)
	})

	t.Run("no disallowed tools omits --no-ask-user", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:   "do something",
			Headless: true,
		})
		for _, a := range args {
			if a == "--no-ask-user" {
				t.Fatalf("did not expect --no-ask-user when DisallowedTools is empty, got %v", args)
			}
		}
	})

	t.Run("does not support system prompt", func(t *testing.T) {
		if adapter.SupportsSystemPrompt() {
			t.Fatal("expected Copilot adapter to not support system prompt")
		}
	})

	t.Run("discover session ID from filesystem", func(t *testing.T) {
		sessionID := "test-session-abc123"
		fakeHome := t.TempDir()
		cwd := t.TempDir()

		// Resolve symlinks so os.Getwd() and the workspace.yaml value agree on macOS.
		canonCwd, err := filepath.EvalSymlinks(cwd)
		if err != nil {
			t.Fatalf("EvalSymlinks: %v", err)
		}

		origHome := os.Getenv("HOME")
		origCwd, _ := os.Getwd()
		t.Cleanup(func() {
			os.Setenv("HOME", origHome)
			_ = os.Chdir(origCwd)
		})
		os.Setenv("HOME", fakeHome)
		if err := os.Chdir(canonCwd); err != nil {
			t.Fatalf("failed to chdir: %v", err)
		}

		sessionStatePath := filepath.Join(fakeHome, ".copilot", "session-state", sessionID)
		if err := os.MkdirAll(sessionStatePath, 0o700); err != nil {
			t.Fatalf("failed to create session-state dir: %v", err)
		}
		workspace := fmt.Sprintf("id: %s\ncwd: %s\n", sessionID, canonCwd)
		if err := os.WriteFile(filepath.Join(sessionStatePath, "workspace.yaml"), []byte(workspace), 0o600); err != nil {
			t.Fatalf("failed to write workspace.yaml: %v", err)
		}

		id := adapter.DiscoverSessionID(&DiscoverOptions{
			SpawnTime: time.Now().Add(-time.Second),
			Headless:  true,
		})
		if id != sessionID {
			t.Fatalf("expected %q, got %q", sessionID, id)
		}
	})

	t.Run("discover session ID uses Workdir when provided", func(t *testing.T) {
		sessionID := "test-session-workdir"
		fakeHome := t.TempDir()
		workdir := t.TempDir()

		canonWorkdir, err := filepath.EvalSymlinks(workdir)
		if err != nil {
			t.Fatalf("EvalSymlinks: %v", err)
		}

		origHome := os.Getenv("HOME")
		t.Cleanup(func() { os.Setenv("HOME", origHome) })
		os.Setenv("HOME", fakeHome)

		sessionStatePath := filepath.Join(fakeHome, ".copilot", "session-state", sessionID)
		if err := os.MkdirAll(sessionStatePath, 0o700); err != nil {
			t.Fatalf("failed to create session-state dir: %v", err)
		}
		workspace := fmt.Sprintf("id: %s\ncwd: %s\n", sessionID, canonWorkdir)
		if err := os.WriteFile(filepath.Join(sessionStatePath, "workspace.yaml"), []byte(workspace), 0o600); err != nil {
			t.Fatalf("failed to write workspace.yaml: %v", err)
		}

		// Pass Workdir explicitly — no os.Chdir needed.
		id := adapter.DiscoverSessionID(&DiscoverOptions{
			SpawnTime: time.Now().Add(-time.Second),
			Headless:  true,
			Workdir:   canonWorkdir,
		})
		if id != sessionID {
			t.Fatalf("expected %q, got %q", sessionID, id)
		}
	})

	t.Run("discover session ID returns empty when no matching session", func(t *testing.T) {
		sessionDir := t.TempDir()
		os.Setenv("HOME", sessionDir)
		t.Cleanup(func() { os.Unsetenv("HOME") })

		id := adapter.DiscoverSessionID(&DiscoverOptions{
			SpawnTime: time.Now(),
			Headless:  true,
		})
		if id != "" {
			t.Fatalf("expected empty string, got %q", id)
		}
	})

	t.Run("interactive mode returns error via InteractiveRejector interface", func(t *testing.T) {
		var a Adapter = adapter
		r, ok := a.(InteractiveRejector)
		if !ok {
			t.Fatal("expected CopilotAdapter to implement InteractiveRejector")
		}
		err := r.InteractiveModeError()
		if err == nil {
			t.Fatal("expected error from InteractiveModeError")
		}
		if !strings.Contains(err.Error(), "interactive mode") || !strings.Contains(err.Error(), "copilot") {
			t.Fatalf("expected error about interactive mode and copilot, got: %v", err)
		}
	})
}

func TestCursorAdapter(t *testing.T) {
	adapter := &CursorAdapter{}

	t.Run("fresh headless cursor step", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:   "do something",
			Headless: true,
		})
		expected := []string{"agent", "-p", "--output-format", "stream-json", "--force", "--trust", "do something"}
		assertArgs(t, expected, args)
	})

	t.Run("fresh headless does not include --resume", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:   "do something",
			Headless: true,
		})
		for _, a := range args {
			if strings.HasPrefix(a, "--resume") {
				t.Fatalf("did not expect --resume for fresh cursor session, got %v", args)
			}
		}
	})

	t.Run("resume headless includes cursor autonomy flags", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:    "continue",
			SessionID: "chat-abc",
			Resume:    true,
			Headless:  true,
		})
		expected := []string{"agent", "-p", "--output-format", "stream-json", "--force", "--trust", "--resume=chat-abc", "continue"}
		assertArgs(t, expected, args)
	})

	t.Run("model specified on fresh cursor step", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:   "do something",
			Model:    "gpt-5.3-codex",
			Headless: true,
		})
		expected := []string{"agent", "-p", "--output-format", "stream-json", "--force", "--trust", "--model", "gpt-5.3-codex", "do something"}
		assertArgs(t, expected, args)
	})

	t.Run("model specified on resumed cursor step is omitted", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:    "continue",
			SessionID: "chat-abc",
			Resume:    true,
			Model:     "gpt-5.3-codex",
			Headless:  true,
		})
		for _, a := range args {
			if a == "--model" {
				t.Fatalf("did not expect --model on resumed cursor step, got %v", args)
			}
		}
	})

	t.Run("effort level is ignored", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:   "do something",
			Effort:   "high",
			Headless: true,
		})
		for _, a := range args {
			if a == "--reasoning-effort" || a == "--effort" {
				t.Fatalf("did not expect any effort flag for cursor, got %v", args)
			}
		}
	})

	t.Run("disallowed tools do not affect args", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:          "do something",
			Headless:        true,
			DisallowedTools: []string{"AskUserQuestion"},
		})
		expected := []string{"agent", "-p", "--output-format", "stream-json", "--force", "--trust", "do something"}
		assertArgs(t, expected, args)
	})

	t.Run("does not support system prompt", func(t *testing.T) {
		if adapter.SupportsSystemPrompt() {
			t.Fatal("expected Cursor adapter to not support system prompt")
		}
	})

	t.Run("system prompt is ignored by adapter", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:       "do something",
			SystemPrompt: "should be ignored",
			Headless:     true,
		})
		expected := []string{"agent", "-p", "--output-format", "stream-json", "--force", "--trust", "do something"}
		assertArgs(t, expected, args)
	})

	t.Run("discover session ID from stream-json init event", func(t *testing.T) {
		output := `{"type":"system","subtype":"init","session_id":"chat-abc-123","model":"composer-1.5","cwd":"/tmp","permissionMode":"default"}`
		id := adapter.DiscoverSessionID(&DiscoverOptions{
			ProcessOutput: output,
			Headless:      true,
		})
		if id != "chat-abc-123" {
			t.Fatalf("expected %q, got %q", "chat-abc-123", id)
		}
	})

	t.Run("discover session ID from later event when earlier lines lack it", func(t *testing.T) {
		output := "not json\n\n" +
			`{"type":"assistant","message":{}}` + "\n" +
			`{"type":"assistant","session_id":"chat-xyz","message":{}}` + "\n"
		id := adapter.DiscoverSessionID(&DiscoverOptions{
			ProcessOutput: output,
			Headless:      true,
		})
		if id != "chat-xyz" {
			t.Fatalf("expected %q, got %q", "chat-xyz", id)
		}
	})

	t.Run("discover session ID from long JSON line", func(t *testing.T) {
		output := `{"type":"assistant","payload":"` + strings.Repeat("x", 70*1024) + `","session_id":"chat-long","message":{}}`
		id := adapter.DiscoverSessionID(&DiscoverOptions{
			ProcessOutput: output,
			Headless:      true,
		})
		if id != "chat-long" {
			t.Fatalf("expected %q, got %q", "chat-long", id)
		}
	})

	t.Run("discover session ID returns empty when no event has session_id", func(t *testing.T) {
		output := `{"type":"assistant","message":{}}`
		id := adapter.DiscoverSessionID(&DiscoverOptions{
			ProcessOutput: output,
			Headless:      true,
		})
		if id != "" {
			t.Fatalf("expected empty string, got %q", id)
		}
	})

	t.Run("discover session ID returns empty for empty output", func(t *testing.T) {
		id := adapter.DiscoverSessionID(&DiscoverOptions{
			ProcessOutput: "",
			Headless:      true,
		})
		if id != "" {
			t.Fatalf("expected empty string, got %q", id)
		}
	})

	t.Run("interactive mode returns error via InteractiveRejector interface", func(t *testing.T) {
		var a Adapter = adapter
		r, ok := a.(InteractiveRejector)
		if !ok {
			t.Fatal("expected CursorAdapter to implement InteractiveRejector")
		}
		err := r.InteractiveModeError()
		if err == nil {
			t.Fatal("expected error from InteractiveModeError")
		}
		if !strings.Contains(err.Error(), "interactive mode") || !strings.Contains(err.Error(), "cursor") {
			t.Fatalf("expected error about interactive mode and cursor, got: %v", err)
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
