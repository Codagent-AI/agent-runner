//go:build dev_audit

package devaudit

import (
	"strings"
	"testing"
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
			result, err := validateValueBatch(&Request{}, pkg, &batch, nil)
			if err != nil {
				t.Fatal(err)
			}
			want := note
			if safeValueNote(note) != nil {
				want = ""
			}
			if len(result) != 1 || result[0].Note != want || result[0].OverallValue != judgment.OverallValue {
				t.Fatalf("observations = %+v", result)
			}
			if batch.Observations[0].Note != note {
				t.Fatal("original model judgment was modified")
			}
			batch.Observations[0].OverallValue = "invented"
			if _, err := validateValueBatch(&Request{}, pkg, &batch, nil); err == nil {
				t.Fatal("invalid judgment accepted")
			}
		})
	}
}
