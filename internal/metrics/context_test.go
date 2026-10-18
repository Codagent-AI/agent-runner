package metrics

import (
	"fmt"
	"testing"
	"time"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/google/go-cmp/cmp"
)

func contextPtr(v int64) *int64 { return &v }
func TestContextClone(t *testing.T) {
	u := model.UsageRecord{Context: &model.ContextMeasurement{Status: model.ContextAvailable, PeakContext: contextPtr(90000), LastRequestInput: contextPtr(30000), ContextWindowLimit: contextPtr(200000)}}
	c := cloneUsage(&u)
	*u.Context.PeakContext = 1
	*u.Context.LastRequestInput = 2
	*u.Context.ContextWindowLimit = 3
	if *c.Context.PeakContext != 90000 || *c.Context.LastRequestInput != 30000 || *c.Context.ContextWindowLimit != 200000 {
		t.Fatal("context was not deep copied")
	}
}
func TestContextLegacyResumeINT002(t *testing.T) {
	start := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	run := func(withContext bool) Artifact {
		dir := t.TempDir()
		c := NewCollector(dir, "run", "workflow", start)
		c.Process(event(audit.EventRunStart, start, nil))
		identity := model.ExecutionIdentity{StepID: "first", StepType: "agent", Kind: "step", CLI: "codex", SessionID: "S", SessionStrategy: "new", AgentInvoked: true}
		c.Process(stepEvent(start.Add(time.Second), identity, cumulativeUsage(model.TokenCounts{model.TokenInput: 100}), nil, "completed", 1))
		legacy := readArtifact(t, dir).Steps[0]
		c = NewCollector(dir, "run", "workflow", start.Add(time.Hour))
		c.Process(event(audit.EventRunStart, start.Add(time.Hour), map[string]any{"resumed": true}))
		for i, id := range []string{"S", "fresh", "", "S"} {
			identity.StepID = []string{"second", "fresh", "unknown", "call"}[i]
			identity.SessionID = id
			identity.SessionResumed = id == "S"
			identity.Kind = "step"
			if i == 3 {
				identity.Kind = "agent-call"
			}
			u := cumulativeUsage(model.TokenCounts{model.TokenInput: int64(140 + i)})
			if withContext {
				u.Context = &model.ContextMeasurement{Status: model.ContextAvailable, Source: "codex:rollout", PeakContext: contextPtr(90000 + int64(i)), LastRequestInput: contextPtr(30000), ContextWindowLimit: contextPtr(200000)}
			}
			c.Process(stepEvent(start.Add(time.Hour+time.Duration(i+1)*time.Second), identity, u, nil, "completed", 1))
		}
		c.Process(stepEvent(start.Add(2*time.Hour), model.ExecutionIdentity{StepID: "shell", StepType: "shell", Kind: "step"}, model.UsageRecord{}, nil, "completed", 1))
		a := readArtifact(t, dir)
		if diff := cmp.Diff(legacy, a.Steps[0]); diff != "" {
			t.Fatal(diff)
		}
		if a.SchemaVersion != SchemaVersion || len(a.SessionRollups) != 2 {
			t.Fatal("schema or execution sessions changed")
		}
		return a
	}
	a, b := run(true), run(false)
	if diff := cmp.Diff(b.Totals, a.Totals); diff != "" {
		t.Fatal(diff)
	}
	for i := range a.SessionRollups {
		if diff := cmp.Diff(b.SessionRollups[i].Totals, a.SessionRollups[i].Totals); diff != "" {
			t.Fatal(diff)
		}
	}
	s := a.AgentSessionContext
	if len(s.Sessions) != 2 || s.UnattributedAttempts != 1 {
		t.Fatalf("summary %+v", s)
	}
	if s.Sessions[0].SessionID != "S" || s.Sessions[0].AttemptCount != 3 || s.Sessions[0].Coverage != model.CoveragePartial || *s.Sessions[0].HighWaterMark != 90003 || *s.Sessions[0].MaxLastRequestInput != 30000 {
		t.Fatalf("session %+v", s.Sessions[0])
	}
	if s.Sessions[1].Coverage != model.CoverageComplete {
		t.Fatal("fresh coverage")
	}
	if b.AgentSessionContext.Sessions[0].Coverage != model.CoverageNone {
		t.Fatal("legacy coverage")
	}
}
func TestContextSummaryEligibility(t *testing.T) {
	c := NewCollector(t.TempDir(), "run", "flow", time.Now())
	for _, cli := range []string{"claude", "codex"} {
		c.artifact.Steps = append(c.artifact.Steps, StepRecord{Kind: "step", Type: "agent", AgentInvoked: true, SessionID: "same", Usage: &model.UsageRecord{CLI: cli, Context: &model.ContextMeasurement{Status: model.ContextPartial, PeakContext: contextPtr(10)}}})
	}
	for _, kind := range []string{"iteration", "nested-agent"} {
		c.artifact.Steps = append(c.artifact.Steps, StepRecord{Kind: kind, Type: "agent", AgentInvoked: true})
	}
	c.artifact.Steps = append(c.artifact.Steps, StepRecord{Kind: "step", Type: "agent"})
	c.refreshAggregatesLocked()
	if len(c.artifact.AgentSessionContext.Sessions) != 2 || c.artifact.AgentSessionContext.UnattributedAttempts != 0 {
		t.Fatalf("summary %+v", c.artifact.AgentSessionContext)
	}
}

func TestContextSummaryFreshAndUnattributedSessions(t *testing.T) {
	var records []StepRecord
	for i := 0; i < 10; i++ {
		records = append(records, StepRecord{Type: "agent", Kind: "step", AgentInvoked: true, SessionID: fmt.Sprint(i), Usage: &model.UsageRecord{CLI: "claude", Context: &model.ContextMeasurement{Status: model.ContextAvailable, PeakContext: contextPtr(int64(i + 1)), LastRequestInput: contextPtr(1)}}})
	}
	records = append(records, StepRecord{Type: "agent", Kind: "step", AgentInvoked: true}, StepRecord{Type: "agent", Kind: "agent-call", AgentInvoked: true})
	summary := summarizeAgentContext(records)
	if len(summary.Sessions) != 10 || summary.UnattributedAttempts != 2 {
		t.Fatalf("summary %+v", summary)
	}
	for i, s := range summary.Sessions {
		if s.SessionID != fmt.Sprint(i) || s.Coverage != model.CoverageComplete || s.AttemptCount != 1 {
			t.Fatalf("session %+v", s)
		}
	}
}
