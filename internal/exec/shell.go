package exec

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"
	"unicode/utf8"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/flowctl"
	"github.com/codagent/agent-runner/internal/interactive"
	"github.com/codagent/agent-runner/internal/model"
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

// interactiveShellRunnerFn runs an interactive shell step with direct terminal
// inheritance. It is replaced in focused executor tests.
var interactiveShellRunnerFn = interactive.RunTerminal

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
		expanded, err := textfmt.InterpolateShellSafeTyped(cmd, ctx.Params, ctx.CapturedVariables, ctx.BuiltinVarsForStep(stepID))
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
	return nestingSegmentsToAuditInfo(ctx.NestingPath)
}

func nestingSegmentsToAuditInfo(path []model.NestingSegment) []audit.NestingInfo {
	result := make([]audit.NestingInfo, len(path))
	for i, seg := range path {
		result[i] = audit.NestingInfo{
			StepID:          seg.StepID,
			Iteration:       seg.Iteration,
			SubWorkflowName: seg.SubWorkflowName,
			RepairAttempt:   seg.RepairAttempt,
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
		captured[k] = v.AuditValue()
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
	startTime := time.Now()
	emitStepStart(ctx, prefix, startTime, map[string]any{"command": step.Command})
	emitStepEnd(ctx, prefix, startTime, "failed", map[string]any{"error": err.Error()}, step)
}

func runShellProcess(step *model.Step, ctx *model.ExecutionContext, runner ProcessRunner, command string) (ProcessResult, bool, error) {
	isInteractive := step.Mode == model.ModeInteractive
	useCapture := step.Capture != "" && !isInteractive

	if !isInteractive {
		result, err := runner.RunShell(command, useCapture, step.Workdir)
		return result, useCapture, err
	}

	executable, err := agentRunnerExecutable()
	if err != nil {
		return ProcessResult{}, false, fmt.Errorf("resolve watchdog executable: %w", err)
	}
	terminalResult, err := interactiveShellRunnerFn(context.Background(), &interactive.TerminalOptions{
		Args: []string{"sh", "-c", command}, Workdir: step.Workdir,
		Before: ctx.SuspendHook, After: ctx.ResumeHook, Foreground: true,
		WatchdogExecutable: executable, Logger: ctx.AuditLogger,
		Prefix: audit.BuildPrefix(nestingToAudit(ctx), step.ID),
		Persist: func(metadata *interactive.ProcessMetadata) {
			setInteractiveAttempt(ctx, metadata)
			if ctx.FlushState != nil {
				ctx.FlushState()
			}
		},
	})
	if err != nil {
		return ProcessResult{}, false, err
	}
	return ProcessResult{ExitCode: terminalResult.ExitCode}, false, nil
}

func captureShellOutput(step *model.Step, ctx *model.ExecutionContext, result ProcessResult) {
	if step.Capture == "" {
		return
	}
	captured := result.Stdout
	if step.CaptureStderr && result.ExitCode != 0 && result.Stderr != "" {
		captured = captured + "\n\nSTDERR:\n" + result.Stderr
	}
	value := model.NewCapturedString(captured)
	ctx.CapturedVariables[step.Capture] = value
	recordPullRequestCapture(ctx, step.ID, step.Capture, value)
}

// shellCheckRun is the outcome of the audit-free shell primitive: the
// process result plus everything an audited wrapper needs to emit its own
// step_start/step_end without re-running the command.
type shellCheckRun struct {
	Result     ProcessResult
	Command    string // interpolated command, before metrics-environment wrapping
	UseCapture bool
	Metrics    *nestedMetricsCapture
	RunErr     error
}

// runShellCheck interpolates step's command and executes it exactly as
// ExecuteShellStep does, but emits no audit events, adds no warning origin,
// and writes no capture. Callers own audit emission and ctx.CapturedVariables.
func runShellCheck(step *model.Step, ctx *model.ExecutionContext, runner ProcessRunner) shellCheckRun {
	command, err := textfmt.InterpolateShellSafeTyped(step.Command, ctx.Params, ctx.CapturedVariables, ctx.BuiltinVarsForStep(step.ID))
	if err != nil {
		return shellCheckRun{RunErr: err}
	}
	metricsCapture, err := prepareNestedMetrics(step, ctx, command)
	if err != nil {
		return shellCheckRun{Command: command, RunErr: err}
	}
	result, useCapture, runErr := runShellProcess(step, ctx, runner, metricsCapture.command)
	return shellCheckRun{Result: result, Command: command, UseCapture: useCapture, Metrics: metricsCapture, RunErr: runErr}
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

	run := runShellCheck(step, ctx, runner)
	if run.RunErr != nil && run.Metrics == nil {
		// Covers both an interpolation failure (Command unresolved) and a
		// nested-metrics preparation failure (Command resolved but the
		// process never ran): neither case ever reaches runShellProcess, so
		// there is nothing more specific to report than the raw step command.
		emitShellInterpolationFailure(ctx, step, run.RunErr)
		return OutcomeFailed, run.RunErr
	}

	log.Printf("  command: %s\n", run.Command)

	prefix := audit.BuildPrefix(nestingToAudit(ctx), step.ID)
	startTime := time.Now()

	// Set the step prefix on the process runner if it supports it (TUI mode).
	if ps, ok := runner.(interface{ SetPrefix(string) }); ok {
		ps.SetPrefix(prefix)
	}

	emitStepStart(ctx, prefix, startTime, map[string]any{"command": truncateForAudit(run.Command)})

	if step.MetricsSource != "" {
		// Nested metric records are emitted even when the tool exits unsuccessfully:
		// model usage is billable independently of the shell outcome.
		emitNestedMetricCapture(ctx, step, prefix, run.Metrics)
	}
	if run.RunErr != nil {
		emitStepEnd(ctx, prefix, startTime, "failed", map[string]any{"error": run.RunErr.Error()}, step)
		return OutcomeFailed, run.RunErr
	}

	if run.UseCapture {
		captureShellOutput(step, ctx, run.Result)
	}

	outcome := OutcomeSuccess
	if run.Result.ExitCode != 0 {
		outcome = OutcomeFailed
	}

	endData := map[string]any{
		"exit_code": run.Result.ExitCode,
		"stderr":    truncateForAudit(run.Result.Stderr),
	}
	if step.Mode != model.ModeInteractive {
		endData["stdout"] = truncateForAudit(run.Result.Stdout)
	}
	addGuardedLinkage(endData, outcome, ctx)
	checkIdentity := executionIdentity(ctx, step, "step", 0, false, "", "")
	failureErr := recordCheckFailure(ctx, step, outcome, prefix, attemptForIdentity(ctx, &checkIdentity), run.Result.ExitCode, run.Result.Stdout, run.Result.Stderr, log)

	emitStepEnd(ctx, prefix, startTime, string(outcome), endData, step)

	return outcome, failureErr
}
