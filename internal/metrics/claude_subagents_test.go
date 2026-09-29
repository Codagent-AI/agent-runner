package metrics

import (
	"testing"
	"time"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/model"
)

func TestClaudeSubagentPartialArtifactAndCoverage(t *testing.T) {
	dir := t.TempDir()
	c := NewCollector(dir, "run", "workflow", time.Now())
	cost := 5.77
	usage := model.UsageRecord{Status: model.UsageCollected, CLI: "claude", Provider: "anthropic", Model: "opus", Source: "claude:result-event", Completeness: model.CompletenessPartial, SubagentCollection: model.CompletenessPartial, SubagentCollectionReason: model.UnavailableSubagentTranscriptMissing, Tokens: model.TokenCounts{model.TokenInput: 3, model.TokenOutput: 7}, TokenTotals: &model.TokenTotals{Input: 3, Output: 7, Total: 10}, Identity: model.InvocationIdentity{EffectiveModel: "opus", ModelSource: model.IdentitySourceTelemetry}, Allocations: []model.UsageAllocation{
		{ID: "main", Kind: "main", Status: model.UsageCollected, Model: "opus", Tokens: model.TokenCounts{model.TokenInput: 1, model.TokenOutput: 2}, TokenTotals: &model.TokenTotals{Input: 1, Output: 2, Total: 3}},
		{ID: "subagent:one:haiku", Kind: "subagent", Status: model.UsageCollected, Model: "haiku", ToolUseID: "one", Tokens: model.TokenCounts{model.TokenInput: 2, model.TokenOutput: 5}, TokenTotals: &model.TokenTotals{Input: 2, Output: 5, Total: 7}},
		{ID: "subagent:two", Kind: "subagent", Status: model.UsageUnavailable, Reason: model.UnavailableSubagentTranscriptMissing, ToolUseID: "two"},
	}}
	c.Process(audit.Event{Type: audit.EventRunStart, Timestamp: time.Now().UTC().Format(time.RFC3339Nano), Data: map[string]any{"execution_session_id": "session"}})
	c.Process(audit.Event{Type: audit.EventStepEnd, Timestamp: time.Now().UTC().Format(time.RFC3339Nano), Data: map[string]any{DataIdentity: model.ExecutionIdentity{StepID: "claude", StepType: "agent", Kind: "step", AgentInvoked: true, ExecutionSessionID: "session"}, DataUsage: usage, DataEstimatedAPICostUSD: &cost, "outcome": "success"}})
	artifact := readArtifact(t, dir)
	if artifact.Totals.UsageCoverage != model.CoveragePartial || artifact.Totals.TokenTotalCoverage != model.CoveragePartial || artifact.Totals.CostCoverage != model.CoverageComplete || artifact.Totals.TokenTotals.Total != 10 || artifact.Totals.Tokens[model.TokenInput] != 3 {
		t.Fatalf("totals=%+v", artifact.Totals)
	}
	if artifact.SessionRollups[0].Totals.UsageCoverage != model.CoveragePartial {
		t.Fatalf("session=%+v", artifact.SessionRollups[0])
	}
	n := artifact.NativeMeasurements[0]
	if n.Version != 2 || len(n.Allocations) != 3 || len(n.Observed) != 2 || n.UnallocatedUsage != nil || n.SubagentCollection == nil || n.SubagentCollection.Reason != model.UnavailableSubagentTranscriptMissing {
		t.Fatalf("native=%+v", n)
	}
	if n.Tokens["normalized_total"].Availability != "partial" || n.Allocations[1].Cost.Availability != "unavailable" || n.Allocations[1].Cost.Reason == nil || *n.Allocations[1].Cost.Reason != "allocation_cost_not_reported" {
		t.Fatalf("native tokens=%+v allocation=%+v", n.Tokens, n.Allocations[1])
	}
}

func TestOtherPartialUsageKeepsCompleteCoverage(t *testing.T) {
	usage := model.UsageRecord{Status: model.UsageCollected, CLI: "codex", Completeness: model.CompletenessPartial, Tokens: model.TokenCounts{model.TokenInput: 5}, TokenTotals: &model.TokenTotals{Input: 5, Total: 5}}
	records := []StepRecord{{Type: "agent", AgentInvoked: true, Usage: &usage}}
	got := totalsForRecords(records, 0)
	if got.UsageCoverage != model.CoverageComplete || got.TokenTotalCoverage != model.CoverageComplete {
		t.Fatalf("coverage=%+v", got)
	}
}
