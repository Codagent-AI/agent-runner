package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Logger writes audit events to a JSONL log file.
type Logger struct {
	file   *os.File
	closed bool
}

// NewLogger creates an audit logger that appends to the given file path.
func NewLogger(filePath string) (*Logger, error) {
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return nil, fmt.Errorf("create audit log dir: %w", err)
	}
	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
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
	l.file.WriteString(line)
}

// Close flushes and closes the log file.
func (l *Logger) Close() {
	if l.closed {
		return
	}
	l.closed = true
	l.file.Close()
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

func encodePath(dirPath string) string {
	return pathUnsafeRe.ReplaceAllString(dirPath, "-")
}

func sanitizeWorkflowName(name string) string {
	sanitized := strings.ReplaceAll(name, "..", "-")
	sanitized = fileUnsafeRe.ReplaceAllString(sanitized, "-")
	sanitized = strings.TrimSpace(sanitized)
	if sanitized == "" {
		return "workflow"
	}
	return sanitized
}

// CreateLogger creates an audit logger for a workflow run.
// Log path: ~/.agent-runner/projects/{encoded-cwd}/logs/{workflow-name}-{timestamp}.log
func CreateLogger(workflowName, cwd string) (*Logger, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	encoded := encodePath(cwd)
	timestamp := time.Now().Format(time.RFC3339)
	timestamp = strings.ReplaceAll(timestamp, ":", "-")
	safeName := sanitizeWorkflowName(workflowName)
	logDir := filepath.Join(home, ".agent-runner", "projects", encoded, "logs")
	logFile := filepath.Join(logDir, fmt.Sprintf("%s-%s.log", safeName, timestamp))
	return NewLogger(logFile)
}
