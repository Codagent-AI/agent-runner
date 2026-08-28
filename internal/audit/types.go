package audit

// EventType identifies the kind of audit event.
type EventType string

// Audit event type constants.
const (
	EventRunStart               EventType = "run_start"
	EventRunEnd                 EventType = "run_end"
	EventStepStart              EventType = "step_start"
	EventStepEnd                EventType = "step_end"
	EventIterationStart         EventType = "iteration_start"
	EventIterationEnd           EventType = "iteration_end"
	EventSubWorkflowStart       EventType = "sub_workflow_start"
	EventSubWorkflowEnd         EventType = "sub_workflow_end"
	EventAgentCallStart         EventType = "agent_call_start"
	EventAgentCallEnd           EventType = "agent_call_end"
	EventNestedAgentEnd         EventType = "nested_agent_end"
	EventWarning                EventType = "warning"
	EventError                  EventType = "error"
	EventCompletionRequested    EventType = "completion_requested"
	EventCompletionAcknowledged EventType = "completion_acknowledged"
	EventTurnCommitted          EventType = "turn_committed"
	EventDurabilityFailure      EventType = "durability_failure"
	EventControlRejected        EventType = "control_rejected"
	EventChildStopped           EventType = "child_stopped"
	EventChildContinued         EventType = "child_continued"
	EventRouteSubmitted         EventType = "route_submitted"
	EventRouteAccepted          EventType = "route_accepted"
	EventRouteRejected          EventType = "route_rejected"
	EventRouteFrozen            EventType = "route_frozen"
	EventRouteLaunchAttempted   EventType = "route_launch_attempted"
	EventRouteLaunchFailed      EventType = "route_launch_failed"
	EventPullRequestRecorded    EventType = "pull_request_recorded"
)

// Event is a single audit log entry.
type Event struct {
	Timestamp string         `json:"timestamp"`
	Prefix    string         `json:"prefix"`
	Type      EventType      `json:"type"`
	Data      map[string]any `json:"data"`
}

// EventLogger is the contract for emitting audit events.
type EventLogger interface {
	Emit(event Event)
}
