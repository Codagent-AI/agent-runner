package runner

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/engine"
	"github.com/codagent/agent-runner/internal/loader"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/stateio"
	"github.com/codagent/agent-runner/internal/workflowcatalog"
	builtinworkflows "github.com/codagent/agent-runner/workflows"
)

// ErrAlreadyCompleted is returned by PrepareResume and ResumeWorkflow when
// the recorded state indicates the workflow finished on a previous run.
// Callers use errors.Is to distinguish it from other setup errors.
var ErrAlreadyCompleted = errors.New("workflow already completed")

// PrepareResume loads the workflow state from stateFilePath, resolves the
// resume step, and calls PrepareRun to initialize the session. Returns a
// RunHandle that callers can pass to ExecuteFromHandle.
func PrepareResume(stateFilePath string, opts *Options) (*RunHandle, error) {
	state, err := stateio.ReadState(stateFilePath)
	if err != nil {
		return nil, err
	}

	if resumeAlreadyCompleted(stateFilePath, &state) {
		return nil, ErrAlreadyCompleted
	}

	workflow, err := loadRecordedWorkflow(state.WorkflowFile)
	if err != nil {
		return nil, err
	}

	// Check workflow hash
	content, readErr := loader.ReadWorkflowFile(state.WorkflowFile)
	if readErr == nil {
		currentHash := stateio.ComputeWorkflowHash(string(content))
		if currentHash != state.WorkflowHash {
			if opts.Log != nil {
				opts.Log.Printf("agent-runner: warning: workflow file has changed since last run\n")
			}
		}
	}

	resumeState := restoreResumeContext(&state)

	// Resolve which step to actually resume from — advance past completed steps.
	resolved, err := model.ResolveResumeStep(workflow.Steps, resumeState.fromStep, resumeState.completed)
	if err != nil {
		return nil, fmt.Errorf("step %q no longer exists in workflow", resumeState.fromStep)
	}
	if resolved.AllDone {
		return nil, ErrAlreadyCompleted
	}
	resumeState.fromStep = resolved.StepID

	// Create engine if configured
	var eng engine.Engine
	if workflow.Engine != nil {
		engConfig := map[string]any{"type": workflow.Engine.Type}
		maps.Copy(engConfig, workflow.Engine.Extras)
		eng, err = engine.Create(engConfig)
		if err != nil {
			return nil, fmt.Errorf("create engine: %w", err)
		}
	}

	resumeOpts := &Options{
		From:                  resumeState.fromStep,
		WorkflowFile:          state.WorkflowFile,
		SessionDir:            filepath.Dir(stateFilePath),
		IntakeHandoff:         state.IntakeHandoff,
		IntakeHandoffContents: state.IntakeHandoffContents,
		IntakeParentRunID:     state.IntakeParentRunID,
		AgentOverride:         state.AgentOverride,
		Engine:                eng,
		SessionIDs:            resumeState.sessionIDs,
		SessionProfiles:       resumeState.sessionProfiles,
		CapturedVariables:     resumeState.capturedVars,
		LastSessionStepID:     resumeState.lastSessionStepID,
		NamedSessions:         resumeState.namedSessions,
		NamedSessionDecls:     resumeState.namedSessionDecls,
		ChildState:            resumeState.childState,
		InteractiveAttempt:    resumeInteractiveAttempt(&state),
		ProcessRunner:         opts.ProcessRunner,
		GlobExpander:          opts.GlobExpander,
		Log:                   opts.Log,
		SuspendHook:           opts.SuspendHook,
		ResumeHook:            opts.ResumeHook,
		PrepareStepHook:       opts.PrepareStepHook,
		UIStepHandler:         opts.UIStepHandler,
	}

	return PrepareRun(&workflow, state.Params, resumeOpts)
}

type restoredResumeContext struct {
	fromStep          string
	sessionIDs        map[string]string
	sessionProfiles   map[string]string
	capturedVars      map[string]model.CapturedValue
	lastSessionStepID string
	namedSessions     map[string]string
	namedSessionDecls map[string]string
	childState        *model.NestedStepState
	completed         bool
}

func restoreResumeContext(state *model.RunState) restoredResumeContext {
	if state.CurrentStep.Nested == nil {
		return restoredResumeContext{fromStep: state.CurrentStep.StepID}
	}
	nested := state.CurrentStep.Nested
	result := restoredResumeContext{
		fromStep:          nested.StepID,
		sessionIDs:        nested.SessionIDs,
		sessionProfiles:   nested.SessionProfiles,
		capturedVars:      nested.CapturedVariables,
		lastSessionStepID: nested.LastSessionStepID,
		namedSessions:     nested.NamedSessions,
		namedSessionDecls: nested.NamedSessionDecls,
		completed:         nested.Completed,
	}
	if nested.Iteration != nil {
		// Top-level loop step captured mid-iteration. Carry the iteration (and
		// any deeper chain) through as ChildState for ExecuteLoopStep to resume.
		result.childState = &model.NestedStepState{
			StepID: nested.StepID, Iteration: nested.Iteration, Child: nested.Child,
		}
	} else {
		result.childState = nested.Child
	}
	return result
}

func resumeAlreadyCompleted(stateFilePath string, state *model.RunState) bool {
	if state.Completed {
		return true
	}
	return auditShowsCompleted(filepath.Join(filepath.Dir(stateFilePath), "audit.log"))
}

// auditShowsCompleted treats an unreadable or malformed audit as inconclusive;
// only a successfully parsed run_end can override incomplete persisted state.
func auditShowsCompleted(auditPath string) bool {
	file, err := os.Open(auditPath) // #nosec G304 -- audit path is derived from the selected state file.
	if err != nil {
		return false
	}
	defer func() { _ = file.Close() }()
	completed, err := audit.LatestRunCompleted(file)
	return err == nil && completed
}

func loadRecordedWorkflow(workflowFile string) (model.Workflow, error) {
	workflow, err := loader.LoadWorkflow(workflowFile, loader.Options{})
	if err == nil {
		return workflow, nil
	}
	var filenameErr *workflowcatalog.FilenameError
	if builtinworkflows.IsRef(workflowFile) && errors.As(err, &filenameErr) {
		return model.Workflow{}, fmt.Errorf(
			"cannot reload workflow: run predates workflow versioning and cannot be resumed by the current binary; restart the workflow with the current binary or finish this run using the older binary",
		)
	}
	return model.Workflow{}, fmt.Errorf("cannot reload workflow: %w", err)
}

func resumeInteractiveAttempt(state *model.RunState) *model.InteractiveAttemptMetadata {
	if state.CurrentStep.Nested == nil {
		return nil
	}
	return state.CurrentStep.Nested.InteractiveAttempt
}

// ResumeWorkflow resumes a workflow from a state file.
// This is a thin wrapper around PrepareResume + ExecuteFromHandle; existing tests
// and non-TUI callers use this unchanged signature.
func ResumeWorkflow(stateFilePath string, opts *Options) (WorkflowResult, error) {
	h, err := PrepareResume(stateFilePath, opts)
	if err != nil {
		// "already completed" is not an error for the caller
		if errors.Is(err, ErrAlreadyCompleted) {
			if opts.Log != nil {
				opts.Log.Println("agent-runner: workflow already completed")
			}
			return ResultSuccess, nil
		}
		return ResultFailed, err
	}
	return ExecuteFromHandle(h, opts), nil
}
