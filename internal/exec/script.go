package exec

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/textfmt"
	builtinworkflows "github.com/codagent/agent-runner/workflows"
)

const scriptStepRevealDelay = 2 * time.Second

type scriptEnvironmentRunner interface {
	RunScriptWithEnv(path string, stdin []byte, captureStdout bool, workdir string, environment []string) (ProcessResult, error)
}

// scriptCheckRun is the outcome of the audit-free script primitive.
type scriptCheckRun struct {
	Result  ProcessResult
	Metrics *nestedMetricsCapture
	RunErr  error
}

// scriptCheckPrep is the result of resolving a script check's path, stdin,
// and nested-metrics environment, before the process is spawned. Splitting
// this from execution lets an audited caller set the runner's prefix and
// emit step_start before the process starts.
type scriptCheckPrep struct {
	scriptPath  string
	stdin       []byte
	metrics     *nestedMetricsCapture
	environment []string
	Err         error
}

// prepareScriptCheck resolves the script path, builds its stdin, and
// prepares its nested metrics environment. It runs nothing and emits no
// audit events.
func prepareScriptCheck(step *model.Step, ctx *model.ExecutionContext) scriptCheckPrep {
	scriptPath, err := resolveScriptPath(step.Script, ctx)
	if err != nil {
		return scriptCheckPrep{Err: err}
	}
	stdin, err := buildScriptInput(step, ctx)
	if err != nil {
		return scriptCheckPrep{Err: err}
	}
	metricsCapture, environment, err := prepareNestedMetricsEnvironment(step, ctx)
	if err != nil {
		return scriptCheckPrep{Err: err}
	}
	return scriptCheckPrep{scriptPath: scriptPath, stdin: stdin, metrics: metricsCapture, environment: environment}
}

// runPreparedScriptCheck executes a script check from an already-prepared
// path/stdin/environment, emitting no audit events, adding no warning
// origin, and writing no capture. Callers own audit emission and
// ctx.CapturedVariables.
func runPreparedScriptCheck(step *model.Step, ctx *model.ExecutionContext, runner ProcessRunner, prep *scriptCheckPrep) scriptCheckRun {
	var result ProcessResult
	var err error
	if len(prep.environment) == 0 {
		result, err = runner.RunScript(prep.scriptPath, prep.stdin, step.Capture != "", step.Workdir)
	} else if environmentRunner, ok := runner.(scriptEnvironmentRunner); ok {
		result, err = environmentRunner.RunScriptWithEnv(prep.scriptPath, prep.stdin, step.Capture != "", step.Workdir, prep.environment)
	} else {
		err = fmt.Errorf("process runner does not support script environments")
	}
	return scriptCheckRun{Result: result, Metrics: prep.metrics, RunErr: err}
}

// runScriptCheck resolves the script path, builds its stdin, and executes it
// exactly as ExecuteScriptStep does, but emits no audit events, adds no
// warning origin, and writes no capture. Callers own audit emission and
// ctx.CapturedVariables.
func runScriptCheck(step *model.Step, ctx *model.ExecutionContext, runner ProcessRunner) scriptCheckRun {
	prep := prepareScriptCheck(step, ctx)
	if prep.Err != nil {
		return scriptCheckRun{RunErr: prep.Err}
	}
	return runPreparedScriptCheck(step, ctx, runner, &prep)
}

func ExecuteScriptStep(step *model.Step, ctx *model.ExecutionContext, runner ProcessRunner, log Logger) (StepOutcome, error) {
	prefix := audit.BuildPrefix(nestingToAudit(ctx), step.ID)
	startTime := time.Now()
	emitStepStart(ctx, prefix, startTime, map[string]any{"script": step.Script})

	log.Printf("  script: %s\n", step.Script)
	switch ps := runner.(type) {
	case interface {
		SetScriptPrefix(string, time.Duration)
	}:
		ps.SetScriptPrefix(prefix, scriptStepRevealDelay)
	case interface{ SetPrefix(string) }:
		ps.SetPrefix(prefix)
	}

	run := runScriptCheck(step, ctx, runner)
	if run.RunErr != nil {
		if rcErr := emitScriptEnd(ctx, prefix, startTime, step, "failed", nil, run.RunErr, log); rcErr != nil {
			return OutcomeFailed, rcErr
		}
		return OutcomeFailed, run.RunErr
	}
	result := run.Result
	if step.MetricsSource != "" {
		emitNestedMetricCapture(ctx, step, prefix, run.Metrics)
	}
	if step.Capture != "" {
		capturedOutput := result.Stdout
		if step.CaptureStderr && result.ExitCode != 0 && result.Stderr != "" {
			capturedOutput += "\n\nSTDERR:\n" + result.Stderr
		}
		captured, err := captureScriptOutput(step.CaptureFormat, capturedOutput)
		if err != nil {
			if rcErr := emitScriptEnd(ctx, prefix, startTime, step, "failed", &result, err, log); rcErr != nil {
				return OutcomeFailed, rcErr
			}
			return OutcomeFailed, err
		}
		ctx.CapturedVariables[step.Capture] = captured
		recordPullRequestCapture(ctx, step.ID, step.Capture, captured)
	}
	if result.ExitCode != 0 {
		if rcErr := emitScriptEnd(ctx, prefix, startTime, step, "failed", &result, nil, log); rcErr != nil {
			return OutcomeFailed, rcErr
		}
		return OutcomeFailed, nil
	}
	if rcErr := emitScriptEnd(ctx, prefix, startTime, step, "success", &result, nil, log); rcErr != nil {
		return OutcomeSuccess, rcErr
	}
	return OutcomeSuccess, nil
}

// emitScriptEnd emits the step_end audit event and updates ctx.LastFailure.
// Returns an error when the check failed and its guarded execution reference
// could not be rebuilt from audit; the audit event is still emitted in that
// case so the check's own evidence is never lost.
func emitScriptEnd(ctx *model.ExecutionContext, prefix string, startTime time.Time, step *model.Step, outcome string, result *ProcessResult, err error, log Logger) error {
	data := map[string]any{}
	if result != nil {
		data["exit_code"] = result.ExitCode
		data["stdout"] = truncateForAudit(result.Stdout)
		data["stderr"] = truncateForAudit(result.Stderr)
	}
	if err != nil {
		data["error"] = err.Error()
	}
	addGuardedLinkage(data, StepOutcome(outcome), ctx)
	exitCode, stdout, stderr := 0, "", ""
	if result != nil {
		exitCode, stdout, stderr = result.ExitCode, result.Stdout, result.Stderr
	}
	checkIdentity := executionIdentity(ctx, step, "step", 0, false, "", "")
	failureErr := recordCheckFailure(ctx, step, StepOutcome(outcome), prefix, attemptForIdentity(ctx, &checkIdentity), exitCode, stdout, stderr, log)
	emitStepEnd(ctx, prefix, startTime, outcome, data, step)
	return failureErr
}

func resolveScriptPath(script string, ctx *model.ExecutionContext) (string, error) {
	if script == "" {
		return "", fmt.Errorf("script path is required")
	}
	clean := path.Clean(script)
	if clean == "." || strings.HasPrefix(clean, "../") || path.IsAbs(clean) {
		return "", fmt.Errorf("invalid script path %q", script)
	}
	if builtinworkflows.IsRef(ctx.WorkflowFile) {
		rel, err := builtinworkflows.RefPath(ctx.WorkflowFile)
		if err != nil {
			return "", err
		}
		namespace, _, ok := strings.Cut(rel, "/")
		if !ok {
			return "", fmt.Errorf("builtin workflow has no namespace: %s", rel)
		}
		return materializeAsset(ctx.SessionDir, namespace, clean)
	}
	baseDir := filepath.Dir(ctx.WorkflowFile)
	scriptPath := filepath.Join(baseDir, filepath.FromSlash(clean))
	resolved, err := filepath.EvalSymlinks(scriptPath)
	if err != nil {
		return "", fmt.Errorf("resolve script path: %w", err)
	}
	baseResolved, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return "", fmt.Errorf("resolve workflow directory: %w", err)
	}
	rel, err := filepath.Rel(baseResolved, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("script must resolve inside workflow directory")
	}
	return resolved, nil
}

func materializeAsset(sessionDir, namespace, relAsset string) (string, error) {
	data, err := builtinworkflows.ReadAsset(path.Join(namespace, relAsset))
	if err != nil {
		return "", err
	}
	target := filepath.Join(sessionDir, "bundled", namespace, filepath.FromSlash(relAsset))
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return "", fmt.Errorf("create bundled asset directory: %w", err)
	}
	mode := os.FileMode(0o600)
	if strings.HasSuffix(relAsset, ".sh") {
		mode = 0o700
	}
	if err := os.WriteFile(target, data, mode); err != nil {
		return "", fmt.Errorf("write bundled asset %s: %w", target, err)
	}
	return target, nil
}

func buildScriptInput(step *model.Step, ctx *model.ExecutionContext) ([]byte, error) {
	if len(step.ScriptInputs) == 0 {
		return nil, nil
	}
	input := make(map[string]any, len(step.ScriptInputs))
	for k, v := range step.ScriptInputs {
		if isWholeInterpolation(v) {
			if typed, err := textfmt.ResolveTypedValue(v, ctx.CapturedVariables); err == nil {
				input[k] = typed.AuditValue()
				continue
			}
		}
		resolved, err := textfmt.InterpolateTyped(v, ctx.Params, ctx.CapturedVariables, ctx.BuiltinVarsForStep(step.ID))
		if err != nil {
			return nil, fmt.Errorf("script_inputs.%s: %w", k, err)
		}
		input[k] = resolved
	}
	return json.Marshal(input)
}

func captureScriptOutput(format, stdout string) (model.CapturedValue, error) {
	if format == "" || format == "text" {
		return model.NewCapturedString(stdout), nil
	}
	out := strings.TrimSpace(stdout)
	if len(out) > 1024*1024 {
		return model.CapturedValue{}, fmt.Errorf("script json capture exceeds 1 MiB")
	}
	if !utf8.ValidString(out) {
		return model.CapturedValue{}, fmt.Errorf("script json capture stdout was not valid UTF-8")
	}
	var v any
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		return model.CapturedValue{}, fmt.Errorf("script json capture: %w", err)
	}
	switch x := v.(type) {
	case []any:
		values := make([]string, len(x))
		for i, item := range x {
			s, ok := item.(string)
			if !ok {
				return model.CapturedValue{}, fmt.Errorf("script json capture array contains non-string at index %d", i)
			}
			values[i] = s
		}
		return model.NewCapturedList(values), nil
	case map[string]any:
		values := make(map[string]string, len(x))
		for k, item := range x {
			s, ok := item.(string)
			if !ok {
				return model.CapturedValue{}, fmt.Errorf("script json capture object field %q is not a string", k)
			}
			values[k] = s
		}
		return model.NewCapturedMap(values), nil
	default:
		return model.CapturedValue{}, fmt.Errorf("script json capture must be an array of strings or object of strings")
	}
}

func isWholeInterpolation(s string) bool {
	return strings.HasPrefix(s, "{{") && strings.HasSuffix(s, "}}") && len(strings.TrimSpace(s)) == len(s)
}
