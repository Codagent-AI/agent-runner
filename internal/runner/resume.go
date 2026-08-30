package runner

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/config"
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

	profileOverride := resumeProfileOverride(opts.ProfileOverride, state.ProfileSet)

	workflow, err := loadRecordedWorkflow(state.WorkflowFile)
	if err != nil {
		return nil, err
	}

	warnIfWorkflowHashChanged(&state, opts.Log)

	// Repository state is additive. A state file without a persisted selection
	// predates fan-out and must retain the legacy resume path even when the
	// current project has repository declarations.
	if len(state.SelectedRepositories) > 0 {
		workspace, err := prepareRepositoryResumeWorkspace(&state, workflow.Scope, opts, profileOverride)
		if err != nil {
			return nil, err
		}
		opts.Workspace = workspace
		if err := validateTaskPlanResume(state.TaskPlan, workspace); err != nil {
			return nil, err
		}
	}

	resumeState := restoreResumeContext(&state)
	workspaceState := state.WorkspaceNamespace
	if workspaceState == nil {
		workspaceState = &model.NamespaceState{
			SessionIDs: resumeState.sessionIDs, SessionProfiles: resumeState.sessionProfiles,
			CapturedVariables: resumeState.capturedVars, LastSessionStepID: resumeState.lastSessionStepID,
			NamedSessions: resumeState.namedSessions, NamedSessionDecls: resumeState.namedSessionDecls,
		}
	}

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
		From:                      resumeState.fromStep,
		WorkflowFile:              state.WorkflowFile,
		WorkflowScope:             workflow.Scope,
		Workspace:                 opts.Workspace,
		RepositoryFrame:           state.RepositoryFrame,
		TaskPlan:                  state.TaskPlan,
		WorkspacePullRequestURL:   state.WorkspacePullRequestURL,
		RepositoryPullRequestURLs: state.RepositoryPullRequestURLs,
		ProjectRoot:               opts.ProjectRoot,
		WorkingDir:                opts.WorkingDir,
		SessionDir:                filepath.Dir(stateFilePath),
		IntakeHandoffContents:     state.IntakeHandoffContents,
		IntakeParentRunID:         state.IntakeParentRunID,
		AgentOverride:             state.AgentOverride,
		Engine:                    eng,
		SessionIDs:                workspaceState.SessionIDs,
		SessionProfiles:           workspaceState.SessionProfiles,
		CapturedVariables:         workspaceState.CapturedVariables,
		LastSessionStepID:         workspaceState.LastSessionStepID,
		NamedSessions:             workspaceState.NamedSessions,
		NamedSessionDecls:         workspaceState.NamedSessionDecls,
		ChildState:                resumeState.childState,
		InteractiveAttempt:        resumeInteractiveAttempt(&state),
		ProcessRunner:             opts.ProcessRunner,
		GlobExpander:              opts.GlobExpander,
		Log:                       opts.Log,
		SuspendHook:               opts.SuspendHook,
		ResumeHook:                opts.ResumeHook,
		PrepareStepHook:           opts.PrepareStepHook,
		UIStepHandler:             opts.UIStepHandler,
		ProfileStore:              opts.ProfileStore,
		ProfileOverride:           profileOverride,
		RepositoryStartIndex:      resumeState.repositoryIndex,
		RepositoryStartSet:        resumeState.repositoryIndexSet,
	}

	return PrepareRun(&workflow, state.Params, resumeOpts)
}

// prepareRepositoryResumeWorkspace reconstructs the current configuration
// then compares it to the immutable state identity before a command can be
// dispatched. State owns selection order; the current configuration only
// proves that those named checkouts still identify the same roots.
func prepareRepositoryResumeWorkspace(state *model.RunState, scope model.Scope, opts *Options, profileOverride config.ProfileOverride) (*model.WorkspaceContext, error) {
	launchDir := opts.WorkingDir
	if launchDir == "" {
		var err error
		launchDir, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("determine resume workspace: %w", err)
		}
	}
	launchDir, err := canonicalDirectory(launchDir)
	if err != nil {
		return nil, fmt.Errorf("resolve resume workspace: %w", err)
	}
	if launchDir != state.WorkspaceDir {
		return nil, fmt.Errorf("resume workspace %s does not match persisted workspace %s", launchDir, state.WorkspaceDir)
	}

	if opts.ProfileStore == nil {
		profiles, loadErr := config.LoadWithProfile(filepath.Join(launchDir, ".agent-runner", "config.yaml"), profileOverride)
		if loadErr != nil {
			return nil, fmt.Errorf("load resume workspace configuration: %w", loadErr)
		}
		opts.ProfileStore = profiles
	}
	workspace, err := prepareWorkspace(scope, launchDir, opts.ProfileStore)
	if err != nil {
		return nil, err
	}

	persistedNames := make([]string, 0, len(state.SelectedRepositories))
	for _, persisted := range state.SelectedRepositories {
		current, ok := workspace.Repositories[persisted.Name]
		if !ok {
			return nil, fmt.Errorf("persisted selected repository %q is no longer configured", persisted.Name)
		}
		if current.Dir != persisted.Dir {
			return nil, fmt.Errorf("repository %q identity mismatch: persisted root %s, current root %s", persisted.Name, persisted.Dir, current.Dir)
		}
		persistedNames = append(persistedNames, persisted.Name)
	}
	if len(state.Params) > 0 {
		if supplied, ok := state.Params[model.RepositoriesParam]; ok {
			selected, parseErr := model.ParseRepositoryTargets(supplied, workspace.Repositories)
			if parseErr != nil {
				return nil, fmt.Errorf("persisted repository selection: %w", parseErr)
			}
			if !slices.Equal(selected, persistedNames) {
				return nil, fmt.Errorf("persisted repository selection order does not match selected repository identity")
			}
		}
	}
	if opts.Workspace != nil && len(opts.Workspace.Selected) > 0 && !slices.Equal(opts.Workspace.Selected, persistedNames) {
		return nil, fmt.Errorf("resume repository selection %v does not match persisted selection %v", opts.Workspace.Selected, persistedNames)
	}
	workspace.Selected = append([]string(nil), persistedNames...)
	return workspace, nil
}

func warnIfWorkflowHashChanged(state *model.RunState, log interface{ Printf(string, ...any) }) {
	content, err := loader.ReadWorkflowFile(state.WorkflowFile)
	if err == nil && stateio.ComputeWorkflowHash(string(content)) != state.WorkflowHash && log != nil {
		log.Printf("agent-runner: warning: workflow file has changed since last run\n")
	}
}

func resumeProfileOverride(override config.ProfileOverride, recorded string) config.ProfileOverride {
	if override.Name == "" && recorded != "" {
		return config.ProfileOverride{Name: recorded, Origin: config.OriginState}
	}
	return override
}

type restoredResumeContext struct {
	fromStep           string
	sessionIDs         map[string]string
	sessionProfiles    map[string]string
	capturedVars       map[string]model.CapturedValue
	lastSessionStepID  string
	namedSessions      map[string]string
	namedSessionDecls  map[string]string
	childState         *model.NestedStepState
	completed          bool
	repositoryIndex    int
	repositoryIndexSet bool
}

func restoreResumeContext(state *model.RunState) restoredResumeContext {
	nested := resumeNestedState(state)
	if nested == nil {
		return restoredResumeContext{fromStep: state.CurrentStep.StepID}
	}
	result := restoredResumeContext{
		fromStep:           nested.StepID,
		sessionIDs:         nested.SessionIDs,
		sessionProfiles:    nested.SessionProfiles,
		capturedVars:       nested.CapturedVariables,
		lastSessionStepID:  nested.LastSessionStepID,
		namedSessions:      nested.NamedSessions,
		namedSessionDecls:  nested.NamedSessionDecls,
		completed:          nested.Completed,
		repositoryIndex:    resumeRepositoryIndex(state),
		repositoryIndexSet: state.RepositoryFrame != nil || state.RepositoryIndex != nil,
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

func resumeNestedState(state *model.RunState) *model.NestedStepState {
	if index, ok := activeRepositoryFrameIndex(state); ok {
		if nested := state.RepositoryFrame.Repositories[index].Nested; nested != nil {
			return nested
		}
	}
	return state.CurrentStep.Nested
}

func resumeRepositoryIndex(state *model.RunState) int {
	if index, ok := activeRepositoryFrameIndex(state); ok {
		return index
	}
	return dereferenceRepositoryIndex(state.RepositoryIndex)
}

func activeRepositoryFrameIndex(state *model.RunState) (int, bool) {
	if state.RepositoryFrame == nil {
		return 0, false
	}
	if state.RepositoryIndex != nil {
		index := *state.RepositoryIndex
		if index >= 0 && index < len(state.RepositoryFrame.Repositories) && state.RepositoryFrame.Repositories[index].Status != model.RepositoryCompleted {
			return index, true
		}
	}
	for index, entry := range state.RepositoryFrame.Repositories {
		if entry.Status != model.RepositoryCompleted {
			return index, true
		}
	}
	return 0, false
}

func dereferenceRepositoryIndex(index *int) int {
	if index == nil {
		return 0
	}
	return *index
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
	nested := resumeNestedState(state)
	if nested == nil {
		return nil
	}
	return nested.InteractiveAttempt
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
