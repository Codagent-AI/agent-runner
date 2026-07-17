package cli

import (
	"context"
	"errors"
	"io/fs"
	"os"
	osexec "os/exec"
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

// cursorFixtureReceipt is the acceptance receipt embedded in the committed
// Cursor fixture's persisted completion-client tool result.
const cursorFixtureReceipt = "3f6bb84a-97c5-4a02-b7f1-8a2f4f4e9d1c"

func TestCursorAdapterImplementsReceiptTurnDurabilityProbe(t *testing.T) {
	var probe TurnDurabilityProbe = &CursorAdapter{}
	if _, ok := probe.(ReceiptTurnDurabilityProbe); !ok {
		t.Fatal("CursorAdapter does not implement ReceiptTurnDurabilityProbe")
	}
}

func TestCursorReceiptDurabilityProbeFindsReceiptToolResultAfterCheckpoint(t *testing.T) {
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := probe.WaitForCommittedTurnWithReceipt(ctx, "cursor-session", checkpoint, cursorFixtureReceipt); err != nil {
		t.Fatalf("WaitForCommittedTurnWithReceipt: %v", err)
	}
}

func TestCursorReceiptDurabilityProbeRejectsAssistantTextAndUnrelatedToolResults(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".cursor", "chats", "workspace", "cursor-session", "store.db")
	copyFixture(t, "testdata/durability/cursor/store.db", path)
	probe := &CursorAdapter{}
	checkpoint, err := probe.Checkpoint("cursor-session")
	if err != nil {
		t.Fatal(err)
	}
	// The intermediate store mutates the existing assistant row's text, adds a
	// new assistant text row, a tool call, and an unrelated tool result. None
	// of these is committed-turn evidence: only the receipt-bearing tool
	// result proves the completion exchange was persisted.
	replaceFixture(t, path, "testdata/durability/cursor/store-intermediate.db")
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	err = probe.WaitForCommittedTurnWithReceipt(ctx, "cursor-session", checkpoint, cursorFixtureReceipt)
	if err == nil {
		t.Fatal("intermediate assistant and tool rows satisfied the receipt probe")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("WaitForCommittedTurnWithReceipt error = %v, want context deadline", err)
	}
}

func TestCursorWaitForCommittedTurnWithoutReceiptFailsHonestly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".cursor", "chats", "workspace", "cursor-session", "store.db")
	copyFixture(t, "testdata/durability/cursor/store-committed.db", path)
	probe := &CursorAdapter{}

	err := probe.WaitForCommittedTurn(context.Background(), "cursor-session", Checkpoint{Artifact: path})
	if err == nil || !strings.Contains(err.Error(), "receipt") {
		t.Fatalf("WaitForCommittedTurn error = %v, want honest receipt-required failure", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := probe.WaitForCommittedTurnWithReceipt(ctx, "cursor-session", Checkpoint{Artifact: path}, ""); err == nil {
		t.Fatal("empty receipt unexpectedly accepted")
	}
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

func TestCursorReceiptDurabilityProbeQueriesToolResultRows(t *testing.T) {
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
	var query string
	probe := &CursorAdapter{runStoreQuery: func(gotQuery string) ([]byte, error) {
		query = gotQuery
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := probe.WaitForCommittedTurnWithReceipt(ctx, "cursor-session", checkpoint, cursorFixtureReceipt); err != nil {
		t.Fatalf("WaitForCommittedTurnWithReceipt: %v", err)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("Cursor store query calls = %d, want baseline, intermediate, committed", got)
	}
	if !strings.Contains(query, "'tool-result'") || !strings.Contains(query, "'tool'") {
		t.Fatalf("Cursor receipt query does not restrict evidence to persisted tool results: %s", query)
	}
	if strings.Contains(query, "'assistant'") {
		t.Fatalf("Cursor receipt query still accepts assistant rows as evidence: %s", query)
	}
}

func TestCursorDurabilityCheckpointAcceptsEmptySQLiteJSONOutput(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".cursor", "chats", "workspace", "cursor-session", "store.db")
	copyFixture(t, "testdata/durability/cursor/store.db", path)
	probe := &CursorAdapter{runStoreQuery: func(string) ([]byte, error) {
		return nil, nil
	}}

	checkpoint, err := probe.Checkpoint("cursor-session")
	if err != nil {
		t.Fatalf("Checkpoint returned error for empty sqlite3 JSON output: %v", err)
	}
	if checkpoint.Artifact != path || checkpoint.Marker != "" {
		t.Fatalf("checkpoint = %#v, want empty marker for %s", checkpoint, path)
	}
}

func TestCursorDurabilityProbeFailsFastWhenSQLiteBinaryMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".cursor", "chats", "workspace", "cursor-session", "store.db")
	copyFixture(t, "testdata/durability/cursor/store.db", path)
	probe := &CursorAdapter{runStoreQuery: func(string) ([]byte, error) {
		return nil, &osexec.Error{Name: "sqlite3", Err: osexec.ErrNotFound}
	}}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	err := probe.WaitForCommittedTurnWithReceipt(ctx, "cursor-session", Checkpoint{Artifact: path}, cursorFixtureReceipt)
	if err == nil || !errors.Is(err, osexec.ErrNotFound) {
		t.Fatalf("WaitForCommittedTurnWithReceipt error = %v, want exec.ErrNotFound", err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("WaitForCommittedTurnWithReceipt polled until the durability deadline: %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("WaitForCommittedTurnWithReceipt took %v, want immediate failure for missing sqlite3 binary", elapsed)
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

func TestCheckpointReportsAbsentSessionStoreAsNotExist(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	probes := map[string]TurnDurabilityProbe{
		"claude":  &ClaudeAdapter{},
		"codex":   &CodexAdapter{},
		"copilot": &CopilotAdapter{},
		"cursor":  &CursorAdapter{},
	}
	for name, probe := range probes {
		if _, err := probe.Checkpoint("absent-session"); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s Checkpoint error = %v, want fs.ErrNotExist classification for an absent store", name, err)
		}
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
