package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRegisteredAdaptersImplementTurnDurabilityProbe(t *testing.T) {
	for _, name := range KnownCLIs() {
		adapter, err := Get(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := adapter.(TurnDurabilityProbe); !ok {
			t.Fatalf("adapter %q does not implement TurnDurabilityProbe", name)
		}
	}
}

func TestClaudeDurabilityProbeFindsCompletedAssistantAfterCheckpoint(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".claude", "projects", "-repo", "claude-session.jsonl")
	copyFixture(t, "testdata/durability/claude/session.jsonl", path)
	probe := &ClaudeAdapter{}
	checkpoint, err := probe.Checkpoint("claude-session")
	if err != nil {
		t.Fatal(err)
	}
	appendFixture(t, path, "testdata/durability/claude/intermediate.jsonl")
	appendFixtureAfter(t, path, "testdata/durability/claude/committed.jsonl")
	waitForProbe(t, probe, "claude-session", checkpoint)
}

func TestCodexDurabilityProbeRequiresTaskCompleteAfterCheckpoint(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".codex", "sessions", "2026", "07", "16", "rollout-codex-session.jsonl")
	copyFixture(t, "testdata/durability/codex/session.jsonl", path)
	probe := &CodexAdapter{}
	checkpoint, err := probe.Checkpoint("codex-session")
	if err != nil {
		t.Fatal(err)
	}
	appendFixture(t, path, "testdata/durability/codex/intermediate.jsonl")
	appendFixtureAfter(t, path, "testdata/durability/codex/committed.jsonl")
	waitForProbe(t, probe, "codex-session", checkpoint)
}

func TestCopilotDurabilityProbeRequiresTurnEndAfterCheckpoint(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".copilot", "session-state", "copilot-session", "events.jsonl")
	copyFixture(t, "testdata/durability/copilot/events.jsonl", path)
	probe := &CopilotAdapter{}
	checkpoint, err := probe.Checkpoint("copilot-session")
	if err != nil {
		t.Fatal(err)
	}
	appendFixture(t, path, "testdata/durability/copilot/intermediate.jsonl")
	appendFixtureAfter(t, path, "testdata/durability/copilot/committed.jsonl")
	waitForProbe(t, probe, "copilot-session", checkpoint)
}

func TestCursorDurabilityProbeFindsNewStoredAssistantMessage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".cursor", "chats", "workspace", "cursor-session", "store.db")
	copyFixture(t, "testdata/durability/cursor/store.db", path)
	probe := &CursorAdapter{}
	checkpoint, err := probe.Checkpoint("cursor-session")
	if err != nil {
		t.Fatal(err)
	}
	replaceFixture(t, path, "testdata/durability/cursor/store-intermediate.db")
	replaceFixtureAfter(t, path, "testdata/durability/cursor/store-committed.db")
	waitForProbe(t, probe, "cursor-session", checkpoint)
}

func TestOpenCodeDurabilityProbeRequiresCompletedFinalAssistantMessage(t *testing.T) {
	baseline := readFixture(t, "testdata/durability/opencode/baseline.json")
	toolCall := readFixture(t, "testdata/durability/opencode/tool-call.json")
	committed := readFixture(t, "testdata/durability/opencode/committed.json")
	var calls atomic.Int32
	probe := &OpenCodeAdapter{runDBQuery: func(string) ([]byte, error) {
		switch calls.Add(1) {
		case 1:
			return baseline, nil
		case 2:
			return toolCall, nil
		default:
			return committed, nil
		}
	}}
	checkpoint, err := probe.Checkpoint("opencode-session")
	if err != nil {
		t.Fatal(err)
	}
	waitForProbe(t, probe, "opencode-session", checkpoint)
}

func TestOpenCodeDurabilityProbeBacksOffDatabaseQueries(t *testing.T) {
	baseline := readFixture(t, "testdata/durability/opencode/baseline.json")
	var calls atomic.Int32
	probe := &OpenCodeAdapter{runDBQuery: func(string) ([]byte, error) {
		calls.Add(1)
		return baseline, nil
	}}
	checkpoint, err := probe.Checkpoint("opencode-session")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()
	if err := probe.WaitForCommittedTurn(ctx, "opencode-session", checkpoint); err == nil {
		t.Fatal("WaitForCommittedTurn unexpectedly found a committed turn")
	}
	if got := calls.Load(); got > 2 {
		t.Fatalf("OpenCode database query calls = %d, want at most checkpoint plus initial inspection", got)
	}
}

func TestOpenCodeDurabilityProbeRetriesTransientQueryFailure(t *testing.T) {
	baseline := readFixture(t, "testdata/durability/opencode/baseline.json")
	committed := readFixture(t, "testdata/durability/opencode/committed.json")
	var calls atomic.Int32
	probe := &OpenCodeAdapter{runDBQuery: func(string) ([]byte, error) {
		switch calls.Add(1) {
		case 1:
			return baseline, nil
		case 2:
			return nil, errors.New("database is temporarily busy")
		default:
			return committed, nil
		}
	}}
	checkpoint, err := probe.Checkpoint("opencode-session")
	if err != nil {
		t.Fatal(err)
	}

	waitForProbe(t, probe, "opencode-session", checkpoint)
}

func TestCursorDurabilityProbeQueriesSemanticAssistantRows(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".cursor", "chats", "workspace", "cursor-session", "store.db")
	copyFixture(t, "testdata/durability/cursor/store.db", path)
	responses := [][]byte{
		readFixture(t, "testdata/durability/cursor/baseline.json"),
		readFixture(t, "testdata/durability/cursor/intermediate.json"),
		readFixture(t, "testdata/durability/cursor/committed.json"),
	}
	var calls atomic.Int32
	probe := &CursorAdapter{runStoreQuery: func(string) ([]byte, error) {
		index := int(calls.Add(1)) - 1
		if index >= len(responses) {
			index = len(responses) - 1
		}
		return responses[index], nil
	}}
	checkpoint, err := probe.Checkpoint("cursor-session")
	if err != nil {
		t.Fatal(err)
	}
	waitForProbe(t, probe, "cursor-session", checkpoint)
	if got := calls.Load(); got != 3 {
		t.Fatalf("Cursor store query calls = %d, want baseline, intermediate, committed", got)
	}
}

func TestDurabilityProbeHonorsContextWhenNoCommittedTurnAppears(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".copilot", "session-state", "copilot-session", "events.jsonl")
	copyFixture(t, "testdata/durability/copilot/events.jsonl", path)
	probe := &CopilotAdapter{}
	checkpoint, err := probe.Checkpoint("copilot-session")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if err := probe.WaitForCommittedTurn(ctx, "copilot-session", checkpoint); err == nil || !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Fatalf("WaitForCommittedTurn error = %v, want context deadline", err)
	}
}

func TestFileCheckpointStartsAtIncompleteTrailingRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	const complete = "{\"type\":\"complete\"}\n"
	if err := os.WriteFile(path, []byte(complete+`{"type":"assistant"`), 0o600); err != nil {
		t.Fatal(err)
	}

	checkpoint, err := fileCheckpoint(path)
	if err != nil {
		t.Fatal(err)
	}
	if checkpoint.Offset != int64(len(complete)) {
		t.Fatalf("checkpoint offset = %d, want incomplete record start %d", checkpoint.Offset, len(complete))
	}
}

func TestClaudeDurabilityProbeAcceptsRecordLargerThanFourMiB(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".claude", "projects", "-repo", "claude-session.jsonl")
	copyFixture(t, "testdata/durability/claude/session.jsonl", path)
	probe := &ClaudeAdapter{}
	checkpoint, err := probe.Checkpoint("claude-session")
	if err != nil {
		t.Fatal(err)
	}
	record := `{"type":"assistant","message":{"role":"assistant","stop_reason":"end_turn","content":"` +
		strings.Repeat("x", 5*1024*1024) + `"}}` + "\n"
	appendFile(t, path, record)

	waitForProbe(t, probe, "claude-session", checkpoint)
}

func waitForProbe(t *testing.T, probe TurnDurabilityProbe, sessionID string, checkpoint Checkpoint) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := probe.WaitForCommittedTurn(ctx, sessionID, checkpoint); err != nil {
		t.Fatalf("WaitForCommittedTurn: %v", err)
	}
}

func appendAfter(t *testing.T, path, content string) {
	t.Helper()
	go func() {
		time.Sleep(30 * time.Millisecond)
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			return
		}
		_, _ = f.WriteString(content)
		_ = f.Close()
	}()
}

func appendFixture(t *testing.T, path, fixture string) {
	t.Helper()
	appendFile(t, path, string(readFixture(t, fixture)))
}

func appendFixtureAfter(t *testing.T, path, fixture string) {
	t.Helper()
	appendAfter(t, path, string(readFixture(t, fixture)))
}

func replaceFixture(t *testing.T, path, fixture string) {
	t.Helper()
	temporary := path + ".replacement"
	if err := os.WriteFile(temporary, readFixture(t, fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(temporary, path); err != nil {
		t.Fatal(err)
	}
}

func replaceFixtureAfter(t *testing.T, path, fixture string) {
	t.Helper()
	data := readFixture(t, fixture)
	go func() {
		time.Sleep(30 * time.Millisecond)
		temporary := path + ".replacement"
		if os.WriteFile(temporary, data, 0o600) == nil {
			_ = os.Rename(temporary, path)
		}
	}()
}

func appendFile(t *testing.T, path, content string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
}

func copyFixture(t *testing.T, fixture, destination string) {
	t.Helper()
	data := readFixture(t, fixture)
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func readFixture(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
