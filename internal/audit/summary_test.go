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
		SubWorkflows: []SubWorkflowBoundary{
			{Timestamp: "2026-05-24T10:00:04Z", Prefix: "[triage, sub:child]", Type: EventSubWorkflowStart, Workflow: "child.yaml", Data: map[string]any{"workflow": "child.yaml"}},
			{Timestamp: "2026-05-24T10:00:05Z", Prefix: "[triage, sub:child]", Type: EventSubWorkflowEnd, Outcome: "success", Data: map[string]any{"outcome": "success"}},
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
