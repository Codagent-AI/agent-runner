//go:build dev_audit

package devaudit

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/metrics"
)

func TestStepEventReferencesOwnershipAndCoverage(t *testing.T) {
	dir := t.TempDir()
	log := `now [audit] step_start {"execution_session_id":"one","mode":"headless","identity":{"step_id":"audit","step_type":"agent"}}
now [audit] step_end {"execution_session_id":"one","identity":{"step_id":"audit","step_type":"agent","attempt":1},"stdout":"first finding","output_writes":{"status":"observed","files":[]},"git_start_checkpoint":{"available":true},"git_end_checkpoint":{"available":true}}
now [audit-fix] step_end {"execution_session_id":"one","identity":{"step_id":"audit-fix","step_type":"agent","attempt":1},"stdout":"fix only"}
now [audit] step_end {"execution_session_id":"two","identity":{"step_id":"audit","step_type":"agent","attempt":1},"stdout":"wrong session"}
now [audit] step_end {"execution_session_id":"one","identity":{"step_id":"audit","step_type":"agent","attempt":2},"stdout":"final finding","output_writes":{"status":"observed","files":[]},"git_start_checkpoint":{"available":true},"git_end_checkpoint":{"available":true}}
`
	_ = os.WriteFile(filepath.Join(dir, "audit.log"), []byte(log), 0o600)
	events, err := readStepEvents(dir)
	if err != nil {
		t.Fatal(err)
	}
	refs := stepReferences(dir, "one", "audit", events, &metrics.Artifact{}, &GitEvidence{Attribution: "no_change"})
	var outputs []string
	none := false
	for _, r := range refs {
		if r.Category == "narrative" {
			outputs = append(outputs, r.Detail)
		}
		if r.Category == "artifact" && r.Status == "none_produced" {
			none = true
		}
		if strings.Contains(r.Detail, "fix only") || strings.Contains(r.Detail, "wrong session") {
			t.Fatal(r)
		}
	}
	if len(outputs) != 2 || outputs[0] != "final finding" || outputs[1] != "first finding" || !none {
		t.Fatalf("%+v", refs)
	}
	legacy := stepReferences(dir, "one", "audit-fix", events, &metrics.Artifact{}, &GitEvidence{})
	if !strings.Contains(legacy[len(legacy)-1].Detail, "not recorded") {
		t.Fatalf("%+v", legacy)
	}
}

func TestArtifactCoverage(t *testing.T) {
	for _, tc := range []struct {
		name   string
		record *audit.WriteRecord
		git    bool
		want   string
	}{
		{"absent", nil, true, "unavailable"},
		{"unavailable", &audit.WriteRecord{Status: "unavailable", Reason: "denied", Files: []audit.FileWrite{}}, true, "unavailable"},
		{"truncated", &audit.WriteRecord{Status: "truncated", Reason: "bound", Files: []audit.FileWrite{}}, true, "partial"},
		{"empty", &audit.WriteRecord{Status: "observed", Files: []audit.FileWrite{}}, true, "none_produced"},
		{"no git", &audit.WriteRecord{Status: "observed", Files: []audit.FileWrite{}}, false, "unavailable"},
		// A binary file limits only its own contribution, so the leaf is
		// partial rather than having produced nothing creditable.
		{"binary file", &audit.WriteRecord{Status: "observed", Files: []audit.FileWrite{{Path: "binary", Change: "created", After: &audit.FileVersion{Size: 7, IdentityComplete: true, Binary: true}, Contribution: &audit.Retained{Kind: "unavailable", Reason: "binary content"}}}}, true, "partial"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			events := stepEventIndex{events: map[string][]stepEvent{stepEventKey("s", "x"): {{outputWrites: tc.record, start: audit.GitCheckpoint{Available: tc.git}, end: audit.GitCheckpoint{Available: tc.git}}}}}
			refs := stepReferences(t.TempDir(), "s", "x", events, &metrics.Artifact{}, &GitEvidence{Attribution: "no_change"})
			if got := refs[len(refs)-1].Status; got != tc.want {
				t.Fatalf("got %s want %s: %+v", got, tc.want, refs)
			}
		})
	}
}

func TestBoundLeafReservesOutputAndContribution(t *testing.T) {
	leaf := LeafEvidence{Skeleton: ObservationSkeleton{ObservationID: "id"}, Evidence: []EvidenceReference{}}
	for i := 0; i < 2000; i++ {
		leaf.Evidence = append(leaf.Evidence, EvidenceReference{ID: strings.Repeat("x", 20), Category: "artifact", Status: "available", Contribution: "created", Detail: strings.Repeat("delta ", 1000)})
	}
	for i := 0; i < 50; i++ {
		leaf.Evidence = append(leaf.Evidence, EvidenceReference{ID: "output", Category: "narrative", Status: "available", Detail: "final response " + strings.Repeat("é", 10000)})
	}
	got := boundLeafDetail(&leaf)
	if encodedJSONBytes(got) > defaultLeafDetailBytes {
		t.Fatal(encodedJSONBytes(got))
	}
	var output, contribution, summary bool
	counts := map[string]int{}
	for _, r := range got.Evidence {
		if r.Status == "summarized" {
			summary = true
			if r.Omitted == 0 || r.Ref == "" {
				t.Fatal(r)
			}
		} else {
			counts[r.Category]++
		}
		if r.Category == "narrative" && strings.Contains(r.Detail, "final response") {
			output = true
		}
		if r.Contribution == "created" && r.Detail != "" {
			contribution = true
		}
	}
	if !output || !contribution || !summary || counts["artifact"] > 8 || counts["narrative"] > 8 {
		t.Fatalf("output %v contribution %v summary %v counts %v", output, contribution, summary, counts)
	}
}

func TestMissingAttemptCannotClaimCompleteArtifactCoverage(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "audit.log"), []byte(`now [x] step_end {"execution_session_id":"s","identity":{"step_id":"x","attempt":2},"stdout":"final","output_writes":{"status":"observed","files":[]},"git_start_checkpoint":{"available":true},"git_end_checkpoint":{"available":true}}`+"\n"), 0o600)
	artifact := metrics.Artifact{Steps: []metrics.StepRecord{}}
	for attempt := 1; attempt <= 2; attempt++ {
		artifact.Steps = append(artifact.Steps, metrics.StepRecord{ID: "x", Type: "agent", Kind: "step", Attempt: attempt, ExecutionSessionID: "s", GitChanges: &audit.GitChangeCounts{Available: true}, GitStart: &audit.GitCheckpoint{Available: true}, GitEnd: &audit.GitCheckpoint{Available: true}})
	}
	leaves := buildLeaves(&Request{SnapshotPath: dir, ExecutionSessionID: "s"}, &artifact, nil, nil)
	outputs := 0
	unavailable := false
	for _, ref := range leaves[0].Evidence {
		if ref.Category == "narrative" {
			outputs++
		}
		if strings.HasPrefix(ref.ID, "written-coverage-") && ref.Status == "unavailable" && strings.Contains(ref.Detail, "not recorded") {
			unavailable = true
		}
	}
	if outputs != 2 || !unavailable {
		t.Fatalf("outputs %d unavailable %v: %+v", outputs, unavailable, leaves[0])
	}
}

func TestCommitPatchCoverageAndDeferredSectionNames(t *testing.T) {
	snapshot := t.TempDir()
	_ = os.Mkdir(filepath.Join(snapshot, "git-patches"), 0o700)
	patch := "diff --git a/own b/own\n--- a/own\n+++ b/own\n@@ -1 +1 @@\n-old\n+new\n"
	_ = os.WriteFile(filepath.Join(snapshot, "git-patches", "sha.patch"), []byte(patch), 0o600)
	for _, truncated := range []bool{false, true} {
		commits := map[string]snapshottedGitCommit{"sha": {SHA: "sha", Patch: "git-patches/sha.patch", PatchTruncated: truncated}}
		leaves := []LeafEvidence{{Skeleton: ObservationSkeleton{Git: GitEvidence{Attribution: "attributed", CommitSHAs: []string{"sha"}}}, Evidence: []EvidenceReference{{ID: "written-coverage-x", Category: "artifact", Status: "available"}}}}
		attachCommitReferences(leaves, commits, snapshot)
		got := leaves[0].Evidence[0].Status
		want := "available"
		if truncated {
			want = "partial"
		}
		if got != want {
			t.Fatalf("truncated %v coverage %s want %s", truncated, got, want)
		}
	}
	quoted := "diff --git \"a/a\\tb\" \"b/a\\tb\"\n--- \"a/a\\tb\"\n+++ \"b/a\\tb\"\n+x\n"
	if got := joinPatchSections(patchSections(quoted), []string{"a\tb"}, true); got != quoted {
		t.Fatalf("quoted path split: %q", got)
	}
}

func TestExportCommitPatchBoundsAndFailureKeepsMetadata(t *testing.T) {
	repo := contentRepo(t)
	_ = os.WriteFile(filepath.Join(repo, "large"), []byte(strings.Repeat("large patch content\n", 10000)), 0o600)
	contentGit(t, repo, "add", ".")
	contentGit(t, repo, "commit", "-m", "[large] feat: large patch")
	sha := contentGit(t, repo, "rev-parse", "HEAD")
	commit := snapshottedGitCommit{SHA: sha, Subject: "[large] feat: large patch"}
	snapshot := t.TempDir()
	exportCommitPatch(repo, snapshot, &commit)
	data, err := os.ReadFile(filepath.Join(snapshot, commit.Patch))
	if err != nil || len(data) > 64<<10 || !commit.PatchTruncated || !strings.HasSuffix(string(data), "[patch truncated]\n") {
		t.Fatalf("export: %v bytes %d commit %+v", err, len(data), commit)
	}
	missing := snapshottedGitCommit{SHA: "missing", Subject: "preserved metadata"}
	exportCommitPatch(repo, snapshot, &missing)
	if missing.Subject != "preserved metadata" || missing.PatchUnavailable == "" || missing.Patch != "" {
		t.Fatalf("%+v", missing)
	}
}

func TestUnresolvedAndMissingBlobRemainPartial(t *testing.T) {
	snapshot := t.TempDir()
	_ = os.WriteFile(filepath.Join(snapshot, "context"), []byte("earlier finding plus unknown edit"), 0o600)
	for _, kind := range []string{"unresolved", "created", "unavailable"} {
		file := audit.FileWrite{Path: "ledger", Change: "modified", Contribution: &audit.Retained{Kind: kind, Context: "context", Blob: "missing", Reason: "baseline unavailable"}}
		events := stepEventIndex{events: map[string][]stepEvent{stepEventKey("s", "x"): {{start: audit.GitCheckpoint{Available: true}, end: audit.GitCheckpoint{Available: true}, outputWrites: &audit.WriteRecord{Status: "observed", Files: []audit.FileWrite{file}}}}}}
		refs := stepReferences(snapshot, "s", "x", events, &metrics.Artifact{}, &GitEvidence{Attribution: "no_change"})
		if refs[len(refs)-1].Status != "partial" {
			t.Fatalf("%s: %+v", kind, refs)
		}
		if kind == "unresolved" && (refs[1].Contribution != "context_possibly_inherited" || !strings.Contains(refs[1].Detail, "earlier finding")) {
			t.Fatal(refs)
		}
	}
}

func TestExportCommitPatchRejectsUnsafeObjectIDs(t *testing.T) {
	for _, sha := range []string{"../escape", "--output=outside", strings.Repeat("a", 39), strings.Repeat("g", 40), strings.Repeat("a", 65)} {
		snapshot := t.TempDir()
		commit := snapshottedGitCommit{SHA: sha}
		exportCommitPatch(t.TempDir(), snapshot, &commit)
		if commit.PatchUnavailable != "invalid commit object ID" || commit.Patch != "" {
			t.Fatalf("%q: %+v", sha, commit)
		}
		entries, err := os.ReadDir(snapshot)
		if err != nil || len(entries) != 0 {
			t.Fatalf("invalid SHA wrote files: %v %v", entries, err)
		}
	}
}

func TestAuditLogAccessCannotEscapeEvidenceRoot(t *testing.T) {
	source := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.log")
	content := []byte("now step_end {\"execution_session_id\":\"s\"}\n")
	if err := os.WriteFile(outside, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(source, "audit.log")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := readStepEvents(source); err == nil {
		t.Fatal("read audit log outside snapshot root")
	}
	if err := os.Remove(filepath.Join(source, "audit.log")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "audit.log"), content, 0o600); err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dest, "audit.log")); err != nil {
		t.Fatal(err)
	}
	if err := snapshotReplayContent(source, dest, map[string]struct{}{"s": {}}); err == nil {
		t.Fatal("wrote replay log outside snapshot root")
	}
	got, err := os.ReadFile(outside)
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("outside evidence changed: %q %v", got, err)
	}
}

func TestDuplicateEventsDoNotHideMissingAttempts(t *testing.T) {
	key := stepEventKey("s", "x")
	index := stepEventIndex{events: map[string][]stepEvent{key: {{attempt: 2}, {attempt: 2}}}}
	index.completeAttempts("s", "x", []metrics.StepRecord{{Attempt: 1}, {Attempt: 2}})
	for _, event := range index.events[key] {
		if event.attempt == 1 && event.missingReason == "no output recorded" && event.outputWrites == nil {
			return
		}
	}
	t.Fatalf("missing attempt hidden by duplicate events: %+v", index.events[key])
}

func TestValidatorScriptMetricsSourceWithoutMeasurement(t *testing.T) {
	for _, source := range []string{"agent-validator", "", "other-tool"} {
		t.Run(source, func(t *testing.T) {
			dir := t.TempDir()
			log := fmt.Sprintf(`now [check] step_start {"execution_session_id":"s","script":"run-validator.sh","metrics_source":%q}
now [check] step_end {"execution_session_id":"s","identity":{"step_id":"check","step_type":"script","attempt":1},"stdout":"checks passed","output_writes":{"status":"observed","files":[{"path":"report.txt","change":"created","contribution":{"kind":"created","blob":"report"}}]},"git_start_checkpoint":{"available":true},"git_end_checkpoint":{"available":true}}
`, source)
			for name, content := range map[string]string{"audit.log": log, "report": "check report"} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			// Only the script step exists; no validator measurement is present.
			artifact := metrics.Artifact{Steps: []metrics.StepRecord{{ID: "check", Type: "script", Kind: "step", Attempt: 1, ExecutionSessionID: "s"}}}
			leaves := buildLeaves(&Request{SnapshotPath: dir, ExecutionSessionID: "s"}, &artifact, nil, nil)
			if len(leaves) != 1 {
				t.Fatalf("leaves = %+v", leaves)
			}
			wantOutput, wantFile := "narrative", "artifact"
			if source == "agent-validator" {
				wantOutput, wantFile = "validation", "validation"
			}
			var output, file bool
			for _, ref := range leaves[0].Evidence {
				if ref.Detail == "checks passed" {
					output = true
					if ref.Category != wantOutput || ref.Status != "available" {
						t.Errorf("output = %+v, want available %s", ref, wantOutput)
					}
				}
				if ref.Path == "report.txt" {
					file = true
					if ref.Category != wantFile || ref.Status != "available" {
						t.Errorf("file = %+v, want available %s", ref, wantFile)
					}
				}
			}
			if !output || !file {
				t.Fatalf("missing content: %+v", leaves[0])
			}
			for _, omitted := range leaves[0].OmittedCategories {
				if source == "agent-validator" && omitted == "validation_unavailable" {
					t.Errorf("declared validator output marked unavailable: %+v", leaves[0])
				}
			}
		})
	}
}

func TestPatchSectionPathIgnoresHunkLinesOfDeletedFile(t *testing.T) {
	patch := "diff --git a/query.sql b/query.sql\ndeleted file mode 100644\n--- a/query.sql\n+++ /dev/null\n@@ -1,2 +0,0 @@\n--- a comment\n-select 1;\n"
	if got := patchSectionPath(patch); got != "query.sql" {
		t.Fatalf("path %q", got)
	}
}
