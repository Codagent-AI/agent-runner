package exec

import (
	"time"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/textfmt"
)

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

	emitAudit(ctx, audit.Event{
		Timestamp: startTime.UTC().Format(time.RFC3339),
		Prefix:    prefix,
		Type:      audit.EventStepStart,
		Data:      map[string]any{"command": command, "context": contextSnapshot(ctx)},
	})

	useCapture := step.Capture != ""
	result, runErr := runner.RunShell(command, useCapture)
	if runErr != nil {
		return OutcomeFailed, runErr
	}

	if useCapture {
		ctx.CapturedVariables[step.Capture] = result.Stdout
	}

	outcome := OutcomeSuccess
	if result.ExitCode != 0 {
		outcome = OutcomeFailed
	}

	endData := map[string]any{
		"exit_code":   result.ExitCode,
		"stderr":      result.Stderr,
		"outcome":     string(outcome),
		"duration_ms": time.Since(startTime).Milliseconds(),
	}
	if useCapture {
		endData["stdout"] = result.Stdout
	}

	emitAudit(ctx, audit.Event{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Prefix:    prefix,
		Type:      audit.EventStepEnd,
		Data:      endData,
	})

	return outcome, nil
}
