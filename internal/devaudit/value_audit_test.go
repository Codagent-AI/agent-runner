//go:build dev_audit

package devaudit

import (
	"testing"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/metrics"
	"github.com/google/go-cmp/cmp"
)

func TestSelectedSessionOutcome(t *testing.T) {
	step := func(prefix, outcome string) metrics.StepRecord {
		return metrics.StepRecord{ExecutionSessionID: "selected", Prefix: prefix, Kind: "step", Outcome: outcome}
	}
	cases := []struct {
		name           string
		hasSession     bool
		sessionOutcome string
		sessionStatus  string
		steps          []metrics.StepRecord
		want           string
	}{
		{name: "warn-only failure with successful run", hasSession: true, sessionOutcome: "success", steps: []metrics.StepRecord{step("", "failed")}, want: "success"},
		{name: "failed run", hasSession: true, sessionOutcome: "failed", steps: []metrics.StepRecord{step("", "success")}, want: "failed"},
		{name: "aborted run", hasSession: true, sessionOutcome: "aborted", steps: []metrics.StepRecord{step("", "success")}, want: "aborted"},
		{name: "legacy nested failure followed by success", steps: []metrics.StepRecord{step("group", "failed"), step("", "success")}, want: "success"},
		{name: "legacy final top-level failure", steps: []metrics.StepRecord{step("", "success"), step("", "failed")}, want: "failed"},
		{name: "interrupted before any step", hasSession: true, want: "unknown"},
		{name: "open session after successful step", hasSession: true, sessionStatus: metrics.SessionOpen, steps: []metrics.StepRecord{step("", "success")}, want: "unknown"},
		{name: "interrupted session after successful step", hasSession: true, sessionStatus: metrics.SessionInterrupted, steps: []metrics.StepRecord{step("", "success")}, want: "unknown"},
		{name: "closed legacy session after successful step", hasSession: true, sessionStatus: metrics.SessionClosed, steps: []metrics.StepRecord{step("", "success")}, want: "success"},
		{name: "legacy session without status after successful step", hasSession: true, steps: []metrics.StepRecord{step("", "success")}, want: "success"},
		{name: "legacy nested failure with no top-level step", steps: []metrics.StepRecord{step("group", "failed")}, want: "failed"},
		{name: "no records", want: "unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			artifact := &metrics.Artifact{Steps: tc.steps}
			if tc.hasSession {
				artifact.Sessions = []metrics.SessionRecord{{ExecutionSessionID: "selected", Outcome: tc.sessionOutcome, Status: tc.sessionStatus}}
			}
			if got := selectedSessionOutcome(artifact, "selected"); got != tc.want {
				t.Fatalf("outcome = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAggregateGitAttributesCommitsFromEveryRetriedAttempt(t *testing.T) {
	cases := []struct {
		name         string
		firstSubject string
		secondSHAs   []string
		wantSHAs     []string
		wantFiles    int64
		wantAdded    int64
		wantDeleted  int64
		wantPaths    []string
	}{
		{
			name: "both attempts commit", firstSubject: "[implement] first change",
			secondSHAs: []string{"C"}, wantSHAs: []string{"B", "C"},
			wantFiles: 3, wantAdded: 12, wantDeleted: 5, wantPaths: []string{"a.go", "shared.go", "z.go"},
		},
		{
			name: "first attempt has no prefixed commit", firstSubject: "unrelated change",
			secondSHAs: []string{"C"}, wantSHAs: []string{"C"},
			wantFiles: 2, wantAdded: 7, wantDeleted: 3, wantPaths: []string{"a.go", "shared.go"},
		},
		{
			name: "overlapping boundaries count each commit once", firstSubject: "[implement] first change",
			secondSHAs: []string{"C", "B", "C"}, wantSHAs: []string{"B", "C"},
			wantFiles: 3, wantAdded: 12, wantDeleted: 5, wantPaths: []string{"a.go", "shared.go", "z.go"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			records := []metrics.StepRecord{
				{
					ID:         "implement",
					GitStart:   &audit.GitCheckpoint{Available: true, HEAD: "A"},
					GitEnd:     &audit.GitCheckpoint{Available: true, HEAD: "B", Commits: []string{"B"}},
					GitChanges: &audit.GitChangeCounts{Available: true},
				},
				{
					ID:         "implement",
					GitStart:   &audit.GitCheckpoint{Available: true, HEAD: "B"},
					GitEnd:     &audit.GitCheckpoint{Available: true, HEAD: "C", Commits: tc.secondSHAs},
					GitChanges: &audit.GitChangeCounts{Available: true},
				},
			}
			commits := map[string]snapshottedGitCommit{
				"B": {SHA: "B", Subject: tc.firstSubject, FilesChanged: 1, LinesAdded: 5, LinesDeleted: 2, Paths: []string{"z.go"}},
				"C": {SHA: "C", Subject: "[implement] second change", FilesChanged: 2, LinesAdded: 7, LinesDeleted: 3, Paths: []string{"shared.go", "a.go"}},
			}
			want := GitEvidence{
				Attribution: "attributed", CommitSHAs: tc.wantSHAs, DeferredSHAs: []string{},
				FilesChanged: &tc.wantFiles, LinesAdded: &tc.wantAdded, LinesDeleted: &tc.wantDeleted, ChangedPaths: tc.wantPaths,
			}
			if diff := cmp.Diff(want, aggregateGit(records, commits)); diff != "" {
				t.Errorf("Git evidence (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAggregateGitUnavailableChanges(t *testing.T) {
	cases := []struct {
		name           string
		firstHEAD      string
		secondHEAD     string
		firstChanges   *audit.GitChangeCounts
		secondChanges  *audit.GitChangeCounts
		wantAttributed bool
	}{
		{name: "later unavailable attempt preserves commit", firstHEAD: "B", secondHEAD: "B", firstChanges: &audit.GitChangeCounts{Available: true}, secondChanges: &audit.GitChangeCounts{Available: false}, wantAttributed: true},
		{name: "later missing changes preserve commit", firstHEAD: "B", secondHEAD: "B", firstChanges: &audit.GitChangeCounts{Available: true}, wantAttributed: true},
		{name: "committing attempt has unavailable changes", firstHEAD: "B", secondHEAD: "B", secondChanges: &audit.GitChangeCounts{Available: true}, wantAttributed: true},
		{name: "unavailable attempt precedes commit", firstHEAD: "A", secondHEAD: "B", secondChanges: &audit.GitChangeCounts{Available: true}, wantAttributed: true},
		{name: "no head movement with one unavailable attempt", firstHEAD: "A", secondHEAD: "A", firstChanges: &audit.GitChangeCounts{Available: true, FilesChanged: 1, LinesAdded: 2}, secondChanges: &audit.GitChangeCounts{Available: false}},
		{name: "no head movement with all attempts unavailable", firstHEAD: "A", secondHEAD: "A", firstChanges: &audit.GitChangeCounts{Available: false}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			records := []metrics.StepRecord{
				{ID: "implement", GitStart: &audit.GitCheckpoint{Available: true, HEAD: "A"}, GitEnd: &audit.GitCheckpoint{Available: true, HEAD: tc.firstHEAD, Commits: []string{"B"}}, GitChanges: tc.firstChanges},
				{ID: "implement", GitStart: &audit.GitCheckpoint{Available: true, HEAD: tc.firstHEAD}, GitEnd: &audit.GitCheckpoint{Available: true, HEAD: tc.secondHEAD, Commits: []string{"B"}}, GitChanges: tc.secondChanges},
			}
			commits := map[string]snapshottedGitCommit{
				"B": {SHA: "B", Subject: "[implement] x", FilesChanged: 1, LinesAdded: 5, LinesDeleted: 2, Paths: []string{"x.go"}},
			}
			want := GitEvidence{Attribution: "unavailable", CommitSHAs: []string{}, DeferredSHAs: []string{}, Reason: "Git checkpoint evidence unavailable"}
			if tc.wantAttributed {
				files, added, deleted := int64(1), int64(5), int64(2)
				want = GitEvidence{Attribution: "attributed", CommitSHAs: []string{"B"}, DeferredSHAs: []string{}, FilesChanged: &files, LinesAdded: &added, LinesDeleted: &deleted, ChangedPaths: []string{"x.go"}}
			}
			if diff := cmp.Diff(want, aggregateGit(records, commits)); diff != "" {
				t.Errorf("Git evidence (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAggregateGitCountsRepeatedCommittedPathOnce(t *testing.T) {
	cases := []struct {
		name       string
		firstPaths []string
		firstFiles int64
		wantFiles  int64
		wantPaths  []string
	}{
		{name: "complete paths", firstPaths: []string{"z.go", "shared.go"}, firstFiles: 2, wantFiles: 3, wantPaths: []string{"a.go", "shared.go", "z.go"}},
		{name: "partial paths retain counts", firstPaths: []string{"z.go", "shared.go"}, firstFiles: 3, wantFiles: 5, wantPaths: []string{"a.go", "shared.go", "z.go"}},
		{name: "missing paths retain counts", firstFiles: 2, wantFiles: 4, wantPaths: []string{"a.go", "shared.go"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			records := []metrics.StepRecord{
				{
					ID:         "implement",
					GitStart:   &audit.GitCheckpoint{Available: true, HEAD: "A"},
					GitEnd:     &audit.GitCheckpoint{Available: true, HEAD: "B", Commits: []string{"B"}},
					GitChanges: &audit.GitChangeCounts{Available: true},
				},
				{
					ID:         "implement",
					GitStart:   &audit.GitCheckpoint{Available: true, HEAD: "B"},
					GitEnd:     &audit.GitCheckpoint{Available: true, HEAD: "C", Commits: []string{"C"}},
					GitChanges: &audit.GitChangeCounts{Available: true},
				},
			}
			commits := map[string]snapshottedGitCommit{
				"B": {SHA: "B", Subject: "[implement] first change", FilesChanged: tc.firstFiles, LinesAdded: 5, LinesDeleted: 2, Paths: tc.firstPaths},
				"C": {SHA: "C", Subject: "[implement] second change", FilesChanged: 2, LinesAdded: 7, LinesDeleted: 3, Paths: []string{"shared.go", "a.go"}},
			}
			added, deleted := int64(12), int64(5)
			want := GitEvidence{
				Attribution: "attributed", CommitSHAs: []string{"B", "C"}, DeferredSHAs: []string{},
				FilesChanged: &tc.wantFiles, LinesAdded: &added, LinesDeleted: &deleted, ChangedPaths: tc.wantPaths,
			}
			if diff := cmp.Diff(want, aggregateGit(records, commits)); diff != "" {
				t.Errorf("Git evidence (-want +got):\n%s", diff)
			}
		})
	}
}

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

func TestAggregateGitUnavailableEndCheckpointWithDifferentHEADIsUnavailable(t *testing.T) {
	records := []metrics.StepRecord{{
		ID:       "commit",
		GitStart: &audit.GitCheckpoint{Available: true, HEAD: "base"},
		GitEnd:   &audit.GitCheckpoint{HEAD: "other", Reason: "Git capture failed"},
	}}
	got := aggregateGit(records, map[string]snapshottedGitCommit{})
	if got.Attribution != "unavailable" || got.Reason != "Git checkpoint evidence unavailable" {
		t.Fatalf("git evidence = %+v, want unavailable checkpoint evidence", got)
	}
}

func TestDeferredCommitAttributionMixedCommitExcludesDeferredPath(t *testing.T) {
	earlier := metrics.StepRecord{
		ID:         "edit",
		GitStart:   &audit.GitCheckpoint{Available: true, HEAD: "base-head"},
		GitEnd:     &audit.GitCheckpoint{Available: true, HEAD: "base-head", Worktree: []audit.GitFileStat{{Path: "shared.txt", Added: 3}}},
		GitChanges: &audit.GitChangeCounts{Available: true, FilesChanged: 1, LinesAdded: 3},
	}
	later := metrics.StepRecord{
		ID:         "commit",
		GitStart:   &audit.GitCheckpoint{Available: true, HEAD: "base-head"},
		GitEnd:     &audit.GitCheckpoint{Available: true, HEAD: "new-head", Commits: []string{"commit-sha"}},
		GitChanges: &audit.GitChangeCounts{Available: true},
	}
	for _, tc := range []struct {
		name  string
		stats []gitPathStat
	}{
		{"per-path stats", []gitPathStat{{Path: "own.txt", LinesAdded: 2, LinesDeleted: 1}, {Path: "shared.txt", LinesAdded: 3}}},
		{"no per-path stats", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			commits := map[string]snapshottedGitCommit{
				"commit-sha": {SHA: "commit-sha", Subject: "[commit] feat: add own and shared", Paths: []string{"own.txt", "shared.txt"}, FilesChanged: 2, LinesAdded: 5, LinesDeleted: 1, PathStats: tc.stats},
			}
			leaves := []LeafEvidence{
				{Skeleton: ObservationSkeleton{Git: aggregateGit([]metrics.StepRecord{earlier}, commits)}},
				{Skeleton: ObservationSkeleton{Git: aggregateGit([]metrics.StepRecord{later}, commits)}},
			}
			applyDeferredCommitAttribution(leaves)
			got := leaves[1].Skeleton.Git
			if got.Attribution != "attributed" || got.FilesChanged == nil || *got.FilesChanged != 1 {
				t.Fatalf("committing leaf = %+v", got)
			}
			if tc.stats == nil {
				if got.LinesAdded != nil || got.LinesDeleted != nil {
					t.Fatalf("lines without per-path stats = %v/%v, want nil", got.LinesAdded, got.LinesDeleted)
				}
				return
			}
			if got.LinesAdded == nil || *got.LinesAdded != 2 || got.LinesDeleted == nil || *got.LinesDeleted != 1 {
				t.Fatalf("lines = %v/%v, want 2/1", got.LinesAdded, got.LinesDeleted)
			}
		})
	}
}
