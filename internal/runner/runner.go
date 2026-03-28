package runner

import (
	"fmt"
	"os"
	"time"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/engine"
	"github.com/codagent/agent-runner/internal/exec"
	"github.com/codagent/agent-runner/internal/flowctl"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/stateio"
	"github.com/codagent/agent-runner/internal/textfmt"
)

// WorkflowResult represents the final result of a workflow run.
type WorkflowResult string

const (
	ResultSuccess WorkflowResult = "success"
	ResultFailed  WorkflowResult = "failed"
	ResultStopped WorkflowResult = "stopped"
)

// Options configures a workflow run.
type Options struct {
	From              string
	WorkflowFile      string
	StateDir          string
	Engine            engine.Engine
	SessionIDs        map[string]string
	CapturedVariables map[string]string
	ChildState        *model.SubWorkflowChildState
	ProcessRunner     exec.ProcessRunner
	GlobExpander      exec.GlobExpander
	Log               exec.Logger
}

func validateParams(workflow model.Workflow, params map[string]string) error {
	for _, param := range workflow.Params {
		if _, ok := params[param.Name]; ok {
			continue
		}
		if param.IsRequired() {
			if param.Default != "" {
				params[param.Name] = param.Default
			} else {
				return fmt.Errorf("Missing required parameter: %s", param.Name)
			}
		}
	}
	return nil
}

func resolveStartIndex(workflow model.Workflow, from string) (int, error) {
	if from == "" {
		return 0, nil
	}
	for i, s := range workflow.Steps {
		if s.ID == from {
			return i, nil
		}
	}
	return 0, fmt.Errorf("step %q not found in workflow", from)
}

func resolveStateDir(eng engine.Engine, params map[string]string, defaultDir string) string {
	if eng != nil {
		dir := eng.GetStateDir(params)
		if dir != "" {
			return dir
		}
	}
	return defaultDir
}

func computeHash(workflowFile string) string {
	if workflowFile == "" {
		return ""
	}
	data, err := os.ReadFile(workflowFile)
	if err != nil {
		return ""
	}
	return stateio.ComputeWorkflowHash(string(data))
}

func nestingToAuditInfo(ctx *model.ExecutionContext) []audit.NestingInfo {
	result := make([]audit.NestingInfo, len(ctx.NestingPath))
	for i, seg := range ctx.NestingPath {
		result[i] = audit.NestingInfo{
			StepID:          seg.StepID,
			Iteration:       seg.Iteration,
			SubWorkflowName: seg.SubWorkflowName,
		}
	}
	return result
}

func nestingToFmt(ctx *model.ExecutionContext) []textfmt.NestingInfo {
	result := make([]textfmt.NestingInfo, len(ctx.NestingPath))
	for i, seg := range ctx.NestingPath {
		result[i] = textfmt.NestingInfo{
			StepID:          seg.StepID,
			Iteration:       seg.Iteration,
			SubWorkflowName: seg.SubWorkflowName,
		}
	}
	return result
}

func emitAudit(ctx *model.ExecutionContext, event audit.Event) {
	if ctx.AuditLogger == nil {
		return
	}
	if al, ok := ctx.AuditLogger.(*audit.Logger); ok {
		al.Emit(event)
	}
}

// RunWorkflow executes a workflow with the given parameters.
func RunWorkflow(
	workflow model.Workflow,
	params map[string]string,
	opts Options,
) (WorkflowResult, error) {
	if err := validateParams(workflow, params); err != nil {
		return ResultFailed, err
	}

	if opts.Engine != nil {
		if err := opts.Engine.ValidateWorkflow(workflow, params, opts.WorkflowFile); err != nil {
			return ResultFailed, err
		}
	}

	startIndex, err := resolveStartIndex(workflow, opts.From)
	if err != nil {
		return ResultFailed, err
	}

	defaultStateDir := opts.StateDir
	if defaultStateDir == "" {
		defaultStateDir, _ = os.Getwd()
	}
	stateDir := resolveStateDir(opts.Engine, params, defaultStateDir)
	workflowHash := computeHash(opts.WorkflowFile)

	auditLogger, err := audit.CreateLogger(workflow.Name, "")
	if err != nil {
		// Non-fatal: continue without audit logging
		auditLogger = nil
	}

	runStartTime := time.Now()

	var engineRef model.Engine
	if opts.Engine != nil {
		engineRef = opts.Engine
	}

	ctx := model.NewRootContext(model.RootContextOptions{
		Params:            params,
		WorkflowFile:      opts.WorkflowFile,
		EngineRef:         engineRef,
		SessionIDs:        opts.SessionIDs,
		CapturedVariables: opts.CapturedVariables,
		AuditLogger:       auditLogger,
	})
	if opts.ChildState != nil {
		ctx.ResumeChildState = opts.ChildState
	}

	if auditLogger != nil {
		auditData := map[string]any{
			"workflow_file": opts.WorkflowFile,
			"workflow_name": workflow.Name,
			"workflow_hash": workflowHash,
			"context": map[string]any{
				"params":            params,
				"capturedVariables": ctx.CapturedVariables,
				"sessionIds":        ctx.SessionIDs,
			},
		}
		if opts.From != "" {
			auditData["resumed"] = true
			auditData["resume_from"] = opts.From
		}
		auditLogger.Emit(audit.Event{
			Timestamp: runStartTime.UTC().Format(time.RFC3339),
			Type:      audit.EventRunStart,
			Data:      auditData,
		})
	}

	log := opts.Log
	if log == nil {
		log = &defaultLogger{}
	}

	log.Printf("\nagent-runner: running workflow %q\n\n", workflow.Name)

	runner := opts.ProcessRunner
	glob := opts.GlobExpander

	var result WorkflowResult = ResultFailed
	ranToCompletion := true
	for i := startIndex; i < len(workflow.Steps); i++ {
		step := workflow.Steps[i]

		if flowctl.ShouldSkip(step.SkipIf, ctx.LastStepOutcome) {
			breadcrumb := textfmt.BuildBreadcrumb(nestingToFmt(ctx), step.ID)
			log.Println(textfmt.Separator())
			log.Println(textfmt.StepHeading(i, len(workflow.Steps), breadcrumb, "", true))

			prefix := audit.BuildPrefix(nestingToAuditInfo(ctx), step.ID)
			emitAudit(ctx, audit.Event{
				Timestamp: time.Now().UTC().Format(time.RFC3339),
				Prefix:    prefix,
				Type:      audit.EventStepStart,
				Data:      map[string]any{"context": contextSnapshot(ctx)},
			})
			emitAudit(ctx, audit.Event{
				Timestamp: time.Now().UTC().Format(time.RFC3339),
				Prefix:    prefix,
				Type:      audit.EventStepEnd,
				Data:      map[string]any{"outcome": "skipped", "skip_if": step.SkipIf, "duration_ms": 0},
			})
			continue
		}

		stepType := step.StepType()
		breadcrumb := textfmt.BuildBreadcrumb(nestingToFmt(ctx), step.ID)
		log.Println(textfmt.Separator())
		log.Println(textfmt.StepHeading(i, len(workflow.Steps), breadcrumb, stepType, false))

		flush := func() {
			writeStepState(step, ctx, workflow, workflowHash, stateDir, nil)
		}
		ctx.FlushState = flush

		var outcome exec.StepOutcome
		var loopResult *exec.LoopResult
		var stepErr error

		if step.Loop != nil && len(step.Steps) > 0 {
			lr, err := exec.ExecuteLoopStep(step, ctx, runner, glob, log, exec.LoopExecuteOptions{})
			stepErr = err
			loopResult = &lr
			outcome = exec.MapLoopOutcomeForRunner(lr.Outcome)
		} else {
			outcome, stepErr = exec.DispatchStep(step, ctx, runner, glob, log)
		}
		ctx.FlushState = nil

		if stepErr != nil {
			result = ResultFailed
			break
		}

		writeStepState(step, ctx, workflow, workflowHash, stateDir, loopResult)

		if outcome == exec.OutcomeAborted {
			log.Println("\nagent-runner: workflow stopped.")
			result = ResultStopped
			ranToCompletion = false
			break
		}

		if outcome == exec.OutcomeFailed {
			o := string(outcome)
			ctx.LastStepOutcome = &o
			if step.ContinueOnFailure {
				log.Printf("--- step %q failed (continue_on_failure) ---\n\n", step.ID)
				continue
			}
			log.Printf("\nagent-runner: step %q failed. Stopping.\n", step.ID)
			result = ResultFailed
			ranToCompletion = false
			break
		}

		o := "success"
		ctx.LastStepOutcome = &o
		log.Printf("--- step %q complete ---\n\n", step.ID)
	}

	if ranToCompletion {
		result = ResultSuccess
	}

	if result == ResultSuccess {
		stateio.DeleteState(stateDir)
		log.Println("agent-runner: workflow complete")
	} else if result == ResultFailed {
		log.Printf("agent-runner: to resume: agent-runner resume %s\n", stateio.GetStateFilePath(stateDir))
	}

	if auditLogger != nil {
		auditLogger.Emit(audit.Event{
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Type:      audit.EventRunEnd,
			Data: map[string]any{
				"outcome":     string(result),
				"duration_ms": time.Since(runStartTime).Milliseconds(),
			},
		})
		auditLogger.Close()
	}

	return result, nil
}

func writeStepState(step model.Step, ctx *model.ExecutionContext, workflow model.Workflow, workflowHash, stateDir string, loopResult *exec.LoopResult) {
	var child *model.NestedStepState

	if loopResult != nil && loopResult.LastIteration >= 0 {
		child = &model.NestedStepState{
			StepID:            fmt.Sprintf("%s:iteration", step.ID),
			SessionIDs:        map[string]string{},
			CapturedVariables: map[string]string{"_iteration": fmt.Sprintf("%d", loopResult.LastIteration)},
			Child:             nil,
		}
	} else if ctx.LastSubWorkflowChild != nil {
		child = toNestedStepState(ctx.LastSubWorkflowChild)
		ctx.LastSubWorkflowChild = nil
	}

	nested := &model.NestedStepState{
		StepID:            step.ID,
		SessionIDs:        copyMap(ctx.SessionIDs),
		CapturedVariables: copyMap(ctx.CapturedVariables),
		Child:             child,
	}

	state := model.RunState{
		WorkflowFile: ctx.WorkflowFile,
		WorkflowName: workflow.Name,
		CurrentStep:  model.CurrentStep{Nested: nested},
		Params:       ctx.Params,
		WorkflowHash: workflowHash,
	}
	stateio.WriteState(state, stateDir)
}

func toNestedStepState(child *model.SubWorkflowChildState) *model.NestedStepState {
	if child == nil {
		return nil
	}
	return &model.NestedStepState{
		StepID:            child.StepID,
		SessionIDs:        copyMap(child.SessionIDs),
		CapturedVariables: copyMap(child.CapturedVariables),
		Child:             toNestedStepState(child.Child),
	}
}

func contextSnapshot(ctx *model.ExecutionContext) map[string]any {
	params := make(map[string]any)
	for k, v := range ctx.Params {
		params[k] = v
	}
	captured := make(map[string]any)
	for k, v := range ctx.CapturedVariables {
		captured[k] = v
	}
	return map[string]any{
		"params":            params,
		"capturedVariables": captured,
	}
}

func copyMap(m map[string]string) map[string]string {
	result := make(map[string]string, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}

type defaultLogger struct{}

func (l *defaultLogger) Println(args ...any)                 { fmt.Println(args...) }
func (l *defaultLogger) Printf(format string, args ...any)   { fmt.Printf(format, args...) }
func (l *defaultLogger) Errorf(format string, args ...any)    { fmt.Fprintf(os.Stderr, format, args...) }
