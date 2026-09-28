package metrics

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/measurements"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/google/go-cmp/cmp"
)

func TestNativeMeasurementsInputUncachedByAdapter(t *testing.T) {
	available := func(value float64, source, origin, derivation string) measurements.Value {
		v := measurements.Value{Availability: "available", Value: pointer(value), Source: pointer(source), Origin: pointer(origin), Precision: pointer("exact")}
		if derivation != "" {
			v.Derivation = pointer(derivation)
		}
		return v
	}
	tests := []struct {
		name   string
		cli    string
		tokens model.TokenCounts
		legacy bool
		want   measurements.Value
	}{
		{name: "codex derives uncached input", cli: "codex", tokens: model.TokenCounts{model.TokenInput: 1000, model.TokenCachedInput: 800}, want: available(200, "runner_derivation", "derived", "codex_input_total_minus_cache_read")},
		{name: "codex missing cached input", cli: "codex", tokens: model.TokenCounts{model.TokenInput: 1000}, want: missingValue("not_reported")},
		{name: "codex missing input", cli: "codex", tokens: model.TokenCounts{model.TokenCachedInput: 800}, want: missingValue("not_reported")},
		{name: "codex cached exceeds input", cli: "codex", tokens: model.TokenCounts{model.TokenInput: 100, model.TokenCachedInput: 800}, want: missingValue("not_reported")},
		{name: "codex negative counts", cli: "codex", tokens: model.TokenCounts{model.TokenInput: -1, model.TokenCachedInput: -100}, want: missingValue("not_reported")},
		{name: "legacy codex", cli: "codex", tokens: model.TokenCounts{model.TokenInput: 1000, model.TokenCachedInput: 800}, legacy: true, want: missingValue("not_reported")},
		{name: "codex unsafe integer", cli: "codex", tokens: model.TokenCounts{model.TokenInput: 9007199254740992, model.TokenCachedInput: 0}, want: missingValue("unsafe_token_integer")},
		{name: "claude observed input", cli: "claude", tokens: model.TokenCounts{model.TokenInput: 200}, want: available(200, "provider_usage", "observed", "")},
		{name: "opencode observed input", cli: "opencode", tokens: model.TokenCounts{model.TokenInput: 200}, want: available(200, "provider_usage", "observed", "")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCollector(t.TempDir(), "run", "wf", time.Now())
			c.Process(audit.Event{Type: audit.EventStepEnd, Timestamp: "2026-09-09T00:00:00Z", Data: map[string]any{
				DataIdentity: model.ExecutionIdentity{StepID: "native", StepType: "agent", Kind: "step", AgentInvoked: true},
				DataUsage:    model.UsageRecord{Status: model.UsageCollected, CLI: tt.cli, Tokens: tt.tokens},
			}})
			if tt.legacy {
				c.artifact.Steps[0].LegacyMeasurement = true
				c.artifact.NativeMeasurements = nil
				c.refreshNativeMeasurementsLocked()
			}
			if len(c.artifact.NativeMeasurements) != 1 {
				t.Fatalf("native measurements = %d, want 1", len(c.artifact.NativeMeasurements))
			}
			got := c.artifact.NativeMeasurements[0].Tokens
			if diff := cmp.Diff(tt.want, got["input_uncached"]); diff != "" {
				t.Errorf("input_uncached mismatch (-want +got):\n%s", diff)
			}
			if tt.cli == "codex" {
				if diff := cmp.Diff(missingValue("not_reported"), got["cache_write"]); diff != "" {
					t.Errorf("cache_write mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

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
