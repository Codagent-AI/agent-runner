package cli

import (
	"encoding/json"
	"fmt"

	"github.com/codagent/agent-runner/internal/model"
)

type contextObservation struct {
	tokens       int64
	unclassified bool
}

func buildContext(source string, obs []contextObservation, window *int64, finalMissing bool) *model.ContextMeasurement {
	c := &model.ContextMeasurement{Status: model.ContextUnavailable, Source: source, ContextWindowLimit: window, Reason: model.UnavailableNoUsageEvent}
	unknown := finalMissing
	for _, o := range obs {
		if o.unclassified {
			unknown = true
			continue
		}
		if c.PeakContext == nil || o.tokens > *c.PeakContext {
			v := o.tokens
			c.PeakContext = &v
		}
	}
	if len(obs) > 0 && !obs[len(obs)-1].unclassified && !finalMissing {
		v := obs[len(obs)-1].tokens
		c.LastRequestInput = &v
	}
	if c.PeakContext != nil {
		c.Status = model.ContextAvailable
		c.Reason = ""
	}
	if unknown {
		c.Reason = model.UnavailableContextUnclassifiedUpdate
		if c.PeakContext != nil {
			c.Status = model.ContextPartial
		}
	}
	return c
}

func claudeContextObservation(raw json.RawMessage) contextObservation {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return contextObservation{unclassified: true}
	}
	var total int64
	for _, key := range []string{"input_tokens", "cache_read_input_tokens", "cache_creation_input_tokens"} {
		var value *int64
		if json.Unmarshal(fields[key], &value) != nil || value == nil {
			return contextObservation{unclassified: true}
		}
		total += *value
	}
	return contextObservation{tokens: total}
}

func recordClaudeContext(observations map[string]contextObservation, order *[]string, id string, raw json.RawMessage) {
	observation := claudeContextObservation(raw)
	if id == "" {
		id = fmt.Sprintf("missing:%d", len(*order))
		observation.unclassified = true
	}
	if _, ok := observations[id]; !ok {
		*order = append(*order, id)
	}
	observations[id] = observation
}

func unsupportedContext(source string) *model.ContextMeasurement {
	return &model.ContextMeasurement{Status: model.ContextUnavailable, Reason: model.UnavailableContextUnsupported, Source: source}
}
