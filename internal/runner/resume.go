package runner

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"

	"github.com/codagent/agent-runner/internal/engine"
	"github.com/codagent/agent-runner/internal/loader"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/stateio"
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

	if state.Completed {
		return nil, ErrAlreadyCompleted
	}

	workflow, err := loader.LoadWorkflow(state.WorkflowFile, loader.Options{})
	if err != nil {
		return nil, fmt.Errorf("cannot reload workflow: %w", err)
	}

	// Check workflow hash
	content, readErr := os.ReadFile(state.WorkflowFile)
	if readErr == nil {
		currentHash := stateio.ComputeWorkflowHash(string(content))
		if currentHash != state.WorkflowHash {
			if opts.Log != nil {
				opts.Log.Printf("agent-runner: warning: workflow file has changed since last run\n")
			}
		}
	}

	// Resolve the step to resume from
	var fromStep string
	var sessionIDs map[string]string
	var sessionProfiles map[string]string
	var capturedVars map[string]string
	var lastSessionStepID string
	var childState *model.NestedStepState

	var completed bool

	if state.CurrentStep.Nested != nil {
		nested := state.CurrentStep.Nested
		fromStep = nested.StepID
		sessionIDs = nested.SessionIDs
		sessionProfiles = nested.SessionProfiles
		capturedVars = nested.CapturedVariables
		lastSessionStepID = nested.LastSessionStepID
		completed = nested.Completed
		if nested.Iteration != nil {
			// Top-level loop step captured mid-iteration. Carry the iteration
			// (and any deeper chain) through as ChildState so ExecuteLoopStep's
			// consumeLoopResume can pick it up when the step is dispatched.
			childState = &model.NestedStepState{
				StepID:    nested.StepID,
				Iteration: nested.Iteration,
				Child:     nested.Child,
			}
		} else if nested.Child != nil {
			childState = nested.Child
		}
	} else {
		fromStep = state.CurrentStep.StepID
	}

	// Resolve which step to actually resume from — advance past completed steps.
	resolved, err := model.ResolveResumeStep(workflow.Steps, fromStep, completed)
	if err != nil {
		return nil, fmt.Errorf("step %q no longer exists in workflow", fromStep)
	}
	if resolved.AllDone {
		return nil, ErrAlreadyCompleted
	}
	fromStep = resolved.StepID

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
		From:              fromStep,
		WorkflowFile:      state.WorkflowFile,
		SessionDir:        filepath.Dir(stateFilePath),
		Engine:            eng,
		SessionIDs:        sessionIDs,
		SessionProfiles:   sessionProfiles,
		CapturedVariables: capturedVars,
		LastSessionStepID: lastSessionStepID,
		ChildState:        childState,
		ProcessRunner:     opts.ProcessRunner,
		GlobExpander:      opts.GlobExpander,
		Log:               opts.Log,
		SuspendHook:       opts.SuspendHook,
		ResumeHook:        opts.ResumeHook,
	}

	return PrepareRun(&workflow, state.Params, resumeOpts)
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
