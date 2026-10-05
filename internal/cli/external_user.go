package cli

import (
	"encoding/json"
	"strings"
)

// ExternalUserTurnText relays all lead assistant text, excluding native
// subagents. FilterOutput keeps its existing capture semantics.
func ExternalUserTurnText(adapter Adapter, stdout string) string {
	if _, ok := adapter.(*ClaudeAdapter); !ok {
		if filter, ok := adapter.(OutputFilter); ok {
			return filter.FilterOutput(stdout)
		}
		return stdout
	}
	var texts []string
	for _, line := range strings.Split(stdout, "\n") {
		var event struct {
			Type    string          `json:"type"`
			Parent  json.RawMessage `json:"parent_tool_use_id"`
			Message struct {
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal([]byte(line), &event) != nil || event.Type != "assistant" {
			continue
		}
		if len(event.Parent) > 0 && string(event.Parent) != "null" {
			continue
		}
		for _, block := range event.Message.Content {
			if block.Type == "text" {
				texts = append(texts, block.Text)
			}
		}
	}
	return strings.Join(texts, "\n\n")
}
