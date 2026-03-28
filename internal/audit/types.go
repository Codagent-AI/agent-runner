package audit

// EventType identifies the kind of audit event.
type EventType string

// Audit event type constants.
const (
	EventRunStart         EventType = "run_start"
	EventRunEnd           EventType = "run_end"
	EventStepStart        EventType = "step_start"
	EventStepEnd          EventType = "step_end"
	EventIterationStart   EventType = "iteration_start"
	EventIterationEnd     EventType = "iteration_end"
	EventSubWorkflowStart EventType = "sub_workflow_start"
	EventSubWorkflowEnd   EventType = "sub_workflow_end"
	EventError            EventType = "error"
)

// Event is a single audit log entry.
type Event struct {
	Timestamp string         `json:"timestamp"`
	Prefix    string         `json:"prefix"`
	Type      EventType      `json:"type"`
	Data      map[string]any `json:"data"`
}
