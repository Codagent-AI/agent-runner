package cli

import (
	"bytes"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOpenCodeAdapter(t *testing.T) {
	adapter := &OpenCodeAdapter{}

	t.Run("fresh headless opencode step", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:   "do something",
			Headless: true,
		})
		expected := []string{"opencode", "run", "--format", "json", "--dangerously-skip-permissions", "do something"}
		assertArgs(t, expected, args)
	})

	t.Run("fresh interactive opencode step uses prompt flag", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:   "review code",
			Headless: false,
		})
		expected := []string{"opencode", "--prompt", "review code"}
		assertArgs(t, expected, args)
	})

	t.Run("fresh headless includes model and variant", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:   "do something",
			Model:    "anthropic/claude-opus-4",
			Effort:   "max",
			Headless: true,
		})
		expected := []string{"opencode", "run", "--format", "json", "--dangerously-skip-permissions", "--model", "anthropic/claude-opus-4", "--variant", "max", "do something"}
		assertArgs(t, expected, args)
	})

	t.Run("fresh interactive includes model and drops variant", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:   "review code",
			Model:    "anthropic/claude-sonnet-4",
			Effort:   "high",
			Headless: false,
		})
		expected := []string{"opencode", "--prompt", "review code", "--model", "anthropic/claude-sonnet-4"}
		assertArgs(t, expected, args)
		if containsString(args, "--variant") {
			t.Fatalf("did not expect --variant in interactive args, got %v", args)
		}
	})

	t.Run("resume headless includes session and omits model and variant", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:    "continue",
			SessionID: "ses_abc123",
			Resume:    true,
			Model:     "anthropic/claude-opus-4",
			Effort:    "max",
			Headless:  true,
		})
		expected := []string{"opencode", "run", "--format", "json", "--dangerously-skip-permissions", "-s", "ses_abc123", "continue"}
		assertArgs(t, expected, args)
	})

	t.Run("resume interactive includes session and omits model and variant", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:    "continue review",
			SessionID: "ses_def456",
			Resume:    true,
			Model:     "anthropic/claude-opus-4",
			Effort:    "max",
			Headless:  false,
		})
		expected := []string{"opencode", "--prompt", "continue review", "-s", "ses_def456"}
		assertArgs(t, expected, args)
	})

	t.Run("interactive omits permission and headless-only flags", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:          "review code",
			Headless:        false,
			DisallowedTools: []string{"AskUserQuestion"},
		})
		for _, disallowed := range []string{"run", "--format", "json", "--dangerously-skip-permissions", "--variant"} {
			if containsString(args, disallowed) {
				t.Fatalf("did not expect %s in interactive args, got %v", disallowed, args)
			}
		}
	})

	t.Run("disallowed tools do not affect args", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:          "do something",
			Headless:        true,
			DisallowedTools: []string{"AskUserQuestion"},
		})
		expected := []string{"opencode", "run", "--format", "json", "--dangerously-skip-permissions", "do something"}
		assertArgs(t, expected, args)
	})

	t.Run("does not support system prompt", func(t *testing.T) {
		if adapter.SupportsSystemPrompt() {
			t.Fatal("expected OpenCode adapter to not support system prompt")
		}
	})

	t.Run("system prompt is ignored by adapter", func(t *testing.T) {
		args := adapter.BuildArgs(&BuildArgsInput{
			Prompt:       "do something",
			SystemPrompt: "should be ignored",
			Headless:     true,
		})
		expected := []string{"opencode", "run", "--format", "json", "--dangerously-skip-permissions", "do something"}
		assertArgs(t, expected, args)
	})

	t.Run("discover headless session ID from jsonl", func(t *testing.T) {
		output := "not json\n" +
			`{"type":"text","part":{"type":"text","text":"hello"}}` + "\n" +
			`{"type":"step_finish","sessionID":"ses_abc123"}` + "\n"
		id := adapter.DiscoverSessionID(&DiscoverOptions{
			ProcessOutput: output,
			Headless:      true,
		})
		if id != "ses_abc123" {
			t.Fatalf("expected %q, got %q", "ses_abc123", id)
		}
	})

	t.Run("discover headless session ID returns first non-empty sessionID", func(t *testing.T) {
		output := `{"type":"step_start","sessionID":""}` + "\n" +
			`{"type":"text","sessionID":"ses_first","part":{"type":"text","text":"hello"}}` + "\n" +
			`{"type":"step_finish","sessionID":"ses_second"}` + "\n"
		id := adapter.DiscoverSessionID(&DiscoverOptions{
			ProcessOutput: output,
			Headless:      true,
		})
		if id != "ses_first" {
			t.Fatalf("expected %q, got %q", "ses_first", id)
		}
	})

	t.Run("discover headless session ID returns empty when absent", func(t *testing.T) {
		id := adapter.DiscoverSessionID(&DiscoverOptions{
			ProcessOutput: `{"type":"text","part":{"type":"text","text":"hello"}}`,
			Headless:      true,
		})
		if id != "" {
			t.Fatalf("expected empty string, got %q", id)
		}
	})

	t.Run("discover headless session ID handles large jsonl lines", func(t *testing.T) {
		output := `{"type":"text","payload":"` + strings.Repeat("x", 2*1024*1024) + `","sessionID":"ses_large"}`
		id := adapter.DiscoverSessionID(&DiscoverOptions{
			ProcessOutput: output,
			Headless:      true,
		})
		if id != "ses_large" {
			t.Fatalf("expected %q, got %q", "ses_large", id)
		}
	})

	t.Run("discover interactive session ID from single post-spawn session diff", func(t *testing.T) {
		fakeHome := t.TempDir()
		t.Setenv("HOME", fakeHome)
		spawnTime := time.Now().Add(-10 * time.Second)

		writeOpenCodeSessionDiff(t, fakeHome, "ses_old", spawnTime.Add(-time.Second))
		writeOpenCodeSessionDiff(t, fakeHome, "ses_new", time.Now().Add(-time.Second))

		id := adapter.DiscoverSessionID(&DiscoverOptions{
			SpawnTime: spawnTime,
			Headless:  false,
		})
		if id != "ses_new" {
			t.Fatalf("expected %q, got %q", "ses_new", id)
		}
	})

	t.Run("discover interactive session ID ignores non-session files", func(t *testing.T) {
		fakeHome := t.TempDir()
		t.Setenv("HOME", fakeHome)
		dir := filepath.Join(fakeHome, ".local", "share", "opencode", "storage", "session_diff")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("mkdir session diff dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "not_session.json"), []byte("{}"), 0o600); err != nil {
			t.Fatalf("write non-session file: %v", err)
		}

		id := adapter.DiscoverSessionID(&DiscoverOptions{
			SpawnTime: time.Now().Add(-time.Second),
			Headless:  false,
		})
		if id != "" {
			t.Fatalf("expected empty string, got %q", id)
		}
	})

	t.Run("discover interactive session ID refuses ambiguous candidates", func(t *testing.T) {
		var logBuf bytes.Buffer
		origWriter := log.Writer()
		origFlags := log.Flags()
		log.SetOutput(&logBuf)
		log.SetFlags(0)
		t.Cleanup(func() {
			log.SetOutput(origWriter)
			log.SetFlags(origFlags)
		})

		fakeHome := t.TempDir()
		t.Setenv("HOME", fakeHome)
		spawnTime := time.Now().Add(-10 * time.Second)
		writeOpenCodeSessionDiff(t, fakeHome, "ses_one", time.Now().Add(-2*time.Second))
		writeOpenCodeSessionDiff(t, fakeHome, "ses_two", time.Now().Add(-1*time.Second))

		id := adapter.DiscoverSessionID(&DiscoverOptions{
			SpawnTime: spawnTime,
			Headless:  false,
		})
		if id != "" {
			t.Fatalf("expected empty session for ambiguous candidates, got %q", id)
		}
		if !strings.Contains(logBuf.String(), "opencode: 2 session candidates") {
			t.Fatalf("expected ambiguity log, got %q", logBuf.String())
		}
	})

	t.Run("does not implement InteractiveRejector", func(t *testing.T) {
		var a Adapter = adapter
		if _, ok := a.(InteractiveRejector); ok {
			t.Fatal("did not expect OpenCodeAdapter to implement InteractiveRejector")
		}
	})

	t.Run("implements OutputFilter interface", func(t *testing.T) {
		var a Adapter = adapter
		if _, ok := a.(OutputFilter); !ok {
			t.Fatal("expected OpenCodeAdapter to implement OutputFilter")
		}
	})

	t.Run("FilterOutput extracts text event parts", func(t *testing.T) {
		output := `{"type":"text","sessionID":"ses_abc","part":{"type":"text","text":"Hello"}}` + "\n" +
			`{"type":"text","sessionID":"ses_abc","part":{"type":"tool","text":"ignored"}}` + "\n" +
			`{"type":"text","sessionID":"ses_abc","part":{"type":"text","text":" world"}}` + "\n" +
			`{"type":"step_finish","sessionID":"ses_abc"}` + "\n"
		got := adapter.FilterOutput(output)
		if got != "Hello world" {
			t.Fatalf("expected filtered OpenCode response, got %q", got)
		}
	})

	t.Run("FilterOutput extracts text from large jsonl lines", func(t *testing.T) {
		longText := strings.Repeat("x", 2*1024*1024)
		output := `{"type":"text","sessionID":"ses_abc","part":{"type":"text","text":"` + longText + `"}}` + "\n"
		got := adapter.FilterOutput(output)
		if got != longText {
			t.Fatalf("expected large OpenCode response length %d, got %d", len(longText), len(got))
		}
	})

	t.Run("FilterOutput returns empty when no text events", func(t *testing.T) {
		got := adapter.FilterOutput(`{"type":"step_finish","sessionID":"ses_abc"}`)
		if got != "" {
			t.Fatalf("expected empty string, got %q", got)
		}
	})

	t.Run("WrapStdout filters jsonl to plain text", func(t *testing.T) {
		var buf bytes.Buffer
		w := adapter.WrapStdout(&buf)

		input := `{"type":"step_start","sessionID":"ses_abc"}` + "\n" +
			`{"type":"text","sessionID":"ses_abc","part":{"type":"text","text":"Hello"}}` + "\n" +
			`{"type":"text","sessionID":"ses_abc","part":{"type":"text","text":" world"}}` + "\n" +
			`{"type":"step_finish","sessionID":"ses_abc"}` + "\n"
		if _, err := w.Write([]byte(input)); err != nil {
			t.Fatalf("unexpected write error: %v", err)
		}
		if got := buf.String(); got != "Hello world" {
			t.Fatalf("expected filtered OpenCode response, got %q", got)
		}
	})

	t.Run("WrapStdout handles chunked writes", func(t *testing.T) {
		var buf bytes.Buffer
		w := adapter.WrapStdout(&buf)

		line := `{"type":"text","sessionID":"ses_abc","part":{"type":"text","text":"hello world"}}` + "\n"
		mid := len(line) / 2
		_, _ = w.Write([]byte(line[:mid]))
		_, _ = w.Write([]byte(line[mid:]))
		if got := buf.String(); got != "hello world" {
			t.Fatalf("expected %q, got %q", "hello world", got)
		}
	})

	t.Run("WrapStdout flushes final unterminated line on close", func(t *testing.T) {
		var buf bytes.Buffer
		w := adapter.WrapStdout(&buf)

		line := `{"type":"text","sessionID":"ses_abc","part":{"type":"text","text":"final answer"}}`
		_, _ = w.Write([]byte(line))
		closer, ok := w.(io.Closer)
		if !ok {
			t.Fatal("expected stdout wrapper to implement io.Closer")
		}
		if err := closer.Close(); err != nil {
			t.Fatalf("unexpected close error: %v", err)
		}
		if got := buf.String(); got != "final answer" {
			t.Fatalf("expected %q, got %q", "final answer", got)
		}
	})

	t.Run("WrapStdout returns downstream write errors", func(t *testing.T) {
		writeErr := errors.New("write failed")
		w := adapter.WrapStdout(errorWriter{err: writeErr})

		line := `{"type":"text","sessionID":"ses_abc","part":{"type":"text","text":"hello"}}` + "\n"
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
}

func writeOpenCodeSessionDiff(t *testing.T, home, sessionID string, modTime time.Time) {
	t.Helper()
	path := filepath.Join(home, ".local", "share", "opencode", "storage", "session_diff", sessionID+".json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir opencode session diff dir: %v", err)
	}
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write opencode session diff: %v", err)
	}
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatalf("chtimes opencode session diff: %v", err)
	}
}
