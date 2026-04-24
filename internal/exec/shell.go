package exec

import (
	"errors"
	"fmt"
	"os/exec"
	"time"
	"unicode/utf8"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/flowctl"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/pty"
	"github.com/codagent/agent-runner/internal/textfmt"
)

// runSkipShell runs a skip_if shell expression and returns its exit code.
// Overridden in tests to avoid spawning subprocesses.
var runSkipShell = func(cmd string) (int, error) {
	c := exec.Command("sh", "-c", cmd) // #nosec G204 -- skip_if command comes from workflow YAML
	err := c.Run()
	if err == nil {
		return 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), nil
	}
	return -1, err
}

// interactiveShellRunnerFn runs an interactive shell step inside a PTY.
// Defaults to pty.RunShellInteractive; replaced in tests.
var interactiveShellRunnerFn = pty.RunShellInteractive

// ShouldSkipStep evaluates a step's skip_if condition. For "previous_success",
// it returns true when the previous step in scope succeeded. For "sh:<cmd>",
// it interpolates the command, runs it through the shell, and returns true
// when the exit code is 0. An empty skip_if returns (false, nil).
//
// The shell form runs directly via os/exec — bypassing ProcessRunner — so
// evaluation output does not leak into the TUI live-run view or clobber the
// surrounding step's output files.
func ShouldSkipStep(skipIf string, lastOutcome *string, ctx *model.ExecutionContext, stepID string) (bool, error) {
	if skipIf == "" {
		return false, nil
	}
	if cmd, ok := flowctl.ShellSkipCommand(skipIf); ok {
		expanded, err := textfmt.InterpolateShellSafe(cmd, ctx.Params, ctx.CapturedVariables, ctx.BuiltinVarsForStep(stepID))
		if err != nil {
			return false, fmt.Errorf("skip_if interpolation: %w", err)
		}
		exitCode, runErr := runSkipShell(expanded)
		if runErr != nil {
			return false, fmt.Errorf("skip_if shell: %w", runErr)
		}
		return exitCode == 0, nil
	}
	return flowctl.ShouldSkip(skipIf, lastOutcome), nil
}

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

func emitShellInterpolationFailure(ctx *model.ExecutionContext, step *model.Step, err error) {
	prefix := audit.BuildPrefix(nestingToAudit(ctx), step.ID)
	now := time.Now().UTC().Format(time.RFC3339)
	emitAudit(ctx, audit.Event{
		Timestamp: now,
		Prefix:    prefix,
		Type:      audit.EventStepStart,
		Data:      map[string]any{"command": step.Command, "context": contextSnapshot(ctx)},
	})
	emitAudit(ctx, audit.Event{
		Timestamp: now,
		Prefix:    prefix,
		Type:      audit.EventStepEnd,
		Data:      map[string]any{"outcome": "failed", "error": err.Error(), "duration_ms": 0},
	})
}

func runShellProcess(step *model.Step, ctx *model.ExecutionContext, runner ProcessRunner, command string) (ProcessResult, bool, error) {
	interactive := step.Mode == model.ModeInteractive
	useCapture := step.Capture != "" && !interactive

	if !interactive {
		result, err := runner.RunShell(command, useCapture, step.Workdir)
		return result, useCapture, err
	}

	if ctx.SuspendHook != nil {
		ctx.SuspendHook()
	}
	ptyResult, err := interactiveShellRunnerFn(command, pty.Options{Workdir: step.Workdir})
	if ctx.ResumeHook != nil {
		ctx.ResumeHook()
	}
	if err != nil {
		return ProcessResult{}, false, err
	}
	return ProcessResult{ExitCode: ptyResult.ExitCode, Stdout: ptyResult.Stdout}, false, nil
}

func captureShellOutput(step *model.Step, ctx *model.ExecutionContext, result ProcessResult) {
	if step.Capture == "" {
		return
	}
	captured := result.Stdout
	if step.CaptureStderr && result.ExitCode != 0 && result.Stderr != "" {
		captured = captured + "\n\nSTDERR:\n" + result.Stderr
	}
	ctx.CapturedVariables[step.Capture] = captured
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

	command, err := textfmt.Interpolate(step.Command, ctx.Params, ctx.CapturedVariables, ctx.BuiltinVarsForStep(step.ID))
	if err != nil {
		emitShellInterpolationFailure(ctx, step, err)
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

	result, useCapture, runErr := runShellProcess(step, ctx, runner, command)
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
		captureShellOutput(step, ctx, result)
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
