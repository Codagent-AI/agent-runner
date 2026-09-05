package audit

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func TestCheckpointLoggerCountsCommittedStepChanges(t *testing.T) {
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
	logger.Emit(Event{Timestamp: started.Format(time.RFC3339Nano), Prefix: "[commit]", Type: EventStepStart, Data: map[string]any{"command": "commit"}})
	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("one\ntwo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "tracked.txt")
	runGit(t, repo, "commit", "-m", "[commit] add second line")
	logger.Emit(Event{Timestamp: started.Add(time.Second).Format(time.RFC3339Nano), Prefix: "[commit]", Type: EventStepEnd, Data: map[string]any{"outcome": "success"}})

	changes, ok := sink.events[1].Data["git_changes"].(GitChangeCounts)
	if !ok || !changes.Available || changes.FilesChanged != 1 || changes.LinesAdded != 1 || changes.LinesDeleted != 0 {
		t.Fatalf("committed Git changes = %#v", sink.events[1].Data["git_changes"])
	}
}

func TestGitUntrackedStatsRejectsExcessiveFileCount(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init")
	for i := 0; i < 257; i++ {
		name := filepath.Join(repo, fmt.Sprintf("untracked-%03d.txt", i))
		if err := os.WriteFile(name, []byte("x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := gitUntrackedStats(repo); err == nil {
		t.Fatal("untracked evidence should be unavailable above the bounded file limit")
	}
}

func TestGitUntrackedStatsRejectsExcessivePathOutput(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init")
	for i := 0; i < 200; i++ {
		name := filepath.Join(repo, fmt.Sprintf("%03d-%s.txt", i, strings.Repeat("a", 220)))
		if err := os.WriteFile(name, []byte("x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := gitUntrackedStats(repo); err == nil {
		t.Fatal("untracked evidence should be unavailable above the bounded output limit")
	}
}

func TestGitUntrackedStatsReadsValidatedDescriptorAfterPathReplacement(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init")
	path := filepath.Join(repo, "untracked.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	originalOpen := openUntrackedFile
	openUntrackedFile = func(name string) (*os.File, error) {
		file, err := originalOpen(name)
		if err != nil {
			return nil, err
		}
		if err := os.Rename(name, name+".validated"); err != nil {
			_ = file.Close()
			return nil, err
		}
		if err := os.WriteFile(name, bytes.Repeat([]byte("x"), maxUntrackedFileBytes+1), 0o600); err != nil {
			_ = file.Close()
			return nil, err
		}
		return file, nil
	}
	t.Cleanup(func() { openUntrackedFile = originalOpen })

	stats, err := gitUntrackedStats(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 1 || stats[0].Path != "untracked.txt" || stats[0].Added != 2 {
		t.Fatalf("stable untracked stats = %#v", stats)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}
