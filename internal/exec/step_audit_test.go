package exec

import (
	"testing"
	"time"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/google/go-cmp/cmp"
)

func TestEmitStepEndAgentUsageFallbackUsesInvocationIdentity(t *testing.T) {
	tests := []struct {
		name    string
		data    map[string]any
		want    model.UnavailableReason
		invoked bool
	}{
		{name: "default identity", want: "not-invoked"},
		{name: "supplied uninvoked identity", data: map[string]any{"identity": model.ExecutionIdentity{AgentInvoked: false}}, want: "not-invoked"},
		{name: "invoked identity", data: map[string]any{"identity": model.ExecutionIdentity{AgentInvoked: true}}, want: model.UnavailableUnsupportedAdapter, invoked: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := &mockAuditLogger{}
			ctx := &model.ExecutionContext{AuditLogger: recorder}
			step := &model.Step{ID: "agent", CLI: "codex", Mode: model.ModeAutonomous, Prompt: "work"}

			emitStepEnd(ctx, "[agent]", time.Now(), "failed", tt.data, step)

			if len(recorder.events) != 1 || recorder.events[0].Type != audit.EventStepEnd {
				t.Fatalf("events = %+v, want one step_end", recorder.events)
			}
			gotUsage := recorder.events[0].Data["usage"].(model.UsageRecord)
			wantUsage := model.UsageRecord{Status: model.UsageUnavailable, Reason: tt.want, CLI: "codex", Source: "agent-runner"}
			if diff := cmp.Diff(wantUsage, gotUsage); diff != "" {
				t.Fatalf("usage mismatch (-want +got):\n%s", diff)
			}
			identity := recorder.events[0].Data["identity"].(model.ExecutionIdentity)
			if identity.AgentInvoked != tt.invoked {
				t.Fatalf("AgentInvoked = %t, want %t", identity.AgentInvoked, tt.invoked)
			}
		})
	}
}

func TestPrelaunchAgentFailureReportsNotInvokedUsage(t *testing.T) {
	tests := []struct {
		name     string
		preStart bool
	}{
		{name: "profile or adapter failure"},
		{name: "invocation construction failure", preStart: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := &mockAuditLogger{}
			ctx := &model.ExecutionContext{AuditLogger: recorder}
			step := &model.Step{ID: "agent", CLI: "codex", Mode: model.ModeAutonomous, Prompt: "work"}

			if tt.preStart {
				emitAgentPreStartFailure(ctx, "[agent]", time.Now(), step, "failed before launch", nil)
			} else {
				emitAgentFailure(ctx, "[agent]", time.Now(), string(model.ModeAutonomous), step, "failed before launch", nil)
			}

			end := findAuditEvent(recorder.events, audit.EventStepEnd)
			if end == nil {
				t.Fatalf("step_end missing from events: %+v", recorder.events)
			}
			wantUsage := model.UsageRecord{Status: model.UsageUnavailable, Reason: "not-invoked", CLI: "codex", Source: "agent-runner"}
			if diff := cmp.Diff(wantUsage, end.Data["usage"]); diff != "" {
				t.Fatalf("usage mismatch (-want +got):\n%s", diff)
			}
			if identity := end.Data["identity"].(model.ExecutionIdentity); identity.AgentInvoked {
				t.Fatalf("prelaunch failure unexpectedly invoked agent: %+v", identity)
			}
		})
	}
}

func TestEmitSkippedChildAgentStepReportsNotInvokedUsage(t *testing.T) {
	recorder := &mockAuditLogger{}
	ctx := &model.ExecutionContext{AuditLogger: recorder}
	step := &model.Step{ID: "skipped", CLI: "claude", Mode: model.ModeAutonomous, Prompt: "work", SkipIf: "previous_success"}

	emitSkippedChildStep(ctx, step)

	end := findAuditEvent(recorder.events, audit.EventStepEnd)
	if end == nil {
		t.Fatalf("step_end missing from events: %+v", recorder.events)
	}
	wantUsage := model.UsageRecord{Status: model.UsageUnavailable, Reason: "not-invoked", CLI: "claude", Source: "agent-runner"}
	if diff := cmp.Diff(wantUsage, end.Data["usage"]); diff != "" {
		t.Fatalf("usage mismatch (-want +got):\n%s", diff)
	}
	if identity := end.Data["identity"].(model.ExecutionIdentity); identity.AgentInvoked {
		t.Fatalf("skipped child unexpectedly invoked agent: %+v", identity)
	}
}
