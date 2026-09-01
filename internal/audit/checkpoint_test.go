package audit

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

type capturedLogger struct{ events []Event }

func (l *capturedLogger) Emit(event Event) { l.events = append(l.events, event) }

func TestCheckpointLoggerRecordsExecutableStepBoundaries(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "tracked.txt")
	runGit(t, repo, "commit", "-m", "initial")

	sink := &capturedLogger{}
	logger := NewCheckpointLogger(sink, repo, "execution-1")
	started := time.Now().UTC()
	logger.Emit(Event{Timestamp: started.Format(time.RFC3339Nano), Prefix: "[write]", Type: EventStepStart, Data: map[string]any{"command": "write"}})
	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("one\ntwo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "new.txt"), []byte("three\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	logger.Emit(Event{Timestamp: started.Add(time.Second).Format(time.RFC3339Nano), Prefix: "[write]", Type: EventStepEnd, Data: map[string]any{"outcome": "success"}})

	if len(sink.events) != 2 {
		t.Fatalf("events = %d, want 2", len(sink.events))
	}
	start, ok := sink.events[0].Data["git_checkpoint"].(GitCheckpoint)
	if !ok || !start.Available || start.HEAD == "" {
		t.Fatalf("start checkpoint = %#v", sink.events[0].Data["git_checkpoint"])
	}
	end, ok := sink.events[1].Data["git_checkpoint"].(GitCheckpoint)
	if !ok || !end.Available || end.HEAD == "" {
		t.Fatalf("end checkpoint = %#v", sink.events[1].Data["git_checkpoint"])
	}
	changes, ok := sink.events[1].Data["git_changes"].(GitChangeCounts)
	if !ok || !changes.Available || changes.FilesChanged != 2 || changes.LinesAdded != 2 || changes.LinesDeleted != 0 {
		t.Fatalf("git changes = %#v", sink.events[1].Data["git_changes"])
	}
	for _, event := range sink.events {
		if got := event.Data["execution_session_id"]; got != "execution-1" {
			t.Fatalf("execution session = %#v", got)
		}
	}
}

func TestCheckpointLoggerRecordsUnavailableEvidenceWithoutChangingEvent(t *testing.T) {
	sink := &capturedLogger{}
	logger := NewCheckpointLogger(sink, t.TempDir(), "execution-1")
	logger.Emit(Event{Timestamp: time.Now().UTC().Format(time.RFC3339Nano), Prefix: "[write]", Type: EventStepStart, Data: map[string]any{"command": "write"}})
	logger.Emit(Event{Timestamp: time.Now().UTC().Format(time.RFC3339Nano), Prefix: "[write]", Type: EventStepEnd, Data: map[string]any{"outcome": "success"}})

	for _, event := range sink.events {
		checkpoint, ok := event.Data["git_checkpoint"].(GitCheckpoint)
		if !ok || checkpoint.Available || checkpoint.Reason == "" {
			t.Fatalf("unavailable checkpoint = %#v", event.Data["git_checkpoint"])
		}
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}
