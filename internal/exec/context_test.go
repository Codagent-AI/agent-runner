package exec

import (
	"testing"

	"github.com/codagent/agent-runner/internal/cli"
	"github.com/codagent/agent-runner/internal/model"
)

func contextValue(v int64) *int64 { return &v }
func TestMergeContext(t *testing.T) {
	partial := &model.ContextMeasurement{Status: model.ContextPartial, Reason: model.UnavailableContextUnclassifiedUpdate, PeakContext: contextValue(180000), Source: "test"}
	available := &model.ContextMeasurement{Status: model.ContextAvailable, PeakContext: contextValue(30000), LastRequestInput: contextValue(25000), ContextWindowLimit: contextValue(1000000), Source: "test"}
	unknown := &model.ContextMeasurement{Status: model.ContextUnavailable, Reason: model.UnavailableNoUsageEvent, Source: "test"}
	for _, tc := range []struct {
		name   string
		turns  []*model.ContextMeasurement
		last   *int64
		reason model.UnavailableReason
	}{
		{"partial peak", []*model.ContextMeasurement{partial, available}, contextValue(25000), partial.Reason},
		{"unavailable final", []*model.ContextMeasurement{partial, available, unknown}, nil, unknown.Reason},
		{"legacy final", []*model.ContextMeasurement{partial, available, nil}, nil, model.UnavailableContextNotRecorded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := mergeContext(tc.turns)
			if c.Status != model.ContextPartial || c.PeakContext == nil || *c.PeakContext != 180000 || c.Reason != tc.reason || c.ContextWindowLimit == nil || *c.ContextWindowLimit != 1000000 {
				t.Fatalf("context: %+v", c)
			}
			if tc.last == nil {
				if c.LastRequestInput != nil {
					t.Fatal("last should be nil")
				}
			} else if c.LastRequestInput == nil || *c.LastRequestInput != *tc.last {
				t.Fatal("wrong last")
			}
		})
	}
	var u externalTurnUsage
	first := &AgentInvocationResult{Usage: model.UsageRecord{CLI: "claude", Context: partial}}
	u.Add(first)
	second := &AgentInvocationResult{Usage: model.UsageRecord{CLI: "claude", Context: available}}
	u.Add(second)
	if second.Usage.Context.Status != model.ContextPartial || *second.Usage.Context.PeakContext != 180000 {
		t.Fatalf("Add context %+v", second.Usage.Context)
	}
}
func TestContextExtractionFallbacks(t *testing.T) {
	for _, ctx := range []cli.InvocationContext{cli.ContextAutonomousHeadless, cli.ContextAutonomousInteractive, cli.ContextInteractive} {
		got, err := extractAgentUsage(&spawnEnvAdapter{}, "fake", ctx, "", cli.UsageContext{})
		if err != nil {
			t.Fatal(err)
		}
		reason := model.UnavailableContextUnsupported
		if ctx == cli.ContextInteractive {
			reason = got.Usage.Reason
		}
		if got.Usage.Context == nil || got.Usage.Context.Status != model.ContextUnavailable || got.Usage.Context.Reason != reason {
			t.Fatalf("context %+v", got.Usage.Context)
		}
	}
	got, err := extractAgentUsage(&cli.ClaudeAdapter{}, "claude", cli.ContextAutonomousHeadless, "broken", cli.UsageContext{})
	if err == nil || got.Usage.Context == nil || got.Usage.Context.Reason != model.UnavailableParseFailure {
		t.Fatalf("parse fallback: %+v %v", got, err)
	}
}

func TestDefaultAgentUsageHasNoContextBeforeLaunch(t *testing.T) {
	for _, headless := range []bool{true, false} {
		u := defaultAgentUsage("claude", headless)
		if u.Context != nil {
			t.Fatalf("pre-launch usage has context: %+v", u.Context)
		}
	}
}

func TestMergeContextAvailableTurns(t *testing.T) {
	first := &model.ContextMeasurement{Status: model.ContextAvailable, PeakContext: contextValue(40000), LastRequestInput: contextValue(39000), ContextWindowLimit: contextValue(1000000)}
	final := &model.ContextMeasurement{Status: model.ContextAvailable, PeakContext: contextValue(75000), LastRequestInput: contextValue(72000)}
	c := mergeContext([]*model.ContextMeasurement{first, final})
	if c.Status != model.ContextAvailable || c.Reason != "" || c.PeakContext == nil || *c.PeakContext != 75000 || c.LastRequestInput == nil || *c.LastRequestInput != 72000 || c.ContextWindowLimit == nil || *c.ContextWindowLimit != 1000000 {
		t.Fatalf("merged context %+v", c)
	}
	c = mergeContext([]*model.ContextMeasurement{nil, {Status: model.ContextUnavailable, Reason: model.UnavailableNoUsageEvent}})
	if c.Status != model.ContextUnavailable || c.LastRequestInput != nil || c.PeakContext != nil || c.Reason != model.UnavailableNoUsageEvent {
		t.Fatalf("unavailable context %+v", c)
	}
}
