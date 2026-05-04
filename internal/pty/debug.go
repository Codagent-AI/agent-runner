package pty

import (
	"bytes"
	"fmt"
	"os"
	"time"
)

const ptyDebugLogEnv = "AGENT_RUNNER_PTY_DEBUG_LOG"

type ptyDebugLogger struct {
	file *os.File
}

func openPTYDebugLogger(label string) *ptyDebugLogger {
	path := os.Getenv(ptyDebugLogEnv)
	if path == "" {
		return nil
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) // #nosec G304,G703 -- debug log path is explicitly user-provided
	if err != nil {
		return nil
	}
	l := &ptyDebugLogger{file: f}
	if label == "" {
		l.logf("opened PTY debug log")
	} else {
		l.logf("opened PTY debug log context=%q", label)
	}
	return l
}

func (l *ptyDebugLogger) close() {
	if l == nil || l.file == nil {
		return
	}
	l.logf("closed PTY debug log")
	_ = l.file.Close()
}

func (l *ptyDebugLogger) logChunk(label string, chunk []byte) {
	if l == nil || l.file == nil {
		return
	}
	l.logf("%s len=%d hex=% x text=%q", label, len(chunk), chunk, string(chunk))
}

func (l *ptyDebugLogger) logResult(result outputResult) {
	if l == nil || l.file == nil {
		return
	}
	l.logf("processed triggered=%t forward_len=%d forward_hex=% x forward_text=%q",
		result.triggered, len(result.forward), result.forward, string(result.forward))
}

func (l *ptyDebugLogger) logMarkerNearMiss(raw []byte, result outputResult, proc *outputProcessor) {
	if l == nil || l.file == nil || result.triggered {
		return
	}
	marker := []byte(textSentinel)
	if !bytes.Contains(raw, marker) && !bytes.Contains(result.forward, marker) {
		return
	}
	l.logf("marker visible but not triggered text_buf=%q text_start_boundary=%t text_saw_visible=%t text_prev_boundary=%t esc_state=%d raw_contains_marker=%t forward_contains_marker=%t",
		string(proc.textBuf),
		proc.textStartBoundary,
		proc.textSawVisible,
		proc.textPrevBoundary,
		proc.escState,
		bytes.Contains(raw, marker),
		bytes.Contains(result.forward, marker),
	)
}

func (l *ptyDebugLogger) logf(format string, args ...any) {
	if l == nil || l.file == nil {
		return
	}
	_, _ = fmt.Fprintf(l.file, "%s "+format+"\n", append([]any{time.Now().Format(time.RFC3339Nano)}, args...)...)
}
