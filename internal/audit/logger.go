// Package audit provides structured JSONL audit logging for workflow runs.
package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Logger writes audit events to a JSONL log file.
type Logger struct {
	file   *os.File
	closed bool
}

// NewLogger creates an audit logger that appends to the given file path.
func NewLogger(filePath string) (*Logger, error) {
	if err := os.MkdirAll(filepath.Dir(filePath), 0o750); err != nil {
		return nil, fmt.Errorf("create audit log dir: %w", err)
	}
	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) // #nosec G304 -- audit log path is constructed internally
	if err != nil {
		return nil, fmt.Errorf("open audit log: %w", err)
	}
	return &Logger{file: f}, nil
}

// Emit writes an event to the log file.
func (l *Logger) Emit(event Event) {
	if l.closed {
		return
	}
	prefixPart := ""
	if event.Prefix != "" {
		prefixPart = " " + event.Prefix
	}
	dataJSON, _ := json.Marshal(event.Data)
	line := fmt.Sprintf("%s%s %s %s\n", event.Timestamp, prefixPart, event.Type, string(dataJSON))
	_, _ = l.file.WriteString(line)
}

// Close flushes and closes the log file.
func (l *Logger) Close() {
	if l.closed {
		return
	}
	l.closed = true
	_ = l.file.Close()
}

// NestingInfo holds the minimum info for building an audit prefix.
type NestingInfo struct {
	StepID          string
	Iteration       *int
	SubWorkflowName string
}

// BuildPrefix constructs the nesting prefix string.
// Examples:
//   - [], "validate" → "[validate]"
//   - [{stepId: "loop", iteration: 0}], "impl" → "[loop:0, impl]"
func BuildPrefix(nestingPath []NestingInfo, stepID string) string {
	tokens := make([]string, 0, len(nestingPath)*2+1)

	for _, seg := range nestingPath {
		if seg.Iteration != nil {
			tokens = append(tokens, fmt.Sprintf("%s:%d", seg.StepID, *seg.Iteration))
		} else {
			tokens = append(tokens, seg.StepID)
		}
		if seg.SubWorkflowName != "" {
			tokens = append(tokens, "sub:"+seg.SubWorkflowName)
		}
	}

	tokens = append(tokens, stepID)
	return "[" + strings.Join(tokens, ", ") + "]"
}

var pathUnsafeRe = regexp.MustCompile(`[/._]`)
var fileUnsafeRe = regexp.MustCompile(`[\\/:*?"<>|]`)

// EncodePath replaces path-unsafe characters (/, ., _) with dashes for use
// in directory names under ~/.agent-runner/projects/.
func EncodePath(dirPath string) string {
	return pathUnsafeRe.ReplaceAllString(dirPath, "-")
}

// SanitizeWorkflowName replaces path traversal sequences and file-unsafe
// characters with dashes, returning "workflow" for empty input.
func SanitizeWorkflowName(name string) string {
	sanitized := strings.ReplaceAll(name, "..", "-")
	sanitized = fileUnsafeRe.ReplaceAllString(sanitized, "-")
	sanitized = strings.TrimSpace(sanitized)
	if sanitized == "" {
		return "workflow"
	}
	return sanitized
}

