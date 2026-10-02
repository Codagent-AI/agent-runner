//go:build dev_audit

package devaudit

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/codagent/agent-runner/internal/cli"
)

const maxCrosscheckDiagnostic = 2048

func crosscheckFailureCategory(err error) string {
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return "process_exit"
	}
	return "output_unavailable"
}

// Keep diagnostics bounded without blocking a provider that writes more stderr.
type diagnosticBuffer struct{ data []byte }

func (b *diagnosticBuffer) Write(p []byte) (int, error) {
	n := len(p)
	remaining := maxCrosscheckDiagnostic - len(b.data)
	if remaining > 0 {
		b.data = append(b.data, p[:min(remaining, n)]...)
	}
	return n, nil
}

func crosscheckDiagnostic(text string) string {
	text = strings.TrimSpace(redactText(text))
	if len(text) > maxCrosscheckDiagnostic {
		text = text[:maxCrosscheckDiagnostic] + " [truncated]"
	}
	return text
}

func runCrosscheckOutput(command *exec.Cmd, adapter cli.Adapter) (raw []byte, at time.Time, started bool, runErr error) {
	var stderr diagnosticBuffer
	command.Stderr = &stderr
	spawnTime := time.Now()
	data, started, err := runBoundedOutput(command, maxCrosscheckOutput)
	if err != nil {
		detail := string(data)
		switch adapter := adapter.(type) {
		case *cli.ClaudeAdapter:
			var result claudeAuditResult
			if json.Unmarshal(data, &result) == nil {
				detail = result.diagnostic()
			}
		case cli.OutputFilter:
			detail = adapter.FilterOutput(detail)
		}
		return data, spawnTime, started, fmt.Errorf("%w; provider: %s; stderr: %s", err, crosscheckDiagnostic(detail), crosscheckDiagnostic(string(stderr.data)))
	}
	return data, spawnTime, started, nil
}

type claudeAuditResult struct {
	Type             string          `json:"type"`
	Subtype          string          `json:"subtype"`
	IsError          bool            `json:"is_error"`
	Result           string          `json:"result"`
	Errors           []string        `json:"errors"`
	StructuredOutput json.RawMessage `json:"structured_output"`
	SessionID        string          `json:"session_id"`
}

func auditSessionID(adapter cli.Adapter, raw []byte, workspace string, spawnTime time.Time) string {
	if _, ok := adapter.(*cli.ClaudeAdapter); ok {
		var result claudeAuditResult
		if json.Unmarshal(raw, &result) == nil && result.SessionID != "" {
			return result.SessionID
		}
	}
	id := adapter.DiscoverSessionID(&cli.DiscoverOptions{SpawnTime: spawnTime, Headless: true, ProcessOutput: string(raw), Workdir: workspace})
	if id == "" {
		return "unknown"
	}
	return id
}

func (r *claudeAuditResult) diagnostic() string {
	return strings.Join(append([]string{r.Subtype, r.Result}, r.Errors...), " ")
}

func crosscheckResponse(adapter cli.Adapter, data []byte, finalFile bool) (string, error) {
	if finalFile {
		return string(data), nil
	}
	if _, ok := adapter.(*cli.ClaudeAdapter); ok {
		var result claudeAuditResult
		if err := json.Unmarshal(data, &result); err != nil {
			return "", fmt.Errorf("decode Claude result: %w; response: %s", err, crosscheckDiagnostic(string(data)))
		}
		if result.Type != "result" || result.IsError || result.Subtype != "success" {
			return "", fmt.Errorf("claude crosscheck failed: %s", crosscheckDiagnostic(result.diagnostic()))
		}
		if len(result.StructuredOutput) == 0 || string(result.StructuredOutput) == "null" {
			return "", fmt.Errorf("claude crosscheck missing structured_output: %s", crosscheckDiagnostic(result.diagnostic()))
		}
		return string(result.StructuredOutput), nil
	}
	if filter, ok := adapter.(cli.OutputFilter); ok {
		return filter.FilterOutput(string(data)), nil
	}
	return string(data), nil
}
