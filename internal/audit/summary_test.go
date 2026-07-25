package audit

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestBuildSummary(t *testing.T) {
	log := strings.Join([]string{
		auditLine(t, Event{Timestamp: "2026-05-24T10:00:00Z", Type: EventRunStart, Data: map[string]any{"workflow_name": "debug"}}),
		auditLine(t, Event{Timestamp: "2026-05-24T10:00:01Z", Prefix: "[triage]", Type: EventStepStart, Data: map[string]any{"type": "agent"}}),
		auditLine(t, Event{Timestamp: "2026-05-24T10:00:02Z", Prefix: "[triage]", Type: EventError, Data: map[string]any{"message": "failed with ghp_AbC123XyZ"}}),
		auditLine(t, Event{Timestamp: "2026-05-24T10:00:03Z", Prefix: "[triage]", Type: EventStepEnd, Data: map[string]any{"outcome": "failed"}}),
		auditLine(t, Event{Timestamp: "2026-05-24T10:00:04Z", Prefix: "[triage, sub:child]", Type: EventSubWorkflowStart, Data: map[string]any{"workflow": "child.yaml"}}),
		auditLine(t, Event{Timestamp: "2026-05-24T10:00:05Z", Prefix: "[triage, sub:child]", Type: EventSubWorkflowEnd, Data: map[string]any{"outcome": "success"}}),
		auditLine(t, Event{Timestamp: "2026-05-24T10:00:06Z", Type: EventRunEnd, Data: map[string]any{"outcome": "failed"}}),
	}, "")

	got, err := BuildSummary(strings.NewReader(log), 64*1024)
	if err != nil {
		t.Fatalf("BuildSummary returned error: %v", err)
	}

	want := Summary{
		RunStart: &EventRef{Timestamp: "2026-05-24T10:00:00Z", Type: EventRunStart, Data: map[string]any{"workflow_name": "debug"}},
		RunEnd:   &EventRef{Timestamp: "2026-05-24T10:00:06Z", Type: EventRunEnd, Data: map[string]any{"outcome": "failed"}},
		Steps: []StepBoundary{
			{Timestamp: "2026-05-24T10:00:01Z", Prefix: "[triage]", Type: EventStepStart, StepType: "agent", Data: map[string]any{"type": "agent"}},
			{Timestamp: "2026-05-24T10:00:03Z", Prefix: "[triage]", Type: EventStepEnd, Outcome: "failed", Data: map[string]any{"outcome": "failed"}},
		},
		AgentCalls: []AgentCallBoundary{},
		SubWorkflows: []SubWorkflowBoundary{
			{Timestamp: "2026-05-24T10:00:04Z", Prefix: "[triage, sub:child]", Type: EventSubWorkflowStart, Workflow: "child.yaml", Data: map[string]any{"workflow": "child.yaml"}},
			{Timestamp: "2026-05-24T10:00:05Z", Prefix: "[triage, sub:child]", Type: EventSubWorkflowEnd, Outcome: "success", Data: map[string]any{"outcome": "success"}},
		},
		Failures: []FailureEvent{
			{Timestamp: "2026-05-24T10:00:03Z", Prefix: "[triage]", Type: EventStepEnd, Outcome: "failed", Data: map[string]any{"outcome": "failed"}},
		},
		Errors: []ErrorEvent{
			{Timestamp: "2026-05-24T10:00:02Z", Prefix: "[triage]", Message: "failed with <REDACTED>", Data: map[string]any{"message": "failed with <REDACTED>"}},
		},
		Truncated:          false,
		DroppedEventsCount: 0,
	}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("summary mismatch (-want +got):\n%s", diff)
	}
}

func TestBuildSummaryClassifiesAgentCallsSeparatelyFromWorkflowSteps(t *testing.T) {
	log := auditLine(t, Event{Timestamp: "2026-05-24T10:00:01Z", Prefix: "[parent, call:call-1]", Type: EventAgentCallStart, Data: map[string]any{
		"call_id": "call-1", "target_kind": "agent", "target_name": "implementor",
	}}) + auditLine(t, Event{Timestamp: "2026-05-24T10:00:02Z", Prefix: "[parent, call:call-1]", Type: EventAgentCallEnd, Data: map[string]any{
		"call_id": "call-1", "outcome": "failed", "error": "child failed",
	}})

	got, err := BuildSummary(strings.NewReader(log), 64*1024)
	if err != nil {
		t.Fatalf("BuildSummary returned error: %v", err)
	}
	if len(got.Steps) != 0 {
		t.Fatalf("agent calls were represented as workflow steps: %+v", got.Steps)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(encoded, &raw); err != nil {
		t.Fatal(err)
	}
	calls, ok := raw["agent_calls"].([]any)
	if !ok || len(calls) != 2 {
		t.Fatalf("agent_calls = %#v, want start/end pair", raw["agent_calls"])
	}
	if len(got.Failures) != 1 || got.Failures[0].Type != EventAgentCallEnd {
		t.Fatalf("failures = %+v, want failed agent call", got.Failures)
	}
}

func TestBuildSummaryTruncatesStructuredEvents(t *testing.T) {
	log := auditLine(t, Event{Timestamp: "2026-05-24T10:00:01Z", Prefix: "[one]", Type: EventStepStart, Data: map[string]any{"type": "shell"}}) +
		auditLine(t, Event{Timestamp: "2026-05-24T10:00:02Z", Prefix: "[two]", Type: EventStepStart, Data: map[string]any{"type": "shell"}}) +
		auditLine(t, Event{Timestamp: "2026-05-24T10:00:03Z", Prefix: "[three]", Type: EventStepStart, Data: map[string]any{"type": "shell"}})

	got, err := BuildSummary(strings.NewReader(log), 1)
	if err != nil {
		t.Fatalf("BuildSummary returned error: %v", err)
	}
	if !got.Truncated {
		t.Fatal("Truncated = false, want true")
	}
	if got.DroppedEventsCount != 3 {
		t.Fatalf("DroppedEventsCount = %d, want 3", got.DroppedEventsCount)
	}
	if len(got.Steps) != 0 {
		t.Fatalf("len(Steps) = %d, want 0", len(got.Steps))
	}
}

func TestBuildSummaryAddsFailureEventsAndSubWorkflowPaths(t *testing.T) {
	log := auditLine(t, Event{Timestamp: "2026-05-24T10:00:00Z", Prefix: "[validator, sub:onboarding-validator]", Type: EventSubWorkflowStart, Data: map[string]any{"workflow_name": "onboarding-validator", "workflow_path": "builtin:onboarding/validator.yaml"}}) +
		auditLine(t, Event{Timestamp: "2026-05-24T10:00:01Z", Prefix: "[validator, sub:onboarding-validator, init]", Type: EventStepEnd, Data: map[string]any{"outcome": "failed", "exit_code": float64(127), "stderr": "sh: : command not found\n", "stdout": "secret ghp_AbC123XyZ"}}) +
		auditLine(t, Event{Timestamp: "2026-05-24T10:00:02Z", Prefix: "[validator, sub:onboarding-validator]", Type: EventSubWorkflowEnd, Data: map[string]any{"outcome": "failed", "workflow_name": "onboarding-validator", "workflow_path": "builtin:onboarding/validator.yaml"}})

	got, err := BuildSummary(strings.NewReader(log), 64*1024)
	if err != nil {
		t.Fatalf("BuildSummary returned error: %v", err)
	}

	if len(got.SubWorkflows) != 2 {
		t.Fatalf("len(SubWorkflows) = %d, want 2", len(got.SubWorkflows))
	}
	if got.SubWorkflows[0].WorkflowPath != "builtin:onboarding/validator.yaml" {
		t.Fatalf("WorkflowPath = %q, want builtin ref", got.SubWorkflows[0].WorkflowPath)
	}
	if len(got.Failures) != 2 {
		t.Fatalf("len(Failures) = %d, want 2: %#v", len(got.Failures), got.Failures)
	}
	stepFailure := got.Failures[0]
	if stepFailure.Type != EventStepEnd || stepFailure.ExitCode == nil || *stepFailure.ExitCode != 127 {
		t.Fatalf("step failure = %#v, want failed step with exit code 127", stepFailure)
	}
	if stepFailure.Stdout != "secret <REDACTED>" {
		t.Fatalf("Stdout = %q, want redacted stdout", stepFailure.Stdout)
	}
	subFailure := got.Failures[1]
	if subFailure.Type != EventSubWorkflowEnd || subFailure.WorkflowPath != "builtin:onboarding/validator.yaml" {
		t.Fatalf("subworkflow failure = %#v, want failed subworkflow with workflow path", subFailure)
	}
}

func TestBuildSummaryOmitsMalformedExitCodes(t *testing.T) {
	log := auditLine(t, Event{
		Timestamp: "2026-05-24T10:00:00Z",
		Prefix:    "[bad]",
		Type:      EventStepEnd,
		Data:      map[string]any{"outcome": "failed", "exit_code": 1.5},
	})

	got, err := BuildSummary(strings.NewReader(log), 64*1024)
	if err != nil {
		t.Fatalf("BuildSummary returned error: %v", err)
	}
	if len(got.Failures) != 1 {
		t.Fatalf("len(Failures) = %d, want 1", len(got.Failures))
	}
	if got.Failures[0].ExitCode != nil {
		t.Fatalf("ExitCode = %v, want nil for malformed numeric value", *got.Failures[0].ExitCode)
	}
}

func TestBuildSummaryUsesCurrentRedactionPatterns(t *testing.T) {
	original := Patterns
	t.Cleanup(func() { Patterns = original })
	Patterns = append(Patterns, regexp.MustCompile(`CUSTOM_SECRET=\S+`))

	log := auditLine(t, Event{
		Timestamp: "2026-05-24T10:00:00Z",
		Prefix:    "[s]",
		Type:      EventError,
		Data:      map[string]any{"message": "saw CUSTOM_SECRET=value"},
	})
	got, err := BuildSummary(strings.NewReader(log), 64*1024)
	if err != nil {
		t.Fatalf("BuildSummary returned error: %v", err)
	}
	if got.Errors[0].Message != "saw <REDACTED>" {
		t.Fatalf("redacted message = %q, want %q", got.Errors[0].Message, "saw <REDACTED>")
	}
}

func TestBuildSummaryRejectsMalformedAuditLine(t *testing.T) {
	_, err := BuildSummary(strings.NewReader("not-a-valid-line\n"), 64*1024)
	if err == nil {
		t.Fatal("expected malformed audit line error")
	}
}

func auditLine(t *testing.T, event Event) string {
	t.Helper()
	data, err := json.Marshal(event.Data)
	if err != nil {
		t.Fatalf("marshal event data: %v", err)
	}
	prefix := ""
	if event.Prefix != "" {
		prefix = " " + event.Prefix
	}
	return event.Timestamp + prefix + " " + string(event.Type) + " " + string(data) + "\n"
}
