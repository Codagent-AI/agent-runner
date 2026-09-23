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

	"github.com/google/go-cmp/cmp"
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

func TestCommittedRenameRemainsAvailable(t *testing.T) {
	repo := newCheckpointRepo(t)
	writeCheckpointFile(t, repo, "a/f", "one\ntwo\nthree\nfour\n")
	runGit(t, repo, "add", "a/f")
	runGit(t, repo, "commit", "-m", "initial")
	start := observeGit(repo)
	if err := os.MkdirAll(filepath.Join(repo, "b"), 0o700); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "mv", "a/f", "b/f")
	writeCheckpointFile(t, repo, "b/f", "one\ntwo\nthree\nchanged\n")
	runGit(t, repo, "add", "b/f")
	runGit(t, repo, "commit", "-m", "rename")
	end := observeGit(repo)
	completeHeadTransition(repo, &start, &end)
	if !end.Available || !end.CommittedObserved {
		t.Fatalf("end checkpoint = %#v", end)
	}
	if diff := cmp.Diff([]string{end.HEAD}, end.Commits); diff != "" {
		t.Fatalf("commits mismatch (-want +got):\n%s", diff)
	}
	want := []GitFileStat{{Path: "a/f", Deleted: 4}, {Path: "b/f", Added: 4}}
	if diff := cmp.Diff(want, end.Committed); diff != "" {
		t.Fatalf("committed stats mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(GitChangeCounts{Available: true, FilesChanged: 2, LinesAdded: 4, LinesDeleted: 4}, deriveGitChanges(&start, &end)); diff != "" {
		t.Fatalf("changes mismatch (-want +got):\n%s", diff)
	}
}

func TestStagedRenameRemainsAvailable(t *testing.T) {
	repo := newCheckpointRepo(t)
	writeCheckpointFile(t, repo, "a/f", "one\ntwo\n")
	runGit(t, repo, "add", "a/f")
	runGit(t, repo, "commit", "-m", "initial")
	if err := os.MkdirAll(filepath.Join(repo, "b"), 0o700); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "mv", "a/f", "b/f")
	checkpoint := observeGit(repo)
	if !checkpoint.Available {
		t.Fatalf("staged rename checkpoint = %#v", checkpoint)
	}
	if diff := cmp.Diff([]GitFileStat{{Path: "a/f", Deleted: 2}, {Path: "b/f", Added: 2}}, checkpoint.Index); diff != "" {
		t.Fatalf("index stats mismatch (-want +got):\n%s", diff)
	}
}

func TestCommittedRenameKeepsUnrelatedStagedStateSeparate(t *testing.T) {
	repo := newCheckpointRepo(t)
	writeCheckpointFile(t, repo, "a/f", "one\ntwo\nthree\nfour\n")
	writeCheckpointFile(t, repo, "unrelated", "old\n")
	runGit(t, repo, "add", "a/f", "unrelated")
	runGit(t, repo, "commit", "-m", "initial")
	writeCheckpointFile(t, repo, "unrelated", "old\nstaged\n")
	runGit(t, repo, "add", "unrelated")
	start := observeGit(repo)
	if err := os.MkdirAll(filepath.Join(repo, "b"), 0o700); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "mv", "a/f", "b/f")
	writeCheckpointFile(t, repo, "b/f", "one\ntwo\nthree\nchanged\n")
	runGit(t, repo, "add", "b/f")
	runGit(t, repo, "commit", "-m", "rename", "--only", "--", "a/f", "b/f")
	end := observeGit(repo)
	completeHeadTransition(repo, &start, &end)
	if !end.Available {
		t.Fatalf("end checkpoint = %#v", end)
	}
	if diff := cmp.Diff(start.Index, end.Index); diff != "" {
		t.Fatalf("unrelated staged state changed (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(GitChangeCounts{Available: true, FilesChanged: 2, LinesAdded: 4, LinesDeleted: 4}, deriveGitChanges(&start, &end)); diff != "" {
		t.Fatalf("changes mismatch (-want +got):\n%s", diff)
	}
}

func TestCommittedChangeOverlappingDirtyStateUnavailable(t *testing.T) {
	start := GitCheckpoint{Available: true, HEAD: "old", Index: []GitFileStat{{Path: "same", Added: 1}}}
	end := GitCheckpoint{Available: true, HEAD: "new", CommittedObserved: true, Committed: []GitFileStat{{Path: "same", Added: 2}}, Index: []GitFileStat{{Path: "same", Added: 1}}}
	want := GitChangeCounts{Reason: "preexisting dirty state prevents conservative commit attribution"}
	if diff := cmp.Diff(want, deriveGitChanges(&start, &end)); diff != "" {
		t.Fatalf("changes mismatch (-want +got):\n%s", diff)
	}
}

func TestCommittedChangeWithUnchangedDirtyStateAndNewDirtyFile(t *testing.T) {
	start := GitCheckpoint{Available: true, HEAD: "old", Index: []GitFileStat{{Path: "staged", Added: 1}}}
	end := GitCheckpoint{Available: true, HEAD: "new", CommittedObserved: true, Committed: []GitFileStat{{Path: "committed", Added: 2}}, Index: []GitFileStat{{Path: "staged", Added: 1}}, Worktree: []GitFileStat{{Path: "new-dirty", Added: 3}}}
	want := GitChangeCounts{Available: true, FilesChanged: 2, LinesAdded: 5}
	if diff := cmp.Diff(want, deriveGitChanges(&start, &end)); diff != "" {
		t.Fatalf("changes mismatch (-want +got):\n%s", diff)
	}
}

func TestCommittedChangeWithModifiedPreexistingDirtyStateUnavailable(t *testing.T) {
	start := GitCheckpoint{Available: true, HEAD: "old", Index: []GitFileStat{{Path: "staged", Added: 1}}}
	end := GitCheckpoint{Available: true, HEAD: "new", CommittedObserved: true, Committed: []GitFileStat{{Path: "committed", Added: 2}}, Index: []GitFileStat{{Path: "staged", Added: 2}}}
	want := GitChangeCounts{Reason: "preexisting dirty state prevents conservative commit attribution"}
	if diff := cmp.Diff(want, deriveGitChanges(&start, &end)); diff != "" {
		t.Fatalf("changes mismatch (-want +got):\n%s", diff)
	}
}

func TestCommittedChangeWithMissingPreexistingBinaryStateUnavailable(t *testing.T) {
	start := GitCheckpoint{Available: true, HEAD: "old", Index: []GitFileStat{{Path: "staged-binary"}}}
	end := GitCheckpoint{Available: true, HEAD: "new", CommittedObserved: true, Committed: []GitFileStat{{Path: "committed", Added: 2}}}
	want := GitChangeCounts{Reason: "preexisting dirty state prevents conservative commit attribution"}
	if diff := cmp.Diff(want, deriveGitChanges(&start, &end)); diff != "" {
		t.Fatalf("changes mismatch (-want +got):\n%s", diff)
	}
}

func TestParseNumstatCountsBinaryFileWithoutLines(t *testing.T) {
	want := []GitFileStat{{Path: "image.png"}}
	got, err := parseNumstat([]byte("-\t-\timage.png\x00"))
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("binary stats mismatch (-want +got):\n%s", diff)
	}
}

func newCheckpointRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test")
	return repo
}

func writeCheckpointFile(t *testing.T, repo, path, content string) {
	t.Helper()
	fullPath := filepath.Join(repo, path)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
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
