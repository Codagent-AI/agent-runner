package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
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

	t.Run("resolves known CLI opencode", func(t *testing.T) {
		adapter, err := Get("opencode")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if adapter == nil {
			t.Fatal("expected non-nil adapter")
		}
	})

	t.Run("KnownCLIs returns all registered names", func(t *testing.T) {
		names := KnownCLIs()
		if len(names) < 5 {
			t.Fatalf("expected at least 5 known CLIs, got %d", len(names))
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
		if !found["opencode"] {
			t.Fatal("expected 'opencode' in known CLIs")
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
		expected := []string{"codex", "--dangerously-bypass-approvals-and-sandbox", "exec", "--json", "do something"}
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
		expected := []string{"codex", "--dangerously-bypass-approvals-and-sandbox", "exec", "resume", "--json", "thread-abc", "continue"}
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

	t.Run("resume interactive normalizes legacy rollout session IDs", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:    "continue review",
			SessionID: "rollout-2026-05-03T22-18-42-019df0c7-daf4-7120-b587-0731815d36cb",
			Headless:  false,
		})
		expected := []string{"codex", "resume", "--no-alt-screen", "019df0c7-daf4-7120-b587-0731815d36cb", "continue review"}
		assertArgs(t, expected, args)
	})

	t.Run("model override headless", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:   "do something",
			Model:    "o3",
			Headless: true,
		})
		expected := []string{"codex", "--dangerously-bypass-approvals-and-sandbox", "exec", "--json", "-m", "o3", "do something"}
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
		expected := []string{"codex", "--dangerously-bypass-approvals-and-sandbox", "exec", "resume", "--json", "thread-abc", "continue"}
		assertArgs(t, expected, args)
	})

	t.Run("resume headless keeps effort config", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:    "continue",
			SessionID: "thread-abc",
			Effort:    "low",
			Headless:  true,
		})
		expected := []string{"codex", "--dangerously-bypass-approvals-and-sandbox", "exec", "resume", "--json", "-c", `model_reasoning_effort="low"`, "thread-abc", "continue"}
		assertArgs(t, expected, args)
	})

	t.Run("effort level specified headless", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:   "do something",
			Effort:   "medium",
			Headless: true,
		})
		expected := []string{"codex", "--dangerously-bypass-approvals-and-sandbox", "exec", "--json", "-c", `model_reasoning_effort="medium"`, "do something"}
		assertArgs(t, expected, args)
	})

	t.Run("effort level specified interactive", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt: "review",
			Effort: "high",
		})
		expected := []string{"codex", "--no-alt-screen", "-c", `model_reasoning_effort="high"`, "review"}
		assertArgs(t, expected, args)
	})

	t.Run("resume interactive keeps effort config", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:    "continue review",
			SessionID: "thread-abc",
			Effort:    "medium",
			Headless:  false,
		})
		expected := []string{"codex", "resume", "--no-alt-screen", "-c", `model_reasoning_effort="medium"`, "thread-abc", "continue review"}
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
		expected := []string{"codex", "--dangerously-bypass-approvals-and-sandbox", "exec", "--json", "do something"}
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

	t.Run("interactive does not include permission bypass flags", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:   "review",
			Headless: false,
		})
		for i, a := range args {
			if a == "-a" && i+1 < len(args) && args[i+1] == "never" {
				t.Fatalf("did not expect -a never for interactive mode, got %v", args)
			}
			if a == "--dangerously-bypass-approvals-and-sandbox" {
				t.Fatalf("did not expect sandbox bypass for interactive mode, got %v", args)
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

	t.Run("discover headless session prefers JSONL over preset", func(t *testing.T) {
		output := `{"type":"thread.started","thread_id":"019df0cd-7acc-7813-9c4e-180741b19f5f"}`
		id := adapter.DiscoverSessionID(&DiscoverOptions{
			ProcessOutput: output,
			PresetID:      "rollout-2026-05-03T22-18-42-019df0c7-daf4-7120-b587-0731815d36cb",
			Headless:      true,
		})
		if id != "019df0cd-7acc-7813-9c4e-180741b19f5f" {
			t.Fatalf("expected JSONL thread id, got %q", id)
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

	t.Run("matches interactive session cwd from payload metadata", func(t *testing.T) {
		sessionFile := filepath.Join(t.TempDir(), "session.jsonl")
		cwd := "/repo/worktree"
		data := `{"type":"session_meta","payload":{"id":"session-abc","cwd":"/repo/worktree"}}` + "\n"
		if err := os.WriteFile(sessionFile, []byte(data), 0o600); err != nil {
			t.Fatalf("write session fixture: %v", err)
		}

		if !matchesSessionCwd(sessionFile, cwd) {
			t.Fatal("expected session_meta payload cwd to match")
		}
	})

	t.Run("discover interactive session returns session_meta id", func(t *testing.T) {
		fakeHome := t.TempDir()
		cwd := t.TempDir()
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
			t.Fatalf("chdir: %v", err)
		}

		sessionDir := filepath.Join(fakeHome, ".codex", "sessions", "2026", "05", "03")
		if err := os.MkdirAll(sessionDir, 0o700); err != nil {
			t.Fatalf("mkdir session dir: %v", err)
		}
		sessionFile := filepath.Join(sessionDir, "rollout-2026-05-03T22-18-42-019df0c7-daf4-7120-b587-0731815d36cb.jsonl")
		data := fmt.Sprintf(`{"type":"session_meta","payload":{"id":"019df0c7-daf4-7120-b587-0731815d36cb","cwd":%q}}`, canonCwd) + "\n"
		if err := os.WriteFile(sessionFile, []byte(data), 0o600); err != nil {
			t.Fatalf("write session fixture: %v", err)
		}

		id := adapter.DiscoverSessionID(&DiscoverOptions{
			SpawnTime: time.Now().Add(-time.Second),
			Headless:  false,
		})
		if id != "019df0c7-daf4-7120-b587-0731815d36cb" {
			t.Fatalf("expected session_meta id, got %q", id)
		}
	})

	t.Run("implements OutputFilter interface", func(t *testing.T) {
		var a Adapter = adapter
		if _, ok := a.(OutputFilter); !ok {
			t.Fatal("expected CodexAdapter to implement OutputFilter")
		}
	})

	t.Run("FilterOutput extracts agent message text from JSONL", func(t *testing.T) {
		output := `{"type":"thread.started","thread_id":"019dc6a3-68a4-7751-8c3a-43c3c84a24ba"}` + "\n" +
			`{"type":"turn.started"}` + "\n" +
			`{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"codex headless smoke ok."}}` + "\n" +
			`{"type":"turn.completed","usage":{"input_tokens":2521,"cached_input_tokens":2432,"output_tokens":3,"reasoning_output_tokens":19}}` + "\n"
		got := adapter.FilterOutput(output)
		if got != "codex headless smoke ok." {
			t.Fatalf("expected filtered Codex response, got %q", got)
		}
	})

	t.Run("FilterOutput extracts codex error events", func(t *testing.T) {
		output := `{"type":"thread.started","thread_id":"019dc6bc-d6c4-7a13-bd75-6aab9fd8b457"}` + "\n" +
			`{"type":"turn.started"}` + "\n" +
			`{"type":"error","message":"You've hit your usage limit."}` + "\n" +
			`{"type":"turn.failed","error":{"message":"You've hit your usage limit."}}` + "\n"
		got := adapter.FilterOutput(output)
		if got != "You've hit your usage limit." {
			t.Fatalf("expected filtered Codex error, got %q", got)
		}
	})

	t.Run("WrapStdout filters JSONL to plain text", func(t *testing.T) {
		var buf bytes.Buffer
		w := adapter.WrapStdout(&buf)
		input := `{"type":"thread.started","thread_id":"019dc6a3-68a4-7751-8c3a-43c3c84a24ba"}` + "\n" +
			`{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"codex headless smoke ok."}}` + "\n" +
			`{"type":"turn.completed","usage":{"input_tokens":2521}}` + "\n"
		if _, err := w.Write([]byte(input)); err != nil {
			t.Fatalf("unexpected write error: %v", err)
		}
		if got := buf.String(); got != "codex headless smoke ok." {
			t.Fatalf("expected filtered Codex response, got %q", got)
		}
	})

	t.Run("implements StderrWrapper interface", func(t *testing.T) {
		var a Adapter = adapter
		if _, ok := a.(StderrWrapper); !ok {
			t.Fatal("expected CodexAdapter to implement StderrWrapper")
		}
	})

	t.Run("WrapStderr suppresses rollout recording warning", func(t *testing.T) {
		var buf bytes.Buffer
		w := adapter.WrapStderr(&buf)
		input := "debug: kept\n" +
			"Reading additional input from stdin...\n" +
			"2026-04-25T21:54:58.585861Z ERROR codex_core::session: failed to record rollout items: thread 019dc6a3-68a4-7751-8c3a-43c3c84a24ba not found\n" +
			"fatal: kept\n"
		if _, err := w.Write([]byte(input)); err != nil {
			t.Fatalf("unexpected write error: %v", err)
		}
		if got := buf.String(); got != "debug: kept\nfatal: kept\n" {
			t.Fatalf("expected rollout warning filtered from live stderr, got %q", got)
		}
	})

	t.Run("WrapStderr preserves apply_patch verification diagnostics", func(t *testing.T) {
		var buf bytes.Buffer
		w := adapter.WrapStderr(&buf)
		input := "debug: kept\n" +
			"2026-04-26T01:38:46.042347Z ERROR codex_core::tools::router: error=apply_patch verification failed: Failed to find expected lines in /repo/src/core/run-executor-helpers.ts:\n" +
			"  effectiveBaseBranch: string;\n" +
			"}\n" +
			"fatal: kept\n"
		if _, err := w.Write([]byte(input)); err != nil {
			t.Fatalf("unexpected write error: %v", err)
		}
		if got := buf.String(); got != input {
			t.Fatalf("expected apply_patch diagnostic to remain in live stderr, got %q", got)
		}
	})

	t.Run("rollout recording error is ignored after completed headless turn", func(t *testing.T) {
		stdout := `{"type":"turn.completed","usage":{"input_tokens":2521}}` + "\n"
		stderr := "Reading additional input from stdin...\n2026-04-25T21:54:58.585861Z ERROR codex_core::session: failed to record rollout items: thread 019dc6a3-68a4-7751-8c3a-43c3c84a24ba not found"
		exitCode, filteredStderr := adapter.FilterHeadlessResult(1, stdout, stderr)
		if exitCode != 0 {
			t.Fatalf("expected exit code normalized to 0, got %d", exitCode)
		}
		if filteredStderr != "" {
			t.Fatalf("expected stderr to be filtered, got %q", filteredStderr)
		}
	})

	t.Run("apply_patch verification diagnostic is ignored after successful headless turn", func(t *testing.T) {
		stdout := `{"type":"turn.completed","usage":{"input_tokens":2521}}` + "\n"
		stderr := "2026-04-26T01:38:46.042347Z ERROR codex_core::tools::router: error=apply_patch verification failed: Failed to find expected lines in /repo/src/core/run-executor-helpers.ts:\n" +
			"  effectiveBaseBranch: string;\n" +
			"}"
		exitCode, filteredStderr := adapter.FilterHeadlessResult(0, stdout, stderr)
		if exitCode != 0 {
			t.Fatalf("expected exit code to remain 0, got %d", exitCode)
		}
		if filteredStderr != "" {
			t.Fatalf("expected apply_patch diagnostic to be filtered, got %q", filteredStderr)
		}
	})

	t.Run("apply_patch verification diagnostic is preserved when headless turn fails", func(t *testing.T) {
		stdout := `{"type":"turn.failed","error":{"message":"failed"}}` + "\n"
		stderr := "2026-04-26T01:38:46.042347Z ERROR codex_core::tools::router: error=apply_patch verification failed: Failed to find expected lines in /repo/src/core/run-executor-helpers.ts:\n" +
			"  effectiveBaseBranch: string;\n" +
			"}"
		exitCode, filteredStderr := adapter.FilterHeadlessResult(1, stdout, stderr)
		if exitCode != 1 {
			t.Fatalf("expected exit code to remain 1, got %d", exitCode)
		}
		if filteredStderr != stderr {
			t.Fatalf("expected apply_patch diagnostic to remain on failure, got %q", filteredStderr)
		}
	})

	t.Run("ignored diagnostics do not hide failure without completed turn", func(t *testing.T) {
		stderr := "Reading additional input from stdin...\ncodex_core::session: failed to record rollout items: thread 019dc6a3-68a4-7751-8c3a-43c3c84a24ba not found"
		exitCode, filteredStderr := adapter.FilterHeadlessResult(1, "", stderr)
		if exitCode != 1 {
			t.Fatalf("expected exit code to remain 1, got %d", exitCode)
		}
		if filteredStderr != "" {
			t.Fatalf("expected ignored stderr diagnostics to be filtered, got %q", filteredStderr)
		}
	})

	t.Run("rollout recording error does not hide unrelated stderr", func(t *testing.T) {
		stdout := `{"type":"turn.completed","usage":{"input_tokens":2521}}` + "\n"
		stderr := "codex_core::session: failed to record rollout items: thread missing not found\nfatal: unrelated"
		exitCode, filteredStderr := adapter.FilterHeadlessResult(1, stdout, stderr)
		if exitCode != 1 {
			t.Fatalf("expected exit code to remain 1 with unrelated stderr, got %d", exitCode)
		}
		if filteredStderr != "fatal: unrelated" {
			t.Fatalf("expected unrelated stderr to remain, got %q", filteredStderr)
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

	t.Run("fresh interactive copilot step uses interactive prompt flag", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:   "review code",
			Headless: false,
		})
		expected := []string{"copilot", "-i", "review code"}
		assertArgs(t, expected, args)
	})

	t.Run("fresh interactive copilot step includes model and effort", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:   "review code",
			Model:    "gpt-5.2",
			Effort:   "high",
			Headless: false,
		})
		expected := []string{"copilot", "-i", "review code", "--model", "gpt-5.2", "--reasoning-effort", "high"}
		assertArgs(t, expected, args)
	})

	t.Run("resume interactive copilot step includes resume and omits model", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:    "continue",
			SessionID: "session-abc",
			Resume:    true,
			Model:     "gpt-5.2",
			Effort:    "high",
			Headless:  false,
		})
		expected := []string{"copilot", "-i", "continue", "--resume=session-abc"}
		assertArgs(t, expected, args)
	})

	t.Run("interactive copilot step omits permission and headless flags", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:          "review code",
			Headless:        false,
			DisallowedTools: []string{"AskUserQuestion"},
		})
		for _, disallowed := range []string{"--allow-all", "--autopilot", "-s", "-p", "--no-ask-user"} {
			if containsString(args, disallowed) {
				t.Fatalf("did not expect %s in interactive args, got %v", disallowed, args)
			}
		}
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

	t.Run("does not implement InteractiveRejector", func(t *testing.T) {
		var a Adapter = adapter
		if _, ok := a.(InteractiveRejector); ok {
			t.Fatal("did not expect CopilotAdapter to implement InteractiveRejector")
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

	t.Run("fresh interactive cursor step uses positional prompt", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:   "review code",
			Headless: false,
		})
		expected := []string{"agent", "review code"}
		assertArgs(t, expected, args)
	})

	t.Run("fresh interactive cursor step includes model", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:   "review code",
			Model:    "gpt-5.3-codex",
			Headless: false,
		})
		expected := []string{"agent", "--model", "gpt-5.3-codex", "review code"}
		assertArgs(t, expected, args)
	})

	t.Run("resume interactive cursor step includes resume and omits model", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:    "continue",
			SessionID: "chat-abc",
			Resume:    true,
			Model:     "gpt-5.3-codex",
			Headless:  false,
		})
		expected := []string{"agent", "--resume=chat-abc", "continue"}
		assertArgs(t, expected, args)
	})

	t.Run("interactive cursor step omits permission and headless flags", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:          "review code",
			Headless:        false,
			DisallowedTools: []string{"AskUserQuestion"},
		})
		for _, disallowed := range []string{"-p", "--output-format", "stream-json", "--trust", "--force"} {
			if containsString(args, disallowed) {
				t.Fatalf("did not expect %s in interactive args, got %v", disallowed, args)
			}
		}
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

	t.Run("discover session ID logs scanner failures", func(t *testing.T) {
		var logBuf bytes.Buffer
		origWriter := log.Writer()
		origFlags := log.Flags()
		log.SetOutput(&logBuf)
		log.SetFlags(0)
		t.Cleanup(func() {
			log.SetOutput(origWriter)
			log.SetFlags(origFlags)
		})

		output := `{"type":"assistant","payload":"` + strings.Repeat("x", 2*1024*1024) + `","session_id":"chat-too-long","message":{}}`
		id := adapter.DiscoverSessionID(&DiscoverOptions{
			ProcessOutput: output,
			Headless:      true,
		})
		if id != "" {
			t.Fatalf("expected empty string on scanner failure, got %q", id)
		}
		logged := logBuf.String()
		if !strings.Contains(logged, "failed to scan cursor session output") {
			t.Fatalf("expected scanner failure log, got %q", logged)
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

	t.Run("discover interactive session ID returns empty even when cursor chat matches workdir", func(t *testing.T) {
		fakeHome := t.TempDir()
		t.Setenv("HOME", fakeHome)

		spawnTime := time.Now().Add(-10 * time.Second)
		wrongWorkspaceID := "11111111-1111-1111-1111-111111111111"
		matchingID := "22222222-2222-2222-2222-222222222222"
		beforeID := "33333333-3333-3333-3333-333333333333"
		workdir := t.TempDir()
		otherWorkdir := t.TempDir()
		writeCursorStoreDB(t, fakeHome, "workspace-a", wrongWorkspaceID, time.Now().Add(-1*time.Second), otherWorkdir)
		writeCursorStoreDB(t, fakeHome, "workspace-b", matchingID, time.Now().Add(-5*time.Second), workdir)
		writeCursorStoreDB(t, fakeHome, "workspace-c", beforeID, spawnTime.Add(-time.Second), workdir)

		id := adapter.DiscoverSessionID(&DiscoverOptions{
			SpawnTime: spawnTime,
			Headless:  false,
			Workdir:   workdir,
		})
		if id != "" {
			t.Fatalf("expected empty string without verified Cursor session provenance, got %q", id)
		}
	})

	t.Run("discover interactive session ID returns empty when no cursor chats match", func(t *testing.T) {
		fakeHome := t.TempDir()
		t.Setenv("HOME", fakeHome)
		writeCursorStoreDB(t, fakeHome, "workspace-a", "11111111-1111-1111-1111-111111111111", time.Now().Add(-10*time.Second), t.TempDir())

		id := adapter.DiscoverSessionID(&DiscoverOptions{
			SpawnTime: time.Now(),
			Headless:  false,
			Workdir:   t.TempDir(),
		})
		if id != "" {
			t.Fatalf("expected empty string, got %q", id)
		}
	})

	t.Run("discover interactive session ID returns empty when cursor chats are ambiguous", func(t *testing.T) {
		fakeHome := t.TempDir()
		t.Setenv("HOME", fakeHome)
		workdir := t.TempDir()
		spawnTime := time.Now().Add(-10 * time.Second)
		writeCursorStoreDB(t, fakeHome, "workspace-a", "11111111-1111-1111-1111-111111111111", time.Now().Add(-2*time.Second), workdir)
		writeCursorStoreDB(t, fakeHome, "workspace-b", "22222222-2222-2222-2222-222222222222", time.Now().Add(-1*time.Second), workdir)

		id := adapter.DiscoverSessionID(&DiscoverOptions{
			SpawnTime: spawnTime,
			Headless:  false,
			Workdir:   workdir,
		})
		if id != "" {
			t.Fatalf("expected empty string for ambiguous cursor chats, got %q", id)
		}
	})

	t.Run("does not implement InteractiveRejector", func(t *testing.T) {
		var a Adapter = adapter
		if _, ok := a.(InteractiveRejector); ok {
			t.Fatal("did not expect CursorAdapter to implement InteractiveRejector")
		}
	})

	t.Run("implements OutputFilter interface", func(t *testing.T) {
		var a Adapter = adapter
		if _, ok := a.(OutputFilter); !ok {
			t.Fatal("expected CursorAdapter to implement OutputFilter")
		}
	})

	t.Run("FilterOutput extracts result text from stream-json", func(t *testing.T) {
		output := `{"type":"system","subtype":"init","session_id":"abc","model":"composer-1.5","cwd":"/tmp","permissionMode":"default"}` + "\n" +
			`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"hello"}]},"session_id":"abc"}` + "\n" +
			`{"type":"result","subtype":"success","result":"The answer is 42","session_id":"abc","is_error":false}` + "\n"
		got := adapter.FilterOutput(output)
		if got != "The answer is 42" {
			t.Fatalf("expected %q, got %q", "The answer is 42", got)
		}
	})

	t.Run("FilterOutput returns empty when no result event", func(t *testing.T) {
		output := `{"type":"system","subtype":"init","session_id":"abc"}` + "\n" +
			`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"hello"}]},"session_id":"abc"}` + "\n"
		got := adapter.FilterOutput(output)
		if got != "" {
			t.Fatalf("expected empty string, got %q", got)
		}
	})

	t.Run("FilterOutput returns empty for empty output", func(t *testing.T) {
		got := adapter.FilterOutput("")
		if got != "" {
			t.Fatalf("expected empty string, got %q", got)
		}
	})

	t.Run("FilterOutput handles result with empty string", func(t *testing.T) {
		output := `{"type":"result","subtype":"success","result":"","session_id":"abc","is_error":false}` + "\n"
		got := adapter.FilterOutput(output)
		if got != "" {
			t.Fatalf("expected empty string for empty result, got %q", got)
		}
	})

	t.Run("WrapStdout filters stream-json to plain text", func(t *testing.T) {
		var buf bytes.Buffer
		w := adapter.WrapStdout(&buf)

		input := `{"type":"system","subtype":"init","session_id":"abc","model":"composer-1.5"}` + "\n" +
			`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"hello"}]},"session_id":"abc"}` + "\n" +
			`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"The answer"}]},"session_id":"abc"}` + "\n" +
			`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"The answer is 42"}]},"session_id":"abc"}` + "\n" +
			`{"type":"result","subtype":"success","result":"The answer is 42","session_id":"abc","is_error":false}` + "\n"

		_, err := w.Write([]byte(input))
		if err != nil {
			t.Fatalf("unexpected write error: %v", err)
		}
		if buf.String() != "The answer is 42" {
			t.Fatalf("expected %q, got %q", "The answer is 42", buf.String())
		}
	})

	t.Run("WrapStdout produces no output for non-assistant events", func(t *testing.T) {
		var buf bytes.Buffer
		w := adapter.WrapStdout(&buf)

		input := `{"type":"system","subtype":"init","session_id":"abc"}` + "\n" +
			`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"hello"}]},"session_id":"abc"}` + "\n"

		_, _ = w.Write([]byte(input))
		if buf.String() != "" {
			t.Fatalf("expected empty output, got %q", buf.String())
		}
	})

	t.Run("WrapStdout handles chunked writes", func(t *testing.T) {
		var buf bytes.Buffer
		w := adapter.WrapStdout(&buf)

		line := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"hello world"}]},"session_id":"abc"}` + "\n"
		// Write in two chunks, splitting mid-line
		mid := len(line) / 2
		_, _ = w.Write([]byte(line[:mid]))
		_, _ = w.Write([]byte(line[mid:]))
		if buf.String() != "hello world" {
			t.Fatalf("expected %q, got %q", "hello world", buf.String())
		}
	})

	t.Run("WrapStdout flushes final unterminated line on close", func(t *testing.T) {
		var buf bytes.Buffer
		w := adapter.WrapStdout(&buf)

		line := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"final answer"}]},"session_id":"abc"}`
		_, _ = w.Write([]byte(line))
		closer, ok := w.(io.Closer)
		if !ok {
			t.Fatal("expected stdout wrapper to implement io.Closer")
		}
		if err := closer.Close(); err != nil {
			t.Fatalf("unexpected close error: %v", err)
		}
		if buf.String() != "final answer" {
			t.Fatalf("expected %q, got %q", "final answer", buf.String())
		}
	})

	t.Run("WrapStdout returns downstream write errors", func(t *testing.T) {
		writeErr := errors.New("write failed")
		w := adapter.WrapStdout(errorWriter{err: writeErr})

		line := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"hello"}]},"session_id":"abc"}` + "\n"
		n, err := w.Write([]byte(line))
		if !errors.Is(err, writeErr) {
			t.Fatalf("expected write error %v, got %v", writeErr, err)
		}
		if n != len(line) {
			t.Fatalf("Write returned n=%d, want %d", n, len(line))
		}
		if _, err := w.Write([]byte(line)); !errors.Is(err, writeErr) {
			t.Fatalf("expected stored write error on subsequent Write, got %v", err)
		}
	})

	t.Run("WrapStdout returns close errors from final line flush", func(t *testing.T) {
		writeErr := errors.New("close write failed")
		w := adapter.WrapStdout(errorWriter{err: writeErr})

		line := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"hello"}]},"session_id":"abc"}`
		_, _ = w.Write([]byte(line))
		closer := w.(io.Closer)
		if err := closer.Close(); !errors.Is(err, writeErr) {
			t.Fatalf("expected close error %v, got %v", writeErr, err)
		}
	})
}

type errorWriter struct {
	err error
}

func (w errorWriter) Write([]byte) (int, error) {
	return 0, w.err
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

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func writeCursorStoreDB(t *testing.T, home, workspaceHash, chatID string, modTime time.Time, workdir string) {
	t.Helper()
	path := filepath.Join(home, ".cursor", "chats", workspaceHash, chatID, "store.db")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir cursor chat dir: %v", err)
	}
	content := []byte("Workspace Path: " + workdir + "\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write cursor store.db: %v", err)
	}
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatalf("chtimes cursor store.db: %v", err)
	}
}
