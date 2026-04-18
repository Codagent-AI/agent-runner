package exec

import (
	"time"
	"unicode/utf8"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/textfmt"
)

const maxAuditValueLen = 4096

// truncateForAudit truncates a string to maxAuditValueLen to prevent large
// blobs from inflating audit logs.
func truncateForAudit(s string) string {
	if len(s) <= maxAuditValueLen {
		return s
	}
	// Walk back to a valid UTF-8 rune boundary to avoid splitting
	// multi-byte sequences.
	cut := maxAuditValueLen
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "...[truncated]"
}

func nestingToAudit(ctx *model.ExecutionContext) []audit.NestingInfo {
	result := make([]audit.NestingInfo, len(ctx.NestingPath))
	for i, seg := range ctx.NestingPath {
		result[i] = audit.NestingInfo{
			StepID:          seg.StepID,
			Iteration:       seg.Iteration,
			SubWorkflowName: seg.SubWorkflowName,
		}
	}
	return result
}

func contextSnapshot(ctx *model.ExecutionContext) map[string]any {
	params := make(map[string]any)
	for k, v := range ctx.Params {
		params[k] = v
	}
	captured := make(map[string]any)
	for k, v := range ctx.CapturedVariables {
		captured[k] = v
	}
	return map[string]any{
		"params":            params,
		"capturedVariables": captured,
	}
}

func emitAudit(ctx *model.ExecutionContext, event audit.Event) {
	if ctx.AuditLogger != nil {
		ctx.AuditLogger.Emit(event)
	}
}

// ExecuteShellStep runs a shell command step.
func ExecuteShellStep(
	step *model.Step,
	ctx *model.ExecutionContext,
	runner ProcessRunner,
	log Logger,
) (StepOutcome, error) {
	if step.Command == "" {
		return OutcomeFailed, nil
	}

	command, err := textfmt.Interpolate(step.Command, ctx.Params, ctx.CapturedVariables)
	if err != nil {
		prefix := audit.BuildPrefix(nestingToAudit(ctx), step.ID)
		emitAudit(ctx, audit.Event{
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Prefix:    prefix,
			Type:      audit.EventStepStart,
			Data:      map[string]any{"command": step.Command, "context": contextSnapshot(ctx)},
		})
		emitAudit(ctx, audit.Event{
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Prefix:    prefix,
			Type:      audit.EventStepEnd,
			Data:      map[string]any{"outcome": "failed", "error": err.Error(), "duration_ms": 0},
		})
		return OutcomeFailed, err
	}

	log.Printf("  command: %s\n", command)

	prefix := audit.BuildPrefix(nestingToAudit(ctx), step.ID)
	startTime := time.Now()

	// Set the step prefix on the process runner if it supports it (TUI mode).
	if ps, ok := runner.(interface{ SetPrefix(string) }); ok {
		ps.SetPrefix(prefix)
	}

	emitAudit(ctx, audit.Event{
		Timestamp: startTime.UTC().Format(time.RFC3339),
		Prefix:    prefix,
		Type:      audit.EventStepStart,
		Data:      map[string]any{"command": truncateForAudit(command), "context": contextSnapshot(ctx)},
	})

	useCapture := step.Capture != ""
	result, runErr := runner.RunShell(command, useCapture, step.Workdir)
	if runErr != nil {
		emitAudit(ctx, audit.Event{
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Prefix:    prefix,
			Type:      audit.EventStepEnd,
			Data: map[string]any{
				"outcome":     "failed",
				"error":       runErr.Error(),
				"duration_ms": time.Since(startTime).Milliseconds(),
			},
		})
		return OutcomeFailed, runErr
	}

	if useCapture {
		captured := result.Stdout
		if step.CaptureStderr && result.ExitCode != 0 && result.Stderr != "" {
			captured = captured + "\n\nSTDERR:\n" + result.Stderr
		}
		ctx.CapturedVariables[step.Capture] = captured
	}

	outcome := OutcomeSuccess
	if result.ExitCode != 0 {
		outcome = OutcomeFailed
	}

	endData := map[string]any{
		"exit_code":   result.ExitCode,
		"stderr":      truncateForAudit(result.Stderr),
		"stdout":      truncateForAudit(result.Stdout),
		"outcome":     string(outcome),
		"duration_ms": time.Since(startTime).Milliseconds(),
	}

	emitAudit(ctx, audit.Event{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Prefix:    prefix,
		Type:      audit.EventStepEnd,
		Data:      endData,
	})

	return outcome, nil
}
