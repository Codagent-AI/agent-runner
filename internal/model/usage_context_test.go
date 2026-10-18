package model

import (
	"encoding/json"
	"testing"
)

func TestContextUnavailableValuesAreNull(t *testing.T) {
	raw, err := json.Marshal(UsageRecord{Context: &ContextMeasurement{Status: ContextUnavailable, Reason: UnavailableContextUnsupported, Source: "test"}})
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err = json.Unmarshal(raw, &record); err != nil {
		t.Fatal(err)
	}
	context := record["context"].(map[string]any)
	for _, key := range []string{"last_request_input_tokens", "peak_context_tokens", "context_window_limit_tokens"} {
		value, exists := context[key]
		if !exists || value != nil {
			t.Fatalf("%s must be explicit null: %s", key, raw)
		}
	}
}
