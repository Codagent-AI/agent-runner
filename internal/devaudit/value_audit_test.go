//go:build dev_audit

package devaudit

import (
	"testing"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/metrics"
	"github.com/google/go-cmp/cmp"
)

func TestAggregateGitExcludesUnchangedPreexistingDirtyPath(t *testing.T) {
	staged := audit.GitFileStat{Path: "preexisting-staged.txt", Added: 2}
	start := &audit.GitCheckpoint{Available: true, HEAD: "same-head", Index: []audit.GitFileStat{staged}}
	end := &audit.GitCheckpoint{
		Available: true,
		HEAD:      "same-head",
		Index:     []audit.GitFileStat{staged},
		Worktree:  []audit.GitFileStat{{Path: "worktree-c.txt", Added: 1}, {Path: "worktree-a.txt", Added: 1}},
		Untracked: []audit.GitFileStat{{Path: "untracked-z.txt", Added: 1}, {Path: "untracked-b.txt", Added: 1}, {Path: "untracked-y.txt", Added: 1}},
	}
	records := []metrics.StepRecord{{GitStart: start, GitEnd: end, GitChanges: &audit.GitChangeCounts{Available: true, FilesChanged: 5, LinesAdded: 5}}}

	got := aggregateGit(records, nil)
	if got.Attribution != "working_tree" {
		t.Errorf("attribution = %q, want working_tree", got.Attribution)
	}
	if got.FilesChanged == nil || *got.FilesChanged != 5 {
		t.Errorf("files changed = %v, want 5", got.FilesChanged)
	}
	wantPaths := []string{"untracked-b.txt", "untracked-y.txt", "untracked-z.txt", "worktree-a.txt", "worktree-c.txt"}
	if diff := cmp.Diff(wantPaths, got.ChangedPaths); diff != "" {
		t.Errorf("changed paths (-want +got):\n%s", diff)
	}
}
