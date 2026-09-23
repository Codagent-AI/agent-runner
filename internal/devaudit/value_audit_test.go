//go:build dev_audit

package devaudit

import (
	"fmt"
	"testing"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/metrics"
	"github.com/google/go-cmp/cmp"
)

func TestAggregateGitWorkingTreeChangedPathsExcludeEarlierDirtyState(t *testing.T) {
	stat := func(path string, added int64) audit.GitFileStat {
		return audit.GitFileStat{Path: path, Added: added}
	}
	initial := []audit.GitFileStat{stat("preexisting.txt", 1)}
	end := append([]audit.GitFileStat{}, initial...)
	want := make([]string, 0, 9)
	for i := range 9 {
		path := fmt.Sprintf("new-%02d.txt", i)
		end = append(end, stat(path, 1))
		want = append(want, path)
	}

	got := aggregateGit([]metrics.StepRecord{{
		GitStart:   &audit.GitCheckpoint{Available: true, HEAD: "same", Worktree: initial},
		GitEnd:     &audit.GitCheckpoint{Available: true, HEAD: "same", Worktree: end},
		GitChanges: &audit.GitChangeCounts{Available: true, FilesChanged: 9},
	}}, nil)
	if got.Attribution != "working_tree" {
		t.Fatalf("attribution = %q, want working_tree", got.Attribution)
	}
	if diff := cmp.Diff(want, got.ChangedPaths); diff != "" {
		t.Errorf("changed paths (-want +got):\n%s", diff)
	}
	if got.FilesChanged == nil || len(got.ChangedPaths) != int(*got.FilesChanged) {
		t.Errorf("changed paths count = %d, files changed = %v", len(got.ChangedPaths), got.FilesChanged)
	}
}

func TestAggregateGitWorkingTreeChangedPathsIncludeOnlySecondStepDelta(t *testing.T) {
	start := make([]audit.GitFileStat, 0, 10)
	end := make([]audit.GitFileStat, 0, 11)
	want := make([]string, 0, 5)
	for i := range 10 {
		path := fmt.Sprintf("dirty-%02d.txt", i)
		start = append(start, audit.GitFileStat{Path: path, Added: 1})
		added := int64(1)
		if i < 4 {
			added = 2
			want = append(want, path)
		}
		end = append(end, audit.GitFileStat{Path: path, Added: added})
	}
	end = append(end, audit.GitFileStat{Path: "new.txt", Added: 1})
	want = append(want, "new.txt")

	got := aggregateGit([]metrics.StepRecord{{
		GitStart:   &audit.GitCheckpoint{Available: true, HEAD: "same", Worktree: start},
		GitEnd:     &audit.GitCheckpoint{Available: true, HEAD: "same", Worktree: end},
		GitChanges: &audit.GitChangeCounts{Available: true, FilesChanged: 5},
	}}, nil)
	if got.Attribution != "working_tree" {
		t.Fatalf("attribution = %q, want working_tree", got.Attribution)
	}
	if diff := cmp.Diff(want, got.ChangedPaths); diff != "" {
		t.Errorf("changed paths (-want +got):\n%s", diff)
	}
	if got.FilesChanged == nil || len(got.ChangedPaths) != int(*got.FilesChanged) {
		t.Errorf("changed paths count = %d, files changed = %v", len(got.ChangedPaths), got.FilesChanged)
	}
}

func TestAggregateGitWorkingTreeChangedPathsIncludeChangedPreexistingPath(t *testing.T) {
	got := aggregateGit([]metrics.StepRecord{{
		GitStart:   &audit.GitCheckpoint{Available: true, HEAD: "same", Worktree: []audit.GitFileStat{{Path: "dirty.txt", Added: 1}}},
		GitEnd:     &audit.GitCheckpoint{Available: true, HEAD: "same", Worktree: []audit.GitFileStat{{Path: "dirty.txt", Added: 2}}},
		GitChanges: &audit.GitChangeCounts{Available: true, FilesChanged: 1},
	}}, nil)
	if got.Attribution != "working_tree" {
		t.Fatalf("attribution = %q, want working_tree", got.Attribution)
	}
	if diff := cmp.Diff([]string{"dirty.txt"}, got.ChangedPaths); diff != "" {
		t.Errorf("changed paths (-want +got):\n%s", diff)
	}
}

func TestDirtyChangedPathsSkipsUnavailableCheckpoint(t *testing.T) {
	records := []metrics.StepRecord{{
		GitStart: &audit.GitCheckpoint{Available: true, Worktree: []audit.GitFileStat{{Path: "dirty.txt", Added: 1}}},
		GitEnd:   &audit.GitCheckpoint{Reason: "Git capture failed"},
	}}

	if diff := cmp.Diff([]string{}, dirtyChangedPaths(records)); diff != "" {
		t.Errorf("changed paths (-want +got):\n%s", diff)
	}
}
