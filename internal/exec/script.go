package exec

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/textfmt"
	builtinworkflows "github.com/codagent/agent-runner/workflows"
)

func ExecuteScriptStep(step *model.Step, ctx *model.ExecutionContext, runner ProcessRunner, log Logger) (StepOutcome, error) {
	scriptPath, err := resolveScriptPath(step.Script, ctx)
	if err != nil {
		return OutcomeFailed, err
	}
	stdin, err := buildScriptInput(step, ctx)
	if err != nil {
		return OutcomeFailed, err
	}

	log.Printf("  script: %s\n", step.Script)
	var result ProcessResult
	result, err = runner.RunScript(scriptPath, stdin, step.Capture != "", step.Workdir)
	if err != nil {
		return OutcomeFailed, err
	}
	if result.ExitCode != 0 {
		return OutcomeFailed, nil
	}
	if step.Capture != "" {
		captured, err := captureScriptOutput(step.CaptureFormat, result.Stdout)
		if err != nil {
			return OutcomeFailed, err
		}
		ctx.CapturedVariables[step.Capture] = captured
	}
	return OutcomeSuccess, nil
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
