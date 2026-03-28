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

// Workflow result constants.
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
	LastSessionStepID string
	ChildState        *model.SubWorkflowChildState
	ProcessRunner     exec.ProcessRunner
	GlobExpander      exec.GlobExpander
	Log               exec.Logger
}

func validateParams(workflow *model.Workflow, params map[string]string) error {
	for _, param := range workflow.Params {
		if _, ok := params[param.Name]; ok {
			continue
		}
		if param.IsRequired() {
			if param.Default != "" {
				params[param.Name] = param.Default
			} else {
				return fmt.Errorf("missing required parameter: %s", param.Name)
			}
		}
	}
	return nil
}

func resolveStartIndex(workflow *model.Workflow, from string) (int, error) {
	if from == "" {
		return 0, nil
	}
	for i := range workflow.Steps {
		if workflow.Steps[i].ID == from {
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
	data, err := os.ReadFile(workflowFile) // #nosec G304 -- workflow file is user-specified CLI input
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

// runState holds the internal state needed during workflow execution.
type runState struct {
	workflow     model.Workflow
	ctx          *model.ExecutionContext
	stateDir     string
	workflowHash string
	auditLogger  *audit.Logger
	runStartTime time.Time
	log          exec.Logger
	runner       exec.ProcessRunner
	glob         exec.GlobExpander
}

func initRunState(workflow *model.Workflow, params map[string]string, opts *Options) (*runState, error) {
	if err := validateParams(workflow, params); err != nil {
		return nil, err
	}

	if opts.Engine != nil {
		if err := opts.Engine.ValidateWorkflow(workflow, params, opts.WorkflowFile); err != nil {
			return nil, err
		}
	}

	defaultStateDir := opts.StateDir
	if defaultStateDir == "" {
		defaultStateDir, _ = os.Getwd()
	}

	var engineRef model.Engine
	if opts.Engine != nil {
		engineRef = opts.Engine
	}

	auditLogger, err := audit.CreateLogger(workflow.Name, "")
	if err != nil {
		auditLogger = nil
	}

	ctx := model.NewRootContext(model.RootContextOptions{
		Params:            params,
		WorkflowFile:      opts.WorkflowFile,
		EngineRef:         engineRef,
		SessionIDs:        opts.SessionIDs,
		CapturedVariables: opts.CapturedVariables,
		AuditLogger:       auditLogger,
	})
	ctx.AgentCmd = workflow.Agent
	if opts.ChildState != nil {
		ctx.ResumeChildState = opts.ChildState
	}
	if opts.LastSessionStepID != "" {
		ctx.LastSessionStepID = opts.LastSessionStepID
	}

	log := opts.Log
	if log == nil {
		log = &defaultLogger{}
	}

	return &runState{
		workflow:     *workflow,
		ctx:          ctx,
		stateDir:     resolveStateDir(opts.Engine, params, defaultStateDir),
		workflowHash: computeHash(opts.WorkflowFile),
		auditLogger:  auditLogger,
		runStartTime: time.Now(),
		log:          log,
		runner:       opts.ProcessRunner,
		glob:         opts.GlobExpander,
	}, nil
}

func emitRunStart(rs *runState, opts *Options) {
	if rs.auditLogger == nil {
		return
	}
	auditData := map[string]any{
		"workflow_file": opts.WorkflowFile,
		"workflow_name": rs.workflow.Name,
		"workflow_hash": rs.workflowHash,
		"context": map[string]any{
			"params":            rs.ctx.Params,
			"capturedVariables": rs.ctx.CapturedVariables,
			"sessionIds":        rs.ctx.SessionIDs,
		},
	}
	if opts.From != "" {
		auditData["resumed"] = true
		auditData["resume_from"] = opts.From
	}
	rs.auditLogger.Emit(audit.Event{
		Timestamp: rs.runStartTime.UTC().Format(time.RFC3339),
		Type:      audit.EventRunStart,
		Data:      auditData,
	})
}

func executeSteps(rs *runState, startIndex int) WorkflowResult {
	for i := startIndex; i < len(rs.workflow.Steps); i++ {
		step := &rs.workflow.Steps[i]

		if flowctl.ShouldSkip(step.SkipIf, rs.ctx.LastStepOutcome) {
			emitSkippedStep(rs, step, i)
			continue
		}

		stepType := step.StepType()
		breadcrumb := textfmt.BuildBreadcrumb(nestingToFmt(rs.ctx), step.ID)
		rs.log.Println(textfmt.Separator())
		rs.log.Println(textfmt.StepHeading(i, len(rs.workflow.Steps), breadcrumb, stepType, false))

		stepRef := step // capture for closure
		rs.ctx.FlushState = func() {
			writeStepState(stepRef, rs.ctx, &rs.workflow, rs.workflowHash, rs.stateDir, nil)
		}

		outcome, loopResult, stepErr := runStep(step, rs)
		rs.ctx.FlushState = nil

		if stepErr != nil {
			return ResultFailed
		}

		writeStepState(step, rs.ctx, &rs.workflow, rs.workflowHash, rs.stateDir, loopResult)

		if outcome == exec.OutcomeAborted {
			rs.log.Println("\nagent-runner: workflow stopped.")
			return ResultStopped
		}

		if outcome == exec.OutcomeFailed {
			o := string(outcome)
			rs.ctx.LastStepOutcome = &o
			if step.ContinueOnFailure {
				rs.log.Printf("--- step %q failed (continue_on_failure) ---\n\n", step.ID)
				continue
			}
			rs.log.Printf("\nagent-runner: step %q failed. Stopping.\n", step.ID)
			return ResultFailed
		}

		o := "success"
		rs.ctx.LastStepOutcome = &o
		rs.log.Printf("--- step %q complete ---\n\n", step.ID)
	}

	return ResultSuccess
}

func emitSkippedStep(rs *runState, step *model.Step, index int) {
	breadcrumb := textfmt.BuildBreadcrumb(nestingToFmt(rs.ctx), step.ID)
	rs.log.Println(textfmt.Separator())
	rs.log.Println(textfmt.StepHeading(index, len(rs.workflow.Steps), breadcrumb, "", true))

	prefix := audit.BuildPrefix(nestingToAuditInfo(rs.ctx), step.ID)
	emitAudit(rs.ctx, audit.Event{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Prefix:    prefix,
		Type:      audit.EventStepStart,
		Data:      map[string]any{"context": contextSnapshot(rs.ctx)},
	})
	emitAudit(rs.ctx, audit.Event{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Prefix:    prefix,
		Type:      audit.EventStepEnd,
		Data:      map[string]any{"outcome": "skipped", "skip_if": step.SkipIf, "duration_ms": 0},
	})
}

func runStep(step *model.Step, rs *runState) (exec.StepOutcome, *exec.LoopResult, error) {
	if step.Loop != nil && len(step.Steps) > 0 {
		lr, err := exec.ExecuteLoopStep(step, rs.ctx, rs.runner, rs.glob, rs.log, exec.LoopExecuteOptions{})
		return exec.MapLoopOutcomeForRunner(lr.Outcome), &lr, err
	}
	outcome, err := exec.DispatchStep(step, rs.ctx, rs.runner, rs.glob, rs.log)
	return outcome, nil, err
}

func finalizeRun(rs *runState, result WorkflowResult) {
	switch result {
	case ResultSuccess:
		stateio.DeleteState(rs.stateDir)
		rs.log.Println("agent-runner: workflow complete")
	case ResultFailed:
		rs.log.Printf("agent-runner: to resume: agent-runner resume %s\n", stateio.GetStateFilePath(rs.stateDir))
	case ResultStopped:
		// No action needed
	}

	if rs.auditLogger != nil {
		rs.auditLogger.Emit(audit.Event{
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Type:      audit.EventRunEnd,
			Data: map[string]any{
				"outcome":     string(result),
				"duration_ms": time.Since(rs.runStartTime).Milliseconds(),
			},
		})
		rs.auditLogger.Close()
	}
}

// RunWorkflow executes a workflow with the given parameters.
func RunWorkflow(
	workflow *model.Workflow,
	params map[string]string,
	opts *Options,
) (WorkflowResult, error) {
	rs, err := initRunState(workflow, params, opts)
	if err != nil {
		return ResultFailed, err
	}

	startIndex, err := resolveStartIndex(workflow, opts.From)
	if err != nil {
		return ResultFailed, err
	}

	emitRunStart(rs, opts)
	rs.log.Printf("\nagent-runner: running workflow %q\n\n", workflow.Name)

	result := executeSteps(rs, startIndex)
	finalizeRun(rs, result)

	return result, nil
}

func writeStepState(step *model.Step, ctx *model.ExecutionContext, workflow *model.Workflow, workflowHash, stateDir string, loopResult *exec.LoopResult) {
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
		LastSessionStepID: ctx.LastSessionStepID,
		Child:             child,
	}

	state := model.RunState{
		WorkflowFile: ctx.WorkflowFile,
		WorkflowName: workflow.Name,
		CurrentStep:  model.CurrentStep{Nested: nested},
		Params:       ctx.Params,
		WorkflowHash: workflowHash,
	}
	_ = stateio.WriteState(&state, stateDir)
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

func (l *defaultLogger) Println(args ...any)               { fmt.Println(args...) }
func (l *defaultLogger) Printf(format string, args ...any) { fmt.Printf(format, args...) }
func (l *defaultLogger) Errorf(format string, args ...any) { fmt.Fprintf(os.Stderr, format, args...) }
