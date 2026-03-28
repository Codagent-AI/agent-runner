package runner

import (
	"fmt"
	"os"

	"github.com/codagent/agent-runner/internal/engine"
	"github.com/codagent/agent-runner/internal/loader"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/stateio"
)

// ResumeWorkflow resumes a workflow from a state file.
func ResumeWorkflow(stateFilePath string, opts *Options) (WorkflowResult, error) {
	state, err := stateio.ReadState(stateFilePath)
	if err != nil {
		return ResultFailed, err
	}

	workflow, err := loader.LoadWorkflow(state.WorkflowFile, loader.Options{})
	if err != nil {
		return ResultFailed, fmt.Errorf("cannot reload workflow: %w", err)
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
	var capturedVars map[string]string
	var lastSessionStepID string
	var childState *model.SubWorkflowChildState

	if state.CurrentStep.Nested != nil {
		nested := state.CurrentStep.Nested
		fromStep = nested.StepID
		sessionIDs = nested.SessionIDs
		capturedVars = nested.CapturedVariables
		lastSessionStepID = nested.LastSessionStepID
		if nested.Child != nil {
			childState = nestedToChildState(nested.Child)
		}
	} else {
		fromStep = state.CurrentStep.StepID
	}

	// Validate that the step still exists
	found := false
	for i := range workflow.Steps {
		if workflow.Steps[i].ID == fromStep {
			found = true
			break
		}
	}
	if !found {
		return ResultFailed, fmt.Errorf("step %q no longer exists in workflow", fromStep)
	}

	// Create engine if configured
	var eng engine.Engine
	if workflow.Engine != nil {
		engConfig := map[string]any{"type": workflow.Engine.Type}
		for k, v := range workflow.Engine.Extras {
			engConfig[k] = v
		}
		eng, err = engine.Create(engConfig)
		if err != nil {
			return ResultFailed, fmt.Errorf("create engine: %w", err)
		}
	}

	return RunWorkflow(&workflow, state.Params, &Options{
		From:              fromStep,
		WorkflowFile:      state.WorkflowFile,
		StateDir:          opts.StateDir,
		Engine:            eng,
		SessionIDs:        sessionIDs,
		CapturedVariables: capturedVars,
		LastSessionStepID: lastSessionStepID,
		ChildState:        childState,
		ProcessRunner:     opts.ProcessRunner,
		GlobExpander:      opts.GlobExpander,
		Log:               opts.Log,
	})
}

func nestedToChildState(nested *model.NestedStepState) *model.SubWorkflowChildState {
	if nested == nil {
		return nil
	}
	return &model.SubWorkflowChildState{
		StepID:            nested.StepID,
		SessionIDs:        copyMap(nested.SessionIDs),
		CapturedVariables: copyMap(nested.CapturedVariables),
		Child:             nestedToChildState(nested.Child),
	}
}
