package exec

import (
	"fmt"
	"time"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/flowctl"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/textfmt"
)

// LoopExecuteOptions controls loop execution behavior.
type LoopExecuteOptions struct {
	ResumeFromIteration int
}

// ExecuteLoopStep executes a step with a loop configuration.
func ExecuteLoopStep(
	step *model.Step,
	ctx *model.ExecutionContext,
	runner ProcessRunner,
	glob GlobExpander,
	log Logger,
	opts LoopExecuteOptions,
) (LoopResult, error) {
	if step.Loop == nil || len(step.Steps) == 0 {
		return LoopResult{Outcome: OutcomeFailed, LastIteration: -1}, nil
	}

	loop := step.Loop

	if loop.Over != "" && loop.As != "" {
		return executeForEachLoop(step.ID, loop.Over, loop.As, step.Steps, ctx, runner, glob, log, opts, loop.RequireMatches)
	}

	if loop.Max != nil {
		return executeCountedLoop(step.ID, *loop.Max, step.Steps, ctx, runner, glob, log, opts)
	}

	return LoopResult{Outcome: OutcomeFailed, LastIteration: -1}, nil
}

func executeCountedLoop(
	stepID string,
	maxIter int,
	steps []model.Step,
	ctx *model.ExecutionContext,
	runner ProcessRunner,
	glob GlobExpander,
	log Logger,
	opts LoopExecuteOptions,
) (LoopResult, error) {
	prefix := audit.BuildPrefix(nestingToAudit(ctx), stepID)
	startTime := time.Now()

	emitAudit(ctx, audit.Event{
		Timestamp: startTime.UTC().Format(time.RFC3339),
		Prefix:    prefix,
		Type:      audit.EventStepStart,
		Data: map[string]any{
			"loop_type": "counted",
			"max":       maxIter,
			"context":   contextSnapshot(ctx),
		},
	})

	startIter := opts.ResumeFromIteration
	lastIter := startIter
	completed := 0

	for i := startIter; i < maxIter; i++ {
		lastIter = i
		iterCtx := model.NewLoopIterationContext(ctx, model.LoopIterationOptions{
			StepID:    stepID,
			Iteration: i,
		})

		result, err := executeIterationWithAudit(steps, iterCtx, runner, glob, log)
		if err != nil {
			emitLoopEnd(ctx, prefix, startTime, completed, false, "failed")
			return LoopResult{Outcome: OutcomeFailed, LastIteration: i}, err
		}
		completed++

		if result.aborted {
			emitLoopEnd(ctx, prefix, startTime, completed, false, "aborted")
			return LoopResult{Outcome: OutcomeAborted, LastIteration: i}, nil
		}
		if result.failed {
			emitLoopEnd(ctx, prefix, startTime, completed, false, "failed")
			return LoopResult{Outcome: OutcomeFailed, LastIteration: i}, nil
		}
		if result.breakTriggered {
			emitLoopEnd(ctx, prefix, startTime, completed, true, "success")
			return LoopResult{Outcome: OutcomeSuccess, LastIteration: i}, nil
		}
	}

	emitLoopEnd(ctx, prefix, startTime, completed, false, "exhausted")
	return LoopResult{Outcome: OutcomeExhausted, LastIteration: lastIter}, nil
}

func executeForEachLoop(
	stepID, overPattern, asVar string,
	steps []model.Step,
	ctx *model.ExecutionContext,
	runner ProcessRunner,
	globExp GlobExpander,
	log Logger,
	opts LoopExecuteOptions,
	requireMatches *bool,
) (LoopResult, error) {
	pattern, err := textfmt.Interpolate(overPattern, ctx.Params, ctx.CapturedVariables)
	if err != nil {
		return LoopResult{Outcome: OutcomeFailed, LastIteration: -1}, err
	}

	matches, err := globExp.Expand(pattern)
	if err != nil {
		return LoopResult{Outcome: OutcomeFailed, LastIteration: -1}, err
	}

	prefix := audit.BuildPrefix(nestingToAudit(ctx), stepID)
	startTime := time.Now()

	emitAudit(ctx, audit.Event{
		Timestamp: startTime.UTC().Format(time.RFC3339),
		Prefix:    prefix,
		Type:      audit.EventStepStart,
		Data: map[string]any{
			"loop_type":        "for-each",
			"glob_pattern":     pattern,
			"resolved_matches": matches,
			"context":          contextSnapshot(ctx),
		},
	})

	if len(matches) == 0 {
		if requireMatches != nil && *requireMatches {
			log.Errorf("agent-runner: for-each loop %q matched 0 files for pattern: %s\n", stepID, pattern)
			emitLoopEnd(ctx, prefix, startTime, 0, false, "failed")
			return LoopResult{Outcome: OutcomeFailed, LastIteration: -1}, nil
		}
		emitLoopEnd(ctx, prefix, startTime, 0, false, "success")
		return LoopResult{Outcome: OutcomeSuccess, LastIteration: -1}, nil
	}

	startIter := opts.ResumeFromIteration
	lastIter := startIter
	completed := 0

	for i := startIter; i < len(matches); i++ {
		lastIter = i
		loopVar := map[string]string{asVar: matches[i]}
		iterCtx := model.NewLoopIterationContext(ctx, model.LoopIterationOptions{
			StepID:    stepID,
			Iteration: i,
			LoopVar:   loopVar,
		})

		result, err := executeIterationWithAudit(steps, iterCtx, runner, globExp, log)
		if err != nil {
			emitLoopEnd(ctx, prefix, startTime, completed, false, "failed")
			return LoopResult{Outcome: OutcomeFailed, LastIteration: i}, err
		}
		completed++

		if result.aborted {
			emitLoopEnd(ctx, prefix, startTime, completed, false, "aborted")
			return LoopResult{Outcome: OutcomeAborted, LastIteration: i}, nil
		}
		if result.failed {
			emitLoopEnd(ctx, prefix, startTime, completed, false, "failed")
			return LoopResult{Outcome: OutcomeFailed, LastIteration: i}, nil
		}
		if result.breakTriggered {
			emitLoopEnd(ctx, prefix, startTime, completed, true, "success")
			return LoopResult{Outcome: OutcomeSuccess, LastIteration: i}, nil
		}
	}

	emitLoopEnd(ctx, prefix, startTime, completed, false, "success")
	return LoopResult{Outcome: OutcomeSuccess, LastIteration: lastIter}, nil
}

func emitLoopEnd(ctx *model.ExecutionContext, prefix string, startTime time.Time, completed int, breakTriggered bool, outcome string) {
	emitAudit(ctx, audit.Event{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Prefix:    prefix,
		Type:      audit.EventStepEnd,
		Data: map[string]any{
			"iterations_completed": completed,
			"break_triggered":      breakTriggered,
			"outcome":              outcome,
			"duration_ms":          time.Since(startTime).Milliseconds(),
		},
	})
}

type iterationResult struct {
	breakTriggered bool
	failed         bool
	aborted        bool
}

func executeIterationWithAudit(
	steps []model.Step,
	iterCtx *model.ExecutionContext,
	runner ProcessRunner,
	glob GlobExpander,
	log Logger,
) (iterationResult, error) {
	nestingPath := iterCtx.NestingPath
	lastSeg := nestingPath[len(nestingPath)-1]
	if lastSeg.Iteration == nil {
		return executeIterationBody(steps, iterCtx, runner, glob, log)
	}

	parentPath := nestingToAudit(iterCtx)
	prefix := audit.BuildPrefix(
		parentPath[:len(parentPath)-1],
		fmt.Sprintf("%s:%d", lastSeg.StepID, *lastSeg.Iteration),
	)

	iterStart := time.Now()
	iteration := *lastSeg.Iteration

	startData := map[string]any{
		"iteration": iteration,
		"context":   contextSnapshot(iterCtx),
	}
	if lastSeg.LoopVar != nil {
		loopVarCopy := make(map[string]any)
		for k, v := range lastSeg.LoopVar {
			loopVarCopy[k] = v
		}
		startData["loop_var"] = loopVarCopy
	}

	emitAudit(iterCtx, audit.Event{
		Timestamp: iterStart.UTC().Format(time.RFC3339),
		Prefix:    prefix,
		Type:      audit.EventIterationStart,
		Data:      startData,
	})

	result, err := executeIterationBody(steps, iterCtx, runner, glob, log)

	outcome := "success"
	if result.aborted {
		outcome = "aborted"
	} else if result.failed {
		outcome = "failed"
	}

	emitAudit(iterCtx, audit.Event{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Prefix:    prefix,
		Type:      audit.EventIterationEnd,
		Data: map[string]any{
			"iteration":   iteration,
			"outcome":     outcome,
			"duration_ms": time.Since(iterStart).Milliseconds(),
		},
	})

	return result, err
}

func executeIterationBody(
	steps []model.Step,
	iterCtx *model.ExecutionContext,
	runner ProcessRunner,
	glob GlobExpander,
	log Logger,
) (iterationResult, error) {
	for i := range steps {
		breadcrumb := textfmt.BuildBreadcrumb(nestingToFmt(iterCtx), steps[i].ID)
		log.Println(textfmt.Separator())
		log.Println(textfmt.StepHeading(i, len(steps), breadcrumb, steps[i].StepType(), false))
		outcome, err := DispatchStep(&steps[i], iterCtx, runner, glob, log)
		if err != nil {
			return iterationResult{failed: true}, err
		}

		if outcome == OutcomeAborted {
			return iterationResult{aborted: true}, nil
		}

		if flowctl.EvaluateBreakIf(steps[i].BreakIf, string(outcome)) {
			return iterationResult{breakTriggered: true}, nil
		}

		o := string(outcome)
		iterCtx.LastStepOutcome = &o

		if outcome == OutcomeFailed && !steps[i].ContinueOnFailure {
			return iterationResult{failed: true}, nil
		}
	}
	return iterationResult{}, nil
}
