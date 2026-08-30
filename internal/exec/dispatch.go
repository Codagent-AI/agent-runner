package exec

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/repositorylock"
)

// DispatchStep routes a step to the correct executor based on its type.
func DispatchStep(
	step *model.Step,
	ctx *model.ExecutionContext,
	runner ProcessRunner,
	glob GlobExpander,
	log Logger,
) (StepOutcome, error) {
	if step.Scope == model.ScopeRepositories && ctx.ActiveRepository == nil {
		return dispatchRepositoryScopedStep(step, ctx, runner, glob, log)
	}
	return dispatchStepOnce(step, ctx, runner, glob, log)
}

// shouldEvaluateSkipBeforeDispatch reports whether skip_if can be evaluated in
// the current context. A repository fan-out evaluates it inside each repository
// so repository interpolation and previous-step state are iteration-specific.
func shouldEvaluateSkipBeforeDispatch(step *model.Step, ctx *model.ExecutionContext) bool {
	return step.Scope != model.ScopeRepositories || ctx.ActiveRepository != nil
}

// dispatchRepositoryScopedStep is the non-top-level repository fan-out
// boundary. The runner owns top-level workflow dispatch; this keeps groups,
// loops, and child workflow bodies from falling back to workspace execution.
func dispatchRepositoryScopedStep(
	step *model.Step,
	ctx *model.ExecutionContext,
	runner ProcessRunner,
	glob GlobExpander,
	log Logger,
) (StepOutcome, error) {
	boundaryPrefix := audit.BuildPrefix(nestingToAudit(ctx), step.ID)
	return executeNestedRepositoryFanout(ctx, boundaryPrefix, func(repositoryCtx *model.ExecutionContext, index int) (StepOutcome, error) {
		repositoryCtx.RepositoryPrefixDepth = ctx.AuditPrefixTokenCount() + 1
		skip, skipErr := ShouldSkipStep(step.SkipIf, repositoryCtx.LastStepOutcome, repositoryCtx, step.ID)
		if skipErr != nil {
			return OutcomeFailed, fmt.Errorf("step %q skip_if evaluation failed for repository %q: %w", step.ID, repositoryCtx.ActiveRepository.Name, skipErr)
		}
		if skip {
			emitSkippedChildStep(repositoryCtx, step)
			return OutcomeSuccess, nil
		}
		return dispatchStepOnce(step, repositoryCtx, runner, glob, log)
	})
}

func executeNestedRepositoryFanout(
	ctx *model.ExecutionContext,
	boundaryPrefix string,
	execute func(*model.ExecutionContext, int) (StepOutcome, error),
) (StepOutcome, error) {
	if ctx.Workspace == nil || len(ctx.Workspace.Selected) == 0 {
		return OutcomeFailed, fmt.Errorf("repository boundary %q has no selected repositories", boundaryPrefix)
	}
	if err := acquireSelectedRepositoryLocks(ctx); err != nil {
		return OutcomeFailed, err
	}
	ensureNestedRepositoryFrame(ctx, boundaryPrefix)
	previousIndex := ctx.RepositoryIndex
	defer func() { ctx.RepositoryIndex = previousIndex }()
	for index, name := range ctx.Workspace.Selected {
		entry := nestedRepositoryEntry(ctx.RepositoryFrame, index)
		if entry != nil && entry.Status == model.RepositoryCompleted {
			continue
		}
		repository, ok := ctx.Workspace.Repositories[name]
		if !ok {
			return OutcomeFailed, fmt.Errorf("selected repository %q is no longer configured", name)
		}
		activeIndex := index
		ctx.RepositoryIndex = &activeIndex
		if entry != nil {
			entry.Status = model.RepositoryActive
			flushRepositoryProgress(ctx)
		}
		repositoryCtx := model.NewRepositoryExecutionContext(ctx, repository, index)
		started := time.Now()
		emitRepositoryBoundaryStart(repositoryCtx, boundaryPrefix, index, len(ctx.Workspace.Selected), started)
		outcome, err := execute(repositoryCtx, index)
		if err != nil {
			if entry != nil {
				entry.Status = model.RepositoryFailed
				flushRepositoryProgress(ctx)
			}
			emitRepositoryBoundaryEnd(repositoryCtx, boundaryPrefix, OutcomeFailed, started)
			return OutcomeFailed, err
		}
		if outcome == OutcomeFailed || outcome == OutcomeAborted {
			if entry != nil {
				entry.Status = model.RepositoryFailed
				flushRepositoryProgress(ctx)
			}
			emitRepositoryBoundaryEnd(repositoryCtx, boundaryPrefix, outcome, started)
			return outcome, nil
		}
		if entry != nil {
			entry.Status = model.RepositoryCompleted
			flushRepositoryProgress(ctx)
		}
		emitRepositoryBoundaryEnd(repositoryCtx, boundaryPrefix, outcome, started)
	}
	return OutcomeSuccess, nil
}

func ensureNestedRepositoryFrame(ctx *model.ExecutionContext, boundaryPrefix string) {
	if ctx.RepositoryFrame != nil && ctx.RepositoryFrame.BoundaryID == boundaryPrefix {
		return
	}
	frame := &model.RepositoryFrame{BoundaryID: boundaryPrefix, Repositories: make([]model.RepositoryExecutionState, 0, len(ctx.Workspace.Selected))}
	for _, name := range ctx.Workspace.Selected {
		repository := ctx.Workspace.Repositories[name]
		frame.Repositories = append(frame.Repositories, model.RepositoryExecutionState{
			Identity: model.RepositoryIdentity(repository),
			Status:   model.RepositoryPending,
		})
	}
	for current := ctx; current != nil; current = current.ParentContext {
		if current.Workspace != nil {
			workspace := *current.Workspace
			workspace.Selected = append([]string(nil), ctx.Workspace.Selected...)
			current.Workspace = &workspace
		}
		current.RepositoryFrame = frame
	}
	flushRepositoryProgress(ctx)
}

func nestedRepositoryEntry(frame *model.RepositoryFrame, index int) *model.RepositoryExecutionState {
	if frame == nil || index < 0 || index >= len(frame.Repositories) {
		return nil
	}
	return &frame.Repositories[index]
}

func flushRepositoryProgress(ctx *model.ExecutionContext) {
	if ctx.FlushState != nil {
		ctx.FlushState()
	}
}

func emitRepositoryBoundaryStart(ctx *model.ExecutionContext, prefix string, index, total int, started time.Time) {
	if ctx.ActiveRepository == nil || ctx.ActiveRepository.Name == "default" {
		return
	}
	emitAudit(ctx, audit.Event{
		Timestamp: started.UTC().Format(time.RFC3339Nano), Prefix: prefix, Type: audit.EventRepositoryStart,
		Data: map[string]any{"position": index, "total": total, "context": contextSnapshot(ctx)},
	})
}

func emitRepositoryBoundaryEnd(ctx *model.ExecutionContext, prefix string, outcome StepOutcome, started time.Time) {
	if ctx.ActiveRepository == nil || ctx.ActiveRepository.Name == "default" {
		return
	}
	emitAudit(ctx, audit.Event{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano), Prefix: prefix, Type: audit.EventRepositoryEnd,
		Data: map[string]any{"outcome": string(outcome), "duration_ms": time.Since(started).Milliseconds()},
	})
}

func acquireSelectedRepositoryLocks(ctx *model.ExecutionContext) error {
	// Direct executor callers do not create a run identity. Production runs
	// always carry SessionDir, which makes this safe to call from both runner
	// and child-workflow dispatch without weakening the process-lifetime lock.
	if ctx.SessionDir == "" || ctx.Workspace == nil || len(ctx.Workspace.Selected) == 0 {
		return nil
	}
	targets := make([]repositorylock.Target, 0, len(ctx.Workspace.Selected))
	for _, name := range ctx.Workspace.Selected {
		repository, ok := ctx.Workspace.Repositories[name]
		if !ok {
			return fmt.Errorf("selected repository %q is no longer configured", name)
		}
		targets = append(targets, repositorylock.Target{Root: repository.Dir, RunID: filepath.Base(filepath.Clean(ctx.SessionDir))})
	}
	if err := repositorylock.AcquireAll(targets); err != nil {
		return fmt.Errorf("acquire selected repository locks: %w", err)
	}
	return nil
}

func dispatchStepOnce(
	step *model.Step,
	ctx *model.ExecutionContext,
	runner ProcessRunner,
	glob GlobExpander,
	log Logger,
) (StepOutcome, error) {
	executable := stepForExecution(step, ctx.WorkingDir)
	if step.Loop != nil && len(step.Steps) > 0 {
		result, err := ExecuteLoopStep(&executable, ctx, runner, glob, log, LoopExecuteOptions{})
		if err != nil {
			return OutcomeFailed, err
		}
		return mapLoopOutcome(step, result.Outcome), nil
	}

	if step.Workflow != "" {
		return ExecuteSubWorkflowStep(&executable, ctx, runner, glob, log)
	}

	if len(step.Steps) > 0 {
		return executeGroupStep(&executable, executable.Steps, ctx, runner, glob, log)
	}

	if step.Command != "" {
		if ctx.PrepareStepHook != nil {
			ctx.PrepareStepHook(step.Mode == model.ModeInteractive)
		}
		return ExecuteShellStep(&executable, ctx, runner, log)
	}

	if step.Script != "" {
		if ctx.PrepareStepHook != nil {
			ctx.PrepareStepHook(false)
		}
		return ExecuteScriptStep(&executable, ctx, runner, log)
	}

	if step.Mode == model.ModeUI {
		if ctx.PrepareStepHook != nil {
			ctx.PrepareStepHook(false)
		}
		return ExecuteUIStep(&executable, ctx, log)
	}

	if step.Agent != "" || step.Prompt != "" {
		if ctx.PrepareStepHook != nil {
			invocationContext := ResolveAgentInvocationContext(step, ctx)
			ctx.PrepareStepHook(!invocationContext.IsHeadless())
		}
		return ExecuteAgentStep(&executable, ctx, runner, log)
	}

	return OutcomeFailed, nil
}

// stepForExecution resolves a process starting directory only at the point a
// step is dispatched. Keeping nested bodies raw until then makes a repository
// container switch their relative roots rather than baking in the workspace.
func stepForExecution(step *model.Step, root string) model.Step {
	executable := *step
	if executable.Workdir == "" {
		executable.Workdir = root
	} else if !filepath.IsAbs(executable.Workdir) {
		executable.Workdir = filepath.Join(root, executable.Workdir)
	}
	return executable
}

// MapLoopOutcomeForRunner maps loop outcomes for the runner's step dispatch.
func MapLoopOutcomeForRunner(step *model.Step, outcome StepOutcome) StepOutcome {
	return mapLoopOutcome(step, outcome)
}

func mapLoopOutcome(step *model.Step, outcome StepOutcome) StepOutcome {
	if outcome == OutcomeSuccess {
		return OutcomeSuccess
	}
	if outcome == OutcomeExhausted && !hasBreakCondition(step.Steps) {
		return OutcomeSuccess
	}
	if outcome == OutcomeAborted {
		return OutcomeAborted
	}
	return OutcomeFailed
}

func hasBreakCondition(steps []model.Step) bool {
	for i := range steps {
		if steps[i].BreakIf != "" || hasBreakCondition(steps[i].Steps) {
			return true
		}
	}
	return false
}

func executeGroupStep(
	step *model.Step,
	steps []model.Step,
	ctx *model.ExecutionContext,
	runner ProcessRunner,
	glob GlobExpander,
	log Logger,
) (StepOutcome, error) {
	prefix := audit.BuildPrefix(nestingToAudit(ctx), step.ID)
	startTime := time.Now()
	emitStepStart(ctx, prefix, startTime, nil)
	originalNestingPath := ctx.NestingPath
	childNestingPath := make([]model.NestingSegment, len(originalNestingPath)+1)
	copy(childNestingPath, originalNestingPath)
	childNestingPath[len(originalNestingPath)] = model.NestingSegment{StepID: step.ID}
	ctx.NestingPath = childNestingPath
	defer func() { ctx.NestingPath = originalNestingPath }()
	resumeStepID, resumeChild, allDone, err := consumeGroupResume(ctx, steps)
	if err != nil {
		emitStepEnd(ctx, prefix, startTime, string(OutcomeFailed), map[string]any{"error": err.Error()}, step)
		return OutcomeFailed, err
	}
	if allDone {
		emitStepEnd(ctx, prefix, startTime, string(OutcomeSuccess), nil, step)
		return OutcomeSuccess, nil
	}
	reached := resumeStepID == ""
	for i := range steps {
		if !reached {
			if steps[i].ID != resumeStepID {
				continue
			}
			reached = true
		}
		if shouldEvaluateSkipBeforeDispatch(&steps[i], ctx) {
			skip, skipErr := ShouldSkipStep(steps[i].SkipIf, ctx.LastStepOutcome, ctx, steps[i].ID)
			if skipErr != nil {
				ctx.NestingPath = originalNestingPath
				emitStepEnd(ctx, prefix, startTime, string(OutcomeFailed), map[string]any{"error": skipErr.Error()}, step)
				return OutcomeFailed, fmt.Errorf("step %q skip_if evaluation failed: %w", steps[i].ID, skipErr)
			}
			if skip {
				ctx.ResumeChildState = nil
				emitSkippedChildStep(ctx, &steps[i])
				recordLastStepOutcome(ctx, OutcomeSkipped)
				continue
			}
		}
		ctx.ResumeChildState = resumeChild
		resumeChild = nil
		outcome, err := DispatchStep(&steps[i], ctx, runner, glob, log)
		ctx.ResumeChildState = nil
		if err != nil {
			ctx.NestingPath = originalNestingPath
			emitStepEnd(ctx, prefix, startTime, string(OutcomeFailed), map[string]any{"error": err.Error()}, step)
			return OutcomeFailed, err
		}
		if outcome == OutcomeAborted {
			ctx.NestingPath = originalNestingPath
			emitStepEnd(ctx, prefix, startTime, string(OutcomeAborted), nil, step)
			return OutcomeAborted, nil
		}
		recordLastStepOutcome(ctx, outcome)
		if outcome == OutcomeFailed && !steps[i].ContinueOnFailure {
			ctx.NestingPath = originalNestingPath
			emitStepEnd(ctx, prefix, startTime, string(OutcomeFailed), nil, step)
			return OutcomeFailed, nil
		}
	}
	ctx.NestingPath = originalNestingPath
	emitStepEnd(ctx, prefix, startTime, string(OutcomeSuccess), nil, step)
	return OutcomeSuccess, nil
}

// consumeGroupResume resolves the group member that owns persisted progress
// and removes that member's wrapper before dispatching it. Unlike a
// sub-workflow, a group does not create an execution context of its own, so it
// must perform this resume layer explicitly.
func consumeGroupResume(
	ctx *model.ExecutionContext,
	steps []model.Step,
) (stepID string, child *model.NestedStepState, allDone bool, err error) {
	resume := ctx.ResumeChildState
	if resume == nil {
		return "", nil, false, nil
	}
	ctx.ResumeChildState = nil
	resolved, err := model.ResolveResumeStep(steps, resume.StepID, resume.Completed)
	if err != nil {
		return "", nil, false, fmt.Errorf("resume step %q not found in group", resume.StepID)
	}
	if resolved.AllDone {
		return "", nil, true, nil
	}
	if resolved.StepID != resume.StepID {
		return resolved.StepID, nil, false, nil
	}
	if resume.Iteration != nil {
		return resolved.StepID, resume, false, nil
	}
	return resolved.StepID, resume.Child, false, nil
}
