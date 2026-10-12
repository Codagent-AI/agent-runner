package cli

import "encoding/json"

// Decode only metadata fields: content, commands, arguments and output are absent.
type activityEvent struct {
	Type            string `json:"type"`
	Subtype         string `json:"subtype"`
	ParentToolUseID string `json:"parent_tool_use_id"`
	Message         struct {
		Content []struct {
			Type string `json:"type"`
			Name string `json:"name"`
		} `json:"content"`
	} `json:"message"`
	Item struct {
		Type   string `json:"type"`
		Server string `json:"server"`
		Tool   string `json:"tool"`
	} `json:"item"`
}

func (*ClaudeAdapter) SummarizeActivity(line []byte) (string, bool) {
	var e activityEvent
	if json.Unmarshal(line, &e) != nil {
		return "", false
	}
	summary := ""
	switch e.Type {
	case "system":
		if e.Subtype == "init" {
			summary = "session started"
		}
	case "result":
		summary = "turn finished"
	case "assistant":
		for _, b := range e.Message.Content {
			switch b.Type {
			case "tool_use":
				summary = "tool_use: " + b.Name
			case "text":
				if summary == "" {
					summary = "assistant message"
				}
			case "thinking":
				if summary == "" {
					summary = "thinking"
				}
			}
		}
	case "user":
		for _, b := range e.Message.Content {
			if b.Type == "tool_result" {
				summary = "tool_result"
			}
		}
	}
	if summary == "" {
		return "", false
	}
	if e.ParentToolUseID != "" {
		summary = "subagent " + summary
	}
	return clampActivity(summary), true
}
func (*CodexAdapter) SummarizeActivity(line []byte) (string, bool) {
	var e activityEvent
	if json.Unmarshal(line, &e) != nil {
		return "", false
	}
	summary := ""
	switch e.Type {
	case "item.started", "item.completed":
		if e.Item.Type != "" {
			summary = e.Type + ": " + e.Item.Type
			if e.Item.Type == "mcp_tool_call" {
				summary += " " + e.Item.Server + "/" + e.Item.Tool
			}
		}
	case "turn.started", "turn.completed", "error":
		summary = e.Type
	}
	return clampActivity(summary), summary != ""
}
func clampActivity(s string) string {
	r := []rune(s)
	for i, c := range r {
		if c == '\n' || c == '\r' {
			r[i] = ' '
		}
	}
	if len(r) > 200 {
		r = r[:200]
	}
	return string(r)
}
