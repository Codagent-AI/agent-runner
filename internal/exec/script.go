package exec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/textfmt"
	builtinworkflows "github.com/codagent/agent-runner/workflows"
)

type scriptRunner interface {
	RunScript(path string, stdin []byte, captureStdout bool, workdir string) (ProcessResult, error)
}

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
	if sr, ok := runner.(scriptRunner); ok {
		result, err = sr.RunScript(scriptPath, stdin, step.Capture != "", step.Workdir)
	} else {
		result, err = runScriptProcess(scriptPath, stdin, step.Capture != "", step.Workdir)
	}
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
	return filepath.Join(filepath.Dir(ctx.WorkflowFile), filepath.FromSlash(clean)), nil
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
	input := make(map[string]string, len(step.ScriptInputs))
	for k, v := range step.ScriptInputs {
		resolved, err := textfmt.Interpolate(v, ctx.Params, ctx.CapturedVariables, ctx.BuiltinVarsForStep(step.ID))
		if err != nil {
			return nil, fmt.Errorf("script_inputs.%s: %w", k, err)
		}
		input[k] = resolved
	}
	return json.Marshal(input)
}

func captureScriptOutput(format, stdout string) (string, error) {
	out := strings.TrimSpace(stdout)
	if format == "" || format == "text" {
		return out, nil
	}
	if len(out) > 1024*1024 {
		return "", fmt.Errorf("script json capture exceeds 1 MiB")
	}
	var v any
	dec := json.NewDecoder(strings.NewReader(out))
	if err := dec.Decode(&v); err != nil {
		return "", fmt.Errorf("script json capture: %w", err)
	}
	switch x := v.(type) {
	case []any:
		values := make([]string, len(x))
		for i, item := range x {
			s, ok := item.(string)
			if !ok {
				return "", fmt.Errorf("script json capture array contains non-string at index %d", i)
			}
			values[i] = s
		}
		compact, _ := json.Marshal(values)
		return string(compact), nil
	case map[string]any:
		values := make(map[string]string, len(x))
		for k, item := range x {
			s, ok := item.(string)
			if !ok {
				return "", fmt.Errorf("script json capture object field %q is not a string", k)
			}
			values[k] = s
		}
		compact, _ := json.Marshal(values)
		return string(compact), nil
	default:
		return "", fmt.Errorf("script json capture must be an array of strings or object of strings")
	}
}

func runScriptProcess(scriptPath string, stdin []byte, captureStdout bool, workdir string) (ProcessResult, error) {
	c := exec.Command(scriptPath) // #nosec G204 -- workflow script path is validated and resolved.
	c.Stdin = bytes.NewReader(stdin)
	if workdir != "" {
		c.Dir = filepath.Clean(workdir) // #nosec G304
	}
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	err := c.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return ProcessResult{}, err
		}
	}
	out := stdout.String()
	if !captureStdout {
		out = ""
	}
	return ProcessResult{ExitCode: exitCode, Stdout: out, Stderr: stderr.String()}, nil
}
