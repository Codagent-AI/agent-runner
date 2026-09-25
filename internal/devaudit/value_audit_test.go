//go:build dev_audit

package devaudit

import (
	"testing"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/metrics"
	"github.com/google/go-cmp/cmp"
)

func TestAggregateGitExcludesPreexistingDirtyPaths(t *testing.T) {
	start := &audit.GitCheckpoint{Available: true,
		Worktree:  []audit.GitFileStat{{Path: "a", Added: 3, Deleted: 1}, {Path: "a2", Added: 1}},
		Untracked: []audit.GitFileStat{{Path: "b", Added: 5}},
	}
	end := &audit.GitCheckpoint{Available: true,
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
			GitStart:   &audit.GitCheckpoint{Available: true, Worktree: []audit.GitFileStat{{Path: "existing", Added: 1}}},
			GitEnd:     &audit.GitCheckpoint{Available: true, Worktree: []audit.GitFileStat{{Path: "existing", Added: 1}, {Path: "c", Added: 1}}},
			GitChanges: &audit.GitChangeCounts{Available: true, FilesChanged: 1, LinesAdded: 1},
		},
		{
			GitStart:   &audit.GitCheckpoint{Available: true, Worktree: []audit.GitFileStat{{Path: "existing", Added: 1}, {Path: "c", Added: 1}}},
			GitEnd:     &audit.GitCheckpoint{Available: true, Worktree: []audit.GitFileStat{{Path: "existing", Added: 1}, {Path: "c", Added: 1}, {Path: "d", Added: 1}}},
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
			GitStart:   &audit.GitCheckpoint{Available: true},
			GitEnd:     &audit.GitCheckpoint{Available: true, Worktree: []audit.GitFileStat{{Path: "c", Added: 1}}},
			GitChanges: &audit.GitChangeCounts{Available: true, FilesChanged: 1, LinesAdded: 1},
		},
		{
			GitStart:   &audit.GitCheckpoint{Available: true, Worktree: []audit.GitFileStat{{Path: "c", Added: 1}}},
			GitEnd:     &audit.GitCheckpoint{Available: true, Worktree: []audit.GitFileStat{{Path: "c", Added: 2}}},
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
		GitStart:   &audit.GitCheckpoint{Available: true},
		GitEnd:     &audit.GitCheckpoint{Available: true, Worktree: []audit.GitFileStat{{Path: "c", Added: 1}}},
		GitChanges: &audit.GitChangeCounts{Available: true, FilesChanged: 1, LinesAdded: 1},
	}
	cases := []struct {
		name   string
		record metrics.StepRecord
	}{
		{name: "missing start", record: metrics.StepRecord{GitEnd: &audit.GitCheckpoint{Available: true, Worktree: []audit.GitFileStat{{Path: "c", Added: 2}}}, GitChanges: &audit.GitChangeCounts{Available: true, FilesChanged: 1, LinesAdded: 1}}},
		{name: "missing end", record: metrics.StepRecord{GitStart: &audit.GitCheckpoint{Available: true, Worktree: []audit.GitFileStat{{Path: "c", Added: 1}}}, GitChanges: &audit.GitChangeCounts{Available: true, FilesChanged: 1, LinesAdded: 1}}},
		{name: "missing path details", record: metrics.StepRecord{GitStart: &audit.GitCheckpoint{Available: true}, GitEnd: &audit.GitCheckpoint{Available: true}, GitChanges: &audit.GitChangeCounts{Available: true, FilesChanged: 1, LinesAdded: 1}}},
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
		GitStart:   &audit.GitCheckpoint{Available: true, Worktree: []audit.GitFileStat{{Path: "tracked.txt", Added: 1, Deleted: 1, Fingerprint: "first"}}},
		GitEnd:     &audit.GitCheckpoint{Available: true, Worktree: []audit.GitFileStat{{Path: "tracked.txt", Added: 1, Deleted: 1, Fingerprint: "later"}}},
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

type deferralOutcome struct {
	Attribution  string
	CommitSHAs   []string
	DeferredSHAs []string
	FilesChanged int64
	LinesAdded   int64
}

func deferralOutcomes(leaves []LeafEvidence) []deferralOutcome {
	outcomes := make([]deferralOutcome, 0, len(leaves))
	for index := range leaves {
		git := leaves[index].Skeleton.Git
		outcome := deferralOutcome{Attribution: git.Attribution, CommitSHAs: git.CommitSHAs, DeferredSHAs: git.DeferredSHAs}
		if git.FilesChanged != nil {
			outcome.FilesChanged = *git.FilesChanged
		}
		if git.LinesAdded != nil {
			outcome.LinesAdded = *git.LinesAdded
		}
		outcomes = append(outcomes, outcome)
	}
	return outcomes
}

func deferralLeaves(earlier *metrics.StepRecord) []LeafEvidence {
	commits := map[string]snapshottedGitCommit{
		"commit-sha": {SHA: "commit-sha", Subject: "[commit] feat: add shared", Paths: []string{"shared.txt"}, FilesChanged: 1, LinesAdded: 3},
	}
	later := metrics.StepRecord{
		ID:         "commit",
		GitStart:   &audit.GitCheckpoint{Available: true, HEAD: "base-head"},
		GitEnd:     &audit.GitCheckpoint{Available: true, HEAD: "new-head", Commits: []string{"commit-sha"}},
		GitChanges: &audit.GitChangeCounts{Available: true},
	}
	leaves := []LeafEvidence{
		{Skeleton: ObservationSkeleton{Git: aggregateGit([]metrics.StepRecord{*earlier}, commits)}},
		{Skeleton: ObservationSkeleton{Git: aggregateGit([]metrics.StepRecord{later}, commits)}},
	}
	applyDeferredCommitAttribution(leaves)
	return leaves
}

func TestDeferredCommitAttributionTransfersCommitToEarlierDirtyStep(t *testing.T) {
	earlier := metrics.StepRecord{
		ID:         "edit",
		GitStart:   &audit.GitCheckpoint{Available: true, HEAD: "base-head"},
		GitEnd:     &audit.GitCheckpoint{Available: true, HEAD: "base-head", Untracked: []audit.GitFileStat{{Path: "shared.txt", Added: 3}}},
		GitChanges: &audit.GitChangeCounts{Available: true, FilesChanged: 1, LinesAdded: 3},
	}

	want := []deferralOutcome{
		{Attribution: "deferred_commit", CommitSHAs: []string{}, DeferredSHAs: []string{"commit-sha"}, FilesChanged: 1, LinesAdded: 3},
		{Attribution: "no_change", CommitSHAs: []string{"commit-sha"}, DeferredSHAs: []string{}},
	}
	if diff := cmp.Diff(want, deferralOutcomes(deferralLeaves(&earlier))); diff != "" {
		t.Errorf("deferral outcomes (-want +got):\n%s", diff)
	}
}

func TestDeferredCommitAttributionIgnoresPathRemovedByEarlierStep(t *testing.T) {
	earlier := metrics.StepRecord{
		ID:         "cleanup",
		GitStart:   &audit.GitCheckpoint{Available: true, HEAD: "base-head", Untracked: []audit.GitFileStat{{Path: "shared.txt"}}},
		GitEnd:     &audit.GitCheckpoint{Available: true, HEAD: "base-head"},
		GitChanges: &audit.GitChangeCounts{Available: true, FilesChanged: 1},
	}

	leaves := deferralLeaves(&earlier)
	want := []deferralOutcome{
		{Attribution: "working_tree", CommitSHAs: []string{}, DeferredSHAs: []string{}, FilesChanged: 1},
		{Attribution: "attributed", CommitSHAs: []string{"commit-sha"}, DeferredSHAs: []string{}, FilesChanged: 1, LinesAdded: 3},
	}
	if diff := cmp.Diff(want, deferralOutcomes(leaves)); diff != "" {
		t.Errorf("deferral outcomes (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"shared.txt"}, leaves[0].Skeleton.Git.ChangedPaths); diff != "" {
		t.Errorf("removal step changed paths (-want +got):\n%s", diff)
	}
}

func TestDirtyChangedPathsSkipsUnavailableCheckpoint(t *testing.T) {
	dirty := []audit.GitFileStat{{Path: "dirty.txt", Added: 1}}
	records := []metrics.StepRecord{
		{
			GitStart: &audit.GitCheckpoint{Available: true, Worktree: dirty},
			GitEnd:   &audit.GitCheckpoint{Reason: "Git capture failed"},
		},
		{
			GitStart: &audit.GitCheckpoint{Reason: "Git capture failed"},
			GitEnd:   &audit.GitCheckpoint{Available: true, Worktree: dirty},
		},
	}

	changed, present := dirtyChangedPaths(records)
	if diff := cmp.Diff([]string{}, changed); diff != "" {
		t.Errorf("changed paths (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{}, present); diff != "" {
		t.Errorf("deferral paths (-want +got):\n%s", diff)
	}
}
