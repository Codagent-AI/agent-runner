package metrics

import (
	"testing"
	"time"

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
