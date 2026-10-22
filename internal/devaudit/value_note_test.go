//go:build dev_audit

package devaudit

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestValidateValueBatchOmitsRejectedOptionalNotes(t *testing.T) {
	for _, note := range []string{
		"Corrections fix empty/environment-leg semantics.",
		"https://example.test/evidence",
		"/Users/alice/private.txt",
		"token=secret",
		"multiple\nlines",
		strings.Repeat("x", 281),
		"High-level correction persisted.",
	} {
		t.Run(note, func(t *testing.T) {
			judgment := ModelValueJudgment{ObservationID: "observation", OverallValue: "medium", ChangeEffect: "intended", UniqueContribution: "unique", DownstreamEvidence: "supporting", Confidence: "medium", EvidenceCoverage: "partial", Note: note}
			batch := ModelValueBatch{BatchID: "value-001", Observations: []ModelValueJudgment{judgment}}
			pkg := ValuePackage{BatchID: batch.BatchID, Leaves: []LeafEvidence{{Skeleton: ObservationSkeleton{ObservationID: judgment.ObservationID}}}}
			result, diagnostics, err := validateValueBatch(&Request{}, pkg, &batch, nil)
			if err != nil {
				t.Fatal(err)
			}
			want := note
			wantDiagnostics := []string{}
			if reason := unsafeValueNote(note); reason != "" {
				want = ""
				wantDiagnostics = append(wantDiagnostics, "value note omitted: "+reason)
			}
			if len(result) != 1 || result[0].Note != want || result[0].OverallValue != judgment.OverallValue {
				t.Fatalf("observations = %+v", result)
			}
			if diff := cmp.Diff(wantDiagnostics, diagnostics); diff != "" {
				t.Fatalf("diagnostics (-want +got):\n%s", diff)
			}
			if batch.Observations[0].Note != note {
				t.Fatal("original model judgment was modified")
			}
			batch.Observations[0].OverallValue = "invented"
			if _, _, err := validateValueBatch(&Request{}, pkg, &batch, nil); err == nil {
				t.Fatal("invalid judgment accepted")
			}
		})
	}
}

func TestUnsafeValueNoteSlashHandling(t *testing.T) {
	allowed := []string{
		"verified F-2/F-3/F-4 in the downstream run",
		"diff was +243/-100 lines",
		"PR is MERGEABLE/CLEAN.",
		"result is pass/fail dependent",
		"requires this and/or that",
		"Corrections fix empty/environment-leg semantics.",
		"works w/o retries, i.e./e.g. fine",
		"Node.js/React.js stack mentioned",
		"ratio 3/4 (about 75%)",
		"",
	}
	blocked := []string{
		"see /Users/alice/private.txt",
		"wrote /tmp/y",
		"read ~/notes",
		"ran ./run.sh",
		"opened ../secret",
		"file C:\\Users\\x",
		"file C:/Users/x",
		"edited internal/devaudit/value_audit.go.",
		"edited (internal/value_audit.go)",
		"path=/etc/passwd",
		"under src/a/b",
		"see https://example.test/evidence",
		"token=abc",
		"uses ghp_abc123",
		"key sk-abc",
		"multiple\nlines",
		strings.Repeat("x", 281),
	}
	for _, note := range allowed {
		if reason := unsafeValueNote(note); reason != "" {
			t.Errorf("note %q withheld: %s", note, reason)
		}
	}
	for _, note := range blocked {
		if reason := unsafeValueNote(note); reason == "" {
			t.Errorf("note %q was allowed", note)
		}
	}
}
