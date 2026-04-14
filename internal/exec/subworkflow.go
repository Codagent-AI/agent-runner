package exec

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/engine"
	"github.com/codagent/agent-runner/internal/flowctl"
	"github.com/codagent/agent-runner/internal/loader"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/textfmt"
)

// ExecuteSubWorkflowStep executes a sub-workflow step.
func ExecuteSubWorkflowStep(
	step *model.Step,
	parentCtx *model.ExecutionContext,
	runner ProcessRunner,
	glob GlobExpander,
	log Logger,
) (StepOutcome, error) {
	if step.Workflow == "" {
		return OutcomeFailed, nil
	}

	prefix := audit.BuildPrefix(nestingToAudit(parentCtx), step.ID)
	startTime := time.Now()

	emitAudit(parentCtx, audit.Event{
		Timestamp: startTime.UTC().Format(time.RFC3339),
		Prefix:    prefix,
		Type:      audit.EventStepStart,
		Data:      map[string]any{"context": contextSnapshot(parentCtx)},
	})

	workflowPath, err := resolveWorkflowPath(step.Workflow, parentCtx)
	if err != nil {
		emitSubEnd(parentCtx, prefix, startTime, "failed", err.Error())
		return OutcomeFailed, err
	}

	workflow, err := loader.LoadWorkflow(workflowPath, loader.Options{IsSubWorkflow: true})
	if err != nil {
		emitSubEnd(parentCtx, prefix, startTime, "failed", err.Error())
		return OutcomeFailed, err
	}

	resolvedParams, err := resolveParams(step.Params, parentCtx)
	if err != nil {
		emitSubEnd(parentCtx, prefix, startTime, "failed", err.Error())
		return OutcomeFailed, err
	}

	if err := validateSubWorkflowParams(&workflow, resolvedParams); err != nil {
		emitSubEnd(parentCtx, prefix, startTime, "failed", err.Error())
		return OutcomeFailed, err
	}

	var childEngine interface{}
	if workflow.Engine != nil {
		engConfig := map[string]any{"type": workflow.Engine.Type}
		for k, v := range workflow.Engine.Extras {
			engConfig[k] = v
		}
		eng, err := engine.Create(engConfig)
		if err != nil {
			emitSubEnd(parentCtx, prefix, startTime, "failed", err.Error())
			return OutcomeFailed, err
		}
		childEngine = eng
	}

	childCtx := model.NewSubWorkflowContext(parentCtx, &model.SubWorkflowContextOptions{
		StepID:          step.ID,
		Params:          resolvedParams,
		WorkflowFile:    workflowPath,
		SubWorkflowName: workflow.Name,
		EngineRef:       childEngine,
		EngineSet:       workflow.Engine != nil,
	})

	startFromStepID, startCompleted := applyResumeState(parentCtx, childCtx)
	childPrefix := buildNestingPrefix(childCtx.NestingPath)

	subStart := time.Now()
	emitAudit(childCtx, audit.Event{
		Timestamp: subStart.UTC().Format(time.RFC3339),
		Prefix:    childPrefix,
		Type:      audit.EventSubWorkflowStart,
		Data: map[string]any{
			"workflow_name": workflow.Name,
			"workflow_path": workflowPath,
			"context":       contextSnapshot(childCtx),
		},
	})

	log.Printf("  sub-workflow: %s (%s)\n", workflow.Name, workflowPath)

	outcome, err := executeChildSteps(&workflow, childCtx, runner, glob, log, startFromStepID, startCompleted)

	emitAudit(childCtx, audit.Event{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Prefix:    childPrefix,
		Type:      audit.EventSubWorkflowEnd,
		Data: map[string]any{
			"outcome":     string(outcome),
			"duration_ms": time.Since(subStart).Milliseconds(),
		},
	})

	emitSubEnd(parentCtx, prefix, startTime, string(outcome), "")
	return outcome, err
}

func executeChildSteps(
	workflow *model.Workflow,
	childCtx *model.ExecutionContext,
	runner ProcessRunner,
	glob GlobExpander,
	log Logger,
	startFromStepID string,
	startCompleted bool,
) (StepOutcome, error) {
	// Resolve which step to actually start from, advancing past completed steps.
	resolvedStartID := startFromStepID
	if startFromStepID != "" {
		resolved, err := model.ResolveResumeStep(workflow.Steps, startFromStepID, startCompleted)
		if err != nil {
			return OutcomeFailed, fmt.Errorf("resume step %q not found in sub-workflow", startFromStepID)
		}
		if resolved.AllDone {
			return OutcomeSuccess, nil
		}
		resolvedStartID = resolved.StepID
	}

	reached := resolvedStartID == ""

	for i := range workflow.Steps {
		if !reached {
			if workflow.Steps[i].ID == resolvedStartID {
				reached = true
			} else {
				continue
			}
		}

		if flowctl.ShouldSkip(workflow.Steps[i].SkipIf, childCtx.LastStepOutcome) {
			breadcrumb := textfmt.BuildBreadcrumb(nestingToFmt(childCtx), workflow.Steps[i].ID)
			log.Println(textfmt.Separator())
			log.Println(textfmt.StepHeading(i, len(workflow.Steps), breadcrumb, "", true))
			continue
		}

		breadcrumb := textfmt.BuildBreadcrumb(nestingToFmt(childCtx), workflow.Steps[i].ID)
		log.Println(textfmt.Separator())
		log.Println(textfmt.StepHeading(i, len(workflow.Steps), breadcrumb, workflow.Steps[i].StepType(), false))

		recordChildProgress(childCtx, workflow.Steps[i].ID, false)
		if childCtx.ParentContext != nil && childCtx.ParentContext.FlushState != nil {
			childCtx.ParentContext.FlushState()
		}

		outcome, err := DispatchStep(&workflow.Steps[i], childCtx, runner, glob, log)
		if err != nil {
			return OutcomeFailed, err
		}
		completed := outcome != OutcomeFailed && outcome != OutcomeAborted
		recordChildProgress(childCtx, workflow.Steps[i].ID, completed)
		if childCtx.ParentContext != nil && childCtx.ParentContext.FlushState != nil {
			childCtx.ParentContext.FlushState()
		}

		if outcome == OutcomeAborted {
			return OutcomeAborted, nil
		}

		o := string(outcome)
		childCtx.LastStepOutcome = &o

		if outcome == OutcomeFailed && !workflow.Steps[i].ContinueOnFailure {
			return OutcomeFailed, nil
		}
	}

	if resolvedStartID != "" && !reached {
		return OutcomeFailed, fmt.Errorf("resume step %q not found in sub-workflow", resolvedStartID)
	}
	return OutcomeSuccess, nil
}

func recordChildProgress(childCtx *model.ExecutionContext, childStepID string, completed bool) {
	parent := childCtx.ParentContext
	if parent == nil {
		return
	}

	var nestedChild *model.SubWorkflowChildState
	if childCtx.LastSubWorkflowChild != nil {
		nestedChild = childCtx.LastSubWorkflowChild
		childCtx.LastSubWorkflowChild = nil
	}

	entry := &model.SubWorkflowChildState{
		StepID:            childStepID,
		SessionIDs:        copyMap(childCtx.SessionIDs),
		SessionProfiles:   copyMap(childCtx.SessionProfiles),
		CapturedVariables: copyMap(childCtx.CapturedVariables),
		Completed:         completed,
	}
	// When the deeper state already describes this same step (e.g. a loop step
	// that has written its own iteration metadata into childCtx.LastSubWorkflowChild),
	// promote its Iteration/Child so we do not produce a duplicated wrapper.
	if nestedChild != nil && nestedChild.StepID == childStepID {
		entry.Iteration = nestedChild.Iteration
		entry.Child = nestedChild.Child
	} else {
		entry.Child = nestedChild
	}
	parent.LastSubWorkflowChild = entry
}

func applyResumeState(parentCtx, childCtx *model.ExecutionContext) (string, bool) {
	resumeChild := parentCtx.ResumeChildState
	parentCtx.ResumeChildState = nil
	if resumeChild == nil {
		return "", false
	}

	for k, v := range resumeChild.SessionIDs {
		childCtx.SessionIDs[k] = v
	}
	for k, v := range resumeChild.SessionProfiles {
		childCtx.SessionProfiles[k] = v
	}
	for k, v := range resumeChild.CapturedVariables {
		childCtx.CapturedVariables[k] = v
	}
	if resumeChild.Iteration != nil {
		// This entry describes a loop step that is being resumed mid-iteration.
		// Keep the full entry on childCtx so the loop executor can read its
		// Iteration (and eventually deeper body-step resume metadata) when the
		// sub-workflow dispatches the loop step.
		childCtx.ResumeChildState = resumeChild
	} else if resumeChild.Child != nil {
		childCtx.ResumeChildState = resumeChild.Child
	}
	return resumeChild.StepID, resumeChild.Completed
}

func buildNestingPrefix(nestingPath []model.NestingSegment) string {
	tokens := make([]string, 0, len(nestingPath)*2)
	for _, seg := range nestingPath {
		if seg.Iteration != nil {
			tokens = append(tokens, fmt.Sprintf("%s:%d", seg.StepID, *seg.Iteration))
		} else {
			tokens = append(tokens, seg.StepID)
		}
		if seg.SubWorkflowName != "" {
			tokens = append(tokens, "sub:"+seg.SubWorkflowName)
		}
	}
	return "[" + strings.Join(tokens, ", ") + "]"
}

func resolveWorkflowPath(workflowField string, ctx *model.ExecutionContext) (string, error) {
	interpolated, err := textfmt.Interpolate(workflowField, ctx.Params, ctx.CapturedVariables)
	if err != nil {
		return "", err
	}
	if ctx.WorkflowFile != "" {
		parentDir := filepath.Dir(ctx.WorkflowFile)
		return filepath.Join(parentDir, interpolated), nil
	}
	return interpolated, nil
}

func resolveParams(params map[string]string, ctx *model.ExecutionContext) (map[string]string, error) {
	if params == nil {
		return map[string]string{}, nil
	}
	resolved := make(map[string]string, len(params))
	for k, v := range params {
		val, err := textfmt.Interpolate(v, ctx.Params, ctx.CapturedVariables)
		if err != nil {
			return nil, err
		}
		resolved[k] = val
	}
	return resolved, nil
}

func validateSubWorkflowParams(workflow *model.Workflow, resolvedParams map[string]string) error {
	for _, param := range workflow.Params {
		if _, ok := resolvedParams[param.Name]; ok {
			continue
		}
		if param.Default != "" {
			resolvedParams[param.Name] = param.Default
		} else if param.IsRequired() {
			return fmt.Errorf("missing required parameter: %s", param.Name)
		}
	}
	return nil
}

func emitSubEnd(ctx *model.ExecutionContext, prefix string, startTime time.Time, outcome, errMsg string) {
	data := map[string]any{
		"outcome":     outcome,
		"duration_ms": time.Since(startTime).Milliseconds(),
	}
	if errMsg != "" {
		data["error"] = errMsg
	}
	emitAudit(ctx, audit.Event{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Prefix:    prefix,
		Type:      audit.EventStepEnd,
		Data:      data,
	})
}

func copyMap(m map[string]string) map[string]string {
	result := make(map[string]string, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}
