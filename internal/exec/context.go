package exec

import "github.com/codagent/agent-runner/internal/model"

// mergeContext reduces turn gauges independently of cumulative usage counters.
func mergeContext(turns []model.UsageRecord) *model.ContextMeasurement {
	c := &model.ContextMeasurement{Status: model.ContextUnavailable, Reason: model.UnavailableNoUsageEvent, Source: "agent-runner"}
	all := len(turns) > 0
	for i := range turns {
		t := turns[i].Context
		if t == nil {
			t = &model.ContextMeasurement{Status: model.ContextUnavailable, Reason: model.UnavailableContextNotRecorded}
		}
		if t.Source != "" {
			c.Source = t.Source
		}
		if t.PeakContext != nil && (c.PeakContext == nil || *t.PeakContext > *c.PeakContext) {
			v := *t.PeakContext
			c.PeakContext = &v
		}
		if t.ContextWindowLimit != nil {
			v := *t.ContextWindowLimit
			c.ContextWindowLimit = &v
		}
		if t.Status != model.ContextAvailable {
			if all || i == len(turns)-1 {
				c.Reason = t.Reason
			}
			all = false
		}
		if i == len(turns)-1 {
			c.LastRequestInput = nil
			if t.LastRequestInput != nil {
				v := *t.LastRequestInput
				c.LastRequestInput = &v
			}
		}
	}
	if all {
		c.Status = model.ContextAvailable
		c.Reason = ""
	} else if c.PeakContext != nil || c.LastRequestInput != nil {
		c.Status = model.ContextPartial
	}
	return c
}
