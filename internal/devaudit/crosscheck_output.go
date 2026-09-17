//go:build dev_audit

package devaudit

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/codagent/agent-runner/internal/cli"
)

const maxCrosscheckDiagnostic = 2048

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

func runCrosscheckOutput(command *exec.Cmd, adapter cli.Adapter) ([]byte, error) {
	var stderr diagnosticBuffer
	command.Stderr = &stderr
	data, err := runBoundedOutput(command, maxCrosscheckOutput)
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
		return data, fmt.Errorf("%w; provider: %s; stderr: %s", err, crosscheckDiagnostic(detail), crosscheckDiagnostic(string(stderr.data)))
	}
	return data, nil
}

type claudeAuditResult struct {
	Type             string          `json:"type"`
	Subtype          string          `json:"subtype"`
	IsError          bool            `json:"is_error"`
	Result           string          `json:"result"`
	Errors           []string        `json:"errors"`
	StructuredOutput json.RawMessage `json:"structured_output"`
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
