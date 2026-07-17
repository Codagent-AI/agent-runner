package cli

import (
	"encoding/json"
	"fmt"

	"github.com/codagent/agent-runner/internal/model"
)

func unavailableUsage(cliName, source string, reason model.UnavailableReason) model.UsageRecord {
	return model.UsageRecord{
		Status: model.UsageUnavailable, Reason: reason, CLI: cliName, Source: source,
	}
}

func completeness(complete bool) model.Completeness {
	if complete {
		return model.CompletenessComplete
	}
	return model.CompletenessPartial
}

// tokenCountsFromObject maps known numeric fields and preserves all other
// numeric vendor categories under the stable other: namespace. complete is
// true only when every known field was present.
func tokenCountsFromObject(raw json.RawMessage, known map[string]string) (model.TokenCounts, bool, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return model.TokenCounts{}, false, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, false, err
	}
	tokens := make(model.TokenCounts)
	present := make(map[string]bool, len(known))
	for vendorKey, value := range fields {
		var count int64
		if err := json.Unmarshal(value, &count); err != nil {
			if _, expected := known[vendorKey]; expected {
				return nil, false, fmt.Errorf("token count %q is not an integer: %w", vendorKey, err)
			}
			// Structured metadata such as service tier and nested cache details
			// are not token categories.
			continue
		}
		canonical, ok := known[vendorKey]
		if !ok {
			canonical = "other:" + vendorKey
		} else {
			present[vendorKey] = true
		}
		tokens[canonical] += count
	}
	if len(tokens) == 0 && len(fields) > 0 {
		return nil, false, fmt.Errorf("usage object has no numeric token counts")
	}
	complete := true
	for vendorKey := range known {
		if !present[vendorKey] {
			complete = false
		}
	}
	return tokens, complete, nil
}
