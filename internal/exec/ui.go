package exec

import (
	"fmt"
	"strings"
	"time"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/textfmt"
)

func ExecuteUIStep(step *model.Step, ctx *model.ExecutionContext, log Logger) (StepOutcome, error) {
	prefix := audit.BuildPrefix(nestingToAudit(ctx), step.ID)
	startTime := time.Now()
	emitStepStart(ctx, prefix, startTime, map[string]any{"title": step.Title})

	request, err := buildUIRequest(step, ctx)
	if err != nil {
		emitUIEnd(ctx, prefix, startTime, "failed", "", err)
		return OutcomeFailed, err
	}
	var result model.UIStepResult
	if ctx.UIStepHandler != nil {
		result, err = ctx.UIStepHandler(&request)
	} else {
		err := fmt.Errorf("UI steps require a TTY")
		emitUIEnd(ctx, prefix, startTime, "failed", "", err)
		return OutcomeFailed, err
	}
	if err != nil {
		emitUIEnd(ctx, prefix, startTime, "failed", "", err)
		return OutcomeFailed, err
	}
	if result.Canceled {
		emitUIEnd(ctx, prefix, startTime, "failed", result.Outcome, nil)
		return OutcomeFailed, nil
	}
	if step.OutcomeCapture != "" {
		ctx.CapturedVariables[step.OutcomeCapture] = model.NewCapturedString(result.Outcome)
	}
	if step.Capture != "" {
		ctx.CapturedVariables[step.Capture] = model.NewCapturedMap(result.Inputs)
	}
	log.Printf("  ui outcome: %s\n", result.Outcome)
	emitUIEnd(ctx, prefix, startTime, "success", result.Outcome, nil)
	return OutcomeSuccess, nil
}

func emitUIEnd(ctx *model.ExecutionContext, prefix string, startTime time.Time, outcome, uiOutcome string, err error) {
	data := map[string]any{}
	if uiOutcome != "" {
		data["ui_outcome"] = uiOutcome
	}
	if err != nil {
		data["error"] = err.Error()
	}
	emitStepEnd(ctx, prefix, startTime, outcome, data)
}

func buildUIRequest(step *model.Step, ctx *model.ExecutionContext) (model.UIStepRequest, error) {
	title, err := textfmt.InterpolateTyped(step.Title, ctx.Params, ctx.CapturedVariables, ctx.BuiltinVarsForStep(step.ID))
	if err != nil {
		return model.UIStepRequest{}, err
	}
	title = textfmt.StripANSI(title)
	body, err := textfmt.InterpolateTyped(step.Body, ctx.Params, ctx.CapturedVariables, ctx.BuiltinVarsForStep(step.ID))
	if err != nil {
		return model.UIStepRequest{}, err
	}
	body = textfmt.StripANSI(body)
	inputs := make([]model.UIInput, len(step.Inputs))
	for i, input := range step.Inputs {
		inputs[i] = input
		prompt, err := textfmt.InterpolateTyped(input.Prompt, ctx.Params, ctx.CapturedVariables, ctx.BuiltinVarsForStep(step.ID))
		if err != nil {
			return model.UIStepRequest{}, fmt.Errorf("inputs[%d].prompt: %w", i, err)
		}
		inputs[i].Prompt = textfmt.StripANSI(prompt)
		options, err := resolveUIOptions(input.Options, ctx)
		if err != nil {
			return model.UIStepRequest{}, fmt.Errorf("inputs[%d].options: %w", i, err)
		}
		if len(options) == 0 {
			return model.UIStepRequest{}, fmt.Errorf("no options available for %s", input.ID)
		}
		for j := range options {
			options[j] = textfmt.StripANSI(options[j])
		}
		inputs[i].Options = options
	}
	actions := make([]model.UIAction, len(step.Actions))
	for i, action := range step.Actions {
		label, err := textfmt.InterpolateTyped(action.Label, ctx.Params, ctx.CapturedVariables, ctx.BuiltinVarsForStep(step.ID))
		if err != nil {
			return model.UIStepRequest{}, fmt.Errorf("actions[%d].label: %w", i, err)
		}
		actions[i] = model.UIAction{Label: textfmt.StripANSI(label), Outcome: action.Outcome}
	}
	return model.UIStepRequest{StepID: step.ID, Title: title, Body: body, Actions: actions, Inputs: inputs}, nil
}

func resolveUIOptions(options []string, ctx *model.ExecutionContext) ([]string, error) {
	if len(options) == 1 && strings.HasPrefix(options[0], "{{") && strings.HasSuffix(options[0], "}}") {
		raw, err := textfmt.ResolveTypedValue(options[0], ctx.CapturedVariables)
		if err == nil {
			if raw.Kind != model.CaptureList {
				return nil, fmt.Errorf("single-select options must resolve to a list of strings")
			}
			return append([]string(nil), raw.List...), nil
		}
	}
	resolved := make([]string, len(options))
	for i, option := range options {
		value, err := textfmt.InterpolateTyped(option, ctx.Params, ctx.CapturedVariables, ctx.BuiltinVars())
		if err != nil {
			return nil, err
		}
		resolved[i] = value
	}
	return resolved, nil
}
