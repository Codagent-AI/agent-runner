package metrics

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/model"
)

func TestNativeMeasurementsSeparateRequestedIdentityAndKnownFields(t *testing.T) {
	dir := t.TempDir()
	c := NewCollector(dir, "run", "wf", time.Now())
	c.Process(audit.Event{Type: audit.EventStepEnd, Timestamp: "2026-09-09T00:00:00Z", Data: map[string]any{
		DataIdentity: model.ExecutionIdentity{StepID: "native", StepType: "agent", Kind: "step", AgentInvoked: true, ExecutionSessionID: "original"},
		DataUsage:    model.UsageRecord{Status: model.UsageCollected, CLI: "codex", Model: "configured", Identity: model.InvocationIdentity{RequestedCLI: "codex", RequestedModel: "configured", EffectiveModel: "configured", ModelSource: model.IdentitySourceInvocation}, Source: "codex:turn.completed", Tokens: model.TokenCounts{model.TokenInput: 11, model.TokenOutput: 7}, TokenTotals: &model.TokenTotals{Input: 11, Output: 7, Total: 18}},
	}})
	b, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	var artifact map[string]any
	_ = json.Unmarshal(b, &artifact)
	native, ok := artifact["native_measurements"].([]any)
	if !ok || len(native) != 1 {
		t.Fatal("native common measurements missing")
	}
	n := native[0].(map[string]any)
	if len(n["observed_identities"].([]any)) != 0 {
		t.Fatal("configured model invented telemetry identity")
	}
	tokens := n["tokens"].(map[string]any)
	if tokens["normalized_total"].(map[string]any)["value"] != float64(18) {
		t.Fatal("canonical total missing")
	}
	if tokens["cache_read"].(map[string]any)["availability"] != "unavailable" {
		t.Fatal("missing cache category invented")
	}
}

func TestNativeReportedCostRemainsAuthoritativeScopedEvidence(t *testing.T) {
	dir := t.TempDir()
	c := NewCollector(dir, "run", "wf", time.Now())
	cost := 0.25
	c.Process(audit.Event{Type: audit.EventStepEnd, Timestamp: "2026-09-09T00:00:00Z", Data: map[string]any{DataIdentity: model.ExecutionIdentity{StepID: "native", StepType: "agent", Kind: "step", AgentInvoked: true}, DataUsage: model.UsageRecord{Status: model.UsageCollected, CLI: "claude", Source: "claude:result-event"}, DataEstimatedAPICostUSD: &cost}})
	if len(c.artifact.NativeMeasurements) != 1 || len(c.artifact.NativeMeasurements[0].Costs) != 1 {
		t.Fatal("provider-reported native cost was lost from authoritative evidence")
	}
	got := c.artifact.NativeMeasurements[0].Costs[0]
	if got.Scope != "attempt" || got.Amount.Value == nil || *got.Amount.Value != cost || got.Currency.Value == nil || *got.Currency.Value != "USD" {
		t.Fatalf("native cost=%+v", got)
	}
}
