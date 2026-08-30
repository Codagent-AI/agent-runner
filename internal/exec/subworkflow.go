package exec

import (
	"fmt"
	"strings"
	"time"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/config"
	"github.com/codagent/agent-runner/internal/engine"
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

	workflowPath, err := resolveWorkflowPath(step.Workflow, parentCtx, step.ID)
	if err != nil {
		emitStepStart(parentCtx, prefix, startTime, nil)
		emitSubEnd(parentCtx, prefix, startTime, step, "failed", err.Error())
		return OutcomeFailed, err
	}
	resolvedParams, err := resolveParams(step.Params, parentCtx, step.ID)
	if err != nil {
		emitStepStart(parentCtx, prefix, startTime, map[string]any{"workflow_path": workflowPath})
		emitSubEnd(parentCtx, prefix, startTime, step, "failed", err.Error())
		return OutcomeFailed, err
	}
	emitStepStart(parentCtx, prefix, startTime, map[string]any{
		"workflow_path": workflowPath,
		"params":        resolvedParams,
	})

	workflow, childCtx, err := prepareSubWorkflow(step, workflowPath, resolvedParams, parentCtx, log)
	if err != nil {
		emitSubEnd(parentCtx, prefix, startTime, step, "failed", err.Error())
		return OutcomeFailed, err
	}

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

	outcome, err := executeScopedChildWorkflow(&workflow, childCtx, runner, glob, log, startFromStepID, startCompleted)

	endData := map[string]any{
		"outcome":     string(outcome),
		"duration_ms": time.Since(subStart).Milliseconds(),
	}
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
		endData["error"] = errMsg
	}
	emitAudit(childCtx, audit.Event{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Prefix:    childPrefix,
		Type:      audit.EventSubWorkflowEnd,
		Data:      endData,
	})

	emitSubEnd(parentCtx, prefix, startTime, step, string(outcome), errMsg)
	return outcome, err
}

// executeScopedChildWorkflow supplies the same repository boundary to a
// complete child workflow body that DispatchStep supplies to nested groups and
// loops. An already-active repository deliberately suppresses another fan-out
// while preserving the parent's complete repositories parameter.
func executeScopedChildWorkflow(
	workflow *model.Workflow,
	ctx *model.ExecutionContext,
	runner ProcessRunner,
	glob GlobExpander,
	log Logger,
	startFromStepID string,
	startCompleted bool,
) (StepOutcome, error) {
	if workflow.Scope != model.ScopeRepositories || ctx.ActiveRepository != nil {
		return executeChildSteps(workflow, ctx, runner, glob, log, startFromStepID, startCompleted)
	}
	boundaryPrefix := buildNestingPrefix(ctx.NestingPath)
	return executeNestedRepositoryFanout(ctx, boundaryPrefix, func(repositoryCtx *model.ExecutionContext, index int) (StepOutcome, error) {
		// The invoking step remains outside the repository-owned child body.
		repositoryCtx.RepositoryPrefixDepth = ctx.AuditPrefixTokenCount()
		if len(ctx.NestingPath) > 0 && ctx.NestingPath[len(ctx.NestingPath)-1].SubWorkflowName != "" {
			repositoryCtx.RepositoryPrefixDepth--
		}
		entry := nestedRepositoryEntry(ctx.RepositoryFrame, index)
		resumeStepID, resumeCompleted := "", false
		if entry != nil && entry.Nested != nil {
			resumeStepID, resumeCompleted = startFromStepID, startCompleted
		}
		return executeChildSteps(workflow, repositoryCtx, runner, glob, log, resumeStepID, resumeCompleted)
	})
}

// prepareSubWorkflow resolves the sub-workflow path, loads it, validates its
// params, constructs the child context, and merges its session declarations.
// Extracted from ExecuteSubWorkflowStep to keep that function under the lint
// length limit.
func prepareSubWorkflow(
	step *model.Step,
	workflowPath string,
	resolvedParams map[string]string,
	parentCtx *model.ExecutionContext,
	log Logger,
) (model.Workflow, *model.ExecutionContext, error) {
	workflow, err := loader.LoadWorkflow(workflowPath, loader.Options{IsSubWorkflow: true})
	if err != nil {
		return model.Workflow{}, nil, err
	}
	if parentCtx.WorkflowScope == model.ScopeRepositories && workflow.Scope == model.ScopeWorkspace {
		return model.Workflow{}, nil, fmt.Errorf("repository-scoped workflows cannot invoke a workspace-scoped child; invoke the workspace child from a workspace-scoped parent")
	}
	if workflow.RequiresRepositoryTargets() && parentCtx.Workspace != nil && len(parentCtx.Workspace.Repositories) == 1 {
		if _, implicit := parentCtx.Workspace.Repositories["default"]; implicit {
			if _, supplied := resolvedParams[model.RepositoriesParam]; !supplied {
				resolvedParams = copyMap(resolvedParams)
				resolvedParams[model.RepositoriesParam] = "default"
			}
		}
	}

	if err := validateSubWorkflowParams(&workflow, resolvedParams); err != nil {
		return model.Workflow{}, nil, err
	}

	var childEngine interface{}
	if workflow.Engine != nil {
		engConfig := map[string]any{"type": workflow.Engine.Type}
		for k, v := range workflow.Engine.Extras {
			engConfig[k] = v
		}
		eng, err := engine.Create(engConfig)
		if err != nil {
			return model.Workflow{}, nil, err
		}
		childEngine = eng
	}

	childCtx := model.NewSubWorkflowContext(parentCtx, &model.SubWorkflowContextOptions{
		StepID:          step.ID,
		Params:          resolvedParams,
		WorkflowFile:    workflowPath,
		SubWorkflowName: workflow.Name,
		WorkflowScope:   workflow.Scope,
		EngineRef:       childEngine,
		EngineSet:       workflow.Engine != nil,
	})
	if workflow.RequiresRepositoryTargets() && childCtx.Workspace != nil {
		selected, err := model.ParseRepositoryTargets(resolvedParams[model.RepositoriesParam], childCtx.Workspace.Repositories)
		if err != nil {
			return model.Workflow{}, nil, err
		}
		workspace := *childCtx.Workspace
		workspace.Selected = append([]string(nil), selected...)
		childCtx.Workspace = &workspace
	}

	if err := MergeSessionDecls(childCtx, workflow.Sessions, log); err != nil {
		return model.Workflow{}, nil, err
	}

	return workflow, childCtx, nil
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

		if shouldEvaluateSkipBeforeDispatch(&workflow.Steps[i], childCtx) {
			skip, skipErr := ShouldSkipStep(workflow.Steps[i].SkipIf, childCtx.LastStepOutcome, childCtx, workflow.Steps[i].ID)
			if skipErr != nil {
				return OutcomeFailed, fmt.Errorf("step %q skip_if evaluation failed: %w", workflow.Steps[i].ID, skipErr)
			}
			if skip {
				skipChildStep(childCtx, &workflow.Steps[i])
				continue
			}
		}

		updateChildProgress(childCtx, workflow.Steps[i].ID, false)

		outcome, err := DispatchStep(&workflow.Steps[i], childCtx, runner, glob, log)
		if err != nil {
			return OutcomeFailed, err
		}
		completed := outcome != OutcomeFailed && outcome != OutcomeAborted
		updateChildProgress(childCtx, workflow.Steps[i].ID, completed)

		if outcome == OutcomeAborted {
			return OutcomeAborted, nil
		}

		recordLastStepOutcome(childCtx, outcome)

		if outcome == OutcomeFailed && !workflow.Steps[i].ContinueOnFailure {
			return OutcomeFailed, nil
		}
	}

	if resolvedStartID != "" && !reached {
		return OutcomeFailed, fmt.Errorf("resume step %q not found in sub-workflow", resolvedStartID)
	}
	return OutcomeSuccess, nil
}

func skipChildStep(childCtx *model.ExecutionContext, step *model.Step) {
	updateChildProgress(childCtx, step.ID, true)
	emitSkippedChildStep(childCtx, step)
}

func updateChildProgress(childCtx *model.ExecutionContext, childStepID string, completed bool) {
	recordChildProgress(childCtx, childStepID, completed)
	flushChildProgress(childCtx)
}

func flushChildProgress(childCtx *model.ExecutionContext) {
	if childCtx.ParentContext != nil && childCtx.ParentContext.FlushState != nil {
		childCtx.ParentContext.FlushState()
	}
}

func emitSkippedChildStep(childCtx *model.ExecutionContext, step *model.Step) {
	prefix := audit.BuildPrefix(nestingToAudit(childCtx), step.ID)
	startTime := time.Now()
	emitStepStart(childCtx, prefix, startTime, nil)
	emitStepEnd(childCtx, prefix, startTime, string(OutcomeSkipped), map[string]any{"skip_if": step.SkipIf}, step)
}

func recordChildProgress(childCtx *model.ExecutionContext, childStepID string, completed bool) {
	parent := childCtx.ParentContext
	if parent == nil {
		return
	}

	var nestedChild *model.NestedStepState
	if childCtx.LastSubWorkflowChild != nil {
		nestedChild = childCtx.LastSubWorkflowChild
		childCtx.LastSubWorkflowChild = nil
	}

	entry := &model.NestedStepState{
		StepID:            childStepID,
		SessionIDs:        copyMap(childCtx.SessionIDs),
		SessionProfiles:   copyMap(childCtx.SessionProfiles),
		CapturedVariables: copyMap(childCtx.CapturedVariables),
		LastSessionStepID: childCtx.LastSessionStepID,
		Completed:         completed,
	}
	// When the deeper state already describes this same step (e.g. a loop step
	// that has written its own iteration metadata into childCtx.LastSubWorkflowChild),
	// promote its Iteration/Child so we do not produce a duplicated wrapper.
	// Matching IDs alone are insufficient because nested sub-workflows may
	// legitimately reuse their parent's step ID.
	if nestedChild != nil && nestedChild.StepID == childStepID && nestedChild.Iteration != nil {
		entry.Iteration = nestedChild.Iteration
		entry.Child = nestedChild.Child
	} else {
		entry.Child = nestedChild
	}
	if childCtx.ActiveRepository != nil && childCtx.RepositoryIndex != nil {
		if repositoryState := nestedRepositoryEntry(childCtx.RepositoryFrame, *childCtx.RepositoryIndex); repositoryState != nil {
			repositoryState.Nested = repositoryProgressChain(childCtx, entry)
		}
	}
	parent.LastSubWorkflowChild = entry
}

// repositoryProgressChain returns the complete state below a repository
// fan-out boundary even when a deep child flushes before its sub-workflows
// unwind. Workspace-owned wrappers stay in the ordinary root chain.
func repositoryProgressChain(childCtx *model.ExecutionContext, leaf *model.NestedStepState) *model.NestedStepState {
	chain := leaf
	for current := childCtx; current != nil && current.ParentContext != nil; current = current.ParentContext {
		parent := current.ParentContext
		if parent.ActiveRepository == nil || parent.ParentContext == nil || parent.ParentContext.ActiveRepository == nil {
			break
		}
		if len(current.NestingPath) == 0 {
			break
		}
		segment := current.NestingPath[len(current.NestingPath)-1]
		if segment.StepID == "" {
			break
		}
		entry := &model.NestedStepState{
			StepID:            segment.StepID,
			SessionIDs:        copyMap(current.SessionIDs),
			SessionProfiles:   copyMap(current.SessionProfiles),
			CapturedVariables: copyMap(current.CapturedVariables),
			LastSessionStepID: current.LastSessionStepID,
			Child:             chain,
		}
		if segment.Iteration != nil {
			iteration := *segment.Iteration
			entry.Iteration = &iteration
		}
		chain = entry
	}
	return chain
}

func applyResumeState(parentCtx, childCtx *model.ExecutionContext) (string, bool) {
	resumeChild := parentCtx.ResumeChildState
	parentCtx.ResumeChildState = nil
	if resumeChild == nil {
		return "", false
	}

	restorePersistedSessions(childCtx, resumeChild)
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

// restorePersistedSessions copies persisted session IDs, session profiles,
// captured variables, and the last-session-step ID from src into ctx. Used
// by both sub-workflow and loop-iteration resume paths.
func restorePersistedSessions(ctx *model.ExecutionContext, src *model.NestedStepState) {
	for k, v := range src.SessionIDs {
		ctx.SessionIDs[k] = v
	}
	for k, v := range src.SessionProfiles {
		ctx.SessionProfiles[k] = v
	}
	for k, v := range src.CapturedVariables {
		ctx.CapturedVariables[k] = v
	}
	if src.LastSessionStepID != "" {
		ctx.LastSessionStepID = src.LastSessionStepID
	}
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

func resolveWorkflowPath(workflowField string, ctx *model.ExecutionContext, stepID string) (string, error) {
	interpolated, err := textfmt.InterpolateTyped(workflowField, ctx.Params, ctx.CapturedVariables, ctx.BuiltinVarsForStep(stepID))
	if err != nil {
		return "", err
	}
	if ctx.WorkflowFile != "" {
		return loader.ResolveRelativeWorkflowPath(ctx.WorkflowFile, interpolated), nil
	}
	return interpolated, nil
}

func resolveParams(params map[string]string, ctx *model.ExecutionContext, stepID string) (map[string]string, error) {
	if params == nil {
		return map[string]string{}, nil
	}
	resolved := make(map[string]string, len(params))
	for k, v := range params {
		val, err := textfmt.InterpolateTyped(v, ctx.Params, ctx.CapturedVariables, ctx.BuiltinVarsForStep(stepID))
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
			continue
		}
		if !param.IsRequired() {
			resolvedParams[param.Name] = ""
			continue
		}
		return fmt.Errorf("missing required parameter: %s", param.Name)
	}
	return nil
}

func emitSubEnd(ctx *model.ExecutionContext, prefix string, startTime time.Time, step *model.Step, outcome, errMsg string) {
	data := map[string]any{}
	if errMsg != "" {
		data["error"] = errMsg
	}
	emitStepEnd(ctx, prefix, startTime, outcome, data, step)
}

// MergeSessionDecls adds session declarations from a newly loaded (sub-)workflow
// into the shared NamedSessionDecls map. Compatible duplicates (same name, same
// agent) are silently merged.
//
// When the same name is declared with different agents:
//   - If a live session already exists, a warning is emitted and the original
//     agent is kept (the CLI session was created under that agent; switching
//     profiles mid-run would strand it).
//   - If no live session exists, the conflict is unrecoverable and an error
//     is returned. Cross-file composition validation (loader.ValidateComposition)
//     should have caught this before runtime, so reaching here means validation
//     was skipped.
func MergeSessionDecls(ctx *model.ExecutionContext, sessions []model.SessionDecl, log Logger) error {
	if len(sessions) == 0 {
		return nil
	}
	for _, decl := range sessions {
		canonicalAgent, warning := config.CanonicalAgentName(decl.Agent)
		if warning != nil {
			EmitAgentDeprecations(ctx, log, []config.Deprecation{*warning})
		}
		decl.Agent = canonicalAgent
		existing := ctx.LookupNamedSessionDecl(decl.Name)
		present := existing != ""
		if present {
			canonicalExisting, existingWarning := config.CanonicalAgentName(existing)
			if existingWarning != nil {
				EmitAgentDeprecations(ctx, log, []config.Deprecation{*existingWarning})
				existing = canonicalExisting
				ctx.SetNamedSessionDecl(decl.Name, canonicalExisting)
			}
		}
		if !present {
			ctx.SetNamedSessionDecl(decl.Name, decl.Agent)
			continue
		}
		if existing == decl.Agent {
			continue
		}
		if ctx.LookupNamedSession(decl.Name) != "" {
			log.Printf("warning: named session %q: declared agent changed from %q to %q; continuing with original agent\n",
				decl.Name, existing, decl.Agent)
			continue
		}
		return fmt.Errorf(
			"incompatible named session declaration %q: already declared with agent %q, cannot redeclare with agent %q",
			decl.Name, existing, decl.Agent,
		)
	}
	return nil
}

func copyMap[K comparable, V any](m map[K]V) map[K]V {
	result := make(map[K]V, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}
