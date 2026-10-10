package metrics

import (
	"slices"
	"testing"
	"time"

	"github.com/codagent/agent-runner/internal/measurements"
	"github.com/google/go-cmp/cmp"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/model"
)

func TestInteractiveClaudePartialCoverage(t *testing.T) {
	dir := t.TempDir()
	c := NewCollector(dir, "run", "workflow", time.Now())
	u := model.UsageRecord{CLI: "claude", Source: "claude:session-transcript", Status: model.UsageCollected, Completeness: model.CompletenessPartial, Tokens: model.TokenCounts{model.TokenInput: 3}, TokenTotals: &model.TokenTotals{Input: 3, Total: 3}, Allocations: []model.UsageAllocation{{ID: "main:opus", Kind: "main", Model: "opus", Status: model.UsageCollected, Reason: model.UnavailableTranscriptInvalid, Completeness: model.CompletenessPartial, Tokens: model.TokenCounts{model.TokenInput: 3}, TokenTotals: &model.TokenTotals{Input: 3, Total: 3}}}}
	c.Process(audit.Event{Type: audit.EventStepEnd, Data: map[string]any{DataIdentity: model.ExecutionIdentity{StepID: "claude", StepType: "agent", Kind: "step", AgentInvoked: true}, DataUsage: u}})
	artifact := readArtifact(t, dir)
	if artifact.Totals.UsageCoverage != model.CoveragePartial || artifact.Totals.TokenTotalCoverage != model.CoveragePartial || artifact.Totals.TokenTotals.Total != 3 {
		t.Fatalf("%+v", artifact.Totals)
	}
	for _, v := range artifact.NativeMeasurements[0].Tokens {
		if v.Value != nil && v.Availability != "partial" {
			t.Fatalf("%+v", v)
		}
	}
}

func TestInteractiveClaudeReportedCost(t *testing.T) {
	dir := t.TempDir()
	c := NewCollector(dir, "run", "workflow", time.Now())
	cost := 1.25
	c.Process(audit.Event{Type: audit.EventStepEnd, Data: map[string]any{
		DataIdentity:            model.ExecutionIdentity{StepID: "claude", StepType: "agent", Kind: "step", AgentInvoked: true},
		DataUsage:               model.UsageRecord{CLI: "claude", Source: "claude:session-transcript", Status: model.UsageCollected},
		DataEstimatedAPICostUSD: &cost,
	}})
	artifact := readArtifact(t, dir)
	if len(artifact.NativeMeasurements) != 1 {
		t.Fatalf("native measurements: %+v", artifact.NativeMeasurements)
	}
	native := artifact.NativeMeasurements[0]
	want := []measurements.Cost{{ID: "reported-cost", Scope: "attempt", Coverage: "full", Overlap: "established", Source: "provider_usage", Currency: measurements.StringEvidence{Availability: "available", Value: pointer("USD")}, Amount: measurements.Value{Availability: "available", Value: &cost, Source: pointer("provider_usage"), Origin: pointer("observed"), Precision: pointer("exact")}}}
	if diff := cmp.Diff(want, native.Costs); diff != "" {
		t.Errorf("costs mismatch (-want +got):\n%s", diff)
	}
	if slices.Contains(native.Limitations, "legacy_cost_scope_unavailable") {
		t.Errorf("unexpected limitations: %v", native.Limitations)
	}
}
