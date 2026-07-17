package cli

import (
	"context"
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
	appendFile(t, path, `{"type":"assistant","message":{"role":"assistant","stop_reason":"tool_use"}}`+"\n")
	appendAfter(t, path, `{"type":"assistant","message":{"role":"assistant","stop_reason":"end_turn"}}`+"\n")
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
	appendFile(t, path, `{"type":"response_item","payload":{"type":"message","role":"assistant","phase":"final_answer"}}`+"\n")
	appendAfter(t, path, `{"type":"event_msg","payload":{"type":"task_complete","turn_id":"turn-2"}}`+"\n")
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
	appendFile(t, path, `{"type":"assistant.message","data":{"turnId":"2"}}`+"\n")
	appendAfter(t, path, `{"type":"assistant.turn_end","data":{"turnId":"2"}}`+"\n")
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
	appendFile(t, path, `{"role":"user","content":[{"type":"text","text":"next"}]}`+"\n")
	appendAfter(t, path, `{"role":"assistant","id":"message-2","content":[{"type":"text","text":"done"}]}`+"\n")
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
