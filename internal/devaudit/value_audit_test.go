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
