//go:build dev_audit

package devaudit

import (
	"testing"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/metrics"
	"github.com/google/go-cmp/cmp"
)

func TestAggregateGitExcludesPreexistingDirtyPaths(t *testing.T) {
	start := &audit.GitCheckpoint{
		Worktree:  []audit.GitFileStat{{Path: "a", Added: 3, Deleted: 1}, {Path: "a2", Added: 1}},
		Untracked: []audit.GitFileStat{{Path: "b", Added: 5}},
	}
	end := &audit.GitCheckpoint{
		Worktree:  []audit.GitFileStat{{Path: "a", Added: 3, Deleted: 1}, {Path: "a2", Added: 2}, {Path: "c", Added: 1}},
		Untracked: []audit.GitFileStat{{Path: "b", Added: 5}},
	}
	records := []metrics.StepRecord{{GitStart: start, GitEnd: end, GitChanges: &audit.GitChangeCounts{Available: true, FilesChanged: 2, LinesAdded: 2}}}

	got := aggregateGit(records, nil)
	if got.Attribution != "working_tree" {
		t.Fatalf("attribution = %q, want working_tree", got.Attribution)
	}
	if diff := cmp.Diff([]string{"a2", "c"}, got.ChangedPaths); diff != "" {
		t.Errorf("changed paths mismatch (-want +got):\n%s", diff)
	}
	if got.FilesChanged == nil || len(got.ChangedPaths) != int(*got.FilesChanged) {
		t.Errorf("changed paths = %d, files changed = %v", len(got.ChangedPaths), got.FilesChanged)
	}
}

func TestAggregateGitUnionsStepLocalDirtyPaths(t *testing.T) {
	records := []metrics.StepRecord{
		{
			GitStart:   &audit.GitCheckpoint{Worktree: []audit.GitFileStat{{Path: "existing", Added: 1}}},
			GitEnd:     &audit.GitCheckpoint{Worktree: []audit.GitFileStat{{Path: "existing", Added: 1}, {Path: "c", Added: 1}}},
			GitChanges: &audit.GitChangeCounts{Available: true, FilesChanged: 1, LinesAdded: 1},
		},
		{
			GitStart:   &audit.GitCheckpoint{Worktree: []audit.GitFileStat{{Path: "existing", Added: 1}, {Path: "c", Added: 1}}},
			GitEnd:     &audit.GitCheckpoint{Worktree: []audit.GitFileStat{{Path: "existing", Added: 1}, {Path: "c", Added: 1}, {Path: "d", Added: 1}}},
			GitChanges: &audit.GitChangeCounts{Available: true, FilesChanged: 1, LinesAdded: 1},
		},
	}

	got := aggregateGit(records, nil)
	if got.Attribution != "working_tree" {
		t.Fatalf("attribution = %q, want working_tree", got.Attribution)
	}
	if diff := cmp.Diff([]string{"c", "d"}, got.ChangedPaths); diff != "" {
		t.Errorf("changed paths mismatch (-want +got):\n%s", diff)
	}
	if got.FilesChanged == nil || len(got.ChangedPaths) != int(*got.FilesChanged) {
		t.Errorf("changed paths = %d, files changed = %v", len(got.ChangedPaths), got.FilesChanged)
	}
}

func TestAggregateGitCountsRepeatedPathOnce(t *testing.T) {
	records := []metrics.StepRecord{
		{
			GitStart:   &audit.GitCheckpoint{},
			GitEnd:     &audit.GitCheckpoint{Worktree: []audit.GitFileStat{{Path: "c", Added: 1}}},
			GitChanges: &audit.GitChangeCounts{Available: true, FilesChanged: 1, LinesAdded: 1},
		},
		{
			GitStart:   &audit.GitCheckpoint{Worktree: []audit.GitFileStat{{Path: "c", Added: 1}}},
			GitEnd:     &audit.GitCheckpoint{Worktree: []audit.GitFileStat{{Path: "c", Added: 2}}},
			GitChanges: &audit.GitChangeCounts{Available: true, FilesChanged: 1, LinesAdded: 1},
		},
	}

	got := aggregateGit(records, nil)
	if got.Attribution != "working_tree" {
		t.Fatalf("attribution = %q, want working_tree", got.Attribution)
	}
	if diff := cmp.Diff([]string{"c"}, got.ChangedPaths); diff != "" {
		t.Errorf("changed paths mismatch (-want +got):\n%s", diff)
	}
	if got.FilesChanged == nil || *got.FilesChanged != 1 {
		t.Errorf("files changed = %v, want 1", got.FilesChanged)
	}
	if got.LinesAdded == nil || *got.LinesAdded != 2 {
		t.Errorf("lines added = %v, want 2", got.LinesAdded)
	}
}

func TestAggregateGitRetainsCountsWhenCheckpointPathsAreIncomplete(t *testing.T) {
	complete := metrics.StepRecord{
		GitStart:   &audit.GitCheckpoint{},
		GitEnd:     &audit.GitCheckpoint{Worktree: []audit.GitFileStat{{Path: "c", Added: 1}}},
		GitChanges: &audit.GitChangeCounts{Available: true, FilesChanged: 1, LinesAdded: 1},
	}
	cases := []struct {
		name   string
		record metrics.StepRecord
	}{
		{name: "missing start", record: metrics.StepRecord{GitEnd: &audit.GitCheckpoint{Worktree: []audit.GitFileStat{{Path: "c", Added: 2}}}, GitChanges: &audit.GitChangeCounts{Available: true, FilesChanged: 1, LinesAdded: 1}}},
		{name: "missing end", record: metrics.StepRecord{GitStart: &audit.GitCheckpoint{Worktree: []audit.GitFileStat{{Path: "c", Added: 1}}}, GitChanges: &audit.GitChangeCounts{Available: true, FilesChanged: 1, LinesAdded: 1}}},
		{name: "missing path details", record: metrics.StepRecord{GitStart: &audit.GitCheckpoint{}, GitEnd: &audit.GitCheckpoint{}, GitChanges: &audit.GitChangeCounts{Available: true, FilesChanged: 1, LinesAdded: 1}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := aggregateGit([]metrics.StepRecord{complete, tc.record}, nil)
			if got.Attribution != "working_tree" {
				t.Fatalf("attribution = %q, want working_tree", got.Attribution)
			}
			if got.FilesChanged == nil || *got.FilesChanged != 2 {
				t.Errorf("files changed = %v, want 2", got.FilesChanged)
			}
			if got.LinesAdded == nil || *got.LinesAdded != 2 {
				t.Errorf("lines added = %v, want 2", got.LinesAdded)
			}
		})
	}
}

func TestAggregateGitIncludesSameCountContentChanges(t *testing.T) {
	records := []metrics.StepRecord{{
		GitStart:   &audit.GitCheckpoint{Worktree: []audit.GitFileStat{{Path: "tracked.txt", Added: 1, Deleted: 1, Fingerprint: "first"}}},
		GitEnd:     &audit.GitCheckpoint{Worktree: []audit.GitFileStat{{Path: "tracked.txt", Added: 1, Deleted: 1, Fingerprint: "later"}}},
		GitChanges: &audit.GitChangeCounts{Available: true, FilesChanged: 1},
	}}

	got := aggregateGit(records, nil)
	if got.Attribution != "working_tree" {
		t.Fatalf("attribution = %q, want working_tree", got.Attribution)
	}
	if diff := cmp.Diff([]string{"tracked.txt"}, got.ChangedPaths); diff != "" {
		t.Errorf("changed paths mismatch (-want +got):\n%s", diff)
	}
	if got.FilesChanged == nil || *got.FilesChanged != 1 {
		t.Errorf("files changed = %v, want 1", got.FilesChanged)
	}
}
