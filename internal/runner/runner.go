package runner

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/config"
	"github.com/codagent/agent-runner/internal/engine"
	"github.com/codagent/agent-runner/internal/exec"
	"github.com/codagent/agent-runner/internal/flowctl"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/runlock"
	"github.com/codagent/agent-runner/internal/stateio"
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
	SessionDir        string // Override session directory (for testing); computed automatically if empty.
	Engine            engine.Engine
	ProfileStore      *config.Config
	SessionIDs        map[string]string
	SessionProfiles   map[string]string
	CapturedVariables map[string]string
	LastSessionStepID string
	ChildState        *model.SubWorkflowChildState
	ProcessRunner     exec.ProcessRunner
	GlobExpander      exec.GlobExpander
	Log               exec.Logger

	// SuspendHook is called just before an interactive agent step takes over
	// the terminal (e.g. p.ReleaseTerminal in TUI mode). Nil = no-op.
	SuspendHook func()
	// ResumeHook is called immediately after an interactive agent step exits
	// (e.g. p.RestoreTerminal in TUI mode). Nil = no-op.
	ResumeHook func()
}

// RunHandle is returned by PrepareRun and PrepareResume. It holds all state
// needed to call ExecuteFromHandle and exposes the session directory so callers
// can construct the TUI before execution starts.
type RunHandle struct {
	rs         *runState
	startIndex int

	// SessionDir is the run's session directory (e.g. ~/.agent-runner/projects/.../runs/<id>).
	SessionDir string
	// ProjectDir is the parent of the runs/ directory.
	ProjectDir string
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

func emitAudit(ctx *model.ExecutionContext, event audit.Event) {
	if ctx.AuditLogger != nil {
		ctx.AuditLogger.Emit(event)
	}
}

// runState holds the internal state needed during workflow execution.
type runState struct {
	workflow     model.Workflow
	ctx          *model.ExecutionContext
	sessionDir   string
	sessionID    string
	workflowHash string
	auditLogger  *audit.Logger
	runStartTime time.Time
	log          exec.Logger
	runner       exec.ProcessRunner
	glob         exec.GlobExpander
}

// workflowNeedsAgentProfiles returns true if any step in the tree is an agent
// step (has a Prompt or Agent field) or delegates to a sub-workflow (Workflow
// field set). Sub-workflows are assumed to potentially contain agent steps
// since the referenced YAML isn't parsed here; loading profiles eagerly is
// cheap and avoids silently falling back to an empty profile at dispatch time.
func workflowNeedsAgentProfiles(steps []model.Step) bool {
	for i := range steps {
		if steps[i].Prompt != "" || steps[i].Agent != "" || steps[i].Workflow != "" {
			return true
		}
		if len(steps[i].Steps) > 0 && workflowNeedsAgentProfiles(steps[i].Steps) {
			return true
		}
	}
	return false
}

func initRunState(workflow *model.Workflow, params map[string]string, opts *Options) (*runState, error) {
	if err := validateParams(workflow, params); err != nil {
		return nil, err
	}

	// Load agent profiles if not already provided and the workflow has agent steps.
	if opts.ProfileStore == nil && workflowNeedsAgentProfiles(workflow.Steps) {
		cfg, err := config.LoadOrGenerate(".agent-runner/config.yaml")
		if err != nil {
			return nil, fmt.Errorf("loading agent profiles: %w", err)
		}
		opts.ProfileStore = cfg
	}

	if opts.Engine != nil {
		if err := opts.Engine.ValidateWorkflow(workflow, params, opts.WorkflowFile); err != nil {
			return nil, err
		}
	}

	// Build session ID and directory.
	safeName := audit.SanitizeWorkflowName(workflow.Name)
	now := time.Now()
	timestamp := strings.NewReplacer(":", "-", ".", "-").Replace(now.UTC().Format(time.RFC3339Nano))
	sessionID := safeName + "-" + timestamp

	sessionDir := opts.SessionDir
	var cwd string
	if sessionDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("cannot determine home directory: %w", err)
		}
		cwd, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("cannot determine working directory: %w", err)
		}
		encoded := audit.EncodePath(cwd)
		sessionDir = filepath.Join(home, ".agent-runner", "projects", encoded, "runs", sessionID)
	} else {
		sessionID = filepath.Base(sessionDir)
	}

	if err := os.MkdirAll(sessionDir, 0o750); err != nil {
		return nil, fmt.Errorf("create session dir: %w", err)
	}

	if err := runlock.Write(sessionDir); err != nil {
		fmt.Fprintf(os.Stderr, "agent-runner: warning: could not write lock file in %s: %v\n", sessionDir, err)
	}

	if opts.SessionDir == "" {
		projectDir := filepath.Dir(filepath.Dir(sessionDir)) // parent of runs/
		writeMetaJSON(projectDir, cwd)
	}

	auditLogger, ctx := buildExecutionContext(workflow, params, opts, sessionDir)

	log := opts.Log
	if log == nil {
		log = &defaultLogger{}
	}

	return &runState{
		workflow:     *workflow,
		ctx:          ctx,
		sessionDir:   sessionDir,
		sessionID:    sessionID,
		workflowHash: computeHash(opts.WorkflowFile),
		auditLogger:  auditLogger,
		runStartTime: now,
		log:          log,
		runner:       opts.ProcessRunner,
		glob:         opts.GlobExpander,
	}, nil
}

func buildExecutionContext(workflow *model.Workflow, params map[string]string, opts *Options, sessionDir string) (*audit.Logger, *model.ExecutionContext) {
	var engineRef interface{}
	if opts.Engine != nil {
		engineRef = opts.Engine
	}

	var auditEventLogger audit.EventLogger
	auditLogger, err := audit.NewLogger(filepath.Join(sessionDir, "audit.log"))
	if err == nil {
		auditEventLogger = auditLogger
	}

	var profileStore any
	if opts.ProfileStore != nil {
		profileStore = opts.ProfileStore
	}

	ctx := model.NewRootContext(&model.RootContextOptions{
		Params:              params,
		WorkflowFile:        opts.WorkflowFile,
		WorkflowName:        workflow.Name,
		WorkflowDescription: workflow.Description,
		EngineRef:           engineRef,
		ProfileStore:        profileStore,
		SessionIDs:          opts.SessionIDs,
		SessionProfiles:     opts.SessionProfiles,
		CapturedVariables:   opts.CapturedVariables,
		AuditLogger:         auditEventLogger,
	})
	if opts.ChildState != nil {
		ctx.ResumeChildState = opts.ChildState
	}
	if opts.LastSessionStepID != "" {
		ctx.LastSessionStepID = opts.LastSessionStepID
	}
	if opts.From != "" {
		ctx.WorkflowResumed = true
	}
	return auditLogger, ctx
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

		// Fresh chain for each top-level step; writeStepState intentionally
		// does not clear it so the mid-step and post-step writes can share.
		rs.ctx.LastSubWorkflowChild = nil

		stepRef := step // capture for closure
		rs.ctx.FlushState = func() {
			writeStepState(stepRef, rs.ctx, &rs.workflow, rs.workflowHash, rs.sessionDir, nil, false)
		}

		outcome, loopResult, stepErr := runStep(step, rs)
		rs.ctx.FlushState = nil

		completed := stepErr == nil && outcome != exec.OutcomeAborted && outcome != exec.OutcomeFailed
		writeStepState(step, rs.ctx, &rs.workflow, rs.workflowHash, rs.sessionDir, loopResult, completed)

		if stepErr != nil {
			rs.log.Printf("\nagent-runner: step %q error: %v\n", step.ID, stepErr)
			return ResultFailed
		}

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
	}

	return ResultSuccess
}

func emitSkippedStep(rs *runState, step *model.Step, index int) {
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
	runlock.Delete(rs.sessionDir)

	switch result {
	case ResultSuccess:
		if err := markStateCompleted(rs.sessionDir); err != nil {
			rs.log.Printf("agent-runner: warning: could not mark state completed: %v\n", err)
		}
	case ResultFailed:
		rs.log.Printf("\nto resume: agent-runner --resume %s\n", rs.sessionID)
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

// markStateCompleted reads the run's state.json, sets Completed=true, and
// rewrites it so the TUI can continue to display the run's metadata after it
// finishes. The state file is intentionally preserved rather than deleted.
func markStateCompleted(sessionDir string) error {
	statePath := filepath.Join(sessionDir, "state.json")
	state, err := stateio.ReadState(statePath)
	if err != nil {
		return err
	}
	state.Completed = true
	return stateio.WriteState(&state, sessionDir)
}

// PrepareRun initializes the session directory, writes the lock file, opens
// the audit logger, and emits run_start. Returns a RunHandle with SessionDir
// exposed so callers can construct the TUI before execution starts.
func PrepareRun(workflow *model.Workflow, params map[string]string, opts *Options) (*RunHandle, error) {
	rs, err := initRunState(workflow, params, opts)
	if err != nil {
		return nil, err
	}

	startIndex, err := resolveStartIndex(workflow, opts.From)
	if err != nil {
		return nil, err
	}

	emitRunStart(rs, opts)
	rs.log.Printf("\nagent-runner: running workflow %q\n\n", workflow.Name)

	projectDir := filepath.Dir(filepath.Dir(rs.sessionDir)) // parent of runs/
	return &RunHandle{
		rs:         rs,
		startIndex: startIndex,
		SessionDir: rs.sessionDir,
		ProjectDir: projectDir,
	}, nil
}

// ExecuteFromHandle runs executeSteps + finalizeRun on an already-prepared handle.
// opts may override the process runner, logger, and suspend/resume hooks (e.g.
// to inject TUI-aware implementations without touching PrepareRun's session setup).
// Safe to call from a background goroutine.
func ExecuteFromHandle(h *RunHandle, opts *Options) WorkflowResult {
	if opts != nil {
		if opts.ProcessRunner != nil {
			h.rs.runner = opts.ProcessRunner
		}
		if opts.GlobExpander != nil {
			h.rs.glob = opts.GlobExpander
		}
		if opts.Log != nil {
			h.rs.log = opts.Log
		}
		if opts.SuspendHook != nil {
			h.rs.ctx.SuspendHook = opts.SuspendHook
		}
		if opts.ResumeHook != nil {
			h.rs.ctx.ResumeHook = opts.ResumeHook
		}
	}
	result := executeSteps(h.rs, h.startIndex)
	finalizeRun(h.rs, result)
	return result
}

// RunWorkflow executes a workflow with the given parameters.
// This is a thin wrapper around PrepareRun + ExecuteFromHandle; existing tests
// and non-TUI callers use this unchanged signature.
func RunWorkflow(
	workflow *model.Workflow,
	params map[string]string,
	opts *Options,
) (WorkflowResult, error) {
	h, err := PrepareRun(workflow, params, opts)
	if err != nil {
		return ResultFailed, err
	}
	return ExecuteFromHandle(h, opts), nil
}

func writeStepState(step *model.Step, ctx *model.ExecutionContext, workflow *model.Workflow, workflowHash, stateDir string, loopResult *exec.LoopResult, completed bool) {
	var child *model.NestedStepState
	var iteration *int

	// When the loop executor wrote iteration metadata onto ctx.LastSubWorkflowChild
	// (top-level loop case), promote Iteration onto the top-level NestedStepState
	// instead of wrapping in a duplicated child entry. Only do this if the stored
	// StepID matches the step we are writing for — otherwise the child is
	// genuinely a nested step.
	//
	// Note: we intentionally do not clear ctx.LastSubWorkflowChild here. The
	// mid-step FlushState callback and the post-step write both read it, and
	// clearing would make the post-step write see an empty chain after the
	// mid-step flush consumed it. executeSteps resets the chain at the top of
	// the next iteration.
	switch {
	case ctx.LastSubWorkflowChild != nil && ctx.LastSubWorkflowChild.StepID == step.ID:
		iteration = ctx.LastSubWorkflowChild.Iteration
		if ctx.LastSubWorkflowChild.Child != nil {
			child = toNestedStepState(ctx.LastSubWorkflowChild.Child)
		}
	case ctx.LastSubWorkflowChild != nil:
		child = toNestedStepState(ctx.LastSubWorkflowChild)
	case loopResult != nil && loopResult.LastIteration >= 0:
		// Fallback: a loop step finished without writing iteration metadata
		// through the new channel (e.g. the mechanism was skipped because the
		// loop ran to exhaustion without any iteration). Record the last
		// completed iteration index so resume can start from the next one.
		next := loopResult.LastIteration + 1
		iteration = &next
	}

	nested := &model.NestedStepState{
		StepID:            step.ID,
		SessionIDs:        copyMap(ctx.SessionIDs),
		SessionProfiles:   copyMap(ctx.SessionProfiles),
		CapturedVariables: copyMap(ctx.CapturedVariables),
		LastSessionStepID: ctx.LastSessionStepID,
		Completed:         completed,
		Iteration:         iteration,
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
		SessionProfiles:   copyMap(child.SessionProfiles),
		CapturedVariables: copyMap(child.CapturedVariables),
		LastSessionStepID: child.LastSessionStepID,
		Completed:         child.Completed,
		Iteration:         child.Iteration,
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

// DiscardLogger drops all log output. Used in TUI mode where the TUI surfaces
// workflow status instead of stdout.
type DiscardLogger struct{}

func (l *DiscardLogger) Println(_ ...any)               {}
func (l *DiscardLogger) Printf(_ string, _ ...any)      {}
func (l *DiscardLogger) Errorf(_ string, _ ...any)      {}

// writeMetaJSON writes a meta.json file to projectDir if it does not already exist.
// Non-fatal: errors are silently ignored.
func writeMetaJSON(projectDir, cwd string) {
	metaPath := filepath.Join(projectDir, "meta.json")
	if _, err := os.Stat(metaPath); err == nil {
		return // already exists
	}
	data, err := json.Marshal(map[string]string{"path": cwd})
	if err != nil {
		return
	}
	_ = os.WriteFile(metaPath, data, 0o600)
}
