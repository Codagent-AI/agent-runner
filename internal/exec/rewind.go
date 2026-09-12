package exec

import (
	"time"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/model"
)

// takeRewind pops and clears ctx.PendingRewind, reporting nil when none is set.
func takeRewind(ctx *model.ExecutionContext) *model.RewindRequest {
	rw := ctx.PendingRewind
	ctx.PendingRewind = nil
	return rw
}

// indexOfStep returns the index of the step with the given ID in steps, or
// -1 if not found.
func indexOfStep(steps []model.Step, id string) int {
	for i := range steps {
		if steps[i].ID == id {
			return i
		}
	}
	return -1
}

// extendedRepairNesting returns basePath with one more segment naming the
// owning check and the attempt about to run, so a rerun-form replay's steps
// (the target, any intermediates, and the check itself) audit under
// [..., checkID, attempt:N, <step>].
func extendedRepairNesting(basePath []model.NestingSegment, checkID string, attempt int) []model.NestingSegment {
	extended := make([]model.NestingSegment, len(basePath)+1)
	copy(extended, basePath)
	extended[len(basePath)] = model.NestingSegment{StepID: checkID, RepairAttempt: &attempt}
	return extended
}

// PrimeReplayResume extends ctx.NestingPath for a repair frame that was
// restored on resume in phase replaying, so the sequencer's own basePath
// (captured just before this call by the caller) sees the same "currently
// mid-replay" nesting a live run would have produced via AfterStepDispatch's
// takeRewind handling. It is a no-op when there is no open frame, the frame
// is not replaying, or the path is already extended (a live run reaching
// this scope again with a still-open frame from an earlier call). The
// resumed attempt is frame.Attempts+1, matching a fresh rewind.
//
// The rerun target executed next may need the repair evidence preface (see
// agent.go's ctx.LastFailure check), and ctx.LastFailure is never persisted,
// so it is rebuilt from audit here, at the nesting depth (including any
// enclosing group/loop/sub-workflow) this scope actually owns the check at.
// Callers must propagate a non-nil error instead of dispatching the replay
// target: without the rebuilt evidence, a resumed agent would silently lose
// the repair-evidence preface and could repeat the original failing action.
func PrimeReplayResume(ctx *model.ExecutionContext, basePath []model.NestingSegment) error {
	frame := ctx.RepairFrame
	if frame == nil || frame.Phase != model.RepairPhaseReplaying {
		return nil
	}
	if len(ctx.NestingPath) > len(basePath) {
		return nil
	}
	attempt := frame.Attempts + 1
	ctx.NestingPath = extendedRepairNesting(basePath, frame.CheckID, attempt)
	return RestoreLastFailureForResume(ctx)
}

// inReplay reports whether ctx is currently within a rerun-form replay
// range: the sequencer has pushed an attempt-nesting segment beyond
// basePath and the frame is still mid-replay.
func inReplay(ctx *model.ExecutionContext, basePath []model.NestingSegment) bool {
	return len(ctx.NestingPath) > len(basePath) &&
		ctx.RepairFrame != nil && ctx.RepairFrame.Phase == model.RepairPhaseReplaying
}

// isBlockingOutcome reports whether outcome would ordinarily stop the
// enclosing scope for step: an abort always blocks, and a failure blocks
// unless the step's own continue_on_failure or warn_on_failure lets it
// through. Only a blocking outcome inside a replay range counts as a failed
// repair attempt; a replayed step whose own flow control tolerates its
// failure proceeds through ordinary sequencing instead.
func isBlockingOutcome(step *model.Step, outcome StepOutcome) bool {
	if outcome == OutcomeAborted {
		return true
	}
	return outcome == OutcomeFailed && !step.ContinueOnFailure && !IsWarningOutcome(step, outcome)
}

// terminalReplayBlocked marks the frame terminally blocked when the rerun
// target's own response declares REPAIR_BLOCKED. It is tested immediately
// after the target step completes and before any later replay step runs, so
// a marker from an unrelated agent later in the replay range is never
// consulted.
func terminalReplayBlocked(ctx *model.ExecutionContext, response string) {
	frame := ctx.RepairFrame
	owningPrefix := audit.BuildPrefix(nestingSegmentsToAuditInfo(repairOwningNestingPath(ctx, frame.CheckID)), frame.CheckID)
	emitAudit(ctx, audit.Event{
		Timestamp: formatAuditTimestamp(time.Now()), Prefix: owningPrefix, Type: audit.EventRepairBlocked,
		Data: map[string]any{"attempt": frame.Attempts, "response": truncateForAudit(response)},
	})
	frame.Phase = model.RepairPhaseFailed
	record := &model.FailureRecord{
		StepID: frame.CheckID, Prefix: owningPrefix, Blocked: true, BlockedBy: response, RepairAttempts: frame.Attempts,
	}
	if frame.Guarded != nil {
		ref := *frame.Guarded
		record.Guarded = &model.AgentExecutionRecord{Ref: ref}
	}
	ctx.LastFailure = record
	if ctx.FlushState != nil {
		ctx.FlushState()
	}
}

// RewindOutcome tells a sequencer what to do after checking for a pending
// rewind following one DispatchStep call.
type RewindOutcome struct {
	// Rewound is true when the sequencer should set its loop index to
	// NextIndex and `continue`, without applying its ordinary per-step
	// handling (skip/break/continue_on_failure) for the just-dispatched step.
	Rewound   bool
	NextIndex int
	// Stopped is true when a replay-range failure exhausted the check's
	// repair budget: the sequencer must stop its scope now, reporting the
	// owning check as a blocking failure (ctx.LastFailure is already set).
	Stopped bool
}

// AfterStepDispatch centralizes the rewind/replay bookkeeping shared by all
// four sequencers (top-level runner, loop body, sub-workflow, group). Call it
// immediately after DispatchStep returns for steps[i], before applying the
// sequencer's ordinary outcome handling:
//
//   - If ctx is currently replaying a rerun-form repair attempt (the
//     sequencer previously extended ctx.NestingPath past basePath for this
//     check) and outcome is a blocking failure or abort, it is absorbed as a
//     failed repair attempt via AbsorbReplayFailure instead of the
//     sequencer's ordinary handling for steps[i]. absorbed reports this: the
//     caller must skip its own failure/abort handling for steps[i] whenever
//     absorbed is true, whether or not the result says to stop or rewind.
//   - Otherwise, if ctx.PendingRewind is now set (by the check itself, or
//     just now by AbsorbReplayFailure), ctx.NestingPath is extended for the
//     replay and Rewound is true with NextIndex naming the rerun target.
//   - Otherwise, if ctx.NestingPath was extended for a replay that has now
//     concluded (the check's own re-entry cleared or terminally failed the
//     frame), it is restored to basePath.
func AfterStepDispatch(ctx *model.ExecutionContext, steps []model.Step, i int, basePath []model.NestingSegment, outcome StepOutcome) (result RewindOutcome, absorbed bool) {
	if inReplay(ctx, basePath) && steps[i].ID == ctx.RepairFrame.Target && outcome == OutcomeSuccess {
		if guarded := ctx.LastAgentExecution; guarded != nil && executionRefMatchesStep(guarded.Ref, ctx.RepairFrame.Target) && repairBlocked(guarded.Response) {
			ctx.NestingPath = basePath
			terminalReplayBlocked(ctx, guarded.Response)
			return RewindOutcome{Stopped: true}, true
		}
	}

	if inReplay(ctx, basePath) && isBlockingOutcome(&steps[i], outcome) {
		absorbed = true
		if stop := AbsorbReplayFailure(ctx, steps[i].ID, outcome); stop {
			ctx.NestingPath = basePath
			return RewindOutcome{Stopped: true}, true
		}
	}

	if rw := takeRewind(ctx); rw != nil {
		targetIndex := indexOfStep(steps, rw.Target)
		attempt := 1
		if ctx.RepairFrame != nil {
			attempt = ctx.RepairFrame.Attempts + 1
		}
		ctx.NestingPath = extendedRepairNesting(basePath, rw.CheckID, attempt)
		return RewindOutcome{Rewound: true, NextIndex: targetIndex}, absorbed
	}

	if len(ctx.NestingPath) > len(basePath) && (ctx.RepairFrame == nil || ctx.RepairFrame.Phase != model.RepairPhaseReplaying) {
		ctx.NestingPath = basePath
	}
	return RewindOutcome{}, absorbed
}

// AbsorbReplayFailure records a blocking failure or abort of a step inside a
// rerun-form replay range as one failed repair attempt owned by the check,
// instead of letting the sequencer terminate the scope directly. It emits
// repair_attempt_end for the failed step, increments the frame's completed
// attempt count, and either re-arms ctx.PendingRewind for another attempt
// (returns false) or marks the frame terminally failed and commits
// ctx.LastFailure (returns true — the sequencer must stop the scope with a
// failed outcome for the owning check).
func AbsorbReplayFailure(ctx *model.ExecutionContext, failingStepID string, outcome StepOutcome) (stop bool) {
	frame := ctx.RepairFrame
	if frame == nil {
		return true
	}
	owningPrefix := audit.BuildPrefix(nestingSegmentsToAuditInfo(repairOwningNestingPath(ctx, frame.CheckID)), frame.CheckID)

	frame.Attempts++
	emitAudit(ctx, audit.Event{
		Timestamp: formatAuditTimestamp(time.Now()), Prefix: owningPrefix, Type: audit.EventRepairAttemptEnd,
		Data: map[string]any{
			"attempt": frame.Attempts, "form": frame.Form, "outcome": string(outcome),
			"failed_step": failingStepID,
		},
	})

	if frame.Attempts >= frame.Budget {
		frame.Phase = model.RepairPhaseFailed
		record := &model.FailureRecord{StepID: frame.CheckID, Prefix: owningPrefix, RepairAttempts: frame.Attempts}
		if frame.Guarded != nil {
			ref := *frame.Guarded
			record.Guarded = &model.AgentExecutionRecord{Ref: ref}
		}
		ctx.LastFailure = record
		if ctx.FlushState != nil {
			ctx.FlushState()
		}
		return true
	}

	attempt := frame.Attempts + 1
	emitAudit(ctx, audit.Event{
		Timestamp: formatAuditTimestamp(time.Now()), Prefix: owningPrefix, Type: audit.EventRepairAttemptStart,
		Data: map[string]any{"attempt": attempt, "form": frame.Form, "target": frame.Target},
	})
	frame.Phase = model.RepairPhaseReplaying
	clearRangeCaptures(ctx, frame.RangeCaptures)
	ctx.PendingRewind = &model.RewindRequest{Target: frame.Target, CheckID: frame.CheckID}
	if ctx.FlushState != nil {
		ctx.FlushState()
	}
	return false
}
