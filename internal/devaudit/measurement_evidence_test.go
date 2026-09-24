//go:build dev_audit

package devaudit

import (
	"encoding/json"
	"testing"

	"github.com/codagent/agent-runner/internal/metrics"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/google/go-cmp/cmp"
)

func TestLeafMeasurementEvidenceIncludesNestedAgentModels(t *testing.T) {
	usage := func(modelName string, tokens int64) *model.UsageRecord {
		return &model.UsageRecord{Status: model.UsageCollected, Model: modelName, TokenTotals: &model.TokenTotals{Total: tokens}}
	}
	artifact := metrics.Artifact{
		Steps: []metrics.StepRecord{
			{RecordID: "leaf", ID: "implement", Kind: "step", Type: "agent", AgentInvoked: true, ExecutionSessionID: "session", Usage: usage("claude-opus-5", 10)},
			{RecordID: "child-1", ID: "child-1", Prefix: "implement", Kind: "nested-agent", Type: "agent", AgentInvoked: true, ExecutionSessionID: "session", Usage: usage("gpt-5.6-luna", 20)},
			{RecordID: "child-2", ID: "child-2", Prefix: "implement", Kind: "nested-agent", Type: "agent", AgentInvoked: true, ExecutionSessionID: "session", Usage: usage("gpt-5.6-luna", 30)},
			{RecordID: "child-3", ID: "child-3", Prefix: "implement, child-2", Kind: "nested-agent", Type: "agent", AgentInvoked: true, ExecutionSessionID: "session", Usage: usage("gpt-5.6-luna", 40)},
		},
		MeasurementHeads: []metrics.MeasurementHead{{
			Attribution: metrics.Attribution{ExecutionSessionID: "session", StepID: "implement"},
			Record:      json.RawMessage(`{"payload":{"observed_identities":[{"model":"claude-opus-5"}]}}`),
		}},
	}
	leaves := buildLeaves(&Request{ExecutionSessionID: "session"}, &artifact, nil, nil)
	if len(leaves) != 1 {
		t.Fatalf("got %d leaves, want 1", len(leaves))
	}
	cost := leaves[0].Skeleton.Cost
	if cost.TotalTokens == nil {
		t.Fatal("total tokens unknown, want 100")
	}
	if diff := cmp.Diff(int64(100), *cost.TotalTokens); diff != "" {
		t.Errorf("total tokens (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"claude-opus-5", "gpt-5.6-luna"}, cost.SourceModels); diff != "" {
		t.Errorf("source models (-want +got):\n%s", diff)
	}
}
