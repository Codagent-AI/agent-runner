package metrics

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/model"
)

type recordingSink struct{ events []audit.Event }

func (s *recordingSink) Emit(event audit.Event) { s.events = append(s.events, event) }

func TestPipelineForwardsNormalizedEventsAndWorksWithoutSink(t *testing.T) {
	started := mustTime(t, "2026-07-17T10:00:00Z")
	collector := NewCollector(t.TempDir(), "run", "workflow", started)
	sink := &recordingSink{}
	pipeline := NewPipeline(collector, sink)
	pipeline.Emit(event(audit.EventRunStart, started, nil))
	pipeline.Emit(stepEvent(started.Add(time.Second), agentIdentity("one", true), unavailableUsage(), nil, "completed", 1))

	if len(sink.events) != 2 {
		t.Fatalf("forwarded events = %d, want 2", len(sink.events))
	}
	identity := sink.events[1].Data[DataIdentity].(model.ExecutionIdentity)
	if identity.Attempt != 1 {
		t.Fatalf("forwarded attempt = %d, want 1", identity.Attempt)
	}

	withoutSink := NewPipeline(NewCollector(t.TempDir(), "run", "workflow", started), nil)
	withoutSink.Emit(event(audit.EventRunStart, started, nil))
	withoutSink.Emit(stepEvent(started.Add(time.Second), agentIdentity("one", true), unavailableUsage(), nil, "completed", 1))
}

func TestCollectorProcessesConcurrentTerminalEventsSafely(t *testing.T) {
	started := time.Now().UTC()
	c := NewCollector(t.TempDir(), "run", "workflow", started)
	c.Process(event(audit.EventRunStart, started, nil))

	const eventCount = 20
	var wait sync.WaitGroup
	wait.Add(eventCount)
	for i := 0; i < eventCount; i++ {
		go func(offset int) {
			defer wait.Done()
			c.Process(stepEvent(started.Add(time.Duration(offset+1)*time.Millisecond), agentIdentity("same", true), unavailableUsage(), nil, "completed", 1))
		}(i)
	}
	wait.Wait()

	a := readArtifact(t, filepath.Dir(c.path))
	seen := make(map[int]bool)
	for _, record := range a.Steps {
		seen[record.Attempt] = true
	}
	if len(a.Steps) != eventCount || len(seen) != eventCount || !seen[1] || !seen[eventCount] {
		t.Fatalf("attempts are not a complete unique sequence: steps=%d attempts=%v", len(a.Steps), seen)
	}
}

func TestCollectorProjectsTerminalEventsAndNormalizesAttempts(t *testing.T) {
	dir := t.TempDir()
	started := mustTime(t, "2026-07-17T10:00:00Z")
	c := NewCollector(dir, "run-1", "workflow", started)
	c.Process(event(audit.EventRunStart, started, nil))
	cost := 0.42
	rawFirst := stepEvent(started.Add(2*time.Second), model.ExecutionIdentity{
		StepID: "plan", Prefix: "", StepType: "agent", Kind: "step", CLI: "claude", SessionID: "abc", SessionStrategy: "new", AgentInvoked: true,
	}, model.UsageRecord{
		Status: model.UsageCollected, CLI: "claude", Provider: "anthropic", Model: "sonnet",
		Tokens: model.TokenCounts{model.TokenInput: 12, model.TokenOutput: 3}, Source: "claude:result-event", Completeness: model.CompletenessComplete,
	}, &cost, "completed", 1200)
	first := c.Process(rawFirst)
	second := c.Process(stepEvent(started.Add(4*time.Second), model.ExecutionIdentity{
		StepID: "plan", StepType: "agent", Kind: "step", CLI: "claude", SessionID: "abc", SessionStrategy: "resume", AgentInvoked: true,
	}, model.UsageRecord{
		Status: model.UsageUnavailable, Reason: model.UnavailableUnsupportedAdapter, CLI: "claude", Source: "agent-runner",
	}, nil, "failed", 900))

	if got := first.Data[DataIdentity].(model.ExecutionIdentity).Attempt; got != 1 {
		t.Fatalf("first attempt = %d, want 1", got)
	}
	if got := second.Data[DataIdentity].(model.ExecutionIdentity).Attempt; got != 2 {
		t.Fatalf("second attempt = %d, want 2", got)
	}
	if rawFirst.Data[DataIdentity].(model.ExecutionIdentity).Attempt != 0 {
		t.Fatal("collector mutated input identity")
	}

	a := readArtifact(t, dir)
	if a.SchemaVersion != SchemaVersion || a.RunID != "run-1" || a.Workflow != "workflow" || !a.HistoryComplete {
		t.Fatalf("artifact header = %+v", a)
	}
	if diff := cmp.Diff([]string{"plan#1", "plan#2"}, []string{a.Steps[0].RecordID, a.Steps[1].RecordID}); diff != "" {
		t.Fatalf("record ids mismatch (-want +got):\n%s", diff)
	}
	if a.Steps[0].EstimatedAPICostUSD == nil || *a.Steps[0].EstimatedAPICostUSD != cost {
		t.Fatalf("first cost = %v, want %v", a.Steps[0].EstimatedAPICostUSD, cost)
	}
	if a.Steps[1].EstimatedAPICostUSD != nil || a.Steps[1].Usage.Status != model.UsageUnavailable {
		t.Fatalf("unavailable record = %+v", a.Steps[1])
	}
}

func TestArtifactKeepsEmptyTotalsAndNullCostExplicit(t *testing.T) {
	dir := t.TempDir()
	started := mustTime(t, "2026-07-17T10:00:00Z")
	c := NewCollector(dir, "run", "workflow", started)
	c.Process(event(audit.EventRunStart, started, nil))
	c.Process(stepEvent(started.Add(time.Second), model.ExecutionIdentity{StepID: "shell", StepType: "shell", Kind: "step"}, zeroUsage(), nil, "completed", 1))

	data, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	totals := raw["totals"].(map[string]any)
	if tokens, exists := totals["tokens"]; !exists || len(tokens.(map[string]any)) != 0 {
		t.Fatalf("totals tokens = %#v, exists=%v; want explicit empty object", tokens, exists)
	}
	if cost, exists := totals["estimated_api_cost_usd"]; !exists || cost != nil {
		t.Fatalf("totals cost = %#v, exists=%v; want explicit null", cost, exists)
	}
}

func TestCollectorAggregatesCoverageAndIterations(t *testing.T) {
	dir := t.TempDir()
	started := mustTime(t, "2026-07-17T10:00:00Z")
	c := NewCollector(dir, "run", "workflow", started)
	c.now = func() time.Time { return started.Add(5 * time.Second) }
	c.Process(event(audit.EventRunStart, started, nil))
	cost := 1.25
	c.Process(stepEvent(started.Add(time.Second), agentIdentity("one", true), collectedUsage(10), &cost, "completed", 10))
	c.Process(stepEvent(started.Add(2*time.Second), agentIdentity("two", true), unavailableUsage(), nil, "completed", 20))
	c.Process(stepEvent(started.Add(3*time.Second), agentIdentity("skipped", false), unavailableUsage(), nil, "skipped", 0))
	c.Process(stepEvent(started.Add(4*time.Second), model.ExecutionIdentity{StepID: "shell", StepType: "shell", Kind: "step"}, zeroUsage(), nil, "completed", 30))
	c.Process(audit.Event{Timestamp: started.Add(5 * time.Second).Format(time.RFC3339), Type: audit.EventIterationEnd, Data: map[string]any{
		DataIdentity: model.ExecutionIdentity{StepID: "loop", Prefix: "[loop:0]", StepType: "loop", Kind: "iteration", Iteration: 0},
		"outcome":    "completed", "duration_ms": int64(40),
	}})

	totals := c.Totals()
	want := model.RunTotals{
		ActiveDurationMS: 5000, Tokens: model.TokenCounts{model.TokenInput: 10}, UsageCoverage: model.CoveragePartial,
		EstimatedAPICostUSD: &cost, CostCoverage: model.CoveragePartial,
	}
	if diff := cmp.Diff(want, totals); diff != "" {
		t.Fatalf("totals mismatch (-want +got):\n%s", diff)
	}
	a := readArtifact(t, dir)
	last := a.Steps[len(a.Steps)-1]
	if last.Kind != "iteration" || last.Usage != nil || last.EstimatedAPICostUSD != nil || last.Iteration == nil || *last.Iteration != 0 {
		t.Fatalf("iteration record = %+v", last)
	}
}

func TestCollectorKeepsIterationAndContainerAttemptSequencesSeparate(t *testing.T) {
	started := mustTime(t, "2026-07-17T10:00:00Z")
	c := NewCollector(t.TempDir(), "run", "workflow", started)
	c.Process(event(audit.EventRunStart, started, nil))
	iterationEvent := c.Process(audit.Event{Timestamp: started.Add(time.Second).Format(time.RFC3339), Type: audit.EventIterationEnd, Data: map[string]any{
		DataIdentity: model.ExecutionIdentity{StepID: "loop", StepType: "loop", Kind: "iteration", Iteration: 0},
		"outcome":    "completed", "duration_ms": int64(10),
	}})
	containerEvent := c.Process(stepEvent(started.Add(2*time.Second), model.ExecutionIdentity{StepID: "loop", StepType: "loop", Kind: "step"}, zeroUsage(), nil, "completed", 20))

	if got := iterationEvent.Data[DataIdentity].(model.ExecutionIdentity).Attempt; got != 1 {
		t.Fatalf("iteration attempt = %d, want 1", got)
	}
	if got := containerEvent.Data[DataIdentity].(model.ExecutionIdentity).Attempt; got != 1 {
		t.Fatalf("container attempt = %d, want 1", got)
	}
	a := readArtifact(t, filepath.Dir(c.path))
	if a.Steps[0].RecordID != "@iteration/loop/0#1" || a.Steps[1].RecordID != "loop#1" {
		t.Fatalf("record ids = %q, %q", a.Steps[0].RecordID, a.Steps[1].RecordID)
	}
}

func TestCollectorRecordIDsCannotCollideAcrossKinds(t *testing.T) {
	started := mustTime(t, "2026-07-17T10:00:00Z")
	c := NewCollector(t.TempDir(), "run", "workflow", started)
	c.Process(event(audit.EventRunStart, started, nil))
	c.Process(audit.Event{Timestamp: started.Add(time.Second).Format(time.RFC3339), Type: audit.EventIterationEnd, Data: map[string]any{
		DataIdentity: model.ExecutionIdentity{StepID: "loop", StepType: "loop", Kind: "iteration", Iteration: 0},
		"outcome":    "completed", "duration_ms": int64(10),
	}})
	c.Process(stepEvent(started.Add(2*time.Second), model.ExecutionIdentity{StepID: "loop:0", StepType: "shell", Kind: "step"}, zeroUsage(), nil, "completed", 20))

	a := readArtifact(t, filepath.Dir(c.path))
	if a.Steps[0].RecordID == a.Steps[1].RecordID {
		t.Fatalf("iteration and step record IDs collided: %q", a.Steps[0].RecordID)
	}
	if a.Steps[0].Attempt != 1 || a.Steps[1].Attempt != 1 {
		t.Fatalf("distinct identities shared attempts: %+v", a.Steps)
	}
}

func TestCollectorCoverageStates(t *testing.T) {
	tests := []struct {
		name    string
		records []struct {
			invoked bool
			usage   model.UsageRecord
			cost    *float64
		}
		wantUsage model.Coverage
		wantCost  model.Coverage
	}{
		{name: "no eligible agents", wantUsage: model.CoverageNone, wantCost: model.CoverageNone},
		{name: "all reporting", records: []struct {
			invoked bool
			usage   model.UsageRecord
			cost    *float64
		}{{true, collectedUsage(1), floatPtr(2)}}, wantUsage: model.CoverageComplete, wantCost: model.CoverageComplete},
		{name: "none reporting", records: []struct {
			invoked bool
			usage   model.UsageRecord
			cost    *float64
		}{{true, unavailableUsage(), nil}}, wantUsage: model.CoverageNone, wantCost: model.CoverageNone},
		{name: "skipped excluded", records: []struct {
			invoked bool
			usage   model.UsageRecord
			cost    *float64
		}{{true, collectedUsage(1), floatPtr(2)}, {false, unavailableUsage(), nil}}, wantUsage: model.CoverageComplete, wantCost: model.CoverageComplete},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			started := time.Now().UTC().Truncate(time.Second)
			c := NewCollector(t.TempDir(), "run", "workflow", started)
			c.Process(event(audit.EventRunStart, started, nil))
			for i, record := range tt.records {
				c.Process(stepEvent(started.Add(time.Duration(i+1)*time.Second), agentIdentity(string(rune('a'+i)), record.invoked), record.usage, record.cost, "completed", 1))
			}
			got := c.Totals()
			if got.UsageCoverage != tt.wantUsage || got.CostCoverage != tt.wantCost {
				t.Fatalf("coverage = usage %q cost %q, want usage %q cost %q", got.UsageCoverage, got.CostCoverage, tt.wantUsage, tt.wantCost)
			}
		})
	}
}

func TestCollectorTotalsIncludesLiveSessionTime(t *testing.T) {
	started := mustTime(t, "2026-07-17T10:00:00Z")
	c := NewCollector(t.TempDir(), "run", "workflow", started)
	c.now = func() time.Time { return started.Add(10 * time.Second) }
	c.Process(event(audit.EventRunStart, started, nil))
	c.Process(stepEvent(started.Add(2*time.Second), agentIdentity("one", true), unavailableUsage(), nil, "completed", 1))

	if got := c.Totals().ActiveDurationMS; got != 10_000 {
		t.Fatalf("live active duration = %dms, want 10000", got)
	}
}

func TestCollectorSessionDurationPreservesFractionalStartTime(t *testing.T) {
	started := mustTime(t, "2026-07-17T10:00:00.500Z")
	c := NewCollector(t.TempDir(), "run", "workflow", started)
	c.Process(event(audit.EventRunStart, started, nil))
	c.Process(stepEvent(started.Add(10*time.Millisecond), agentIdentity("one", true), unavailableUsage(), nil, "completed", 1))

	a := readArtifact(t, filepath.Dir(c.path))
	if got := a.Sessions[0].DurationMS; got != 10 {
		t.Fatalf("session duration = %dms, want 10", got)
	}
}

func TestCollectorAttributesCumulativeUsage(t *testing.T) {
	tests := []struct {
		name       string
		prior      *model.TokenCounts
		strategy   string
		reported   model.TokenCounts
		wantStatus model.UsageStatus
		wantReason model.UnavailableReason
		wantTokens model.TokenCounts
	}{
		{name: "new session attributes from zero", strategy: "new", reported: model.TokenCounts{model.TokenInput: 10}, wantStatus: model.UsageCollected, wantTokens: model.TokenCounts{model.TokenInput: 10}},
		{name: "resumed session uses baseline", prior: counts(model.TokenCounts{model.TokenInput: 10, model.TokenOutput: 4}), strategy: "resume", reported: model.TokenCounts{model.TokenInput: 15, model.TokenOutput: 6}, wantStatus: model.UsageCollected, wantTokens: model.TokenCounts{model.TokenInput: 5, model.TokenOutput: 2}},
		{name: "resume without baseline is unavailable", strategy: "resume", reported: model.TokenCounts{model.TokenInput: 15}, wantStatus: model.UsageUnavailable, wantReason: model.UnavailableNoBaseline},
		{name: "counter reset is unavailable", prior: counts(model.TokenCounts{model.TokenInput: 10}), strategy: "resume", reported: model.TokenCounts{model.TokenInput: 3}, wantStatus: model.UsageUnavailable, wantReason: model.UnavailableCounterReset},
		{name: "missing category stays absent and new category starts at zero", prior: counts(model.TokenCounts{model.TokenInput: 10, model.TokenOutput: 4}), strategy: "inherit", reported: model.TokenCounts{model.TokenInput: 13, model.TokenReasoning: 2}, wantStatus: model.UsageCollected, wantTokens: model.TokenCounts{model.TokenInput: 3, model.TokenReasoning: 2}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			started := mustTime(t, "2026-07-17T10:00:00Z")
			c := NewCollector(dir, "run", "workflow", started)
			c.Process(event(audit.EventRunStart, started, nil))
			if tt.prior != nil {
				c.Process(stepEvent(started.Add(time.Second), model.ExecutionIdentity{StepID: "prior", StepType: "agent", Kind: "step", CLI: "codex", SessionID: "session", SessionStrategy: "new", AgentInvoked: true}, cumulativeUsage(*tt.prior), nil, "completed", 10))
			}
			gotEvent := c.Process(stepEvent(started.Add(2*time.Second), model.ExecutionIdentity{StepID: "current", StepType: "agent", Kind: "step", CLI: "codex", SessionID: "session", SessionStrategy: tt.strategy, AgentInvoked: true}, cumulativeUsage(tt.reported), nil, "completed", 10))
			got := gotEvent.Data[DataUsage].(model.UsageRecord)
			if got.Status != tt.wantStatus || got.Reason != tt.wantReason || !cmp.Equal(got.Tokens, tt.wantTokens) || !cmp.Equal(got.RawCumulative, tt.reported) {
				t.Fatalf("attributed usage = %+v, want status=%q reason=%q tokens=%v raw=%v", got, tt.wantStatus, tt.wantReason, tt.wantTokens, tt.reported)
			}
		})
	}
}

func TestCollectorRebasesAfterUnavailableCumulativeValues(t *testing.T) {
	for _, tc := range []struct {
		name, strategy string
		seed           *model.TokenCounts
		first, second  model.TokenCounts
		wantReason     model.UnavailableReason
		wantDelta      model.TokenCounts
	}{
		{name: "missing baseline", strategy: "resume", first: model.TokenCounts{model.TokenInput: 100}, second: model.TokenCounts{model.TokenInput: 125}, wantReason: model.UnavailableNoBaseline, wantDelta: model.TokenCounts{model.TokenInput: 25}},
		{name: "counter reset", strategy: "resume", seed: counts(model.TokenCounts{model.TokenInput: 100}), first: model.TokenCounts{model.TokenInput: 20}, second: model.TokenCounts{model.TokenInput: 25}, wantReason: model.UnavailableCounterReset, wantDelta: model.TokenCounts{model.TokenInput: 5}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			started := mustTime(t, "2026-07-17T10:00:00Z")
			c := NewCollector(t.TempDir(), "run", "workflow", started)
			c.Process(event(audit.EventRunStart, started, nil))
			if tc.seed != nil {
				c.Process(stepEvent(started.Add(time.Second), model.ExecutionIdentity{StepID: "baseline", StepType: "agent", Kind: "step", CLI: "codex", SessionID: "session", SessionStrategy: "new", AgentInvoked: true}, cumulativeUsage(*tc.seed), nil, "completed", 1))
			}
			first := c.Process(stepEvent(started.Add(2*time.Second), model.ExecutionIdentity{StepID: "first", StepType: "agent", Kind: "step", CLI: "codex", SessionID: "session", SessionStrategy: tc.strategy, AgentInvoked: true}, cumulativeUsage(tc.first), nil, "completed", 1))
			if got := first.Data[DataUsage].(model.UsageRecord).Reason; got != tc.wantReason {
				t.Fatalf("first unavailable reason = %q, want %q", got, tc.wantReason)
			}
			second := c.Process(stepEvent(started.Add(3*time.Second), model.ExecutionIdentity{StepID: "second", StepType: "agent", Kind: "step", CLI: "codex", SessionID: "session", SessionStrategy: "resume", AgentInvoked: true}, cumulativeUsage(tc.second), nil, "completed", 1))
			if diff := cmp.Diff(tc.wantDelta, second.Data[DataUsage].(model.UsageRecord).Tokens); diff != "" {
				t.Fatalf("rebased delta mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCollectorRehydratesBaselinesAndExcludesPausedTime(t *testing.T) {
	dir := t.TempDir()
	firstStart := mustTime(t, "2026-07-17T10:00:00Z")
	first := NewCollector(dir, "run", "workflow", firstStart)
	first.Process(event(audit.EventRunStart, firstStart, nil))
	first.Process(stepEvent(firstStart.Add(5*time.Minute), model.ExecutionIdentity{StepID: "same", StepType: "agent", Kind: "step", CLI: "codex", SessionID: "session", SessionStrategy: "new", AgentInvoked: true}, cumulativeUsage(model.TokenCounts{model.TokenInput: 100}), nil, "completed", 1))

	secondStart := firstStart.Add(65 * time.Minute)
	second := NewCollector(dir, "run", "workflow", secondStart)
	second.now = func() time.Time { return secondStart.Add(3 * time.Minute) }
	second.Process(event(audit.EventRunStart, secondStart, map[string]any{"resumed": true}))
	gotEvent := second.Process(stepEvent(secondStart.Add(3*time.Minute), model.ExecutionIdentity{StepID: "same", StepType: "agent", Kind: "step", CLI: "codex", SessionID: "session", SessionStrategy: "resume", AgentInvoked: true}, cumulativeUsage(model.TokenCounts{model.TokenInput: 140}), nil, "completed", 1))
	gotUsage := gotEvent.Data[DataUsage].(model.UsageRecord)
	if diff := cmp.Diff(model.TokenCounts{model.TokenInput: 40}, gotUsage.Tokens); diff != "" {
		t.Fatalf("rehydrated delta mismatch (-want +got):\n%s", diff)
	}
	if got := gotEvent.Data[DataIdentity].(model.ExecutionIdentity).Attempt; got != 2 {
		t.Fatalf("rehydrated attempt = %d, want 2", got)
	}
	if got := second.Totals().ActiveDurationMS; got != int64(8*time.Minute/time.Millisecond) {
		t.Fatalf("active duration = %dms, want 8m", got)
	}
	a := readArtifact(t, dir)
	if len(a.Sessions) != 2 || a.Sessions[0].Status != SessionClosed || a.Sessions[0].EndedAt != a.Sessions[0].LastObservedAt || a.Sessions[1].Status != SessionOpen {
		t.Fatalf("sessions = %+v", a.Sessions)
	}
}

func TestCollectorRecoversArtifactWithMismatchedRunIdentity(t *testing.T) {
	tests := []struct {
		name        string
		newRunID    string
		newWorkflow string
	}{
		{name: "run id differs", newRunID: "run-new", newWorkflow: "workflow-old"},
		{name: "workflow differs", newRunID: "run-old", newWorkflow: "workflow-new"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			started := mustTime(t, "2026-07-17T10:00:00Z")
			old := NewCollector(dir, "run-old", "workflow-old", started)
			old.Process(event(audit.EventRunStart, started, nil))
			old.Process(stepEvent(started.Add(time.Second), agentIdentity("old", true), unavailableUsage(), nil, "completed", 1))
			original, err := os.ReadFile(filepath.Join(dir, FileName))
			if err != nil {
				t.Fatal(err)
			}

			resumedAt := started.Add(time.Hour)
			fresh := NewCollector(dir, tt.newRunID, tt.newWorkflow, resumedAt)
			fresh.Process(event(audit.EventRunStart, resumedAt, map[string]any{"resumed": true}))
			fresh.Process(stepEvent(resumedAt.Add(time.Second), agentIdentity("new", true), unavailableUsage(), nil, "completed", 1))

			a := readArtifact(t, dir)
			if a.RunID != tt.newRunID || a.Workflow != tt.newWorkflow || a.HistoryComplete || len(a.Steps) != 1 || a.Steps[0].ID != "new" {
				t.Fatalf("fresh artifact = %+v", a)
			}
			backups, err := filepath.Glob(filepath.Join(dir, FileName+".bak-*"))
			if err != nil || len(backups) != 1 {
				t.Fatalf("backups = %v, err = %v", backups, err)
			}
			preserved, err := os.ReadFile(backups[0])
			if err != nil || !cmp.Equal(original, preserved) {
				t.Fatalf("preserved artifact differs: err=%v", err)
			}
		})
	}
}

func TestCollectorSessionObservationNeverRegresses(t *testing.T) {
	dir := t.TempDir()
	started := mustTime(t, "2026-07-17T10:00:00Z")
	c := NewCollector(dir, "run", "workflow", started)
	c.Process(event(audit.EventRunStart, started, nil))
	c.Process(stepEvent(started.Add(10*time.Second), agentIdentity("later", true), unavailableUsage(), nil, "completed", 1))
	c.Process(stepEvent(started.Add(3*time.Second), agentIdentity("earlier", true), unavailableUsage(), nil, "completed", 1))
	c.Process(event(audit.EventRunEnd, started.Add(8*time.Second), nil))

	a := readArtifact(t, dir)
	session := a.Sessions[0]
	wantObserved := started.Add(10 * time.Second).Format(time.RFC3339Nano)
	if session.LastObservedAt != wantObserved || session.EndedAt != wantObserved || session.DurationMS != 10_000 || session.Status != SessionClosed {
		t.Fatalf("session regressed: %+v", session)
	}
}

func TestCollectorClosesSessionAndEmbedsFinalTotals(t *testing.T) {
	dir := t.TempDir()
	started := mustTime(t, "2026-07-17T10:00:00Z")
	c := NewCollector(dir, "run", "workflow", started)
	c.Process(event(audit.EventRunStart, started, nil))
	c.Process(stepEvent(started.Add(2*time.Second), agentIdentity("one", true), collectedUsage(5), nil, "completed", 10))
	totals := c.Totals()
	c.Process(event(audit.EventRunEnd, started.Add(3*time.Second), map[string]any{DataTotals: totals}))
	a := readArtifact(t, dir)
	if a.Sessions[0].Status != SessionClosed || a.Sessions[0].EndedAt != started.Add(3*time.Second).Format(time.RFC3339) || a.Totals.ActiveDurationMS != 3000 {
		t.Fatalf("final artifact = %+v", a)
	}
}

func TestCollectorRecoversCorruptAndUnsupportedArtifacts(t *testing.T) {
	for _, tc := range []struct{ name, contents string }{
		{name: "corrupt", contents: "not-json"},
		{name: "newer schema", contents: `{"schema_version":2}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, FileName)
			if err := os.WriteFile(path, []byte(tc.contents), 0o600); err != nil {
				t.Fatal(err)
			}
			started := mustTime(t, "2026-07-17T10:00:00Z")
			c := NewCollector(dir, "run", "workflow", started)
			c.Process(event(audit.EventRunStart, started, map[string]any{"resumed": true}))
			c.Process(stepEvent(started.Add(time.Second), agentIdentity("one", true), unavailableUsage(), nil, "completed", 1))
			a := readArtifact(t, dir)
			if a.HistoryComplete {
				t.Fatal("fresh artifact history_complete = true, want false")
			}
			backups, err := filepath.Glob(path + ".bak-*")
			if err != nil || len(backups) != 1 {
				t.Fatalf("backups = %v, err = %v", backups, err)
			}
			preserved, err := os.ReadFile(backups[0])
			if err != nil || string(preserved) != tc.contents {
				t.Fatalf("backup contents = %q, err = %v", preserved, err)
			}
			if len(c.Errors()) == 0 {
				t.Fatal("recovery warning was not retained")
			}
		})
	}
}

func TestCollectorRecoveryBackupNamesAreUnique(t *testing.T) {
	dir := t.TempDir()
	started := mustTime(t, "2026-07-17T10:00:00Z")
	path := filepath.Join(dir, FileName)
	firstBackup := path + ".bak-" + started.Format(time.RFC3339)
	if err := os.WriteFile(firstBackup, []byte("older"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	NewCollector(dir, "run", "workflow", started)

	data, err := os.ReadFile(firstBackup)
	if err != nil || string(data) != "older" {
		t.Fatalf("original backup changed: %q, %v", data, err)
	}
	if _, err := os.Stat(firstBackup + "-2"); err != nil {
		t.Fatalf("unique second backup missing: %v", err)
	}
}

func TestCollectorRetainsWriteErrors(t *testing.T) {
	dir := t.TempDir()
	c := NewCollector(dir, "run", "workflow", time.Now())
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	c.Process(event(audit.EventRunStart, time.Now(), nil))
	c.Process(stepEvent(time.Now(), agentIdentity("one", true), unavailableUsage(), nil, "completed", 1))
	if len(c.Errors()) == 0 || !strings.Contains(c.Errors()[0].Error(), "run-metrics") {
		t.Fatalf("errors = %v", c.Errors())
	}
}

func TestCollectorConsolidatesRepeatedWriteErrors(t *testing.T) {
	dir := t.TempDir()
	c := NewCollector(dir, "run", "workflow", time.Now())
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	c.Process(event(audit.EventRunStart, time.Now(), nil))
	for i := 0; i < 25; i++ {
		c.Process(stepEvent(time.Now(), agentIdentity("same", true), unavailableUsage(), nil, "completed", 1))
	}

	errors := c.Errors()
	if len(errors) != 1 || !strings.Contains(errors[0].Error(), "25 times") {
		t.Fatalf("repeated write errors were not consolidated: %v", errors)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	c.Process(stepEvent(time.Now(), agentIdentity("same", true), unavailableUsage(), nil, "completed", 1))
	if errors := c.Errors(); len(errors) != 1 || !strings.Contains(errors[0].Error(), "25 times") {
		t.Fatalf("consolidated warning was not retained after recovery: %v", errors)
	}
}

func readArtifact(t *testing.T, dir string) Artifact {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	var a Artifact
	if err := json.Unmarshal(data, &a); err != nil {
		t.Fatalf("decode artifact: %v", err)
	}
	return a
}

func event(typ audit.EventType, at time.Time, data map[string]any) audit.Event {
	if data == nil {
		data = map[string]any{}
	}
	return audit.Event{Timestamp: at.UTC().Format(time.RFC3339Nano), Type: typ, Data: data}
}

func stepEvent(at time.Time, identity model.ExecutionIdentity, usage model.UsageRecord, cost *float64, outcome string, duration int64) audit.Event { //nolint:gocritic // value copies keep test cases concise and isolated
	return audit.Event{Timestamp: at.UTC().Format(time.RFC3339Nano), Prefix: identity.Prefix, Type: audit.EventStepEnd, Data: map[string]any{
		DataIdentity: identity, DataUsage: usage, DataEstimatedAPICostUSD: cost, "outcome": outcome, "duration_ms": duration,
	}}
}

func agentIdentity(id string, invoked bool) model.ExecutionIdentity {
	return model.ExecutionIdentity{StepID: id, StepType: "agent", Kind: "step", CLI: "claude", SessionStrategy: "new", AgentInvoked: invoked}
}

func collectedUsage(input int64) model.UsageRecord {
	return model.UsageRecord{Status: model.UsageCollected, CLI: "claude", Tokens: model.TokenCounts{model.TokenInput: input}, Source: "test", Completeness: model.CompletenessComplete}
}

func unavailableUsage() model.UsageRecord {
	return model.UsageRecord{Status: model.UsageUnavailable, Reason: model.UnavailableUnsupportedAdapter, CLI: "claude", Source: "agent-runner"}
}

func zeroUsage() model.UsageRecord {
	return model.UsageRecord{Status: model.UsageCollected, Tokens: model.TokenCounts{}, Source: "agent-runner", Completeness: model.CompletenessComplete}
}

func cumulativeUsage(raw model.TokenCounts) model.UsageRecord {
	return model.UsageRecord{Status: model.UsageCollected, CLI: "codex", RawCumulative: raw, Source: "codex:turn.completed", Completeness: model.CompletenessComplete}
}

func counts(v model.TokenCounts) *model.TokenCounts { return &v }

func floatPtr(v float64) *float64 { return &v }

func mustTime(t *testing.T, value string) time.Time {
	t.Helper()
	got, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatal(err)
	}
	return got
}
