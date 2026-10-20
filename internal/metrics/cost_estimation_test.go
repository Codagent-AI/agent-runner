package metrics

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/cli"
	"github.com/codagent/agent-runner/internal/measurements"
	"github.com/codagent/agent-runner/internal/model"
)

func pricedUsage() model.UsageRecord {
	zero := int64(0)
	return model.UsageRecord{CLI: "codex", Provider: "openai", Model: "gpt-6.1-sol", Status: model.UsageCollected, Completeness: model.CompletenessPartial, Source: "codex:turn.completed", Tokens: model.TokenCounts{model.TokenInput: 100000, model.TokenCachedInput: 40000, model.TokenOutput: 1000}, RequestEvidence: &model.RequestEvidence{Status: model.UsageCollected, Requests: []model.RequestUsage{{Input: 100000, Cached: 40000, Output: 1000, CacheWrite: &zero}}}}
}
func TestCostEligibilityAndPrecedence(t *testing.T) {
	for _, tt := range []struct {
		name   string
		change func(*model.UsageRecord)
		reason model.UnavailableReason
	}{
		{"optional reasoning", func(*model.UsageRecord) {}, ""},
		{"unavailable", func(u *model.UsageRecord) { u.Status = model.UsageUnavailable }, model.UnavailableUsageUnavailable},
		{"attribution", func(u *model.UsageRecord) { u.Reason = model.UnavailableNoBaseline }, model.UnavailableUsageIncomplete},
		{"required", func(u *model.UsageRecord) { delete(u.Tokens, model.TokenCachedInput) }, model.UnavailableRequiredCategoryMissing},
		{"unknown", func(u *model.UsageRecord) { u.Model = "" }, model.UnavailableModelUnknown},
		{"unpriced", func(u *model.UsageRecord) { u.Model += "-preview" }, model.UnavailableModelUnpriced},
		{"no rollout", func(u *model.UsageRecord) { u.RequestEvidence = nil }, model.UnavailableContextBandUnknown},
		{"mismatch", func(u *model.UsageRecord) { u.RequestEvidence.Requests[0].Input-- }, model.UnavailableRequestEvidenceMismatch},
	} {
		t.Run(tt.name, func(t *testing.T) {
			u := pricedUsage()
			tt.change(&u)
			c := NewCollector(t.TempDir(), "run", "workflow", time.Now())
			got := c.Process(stepEvent(time.Now(), model.ExecutionIdentity{StepID: "cost", StepType: "agent", Kind: "step", CLI: "codex", AgentInvoked: true}, u, nil, "success", 1))
			step := c.artifact.Steps[0]
			if step.CostUnavailableReason != tt.reason {
				t.Fatalf("reason %q want %q", step.CostUnavailableReason, tt.reason)
			}
			if tt.reason == "" {
				if step.EstimatedAPICostUSD == nil || *step.EstimatedAPICostUSD != .134 || step.CostBasis != model.CostBasisEstimated || step.PriceTableVersion == "" {
					t.Fatalf("step %+v", step)
				}
				if got.Data["cost_basis"] != model.CostBasisEstimated {
					t.Fatal(got.Data)
				}
			} else if step.EstimatedAPICostUSD != nil || step.CostBasis != "" {
				t.Fatal(step)
			}
		})
	}
	c := NewCollector(t.TempDir(), "run", "workflow", time.Now())
	cost := .9
	c.Process(stepEvent(time.Now(), model.ExecutionIdentity{StepID: "reported", StepType: "agent", Kind: "step", AgentInvoked: true}, pricedUsage(), &cost, "success", 1))
	if c.artifact.Steps[0].CostBasis != model.CostBasisReported {
		t.Fatal("reported precedence")
	}
	nested := stepEvent(time.Now(), model.ExecutionIdentity{StepID: "nested", StepType: "agent", Kind: "nested-agent", AgentInvoked: true}, pricedUsage(), nil, "success", 1)
	nested.Type = audit.EventNestedAgentEnd
	c.Process(nested)
	if c.artifact.Steps[1].EstimatedAPICostUSD != nil {
		t.Fatal("nested estimated")
	}
}

func TestCostTurnsTotalsAndClone(t *testing.T) {
	u := pricedUsage()
	cloned := cloneUsage(&u)
	*u.RequestEvidence.Requests[0].CacheWrite = 10
	if *cloned.RequestEvidence.Requests[0].CacheWrite != 0 {
		t.Fatal("request evidence not cloned")
	}
	c := NewCollector(t.TempDir(), "run", "workflow", time.Now())
	turn := pricedUsage()
	other := pricedUsage()
	other.Model = "gpt-6-luna"
	u = pricedUsage()
	u.Turns = []model.UsageRecord{turn, other}
	c.Process(stepEvent(time.Now(), model.ExecutionIdentity{StepID: "turns", StepType: "agent", Kind: "step", AgentInvoked: true}, u, nil, "success", 1))
	step := c.artifact.Steps[0]
	if step.EstimatedAPICostUSD == nil || *step.EstimatedAPICostUSD != .1409 {
		t.Fatalf("turn sum %+v", step)
	}
	u.Turns = append(u.Turns, model.UsageRecord{CLI: "codex", Status: model.UsageUnavailable})
	c.Process(stepEvent(time.Now(), model.ExecutionIdentity{StepID: "partial", StepType: "agent", Kind: "step", AgentInvoked: true}, u, nil, "success", 1))
	step = c.artifact.Steps[1]
	if step.CostUnavailableReason != model.UnavailableTurnUnattributed || step.EstimatedAPICostUSD != nil || step.CostEstimate == nil || step.CostEstimate.PartialSubtotalUSD == nil {
		t.Fatalf("partial %+v", step)
	}
	cost := .2
	c.Process(stepEvent(time.Now(), model.ExecutionIdentity{StepID: "reported", StepType: "agent", Kind: "step", AgentInvoked: true}, model.UsageRecord{CLI: "claude"}, &cost, "success", 1))
	totals := c.Totals()
	if totals.CostBasis == nil || *totals.CostBasis != "mixed" || totals.CostByBasis["estimated"] != .1409 || totals.CostByBasis["reported"] != .2 {
		t.Fatalf("totals %+v", totals)
	}
	for _, s := range c.artifact.Steps {
		if s.EstimatedAPICostUSD != nil && s.CostBasis == "" {
			t.Fatal("cost without basis")
		}
	}
}

func TestCostAdapterEligibilityINT004(t *testing.T) {
	home := t.TempDir()
	sessions := filepath.Join(home, "sessions")
	if err := os.MkdirAll(sessions, 0o700); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	extract := func(thread string, input int64, cached bool) model.UsageRecord {
		t.Helper()
		raw := fmt.Sprintf(`{"timestamp":"2026-10-20T00:00:01Z","type":"token_usage_record","payload":{"thread_id":%q,"response_id":"r","usage":{"input_tokens":100000,"cached_input_tokens":40000,"output_tokens":1000,"cache_write_input_tokens":0}}}`, thread)
		if err := os.WriteFile(filepath.Join(sessions, "rollout-test-"+thread+".jsonl"), []byte(raw), 0o600); err != nil {
			t.Fatal(err)
		}
		field := ""
		if cached {
			field = fmt.Sprintf(`,"cached_input_tokens":%d`, input*4/10)
		}
		stdout := fmt.Sprintf("{\"type\":\"thread.started\",\"thread_id\":%q}\n{\"type\":\"turn.completed\",\"usage\":{\"input_tokens\":%d%s,\"output_tokens\":%d}}", thread, input, field, input/100)
		got, err := (&cli.CodexAdapter{}).ExtractUsageWithContext(stdout, cli.UsageContext{StartedAt: start, Env: []string{"CODEX_HOME=" + home}})
		if err != nil {
			t.Fatal(err)
		}
		if got.Usage.Completeness != model.CompletenessPartial || got.Usage.Reason != "" {
			t.Fatal(got.Usage)
		}
		got.Usage.Identity.EffectiveModel = "gpt-6.1-sol"
		return got.Usage
	}
	c := NewCollector(t.TempDir(), "run", "workflow", start)
	id := model.ExecutionIdentity{StepID: "fresh", StepType: "agent", Kind: "step", CLI: "codex", SessionID: "S", AgentInvoked: true}
	c.Process(stepEvent(start, id, extract("S", 100000, true), nil, "success", 1))
	id.StepID = "resume"
	id.SessionResumed = true
	c.Process(stepEvent(start, id, extract("S", 200000, true), nil, "success", 1))
	id.StepID = "turns"
	id.SessionID = "T"
	id.SessionResumed = false
	u := extract("T", 200000, true)
	u.Turns = []model.UsageRecord{extract("T", 100000, true), extract("T", 200000, true)}
	c.Process(stepEvent(start, id, u, nil, "success", 1))
	id.StepID = "missing"
	id.SessionID = "M"
	c.Process(stepEvent(start, id, extract("M", 100000, false), nil, "success", 1))
	id.StepID = "unattributed"
	id.SessionID = "U"
	u = extract("U", 100000, false)
	u.Turns = []model.UsageRecord{extract("U", 100000, true), {CLI: "codex", Status: model.UsageUnavailable}, u}
	c.Process(stepEvent(start, id, u, nil, "success", 1))
	for i, s := range c.artifact.Steps[:3] {
		want := []float64{.134, .134, .268}[i]
		if s.CostBasis != model.CostBasisEstimated || s.EstimatedAPICostUSD == nil || *s.EstimatedAPICostUSD != want {
			t.Fatalf("%s %+v", s.ID, s)
		}
	}
	for i, s := range c.artifact.Steps[3:] {
		want := []model.UnavailableReason{model.UnavailableRequiredCategoryMissing, model.UnavailableTurnUnattributed}[i]
		if s.CostUnavailableReason != want {
			t.Fatalf("%s reason %s", s.ID, s.CostUnavailableReason)
		}
	}
}

func TestCostPersistenceResumeAndNativeContractINT002(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	cost := .2
	old := pricedUsage()
	old.RequestEvidence = nil
	old.RawCumulative = cloneCounts(old.Tokens)
	legacy := Artifact{SchemaVersion: 4, RunID: "run", Workflow: "workflow", HistoryComplete: true,
		Sessions: []SessionRecord{{ExecutionSessionID: "original", StartedAt: start.Add(-time.Hour).Format(time.RFC3339), LastObservedAt: start.Add(-time.Minute).Format(time.RFC3339), EndedAt: start.Add(-time.Minute).Format(time.RFC3339), Status: SessionClosed}}, Steps: []StepRecord{
			{RecordID: "old#1", ID: "old", Kind: "step", Type: "agent", AgentInvoked: true, EstimatedAPICostUSD: &cost, ExecutionSessionID: "original", Usage: &model.UsageRecord{CLI: "claude", Status: model.UsageCollected}},
			{RecordID: "codex#1", ID: "codex", Kind: "step", Type: "agent", AgentInvoked: true, SessionID: "S", Usage: &old, ExecutionSessionID: "original"},
		}}
	raw, _ := json.Marshal(legacy)
	if err := os.WriteFile(filepath.Join(dir, FileName), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	c := NewCollector(dir, "run", "workflow", start)
	c.Process(event(audit.EventRunStart, start, map[string]any{"execution_session_id": "resumed", "resumed": true}))
	u := pricedUsage()
	u.RawCumulative = model.TokenCounts{model.TokenInput: 200000, model.TokenCachedInput: 80000, model.TokenOutput: 2000}
	c.Process(stepEvent(start, model.ExecutionIdentity{StepID: "resumed", StepType: "agent", Kind: "step", AgentInvoked: true, CLI: "codex", SessionID: "S", SessionResumed: true}, u, nil, "success", 1))
	turn := pricedUsage()
	u = pricedUsage()
	u.Turns = []model.UsageRecord{turn, pricedUsage(), pricedUsage()}
	u.Turns[1].Model = "gpt-6-luna"
	c.Process(stepEvent(start, model.ExecutionIdentity{StepID: "turns", StepType: "agent", Kind: "step", AgentInvoked: true}, u, nil, "success", 1))
	u.Turns = append(u.Turns, model.UsageRecord{CLI: "codex", Status: model.UsageUnavailable})
	c.Process(stepEvent(start, model.ExecutionIdentity{StepID: "partial", StepType: "agent", Kind: "step", AgentInvoked: true}, u, nil, "success", 1))
	u = pricedUsage()
	u.Model = "unlisted"
	c.Process(stepEvent(start, model.ExecutionIdentity{StepID: "unpriced", StepType: "agent", Kind: "step", AgentInvoked: true}, u, nil, "success", 1))
	c.Process(stepEvent(start, model.ExecutionIdentity{StepID: "shell", StepType: "shell", Kind: "step"}, model.UsageRecord{}, nil, "success", 1))
	// A real producer head, without running the producer executable.
	record := revisionFixture(t, 1, 18)
	var envelope map[string]any
	_ = json.Unmarshal(record, &envelope)
	payload := envelope["payload"].(map[string]any)
	payload["provider_reported_costs"] = []measurements.Cost{{ID: "reported", Scope: "attempt", Coverage: "full", Overlap: "established", Source: "provider_usage", Currency: measurements.StringEvidence{Availability: "available", Value: pointer("USD")}, Amount: measurements.Value{Availability: "available", Value: &cost, Source: pointer("provider_usage"), Origin: pointer("observed"), Precision: pointer("exact")}}}
	record = signedCostRecord(t, envelope)
	if err := c.IncorporateValidator(Attribution{ContextID: "ctx", ExecutionSessionID: "resumed", StepID: "validate"}, "store", []json.RawMessage{record}, DeliveryContext{}); err != nil {
		t.Fatal(err)
	}
	for refresh := 0; refresh < 2; refresh++ {
		c.refreshAggregatesLocked()
		c.persist()
		a := readArtifact(t, dir)
		if a.SchemaVersion != 4 || a.Steps[0].CostBasis != model.CostBasisReported || a.Steps[1].EstimatedAPICostUSD != nil || a.Steps[1].CostUnavailableReason != "" {
			t.Fatalf("legacy normalization %+v", a.Steps[:2])
		}
		if a.Steps[2].EstimatedAPICostUSD == nil || *a.Steps[2].EstimatedAPICostUSD != .134 {
			t.Fatalf("resumed delta %+v", a.Steps[2])
		}
		for _, s := range a.Steps {
			if s.EstimatedAPICostUSD != nil && s.CostBasis == "" {
				t.Fatal("missing basis")
			}
			if s.Kind == "nested-agent" && (s.CostBasis != model.CostBasisReported || s.EstimatedAPICostUSD == nil || *s.EstimatedAPICostUSD != cost) {
				t.Fatalf("nested %+v", s)
			}
		}
		if a.Totals.CostBasis == nil || *a.Totals.CostBasis != "mixed" || a.Totals.CostCoverage != model.CoveragePartial {
			t.Fatalf("totals %+v", a.Totals)
		}
		if len(a.SessionRollups) != 2 || a.SessionRollups[0].Totals.CostBasis == nil || *a.SessionRollups[0].Totals.CostBasis != "reported" || a.SessionRollups[1].Totals.CostBasis == nil || *a.SessionRollups[1].Totals.CostBasis != "mixed" {
			t.Fatalf("session bases %+v", a.SessionRollups)
		}
		for _, rollup := range a.SessionRollups {
			if rollup.Totals.EstimatedAPICostUSD != nil && math.Abs(rollup.Totals.CostByBasis["reported"]+rollup.Totals.CostByBasis["estimated"]-*rollup.Totals.EstimatedAPICostUSD) > 1e-15 {
				t.Fatal("session subtotal mismatch")
			}
		}
		if diff := math.Abs(a.Totals.CostByBasis["reported"] + a.Totals.CostByBasis["estimated"] - *a.Totals.EstimatedAPICostUSD); diff > 1e-15 {
			t.Fatal("basis subtotals")
		}
		full, partial := false, false
		for _, n := range a.NativeMeasurements {
			for _, cost := range n.Costs {
				if cost.ID != "estimated-cost" {
					continue
				}
				full = full || cost.Coverage == "full"
				partial = partial || cost.Coverage == "partial"
				if cost.Amount.Origin == nil || *cost.Amount.Origin != "derived" || cost.Amount.Precision == nil || *cost.Amount.Precision != "approximate" {
					t.Fatal(cost)
				}
				payload["provider_reported_costs"] = []measurements.Cost{cost}
				contract := signedCostRecord(t, envelope)
				if _, err := measurements.ValidateRecord(contract, "agent-runner", "ctx"); err != nil {
					t.Fatalf("native cost contract %s: %v\n%s", cost.Coverage, err, contract)
				}
			}
		}
		if !full || !partial {
			t.Fatalf("native costs full=%v partial=%v", full, partial)
		}
		c = NewCollector(dir, "run", "workflow", start.Add(time.Hour))
	}
}
func signedCostRecord(t *testing.T, envelope map[string]any) json.RawMessage {
	t.Helper()
	raw, _ := json.Marshal(envelope)
	canonical, err := measurements.CanonicalRecord(raw)
	if err != nil {
		t.Fatal(err)
	}
	envelope["digest"].(map[string]any)["value"] = fmt.Sprintf("%x", sha256.Sum256(canonical))
	raw, _ = json.Marshal(envelope)
	return raw
}

func TestCostTurnArithmetic(t *testing.T) {
	u := pricedUsage()
	for _, input := range []int64{50000, 100000, 150000} {
		turn := pricedUsage()
		turn.Tokens = model.TokenCounts{model.TokenInput: input, model.TokenCachedInput: 0, model.TokenOutput: 0}
		turn.RequestEvidence.Requests = []model.RequestUsage{{Input: input, CacheWrite: pointer(int64(0))}}
		u.Turns = append(u.Turns, turn)
	}
	got := estimateTurns(&u)
	if got.amount == nil || *got.amount != .6 {
		t.Fatalf("turn arithmetic %+v", got)
	}
}

func TestCostAttributionLimitationSurvivesCumulativeSnapshot(t *testing.T) {
	c := NewCollector(t.TempDir(), "run", "workflow", time.Now())
	u := pricedUsage()
	u.RawCumulative = cloneCounts(u.Tokens)
	u.Reason = model.UnavailableSessionSwitched
	c.Process(stepEvent(time.Now(), model.ExecutionIdentity{StepID: "limited", StepType: "agent", Kind: "step", AgentInvoked: true, CLI: "codex"}, u, nil, "success", 1))
	if got := c.artifact.Steps[0]; got.CostUnavailableReason != model.UnavailableUsageIncomplete || got.EstimatedAPICostUSD != nil {
		t.Fatalf("lost attribution limitation: %+v", got)
	}
}
