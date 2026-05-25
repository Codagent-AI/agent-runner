package audit

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type Summary struct {
	Path               string                `json:"path"`
	RunStart           *EventRef             `json:"run_start,omitempty"`
	RunEnd             *EventRef             `json:"run_end,omitempty"`
	Steps              []StepBoundary        `json:"steps"`
	SubWorkflows       []SubWorkflowBoundary `json:"sub_workflows"`
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

type SubWorkflowBoundary struct {
	Timestamp string         `json:"timestamp,omitempty"`
	Prefix    string         `json:"prefix,omitempty"`
	Type      EventType      `json:"type"`
	Workflow  string         `json:"workflow,omitempty"`
	Outcome   string         `json:"outcome,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
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
		SubWorkflows: []SubWorkflowBoundary{},
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
	case EventSubWorkflowStart, EventSubWorkflowEnd:
		item := SubWorkflowBoundary{
			Timestamp: event.Timestamp,
			Prefix:    event.Prefix,
			Type:      event.Type,
			Workflow:  stringField(event.Data, "workflow"),
			Outcome:   stringField(event.Data, "outcome"),
			Data:      event.Data,
		}
		if !fitsCap(item, capBytes, used) {
			return false
		}
		summary.SubWorkflows = append(summary.SubWorkflows, item)
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
