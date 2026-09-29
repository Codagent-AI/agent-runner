package metrics

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/measurements"
	"github.com/codagent/agent-runner/internal/model"
)

func TestCodexRateLimitRollupGroupsAndCoverage(t *testing.T) {
	start := time.Date(2026, 9, 29, 2, 0, 0, 0, time.UTC)
	steps := []StepRecord{
		{CodexRateLimits: rateLimitEvidenceForTest("acct-a", 100, 40, 43, start)},
		{CodexRateLimits: rateLimitEvidenceForTest("acct-a", 100, 43, 45, start.Add(time.Minute))},
		{CodexRateLimits: rateLimitEvidenceForTest("acct-b", 100, 10, 11, start.Add(2*time.Minute))},
		{CodexRateLimits: rateLimitEvidenceForTest("acct-a", 200, 1, 2, start.Add(3*time.Minute))},
	}
	rollup := rollupCodexRateLimits(steps)
	if rollup.MeasuredAttempts != 4 || len(rollup.Windows) != 3 || rollup.Coverage[0].Coverage != "complete" {
		t.Fatalf("unexpected rollup: %+v", rollup)
	}
	for _, group := range rollup.Windows {
		if group.AccountScope == "acct-a" && group.ResetsAt == 100 {
			if group.Contributing != 2 || group.DeltaSum.PercentagePoints == nil || *group.DeltaSum.PercentagePoints != 5 || group.Span.PercentagePoints == nil || *group.Span.PercentagePoints != 5 {
				t.Fatalf("same-window group: %+v", group)
			}
		}
	}
	steps = append(steps, StepRecord{CodexRateLimits: &model.CodexRateLimitEvidence{Status: "unavailable", Reason: "session-log-unavailable"}})
	if got := rollupCodexRateLimits(steps).Coverage[0]; got.WithDelta != 4 || got.Measured != 5 || got.Coverage != "partial" {
		t.Fatalf("partial coverage: %+v", got)
	}
}

func TestCodexRateLimitRollupUnverifiedAndOverlap(t *testing.T) {
	at := time.Date(2026, 9, 29, 2, 0, 0, 0, time.UTC)
	a := rateLimitEvidenceForTest("acct-a", 100, 40, 43, at)
	b := rateLimitEvidenceForTest("acct-a", 100, 43, 45, at.Add(500*time.Millisecond))
	c := rateLimitEvidenceForTest("unverified", 100, 1, 2, at.Add(time.Minute))
	got := rollupCodexRateLimits([]StepRecord{{CodexRateLimits: a}, {CodexRateLimits: b}, {CodexRateLimits: c}})
	if len(got.Windows) != 2 {
		t.Fatalf("groups: %+v", got.Windows)
	}
	for _, group := range got.Windows {
		switch group.AccountScope {
		case "acct-a":
			if len(group.Limitations) != 1 || group.Limitations[0] != "overlapping-attempts" || group.DeltaSum.Availability != "unavailable" || group.DeltaSum.Reason != "overlapping-attempts" || group.DeltaSum.PercentagePoints != nil {
				t.Fatalf("overlap: %+v", group)
			}
		case "unverified":
			if group.DeltaSum.Reason != "account-unverified" || group.Span.Reason != "account-unverified" || group.Contributing != 1 {
				t.Fatalf("unverified: %+v", group)
			}
		default:
			t.Fatalf("unexpected group: %+v", group)
		}
	}
}

func TestCodexRateLimitEnrichmentRevisionAndResume(t *testing.T) {
	dir := t.TempDir()
	c := NewCollector(dir, "run", "wf", time.Now())
	reads := 0
	c.SetCodexRateLimitReader(func(req model.CodexRateLimitRequest) model.CodexRateLimitEvidence {
		reads++
		if req.ThreadID != "thread-1" || req.EndTolerance != 2*time.Second {
			t.Fatalf("wrong nested reader request: %+v", req)
		}
		return *rateLimitEvidenceForTest("acct-a", 100, 40, float64(40+reads), req.StartedAt)
	})
	attr := Attribution{ContextID: "ctx", ExecutionSessionID: "execution", StepID: "validator"}
	first := codexMeasurementFixture(t, 1, "2026-09-06T12:00:01Z", true)
	if err := c.IncorporateValidator(attr, "store", []json.RawMessage{first}, DeliveryContext{}); err != nil {
		t.Fatal(err)
	}
	if reads != 1 || len(c.artifact.RateLimitEnrichments) != 1 || c.artifact.Steps[0].CodexRateLimits == nil || c.artifact.Steps[0].CodexRateLimits.End == nil {
		t.Fatalf("enrichment absent: %+v", c.artifact)
	}
	if string(c.artifact.MeasurementHeads[0].Record) != string(first) {
		t.Fatal("accepted producer bytes changed")
	}
	c.refreshAggregatesLocked()
	if c.artifact.Steps[0].CodexRateLimits.End == nil {
		t.Fatal("refresh dropped enrichment")
	}
	second := codexMeasurementFixture(t, 2, "2026-09-06T12:00:02Z", true)
	if err := c.IncorporateValidator(attr, "store", []json.RawMessage{second}, DeliveryContext{}); err != nil {
		t.Fatal(err)
	}
	if reads != 2 || *c.artifact.Steps[0].CodexRateLimits.Deltas[0].PercentagePoints != 2 {
		t.Fatal("revision did not recapture")
	}
	loaded := NewCollector(dir, "run", "wf", time.Now())
	if loaded.artifact.Steps[0].CodexRateLimits == nil || loaded.artifact.Steps[0].CodexRateLimits.End == nil {
		t.Fatal("rehydrate lost enrichment")
	}
	third := codexMeasurementFixture(t, 3, "2026-09-06T12:00:03Z", true)
	if err := loaded.IncorporateValidator(attr, "store", []json.RawMessage{third}, DeliveryContext{}); err != nil {
		t.Fatal(err)
	}
	if got := loaded.artifact.Steps[0].CodexRateLimits; got.Reason != "stale-enrichment" || got.End != nil {
		t.Fatalf("stale enrichment: %+v", got)
	}
}

func TestCodexRateLimitEnrichmentUnavailableAndConflict(t *testing.T) {
	c := NewCollector(t.TempDir(), "run", "wf", time.Now())
	c.SetCodexRateLimitReader(func(req model.CodexRateLimitRequest) model.CodexRateLimitEvidence {
		if req.ThreadID == "" {
			return model.CodexRateLimitEvidence{Status: "unavailable", Reason: "session-unidentified", Source: "codex:session-log"}
		}
		return model.CodexRateLimitEvidence{Status: "unavailable", Reason: "session-log-unavailable", Source: "codex:session-log"}
	})
	attr := Attribution{ContextID: "ctx", ExecutionSessionID: "execution", StepID: "validator"}
	first := codexMeasurementFixture(t, 1, "2026-09-06T12:00:01Z", false)
	if err := c.IncorporateValidator(attr, "store", []json.RawMessage{first}, DeliveryContext{}); err != nil {
		t.Fatal(err)
	}
	if got := c.artifact.Steps[0].CodexRateLimits; got == nil || got.Reason != "session-unidentified" {
		t.Fatalf("missing session: %+v", got)
	}
	second := codexMeasurementFixture(t, 2, "2026-09-06T12:00:02Z", true)
	if err := c.IncorporateValidator(attr, "store", []json.RawMessage{second}, DeliveryContext{}); err != nil {
		t.Fatal(err)
	}
	if got := c.artifact.Steps[0].CodexRateLimits; got.Reason != "session-log-unavailable" || got.End != nil {
		t.Fatalf("missing log recapture: %+v", got)
	}
	conflicting := codexMeasurementFixture(t, 2, "2026-09-06T12:00:03Z", true)
	if err := c.IncorporateValidator(attr, "store", []json.RawMessage{conflicting}, DeliveryContext{}); err == nil {
		t.Fatal("conflicting revision accepted")
	}
	if got := c.artifact.Steps[0].CodexRateLimits; got.Reason != "excluded-measurement" || c.artifact.CodexRateLimits.Coverage[0].Measured != 1 {
		t.Fatalf("conflict projection: %+v", got)
	}
}

func TestCodexRateLimitUnsupportedHeadExcludedFromCoverage(t *testing.T) {
	c := NewCollector(t.TempDir(), "run", "wf", time.Now())
	c.SetCodexRateLimitReader(func(req model.CodexRateLimitRequest) model.CodexRateLimitEvidence {
		return *rateLimitEvidenceForTest("acct-a", 100, 40, 43, req.StartedAt)
	})
	attr := Attribution{ContextID: "ctx", ExecutionSessionID: "execution", StepID: "validator"}
	if err := c.IncorporateValidator(attr, "store", []json.RawMessage{codexMeasurementFixture(t, 1, "2026-09-06T12:00:01Z", true)}, DeliveryContext{}); err != nil {
		t.Fatal(err)
	}
	if err := c.IncorporateValidator(attr, "store", nil, DeliveryContext{Delivery: "blocked", Gaps: []string{"unsupported_measurement_version"}}); err != nil {
		t.Fatal(err)
	}
	if len(c.artifact.Steps) != 0 || c.artifact.CodexRateLimits != nil {
		t.Fatalf("unsupported head counted: %+v", c.artifact.CodexRateLimits)
	}
}

func codexMeasurementFixture(t *testing.T, revision int, endedAt string, session bool) json.RawMessage {
	t.Helper()
	raw := revisionFixture(t, revision, 10)
	var record map[string]any
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatal(err)
	}
	p := record["payload"].(map[string]any)
	p["adapter"] = "codex"
	p["requested_identity"].(map[string]any)["adapter"] = "codex"
	p["resolved_identity"].(map[string]any)["adapter"] = "codex"
	p["lifecycle"].(map[string]any)["ended_at"] = endedAt
	if session {
		p["provider_native_usage"] = []any{map[string]any{"source": "provider_event", "name": "provider_session_id", "value": "thread-1"}}
	}
	raw, _ = json.Marshal(record)
	canonical, err := measurements.CanonicalRecord(raw)
	if err != nil {
		t.Fatal(err)
	}
	record["digest"].(map[string]any)["value"] = fmt.Sprintf("%x", sha256.Sum256(canonical))
	raw, _ = json.Marshal(record)
	return raw
}

func TestCodexRateLimitTerminalPersistence(t *testing.T) {
	dir := t.TempDir()
	c := NewCollector(dir, "run", "wf", time.Now())
	evidence := rateLimitEvidenceForTest("acct-a", 100, 40, 43, time.Now())
	c.Process(audit.Event{Type: audit.EventStepEnd, Timestamp: time.Now().Format(time.RFC3339Nano), Data: map[string]any{
		DataIdentity: model.ExecutionIdentity{StepID: "codex", StepType: "agent", Kind: "step", CLI: "codex", AgentInvoked: true},
		DataUsage:    model.UsageRecord{Status: model.UsageCollected, CLI: "codex"}, DataCodexRateLimits: *evidence, "outcome": "success",
	}})
	loaded := NewCollector(dir, "run", "wf", time.Now())
	if len(loaded.artifact.Steps) != 1 || loaded.artifact.Steps[0].CodexRateLimits == nil || loaded.artifact.CodexRateLimits == nil {
		t.Fatalf("evidence did not survive rehydrate: %+v", loaded.artifact)
	}
	data, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil || !strings.Contains(string(data), `"codex_rate_limits"`) {
		t.Fatalf("artifact missing evidence: %v", err)
	}
	var artifact Artifact
	if err := json.Unmarshal(data, &artifact); err != nil || artifact.SchemaVersion != 4 {
		t.Fatalf("schema version changed: %v %+v", err, artifact)
	}
}

func rateLimitEvidenceForTest(scope string, reset int64, before, after float64, at time.Time) *model.CodexRateLimitEvidence {
	limit := "codex"
	minutes := int64(10080)
	start := &model.RateLimitSnapshot{ObservedAt: at.Format(time.RFC3339Nano), LimitID: &limit, Primary: model.RateLimitWindow{Reported: true, UsedPercent: &before, WindowMinutes: &minutes, ResetsAt: &reset}}
	end := &model.RateLimitSnapshot{ObservedAt: at.Add(time.Second).Format(time.RFC3339Nano), LimitID: &limit, Primary: model.RateLimitWindow{Reported: true, UsedPercent: &after, WindowMinutes: &minutes, ResetsAt: &reset}}
	delta := after - before
	return &model.CodexRateLimitEvidence{Status: "captured", Source: "codex:session-log", AccountScope: scope, Start: start, End: end, AttemptStartedAt: at.Format(time.RFC3339Nano), AttemptEndedAt: at.Add(time.Second).Format(time.RFC3339Nano), Deltas: []model.RateLimitDelta{{Window: "primary", Availability: "available", PercentagePoints: &delta}, {Window: "secondary", Availability: "unavailable", Reason: "window-not-reported"}}}
}
