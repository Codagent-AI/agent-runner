package metrics

import "github.com/codagent/agent-runner/internal/model"

type AgentSessionContextSummary struct {
	Sessions             []AgentSessionContext `json:"sessions"`
	UnattributedAttempts int                   `json:"unattributed_attempts"`
}

type AgentSessionContext struct {
	CLI                 string         `json:"cli"`
	SessionID           string         `json:"session_id"`
	HighWaterMark       *int64         `json:"high_water_mark_tokens"`
	MaxLastRequestInput *int64         `json:"max_last_request_input_tokens"`
	ContextWindowLimit  *int64         `json:"context_window_limit_tokens"`
	AttemptCount        int            `json:"attempt_count"`
	Coverage            model.Coverage `json:"coverage"`
}

func summarizeAgentContext(records []StepRecord) AgentSessionContextSummary {
	summary := AgentSessionContextSummary{Sessions: []AgentSessionContext{}}
	indexes := map[[2]string]int{}
	incomplete := map[[2]string]bool{}
	for i := range records {
		r := &records[i]
		if !r.AgentInvoked || r.Type != "agent" || (r.Kind != "step" && r.Kind != "agent-call") {
			continue
		}
		if r.SessionID == "" {
			summary.UnattributedAttempts++
			continue
		}
		cli := r.Tool
		context := &model.ContextMeasurement{Status: model.ContextUnavailable, Reason: model.UnavailableContextNotRecorded}
		if r.Usage != nil {
			if r.Usage.CLI != "" {
				cli = r.Usage.CLI
			}
			if r.Usage.Context != nil {
				context = r.Usage.Context
			}
		}
		key := [2]string{cli, r.SessionID}
		index, ok := indexes[key]
		if !ok {
			index = len(summary.Sessions)
			indexes[key] = index
			summary.Sessions = append(summary.Sessions, AgentSessionContext{CLI: cli, SessionID: r.SessionID})
		}
		s := &summary.Sessions[index]
		s.AttemptCount++
		maxContextValue(&s.HighWaterMark, context.PeakContext)
		maxContextValue(&s.MaxLastRequestInput, context.LastRequestInput)
		maxContextValue(&s.ContextWindowLimit, context.ContextWindowLimit)
		incomplete[key] = incomplete[key] || context.Status != model.ContextAvailable
		switch {
		case s.HighWaterMark == nil && s.MaxLastRequestInput == nil:
			s.Coverage = model.CoverageNone
		case incomplete[key]:
			s.Coverage = model.CoveragePartial
		default:
			s.Coverage = model.CoverageComplete
		}
	}
	return summary
}
func maxContextValue(target **int64, value *int64) {
	if value != nil && (*target == nil || *value > **target) {
		v := *value
		*target = &v
	}
}
func cloneContext(c *model.ContextMeasurement) *model.ContextMeasurement {
	if c == nil {
		return nil
	}
	cloned := *c
	cloned.PeakContext = nil
	maxContextValue(&cloned.PeakContext, c.PeakContext)
	cloned.LastRequestInput = nil
	maxContextValue(&cloned.LastRequestInput, c.LastRequestInput)
	cloned.ContextWindowLimit = nil
	maxContextValue(&cloned.ContextWindowLimit, c.ContextWindowLimit)
	return &cloned
}
