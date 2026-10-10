package cli

import (
	"encoding/json"
	"os"
	"regexp"
	"time"
)

var claudeNotificationRE = regexp.MustCompile(`(?s)<task-notification>.*?</task-notification>`)
var claudeToolIDRE = regexp.MustCompile(`<tool-use-id>([^<]+)</tool-use-id>`)
var claudeStatusRE = regexp.MustCompile(`<status>(completed|failed|killed)</status>`)

func claudeCompletedTools(e *claudeEntry) []string {
	var ids []string
	if e.Type == "user" && !e.ToolUseResult.IsAsync && e.ToolUseResult.Status != "async_launched" {
		var blocks []claudeTool
		if json.Unmarshal(e.Message.Content, &blocks) == nil {
			for _, b := range blocks {
				if b.Type == "tool_result" && b.ToolUseID != "" {
					ids = append(ids, b.ToolUseID)
				}
			}
		}
	}
	text := ""
	if e.Type == "attachment" && e.Attachment.Type == "queued_command" {
		text = e.Attachment.Prompt
	}
	if e.Type == "queue-operation" {
		_ = json.Unmarshal(e.Content, &text)
	}
	for _, notification := range claudeNotificationRE.FindAllString(text, -1) {
		match := claudeToolIDRE.FindStringSubmatch(notification)
		if len(match) == 2 && claudeStatusRE.MatchString(notification) {
			ids = append(ids, match[1])
		}
	}
	return ids
}

// Lifecycle is evaluated at every depth before collection. Nested settlement
// projects to the top-level ancestor, whose result must consume its children.
func interactiveClaudeLifecycle(root *os.Root, subs string, parent *claudeInteractiveSpan) (map[string]bool, bool) {
	l := claudeLifecycle{root: root, index: indexClaudeSidecars(root, subs), running: map[string]bool{}, seen: map[string]bool{}, failed: parent.failed, finalPosition: parent.messages[parent.final].position}
	complete := l.visit(parent, claudeSettlement{})
	return l.running, complete
}

type claudeLifecycle struct {
	root                  *os.Root
	index                 map[string]claudeSidecarFile
	running, seen, failed map[string]bool
	finalPosition         int64
}

func (l *claudeLifecycle) visit(span *claudeInteractiveSpan, ancestor claudeSettlement) bool {
	complete := true
	for id := range span.spawns {
		if l.seen[id] {
			continue
		}
		l.seen[id] = true
		if !l.inspect(span, id, ancestor) {
			complete = false
		}
	}
	return complete
}

func (l *claudeLifecycle) inspect(span *claudeInteractiveSpan, id string, ancestor claudeSettlement) bool {
	evidence, finished := span.settled[id]
	file, found := l.index[id]
	if !found && span.failed[id] {
		return true
	}
	projection := ancestor
	if projection.position == 0 {
		projection = evidence
	}
	childComplete := false
	if found && l.root != nil {
		child := readClaudeInteractiveSpan(l.root, file.transcript, 0, false)
		childComplete = child.reason == "" && len(child.messages) > 0
		for failedID := range child.failed {
			l.failed[failedID] = true
		}
		if !claudeActivityBefore(&child, evidence.timestamp) {
			finished = false
		}
		if !l.visit(&child, projection) {
			childComplete = false
		}
	}
	if !projection.timestamp.IsZero() && evidence.timestamp.After(projection.timestamp) {
		finished = false
	}
	l.running[id] = !finished
	return finished && childComplete && projection.position > 0 && projection.position < l.finalPosition
}

func claudeActivityBefore(span *claudeInteractiveSpan, end time.Time) bool {
	if end.IsZero() {
		return false
	}
	for _, m := range span.messages {
		if m.latestTimestamp.IsZero() || m.latestTimestamp.After(end) {
			return false
		}
	}
	return true
}
