package audit

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

type Summary struct {
	Path               string                `json:"path"`
	SessionDir         string                `json:"session_dir,omitempty"`
	ProjectDir         string                `json:"project_dir,omitempty"`
	RunStart           *EventRef             `json:"run_start,omitempty"`
	RunEnd             *EventRef             `json:"run_end,omitempty"`
	Steps              []StepBoundary        `json:"steps"`
	AgentCalls         []AgentCallBoundary   `json:"agent_calls"`
	SubWorkflows       []SubWorkflowBoundary `json:"sub_workflows"`
	Failures           []FailureEvent        `json:"failures"`
	Errors             []ErrorEvent          `json:"errors"`
	Truncated          bool                  `json:"truncated"`
	DroppedEventsCount int                   `json:"dropped_events_count"`
}

type EventRef struct {
	Timestamp string         `json:"timestamp,omitempty"`
	Prefix    string         `json:"prefix,omitempty"`
	Type      EventType      `json:"type"`
	Data      map[string]any `json:"data,omitempty"`
}

type StepBoundary struct {
	Timestamp string         `json:"timestamp,omitempty"`
	Prefix    string         `json:"prefix,omitempty"`
	Type      EventType      `json:"type"`
	StepType  string         `json:"step_type,omitempty"`
	Outcome   string         `json:"outcome,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
}

type AgentCallBoundary struct {
	Timestamp       string         `json:"timestamp,omitempty"`
	Prefix          string         `json:"prefix,omitempty"`
	Type            EventType      `json:"type"`
	CallID          string         `json:"call_id,omitempty"`
	ParentAttemptID string         `json:"parent_attempt_id,omitempty"`
	TargetKind      string         `json:"target_kind,omitempty"`
	TargetName      string         `json:"target_name,omitempty"`
	Outcome         string         `json:"outcome,omitempty"`
	Data            map[string]any `json:"data,omitempty"`
}

type SubWorkflowBoundary struct {
	Timestamp    string         `json:"timestamp,omitempty"`
	Prefix       string         `json:"prefix,omitempty"`
	Type         EventType      `json:"type"`
	Workflow     string         `json:"workflow,omitempty"`
	WorkflowPath string         `json:"workflow_path,omitempty"`
	Outcome      string         `json:"outcome,omitempty"`
	Data         map[string]any `json:"data,omitempty"`
}

type FailureEvent struct {
	Timestamp    string         `json:"timestamp,omitempty"`
	Prefix       string         `json:"prefix,omitempty"`
	Type         EventType      `json:"type"`
	StepType     string         `json:"step_type,omitempty"`
	Workflow     string         `json:"workflow,omitempty"`
	WorkflowPath string         `json:"workflow_path,omitempty"`
	Outcome      string         `json:"outcome,omitempty"`
	ExitCode     *int           `json:"exit_code,omitempty"`
	Error        string         `json:"error,omitempty"`
	Stderr       string         `json:"stderr,omitempty"`
	Stdout       string         `json:"stdout,omitempty"`
	Data         map[string]any `json:"data,omitempty"`
}

type ErrorEvent struct {
	Timestamp string         `json:"timestamp,omitempty"`
	Prefix    string         `json:"prefix,omitempty"`
	Message   string         `json:"message,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
}

func BuildSummary(r io.Reader, capBytes int) (Summary, error) {
	summary := Summary{
		Steps:        []StepBoundary{},
		AgentCalls:   []AgentCallBoundary{},
		SubWorkflows: []SubWorkflowBoundary{},
		Failures:     []FailureEvent{},
		Errors:       []ErrorEvent{},
	}
	if capBytes < 0 {
		capBytes = 0
	}

	used := 0
	scanner := bufio.NewScanner(r)
	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		event, err := parseAuditLine(line)
		if err != nil {
			return Summary{}, fmt.Errorf("parse audit line %d: %w", lineNo, err)
		}
		event.Data = redactValue(event.Data).(map[string]any)
		if !appendClassifiedEvent(&summary, event, capBytes, &used) {
			summary.Truncated = true
			summary.DroppedEventsCount++
		}
	}
	if err := scanner.Err(); err != nil {
		return Summary{}, err
	}
	return summary, nil
}

func appendClassifiedEvent(summary *Summary, event Event, capBytes int, used *int) bool {
	switch event.Type {
	case EventRunStart:
		ref := eventRef(event)
		if !fitsCap(ref, capBytes, used) {
			return false
		}
		summary.RunStart = &ref
	case EventRunEnd:
		ref := eventRef(event)
		if !fitsCap(ref, capBytes, used) {
			return false
		}
		summary.RunEnd = &ref
	case EventStepStart, EventStepEnd:
		item := StepBoundary{
			Timestamp: event.Timestamp,
			Prefix:    event.Prefix,
			Type:      event.Type,
			StepType:  stringField(event.Data, "type"),
			Outcome:   stringField(event.Data, "outcome"),
			Data:      event.Data,
		}
		if !fitsCap(item, capBytes, used) {
			return false
		}
		summary.Steps = append(summary.Steps, item)
		if event.Type == EventStepEnd && stringField(event.Data, "outcome") == "failed" {
			failure := failureEvent(event)
			if !fitsCap(failure, capBytes, used) {
				return false
			}
			summary.Failures = append(summary.Failures, failure)
		}
	case EventAgentCallStart, EventAgentCallEnd:
		item := AgentCallBoundary{
			Timestamp:       event.Timestamp,
			Prefix:          event.Prefix,
			Type:            event.Type,
			CallID:          stringField(event.Data, "call_id"),
			ParentAttemptID: stringField(event.Data, "parent_attempt_id"),
			TargetKind:      stringField(event.Data, "target_kind"),
			TargetName:      stringField(event.Data, "target_name"),
			Outcome:         stringField(event.Data, "outcome"),
			Data:            event.Data,
		}
		if !fitsCap(item, capBytes, used) {
			return false
		}
		summary.AgentCalls = append(summary.AgentCalls, item)
		if event.Type == EventAgentCallEnd && stringField(event.Data, "outcome") == "failed" {
			failure := failureEvent(event)
			if !fitsCap(failure, capBytes, used) {
				return false
			}
			summary.Failures = append(summary.Failures, failure)
		}
	case EventSubWorkflowStart, EventSubWorkflowEnd:
		item := SubWorkflowBoundary{
			Timestamp:    event.Timestamp,
			Prefix:       event.Prefix,
			Type:         event.Type,
			Workflow:     firstStringField(event.Data, "workflow", "workflow_name"),
			WorkflowPath: stringField(event.Data, "workflow_path"),
			Outcome:      stringField(event.Data, "outcome"),
			Data:         event.Data,
		}
		if !fitsCap(item, capBytes, used) {
			return false
		}
		summary.SubWorkflows = append(summary.SubWorkflows, item)
		if event.Type == EventSubWorkflowEnd && stringField(event.Data, "outcome") == "failed" {
			failure := failureEvent(event)
			if !fitsCap(failure, capBytes, used) {
				return false
			}
			summary.Failures = append(summary.Failures, failure)
		}
	case EventError:
		item := ErrorEvent{
			Timestamp: event.Timestamp,
			Prefix:    event.Prefix,
			Message:   stringField(event.Data, "message"),
			Data:      event.Data,
		}
		if !fitsCap(item, capBytes, used) {
			return false
		}
		summary.Errors = append(summary.Errors, item)
	}
	return true
}

const failureSnippetLimit = 2000

func failureEvent(event Event) FailureEvent {
	return FailureEvent{
		Timestamp:    event.Timestamp,
		Prefix:       event.Prefix,
		Type:         event.Type,
		StepType:     stringField(event.Data, "type"),
		Workflow:     firstStringField(event.Data, "workflow", "workflow_name"),
		WorkflowPath: stringField(event.Data, "workflow_path"),
		Outcome:      stringField(event.Data, "outcome"),
		ExitCode:     intField(event.Data, "exit_code"),
		Error:        stringField(event.Data, "error"),
		Stderr:       snippetStringField(event.Data, "stderr"),
		Stdout:       snippetStringField(event.Data, "stdout"),
		Data:         event.Data,
	}
}

func eventRef(event Event) EventRef {
	return EventRef(event)
}

func fitsCap(v any, capBytes int, used *int) bool {
	data, err := json.Marshal(v)
	if err != nil {
		return false
	}
	if *used+len(data) > capBytes {
		return false
	}
	*used += len(data)
	return true
}

func parseAuditLine(line string) (Event, error) {
	var event Event
	sp := strings.IndexByte(line, ' ')
	if sp < 0 {
		return event, fmt.Errorf("no space after timestamp")
	}
	event.Timestamp = line[:sp]
	rest := line[sp+1:]
	if strings.HasPrefix(rest, "[") {
		end := strings.IndexByte(rest, ']')
		if end < 0 {
			return event, fmt.Errorf("unclosed prefix")
		}
		event.Prefix = rest[:end+1]
		rest = strings.TrimLeft(rest[end+1:], " ")
	}
	sp = strings.IndexByte(rest, ' ')
	if sp < 0 {
		event.Type = EventType(rest)
		event.Data = map[string]any{}
		return event, nil
	}
	event.Type = EventType(rest[:sp])
	rawData := rest[sp+1:]
	event.Data = map[string]any{}
	if rawData != "" {
		if err := json.Unmarshal([]byte(rawData), &event.Data); err != nil {
			return event, fmt.Errorf("decode data: %w", err)
		}
	}
	return event, nil
}

func redactValue(v any) any {
	switch value := v.(type) {
	case string:
		return Redact(value)
	case []any:
		out := make([]any, len(value))
		for i := range value {
			out[i] = redactValue(value[i])
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(value))
		for key, item := range value {
			out[key] = redactValue(item)
		}
		return out
	default:
		return value
	}
}

func stringField(data map[string]any, key string) string {
	value, _ := data[key].(string)
	return value
}

func firstStringField(data map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringField(data, key); value != "" {
			return value
		}
	}
	return ""
}

func snippetStringField(data map[string]any, key string) string {
	value := strings.TrimSpace(stringField(data, key))
	if len(value) <= failureSnippetLimit {
		return value
	}
	return value[:failureSnippetLimit] + "...[truncated]"
}

func intField(data map[string]any, key string) *int {
	switch value := data[key].(type) {
	case int:
		return &value
	case int64:
		if value < math.MinInt || value > math.MaxInt {
			return nil
		}
		v := int(value)
		return &v
	case float64:
		if math.Trunc(value) != value || value < math.MinInt || value > math.MaxInt {
			return nil
		}
		v := int(value)
		return &v
	case json.Number:
		parsed, err := strconv.ParseInt(string(value), 10, 0)
		if err != nil {
			return nil
		}
		v := int(parsed)
		return &v
	default:
		return nil
	}
}
