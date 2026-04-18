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

	resumeIter, resumeBody, _ := consumeLoopResume(ctx, stepID)
	if resumeIter > opts.ResumeFromIteration {
		opts.ResumeFromIteration = resumeIter
	}

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
		recordLoopIterationProgress(ctx, stepID, i, false)
		iterCtx := model.NewLoopIterationContext(ctx, model.LoopIterationOptions{
			StepID:    stepID,
			Iteration: i,
		})
		if i == resumeIter && resumeBody != nil {
			iterCtx.ResumeChildState = resumeBody
			resumeBody = nil
		}

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
		recordLoopIterationProgress(ctx, stepID, i+1, false)
		flushLoopState(ctx)
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

	resumeIter, resumeBody, _ := consumeLoopResume(ctx, stepID)
	if resumeIter > opts.ResumeFromIteration {
		opts.ResumeFromIteration = resumeIter
	}

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
		recordLoopIterationProgress(ctx, stepID, i, false)
		iterCtx := model.NewLoopIterationContext(ctx, model.LoopIterationOptions{
			StepID:    stepID,
			Iteration: i,
			LoopVar:   loopVar,
		})
		if i == resumeIter && resumeBody != nil {
			iterCtx.ResumeChildState = resumeBody
			resumeBody = nil
		}

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
		recordLoopIterationProgress(ctx, stepID, i+1, false)
		flushLoopState(ctx)
		if result.breakTriggered {
			emitLoopEnd(ctx, prefix, startTime, completed, true, "success")
			return LoopResult{Outcome: OutcomeSuccess, LastIteration: i}, nil
		}
	}

	emitLoopEnd(ctx, prefix, startTime, completed, false, "success")
	return LoopResult{Outcome: OutcomeSuccess, LastIteration: lastIter}, nil
}

// recordLoopIterationProgress stores the loop step's current iteration index
// on ctx.LastSubWorkflowChild using the loop step's own ID. The merge branch
// in recordChildProgress (triggered when the stored StepID matches childStepID)
// and the top-level branch in writeStepState both recognise this and promote
// Iteration onto the correct entry in the state chain.
func recordLoopIterationProgress(ctx *model.ExecutionContext, loopStepID string, iteration int, loopCompleted bool) {
	entry := newLoopStepMarker(ctx, loopStepID, iteration, nil)
	entry.Completed = loopCompleted
	ctx.LastSubWorkflowChild = entry
}

// newLoopStepMarker builds a NestedStepState whose StepID is the loop step
// and whose Iteration points to iteration. Session-scope fields are copied
// from src (the loop-driving context). Child is attached as provided.
func newLoopStepMarker(src *model.ExecutionContext, loopStepID string, iteration int, child *model.NestedStepState) *model.NestedStepState {
	iter := iteration
	return &model.NestedStepState{
		StepID:            loopStepID,
		SessionIDs:        copyMap(src.SessionIDs),
		SessionProfiles:   copyMap(src.SessionProfiles),
		CapturedVariables: copyMap(src.CapturedVariables),
		LastSessionStepID: src.LastSessionStepID,
		Iteration:         &iter,
		Child:             child,
	}
}

// newIterationBodyEntry builds a NestedStepState for the body step currently
// executing inside a loop iteration. If deeperChild describes the same body
// step (a sub-workflow or inner loop dispatched from this body step wrote
// into iterCtx.LastSubWorkflowChild) its Iteration/Child are promoted;
// otherwise deeperChild is attached as Child.
func newIterationBodyEntry(iterCtx *model.ExecutionContext, bodyStepID string, bodyCompleted bool, deeperChild *model.NestedStepState) *model.NestedStepState {
	if bodyStepID == "" {
		return deeperChild
	}
	entry := &model.NestedStepState{
		StepID:            bodyStepID,
		SessionIDs:        copyMap(iterCtx.SessionIDs),
		SessionProfiles:   copyMap(iterCtx.SessionProfiles),
		CapturedVariables: copyMap(iterCtx.CapturedVariables),
		LastSessionStepID: iterCtx.LastSessionStepID,
		Completed:         bodyCompleted,
	}
	if deeperChild != nil && deeperChild.StepID == bodyStepID {
		entry.Iteration = deeperChild.Iteration
		entry.Child = deeperChild.Child
	} else {
		entry.Child = deeperChild
	}
	return entry
}

// flushLoopState propagates ctx.LastSubWorkflowChild up through the context
// chain via recordChildProgress at each level, then triggers a state.json write
// through the inherited FlushState callback. Walking up is necessary because
// mid-loop flushes happen inside a DispatchStep call, before the enclosing
// sub-workflow's post-step recordChildProgress refreshes the chain.
func flushLoopState(ctx *model.ExecutionContext) {
	cur := ctx
	for cur.ParentContext != nil {
		if len(cur.NestingPath) == 0 {
			break
		}
		seg := cur.NestingPath[len(cur.NestingPath)-1]
		if seg.StepID == "" {
			break
		}
		recordChildProgress(cur, seg.StepID, false)
		cur = cur.ParentContext
	}
	if ctx.FlushState != nil {
		ctx.FlushState()
	}
}

// consumeLoopResume checks if the context carries resume state for this loop
// step and, if so, extracts the iteration index plus any deeper body-step
// resume chain, then clears the pointer.
func consumeLoopResume(ctx *model.ExecutionContext, loopStepID string) (int, *model.NestedStepState, bool) {
	if ctx.ResumeChildState == nil || ctx.ResumeChildState.StepID != loopStepID || ctx.ResumeChildState.Iteration == nil {
		return 0, nil, false
	}
	iter := *ctx.ResumeChildState.Iteration
	body := ctx.ResumeChildState.Child
	ctx.ResumeChildState = nil
	return iter, body, true
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
	loopStepID, iteration := loopSegmentOf(iterCtx)

	resumeBody, resolvedStartID, err := resolveIterationResume(iterCtx, steps)
	if err != nil {
		return iterationResult{failed: true}, err
	}
	if resumeBody != nil && resolvedStartID == "" {
		// All body steps completed on the previous run; nothing to do.
		return iterationResult{}, nil
	}
	reached := resolvedStartID == ""

	setBody, restoreFlush := installIterationFlush(iterCtx, loopStepID, iteration)
	defer restoreFlush()

	for i := range steps {
		if !reached {
			if steps[i].ID != resolvedStartID {
				continue
			}
			reached = true
			if resumeBody != nil {
				applyIterationBodyResume(iterCtx, resumeBody)
			}
		}

		bodyStepID := steps[i].ID
		setBody(bodyStepID, false)

		outcome, dispatchErr := DispatchStep(&steps[i], iterCtx, runner, glob, log)
		if dispatchErr != nil {
			persistIterationFailState(iterCtx, loopStepID, iteration, bodyStepID, false)
			return iterationResult{failed: true}, dispatchErr
		}

		bodyCompleted := outcome != OutcomeFailed && outcome != OutcomeAborted
		setBody(bodyStepID, bodyCompleted)
		if bodyCompleted && iterCtx.FlushState != nil {
			iterCtx.FlushState()
		}

		if outcome == OutcomeAborted {
			persistIterationFailState(iterCtx, loopStepID, iteration, bodyStepID, bodyCompleted)
			return iterationResult{aborted: true}, nil
		}

		if flowctl.EvaluateBreakIf(steps[i].BreakIf, string(outcome)) {
			return iterationResult{breakTriggered: true}, nil
		}

		o := string(outcome)
		iterCtx.LastStepOutcome = &o

		if outcome == OutcomeFailed && !steps[i].ContinueOnFailure {
			persistIterationFailState(iterCtx, loopStepID, iteration, bodyStepID, bodyCompleted)
			return iterationResult{failed: true}, nil
		}
	}
	return iterationResult{}, nil
}

// resolveIterationResume extracts any resume state from iterCtx and resolves
// which body step to re-enter at. Returns a non-nil resumeBody when resume
// state was present (even if all body steps were completed, in which case
// resolvedStartID is ""). Returns an error if the persisted body step is
// no longer present in the loop definition.
func resolveIterationResume(iterCtx *model.ExecutionContext, steps []model.Step) (*model.NestedStepState, string, error) {
	if iterCtx.ResumeChildState == nil {
		return nil, "", nil
	}
	resumeBody := iterCtx.ResumeChildState
	iterCtx.ResumeChildState = nil

	if resumeBody.StepID == "" {
		return resumeBody, "", nil
	}
	resolved, err := model.ResolveResumeStep(steps, resumeBody.StepID, resumeBody.Completed)
	if err != nil {
		return resumeBody, "", fmt.Errorf("resume body step %q not found in loop: %w", resumeBody.StepID, err)
	}
	if resolved.AllDone {
		return resumeBody, "", nil
	}
	return resumeBody, resolved.StepID, nil
}

// installIterationFlush overrides iterCtx.FlushState so mid-body-step flushes
// (triggered from within nested sub-workflows / loops) capture
// iterCtx.LastSubWorkflowChild under an enclosing loop-step marker before the
// root flush runs. The override is non-destructive: it builds a fresh chain
// from a read-only snapshot of the context tree, temporarily installs it on
// the outermost ancestor for the flush, then restores prior state.
//
// Returns (setBody, restoreFlush). setBody(stepID, completed) advances the
// body-step position the next flush will capture. restoreFlush should be
// deferred to reinstate the prior FlushState.
//
// Without this override, the root state.json write would see no iteration
// context and collapse the child chain. A destructive walk-up would break
// the subsequent recordChildProgress calls in executeChildSteps.
func installIterationFlush(
	iterCtx *model.ExecutionContext,
	loopStepID string,
	iteration *int,
) (setBody func(stepID string, completed bool), restoreFlush func()) {
	outerFlush := iterCtx.FlushState
	var bodyStepID string
	var bodyCompleted bool
	setBody = func(stepID string, completed bool) {
		bodyStepID = stepID
		bodyCompleted = completed
	}
	iterCtx.FlushState = func() {
		root, chain := buildIterationFlushChain(iterCtx, loopStepID, iteration, bodyStepID, bodyCompleted)
		if root == nil || chain == nil {
			// Defensive: reached only when an ancestor context has empty
			// NestingPath or empty StepID, which should be unreachable under
			// normal workflow execution. Emit an error event so a resume
			// landing at the wrong position is debuggable rather than silent.
			emitAudit(iterCtx, audit.Event{
				Timestamp: time.Now().UTC().Format(time.RFC3339),
				Type:      audit.EventError,
				Data: map[string]any{
					"error":      "iteration flush chain construction failed",
					"loopStepId": loopStepID,
					"bodyStepId": bodyStepID,
				},
			})
			if outerFlush != nil {
				outerFlush()
			}
			return
		}
		saved := root.LastSubWorkflowChild
		root.LastSubWorkflowChild = chain
		if outerFlush != nil {
			outerFlush()
		}
		root.LastSubWorkflowChild = saved
	}
	restoreFlush = func() { iterCtx.FlushState = outerFlush }
	return setBody, restoreFlush
}

// persistIterationFailState permanently updates iterCtx.ParentContext.LastSubWorkflowChild
// to reflect the current body-step position at the moment the iteration is
// exiting abnormally (failure or abort). Without this, the runner's post-step
// writeStepState would see only the iteration-boundary marker (set by
// recordLoopIterationProgress) and lose the body-step information needed for
// mid-iteration resume. Relies on the existing recordChildProgress merge
// branch to propagate this through any enclosing sub-workflow levels.
func persistIterationFailState(iterCtx *model.ExecutionContext, loopStepID string, iteration *int, bodyStepID string, bodyCompleted bool) {
	parent := iterCtx.ParentContext
	if parent == nil || loopStepID == "" || iteration == nil {
		return
	}

	deep := iterCtx.LastSubWorkflowChild
	iterCtx.LastSubWorkflowChild = nil
	bodyEntry := newIterationBodyEntry(iterCtx, bodyStepID, bodyCompleted, deep)
	parent.LastSubWorkflowChild = newLoopStepMarker(parent, loopStepID, *iteration, bodyEntry)
}

// loopSegmentOf returns the loop step ID and iteration index from the
// innermost iteration segment of iterCtx. Returns zero values if the
// innermost segment is not a loop iteration.
func loopSegmentOf(iterCtx *model.ExecutionContext) (loopStepID string, iteration *int) {
	if len(iterCtx.NestingPath) == 0 {
		return "", nil
	}
	seg := iterCtx.NestingPath[len(iterCtx.NestingPath)-1]
	if seg.Iteration == nil {
		return "", nil
	}
	iter := *seg.Iteration
	return seg.StepID, &iter
}

// buildIterationFlushChain constructs, non-destructively, the full nested
// state chain for a mid-iteration flush. It snapshots iterCtx and its
// ancestors' NestingPath segments to produce a fresh *NestedStepState tree
// rooted at the outermost ancestor (runner top-level context). The returned
// root context's LastSubWorkflowChild should be temporarily replaced with
// the returned chain for the duration of the outer flush.
//
// Returns (nil, nil) if the chain cannot be walked all the way up to a root
// (no ParentContext) — e.g. because a context along the way has an empty
// NestingPath. Installing a partially-built chain at a non-root level
// would leave intermediate state inconsistent.
func buildIterationFlushChain(
	iterCtx *model.ExecutionContext,
	loopStepID string,
	iteration *int,
	bodyStepID string,
	bodyCompleted bool,
) (root *model.ExecutionContext, chain *model.NestedStepState) {
	bodyEntry := newIterationBodyEntry(iterCtx, bodyStepID, bodyCompleted, iterCtx.LastSubWorkflowChild)

	if loopStepID != "" && iteration != nil && iterCtx.ParentContext != nil {
		chain = newLoopStepMarker(iterCtx.ParentContext, loopStepID, *iteration, bodyEntry)
	} else {
		chain = bodyEntry
	}

	cur := iterCtx.ParentContext
	for cur != nil && cur.ParentContext != nil {
		if len(cur.NestingPath) == 0 {
			return nil, nil
		}
		seg := cur.NestingPath[len(cur.NestingPath)-1]
		if seg.StepID == "" {
			return nil, nil
		}
		parent := cur.ParentContext
		entry := &model.NestedStepState{
			StepID:            seg.StepID,
			SessionIDs:        copyMap(parent.SessionIDs),
			SessionProfiles:   copyMap(parent.SessionProfiles),
			CapturedVariables: copyMap(parent.CapturedVariables),
			LastSessionStepID: parent.LastSessionStepID,
			Child:             chain,
		}
		if seg.Iteration != nil {
			iter := *seg.Iteration
			entry.Iteration = &iter
		}
		chain = entry
		cur = parent
	}

	return cur, chain
}

// applyIterationBodyResume restores persisted iteration-scoped state
// (sessions, captured variables, deeper resume pointer) into iterCtx so
// the body step re-enters with the same context it had at flush time.
func applyIterationBodyResume(iterCtx *model.ExecutionContext, resumeBody *model.NestedStepState) {
	restorePersistedSessions(iterCtx, resumeBody)
	if resumeBody.Child != nil {
		iterCtx.ResumeChildState = resumeBody.Child
	}
}
