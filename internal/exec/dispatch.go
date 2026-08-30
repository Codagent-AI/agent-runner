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
	if ctx.Workspace == nil || len(ctx.Workspace.Selected) == 0 {
		return OutcomeFailed, fmt.Errorf("repository-scoped step %q has no selected repositories", step.ID)
	}
	if err := acquireSelectedRepositoryLocks(ctx); err != nil {
		return OutcomeFailed, err
	}
	for index, name := range ctx.Workspace.Selected {
		repository, ok := ctx.Workspace.Repositories[name]
		if !ok {
			return OutcomeFailed, fmt.Errorf("selected repository %q is no longer configured", name)
		}
		repositoryCtx := model.NewRepositoryExecutionContext(ctx, repository, index)
		repositoryCtx.RepositoryPrefixDepth = ctx.AuditPrefixTokenCount() + 1
		started := time.Now()
		boundaryPrefix := audit.BuildPrefix(nestingToAudit(ctx), step.ID)
		emitRepositoryBoundaryStart(repositoryCtx, boundaryPrefix, index, len(ctx.Workspace.Selected), started)
		skip, skipErr := ShouldSkipStep(step.SkipIf, repositoryCtx.LastStepOutcome, repositoryCtx, step.ID)
		if skipErr != nil {
			emitRepositoryBoundaryEnd(repositoryCtx, boundaryPrefix, OutcomeFailed, started)
			return OutcomeFailed, fmt.Errorf("step %q skip_if evaluation failed for repository %q: %w", step.ID, name, skipErr)
		}
		if skip {
			emitSkippedChildStep(repositoryCtx, step)
			emitRepositoryBoundaryEnd(repositoryCtx, boundaryPrefix, OutcomeSuccess, started)
			continue
		}
		outcome, err := dispatchStepOnce(step, repositoryCtx, runner, glob, log)
		if err != nil {
			emitRepositoryBoundaryEnd(repositoryCtx, boundaryPrefix, OutcomeFailed, started)
			return OutcomeFailed, err
		}
		emitRepositoryBoundaryEnd(repositoryCtx, boundaryPrefix, outcome, started)
		if outcome == OutcomeFailed || outcome == OutcomeAborted {
			return outcome, nil
		}
	}
	return OutcomeSuccess, nil
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
	for i := range steps {
		if shouldEvaluateSkipBeforeDispatch(&steps[i], ctx) {
			skip, skipErr := ShouldSkipStep(steps[i].SkipIf, ctx.LastStepOutcome, ctx, steps[i].ID)
			if skipErr != nil {
				ctx.NestingPath = originalNestingPath
				emitStepEnd(ctx, prefix, startTime, string(OutcomeFailed), map[string]any{"error": skipErr.Error()}, step)
				return OutcomeFailed, fmt.Errorf("step %q skip_if evaluation failed: %w", steps[i].ID, skipErr)
			}
			if skip {
				emitSkippedChildStep(ctx, &steps[i])
				recordLastStepOutcome(ctx, OutcomeSkipped)
				continue
			}
		}
		outcome, err := DispatchStep(&steps[i], ctx, runner, glob, log)
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
