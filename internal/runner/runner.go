package runner

import (
	"encoding/json"
	"fmt"
	"os"
	osexec "os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/config"
	"github.com/codagent/agent-runner/internal/control"
	"github.com/codagent/agent-runner/internal/engine"
	"github.com/codagent/agent-runner/internal/exec"
	"github.com/codagent/agent-runner/internal/interactive"
	"github.com/codagent/agent-runner/internal/loader"
	"github.com/codagent/agent-runner/internal/metrics"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/repositorylock"
	"github.com/codagent/agent-runner/internal/runlock"
	"github.com/codagent/agent-runner/internal/stateio"
	"github.com/codagent/agent-runner/internal/usersettings"
	builtinworkflows "github.com/codagent/agent-runner/workflows"
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
	From         string
	Until        string
	WorkflowFile string
	// WorkflowScope is copied from the loaded workflow before root preparation
	// so scope-aware runs can enforce their Git-backed workspace contract.
	WorkflowScope model.Scope
	Workspace     *model.WorkspaceContext
	// RepositoryStartIndex resumes a repository fan-out at the persisted
	// repository boundary. RepositoryStartSet distinguishes it from a fresh
	// run's zero value.
	RepositoryStartIndex      int
	RepositoryStartSet        bool
	RepositoryFrame           *model.RepositoryFrame
	WorkspacePullRequestURL   string
	RepositoryPullRequestURLs map[string]string
	// IntakeHandoffContents carries the sealed intake handoff into a fresh run or
	// restores it on resume. It is internal provenance, not a user parameter.
	IntakeHandoffContents string
	IntakeParentRunID     string
	AgentOverride         *model.AgentOverride
	// ProjectRoot and WorkingDir may be supplied by embedding callers. When
	// empty, PrepareRun discovers and canonicalizes them once for the run.
	ProjectRoot        string
	WorkingDir         string
	SessionDir         string // Override session directory (for testing); computed automatically if empty.
	Engine             engine.Engine
	ProfileStore       *config.Config
	ProfileOverride    config.ProfileOverride
	SessionIDs         map[string]string
	SessionProfiles    map[string]string
	CapturedVariables  map[string]model.CapturedValue
	LastSessionStepID  string
	ChildState         *model.NestedStepState
	InteractiveAttempt *model.InteractiveAttemptMetadata
	// NamedSessions and NamedSessionDecls are restored from state on --resume.
	NamedSessions     map[string]string
	NamedSessionDecls map[string]string
	ProcessRunner     exec.ProcessRunner
	GlobExpander      exec.GlobExpander
	Log               exec.Logger

	// SuspendHook is called just before an interactive step takes over the
	// terminal (e.g. p.ReleaseTerminal in TUI mode). Nil = no-op.
	SuspendHook func() error
	// ResumeHook is called immediately after an interactive step exits (e.g.
	// p.RestoreTerminal in TUI mode). Nil = no-op.
	ResumeHook func() error

	// PrepareStepHook is called before each leaf step begins. The boolean
	// argument is true when the step will be interactive. Used by the TUI
	// coordinator to manage terminal state transitions without flicker.
	PrepareStepHook func(interactive bool)

	UIStepHandler func(*model.UIStepRequest) (model.UIStepResult, error)
}

var repositoryNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

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
		if param.Default != "" {
			params[param.Name] = param.Default
			continue
		}
		if !param.IsRequired() {
			params[param.Name] = ""
			continue
		}
		return fmt.Errorf("missing required parameter: %s", param.Name)
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

func validateUntilStep(workflow *model.Workflow, until string) error {
	if until == "" {
		return nil
	}
	for i := range workflow.Steps {
		if workflow.Steps[i].ID == until {
			return nil
		}
	}
	return fmt.Errorf("--until step %q not found in top-level workflow steps", until)
}

func resolveExecutionRoots(opts *Options) (workingDir, projectRoot string, err error) {
	workingDir = opts.WorkingDir
	if workingDir == "" {
		workingDir, err = os.Getwd()
		if err != nil {
			return "", "", fmt.Errorf("cannot determine working directory: %w", err)
		}
	}
	workingDir, err = canonicalDirectory(workingDir)
	if err != nil {
		return "", "", fmt.Errorf("resolve working directory: %w", err)
	}
	if opts.WorkflowScope != model.ScopeLegacy {
		workspace := opts.Workspace
		if workspace == nil {
			var workspaceErr error
			workspace, workspaceErr = prepareWorkspace(opts.WorkflowScope, workingDir, opts.ProfileStore)
			if workspaceErr != nil {
				return "", "", workspaceErr
			}
		}
		opts.Workspace = workspace
		return workspace.Dir, workspace.Dir, nil
	}
	if opts.ProjectRoot != "" {
		projectRoot, err = canonicalDirectory(opts.ProjectRoot)
		if err != nil {
			return "", "", fmt.Errorf("resolve project root: %w", err)
		}
		return workingDir, projectRoot, nil
	}
	projectRoot, err = discoverProjectRoot(opts.WorkflowFile, workingDir)
	if err != nil {
		return "", "", err
	}
	return workingDir, projectRoot, nil
}

// PrepareWorkspaceForLaunch establishes the scoped workspace before workflow
// prevalidation or engine setup. It is used by CLI launch wiring and can be
// passed through Options so PrepareRun does not repeat discovery.
func PrepareWorkspaceForLaunch(scope model.Scope, launchDir string) (*model.WorkspaceContext, error) {
	if scope == model.ScopeLegacy {
		return nil, nil
	}
	cfg, err := config.Load(filepath.Join(launchDir, ".agent-runner", "config.yaml"))
	if err != nil {
		return nil, fmt.Errorf("load workspace configuration: %w", err)
	}
	return prepareWorkspace(scope, launchDir, cfg)
}

// prepareWorkspace validates the immutable workspace/repository contract for
// a scope-aware run. Legacy workflows do not call this function: they retain
// their existing non-Git launch behavior.
func prepareWorkspace(scope model.Scope, launchDir string, cfg *config.Config) (*model.WorkspaceContext, error) {
	if scope == model.ScopeLegacy {
		return nil, nil
	}
	canonicalLaunch, err := canonicalDirectory(launchDir)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace directory: %w", err)
	}
	workspaceRoot, err := gitWorktreeRoot(canonicalLaunch)
	if err != nil {
		return nil, err
	}
	if workspaceRoot == "" {
		return nil, fmt.Errorf("scope-aware workflows must launch from a canonical workspace root of a Git worktree")
	}
	if canonicalLaunch != workspaceRoot {
		return nil, fmt.Errorf("scope-aware workflows must launch from the canonical workspace root %s, not %s", workspaceRoot, canonicalLaunch)
	}

	workspace := &model.WorkspaceContext{Dir: workspaceRoot, Repositories: map[string]model.Repository{}}
	if cfg == nil || len(cfg.Repositories) == 0 {
		workspace.Repositories["default"] = model.Repository{Name: "default", Dir: workspaceRoot}
		return workspace, nil
	}

	roots := make(map[string]string, len(cfg.Repositories))
	for name, declaration := range cfg.Repositories {
		if err := validateRepositoryName(name); err != nil {
			return nil, err
		}
		if declaration.Path == "" {
			return nil, fmt.Errorf("repository %q path is required", name)
		}
		declaredPath := declaration.Path
		if !filepath.IsAbs(declaredPath) {
			declaredPath = filepath.Join(workspaceRoot, declaredPath)
		}
		repositoryDir, err := canonicalDirectory(declaredPath)
		if err != nil {
			return nil, fmt.Errorf("repository %q path %q: %w", name, declaration.Path, err)
		}
		gitRoot, rootErr := gitWorktreeRoot(repositoryDir)
		if rootErr != nil {
			return nil, fmt.Errorf("repository %q path %q: %w", name, declaration.Path, rootErr)
		}
		if gitRoot == "" || gitRoot != repositoryDir {
			return nil, fmt.Errorf("repository %q path %q must resolve to a Git worktree root", name, declaration.Path)
		}
		if repositoryDir == workspaceRoot {
			return nil, fmt.Errorf("repository %q must not resolve to the workspace root", name)
		}
		if other, exists := roots[repositoryDir]; exists {
			return nil, fmt.Errorf("repositories %q and %q resolve to the same Git worktree root %s", other, name, repositoryDir)
		}
		roots[repositoryDir] = name
		workspace.Repositories[name] = model.Repository{Name: name, Dir: repositoryDir}
	}
	return workspace, nil
}

func validateRepositoryName(name string) error {
	switch {
	case name == "default":
		return fmt.Errorf("repository name %q is reserved", name)
	case len(name) > 63:
		return fmt.Errorf("repository name %q must be at most 63 characters", name)
	case !repositoryNameRe.MatchString(name):
		return fmt.Errorf("repository name %q must match %s", name, repositoryNameRe.String())
	}
	return nil
}

func discoverProjectRoot(workflowFile, workingDir string) (string, error) {
	workingDir, err := canonicalDirectory(workingDir)
	if err != nil {
		return "", fmt.Errorf("resolve project working directory: %w", err)
	}
	if root, ok := findRepositoryRoot(workingDir); ok {
		return root, nil
	}
	if workflowFile != "" && !builtinworkflows.IsRef(workflowFile) {
		workflowPath := workflowFile
		if !filepath.IsAbs(workflowPath) {
			workflowPath = filepath.Join(workingDir, workflowPath)
		}
		if root, ok := findRepositoryRoot(filepath.Dir(filepath.Clean(workflowPath))); ok {
			return root, nil
		}
	}
	return workingDir, nil
}

func findRepositoryRoot(start string) (string, bool) {
	if root, err := gitWorktreeRoot(start); err == nil && root != "" {
		return root, true
	}
	// Preserve the legacy discovery contract for callers and tests which only
	// need the historical project-root heuristic. Scope-aware preparation uses
	// gitWorktreeRoot directly and therefore never accepts this fallback.
	dir, err := canonicalDirectory(start)
	if err != nil {
		return "", false
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil { // #nosec G703 -- fixed marker beneath a canonical directory.
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func gitWorktreeRoot(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	cmd := osexec.Command("git", "-C", dir, "rev-parse", "--show-toplevel") // #nosec G204 -- canonical local directory is an argument, never a shell command.
	output, err := cmd.CombinedOutput()
	if err != nil {
		diagnostic := strings.TrimSpace(string(output))
		if strings.Contains(diagnostic, "not a git repository") {
			return "", nil
		}
		if diagnostic == "" {
			return "", fmt.Errorf("git rev-parse in %s: %w", dir, err)
		}
		return "", fmt.Errorf("git rev-parse in %s: %w: %s", dir, err, diagnostic)
	}
	root, err := canonicalDirectory(strings.TrimSpace(string(output)))
	if err != nil {
		return "", err
	}
	return root, nil
}

func canonicalDirectory(dirPath string) (string, error) {
	absolute, err := filepath.Abs(dirPath)
	if err != nil {
		return "", err
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(canonical) // #nosec G703 -- callers provide trusted run configuration paths.
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", dirPath)
	}
	return canonical, nil
}

func computeHash(workflowFile string) string {
	if workflowFile == "" {
		return ""
	}
	data, err := loader.ReadWorkflowFile(workflowFile)
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
		if ctx.ActiveRepository != nil {
			event = audit.WithRepository(event, ctx.ActiveRepository.Name, ctx.ActiveRepository.Dir)
		}
		ctx.AuditLogger.Emit(event)
	}
}

// runState holds the internal state needed during workflow execution.
type runState struct {
	workflow             model.Workflow
	ctx                  *model.ExecutionContext
	until                string
	untilLeavesRemaining bool
	sessionDir           string
	sessionID            string
	workflowHash         string
	auditLogger          *audit.Logger
	metricsCollector     *metrics.Collector
	runStartTime         time.Time
	log                  exec.Logger
	runner               exec.ProcessRunner
	glob                 exec.GlobExpander
	repositoryStartIndex int
	repositoryStartSet   bool
}

func initRunState(workflow *model.Workflow, params map[string]string, opts *Options) (*runState, error) {
	if params == nil {
		params = map[string]string{}
	}
	opts.WorkflowScope = workflow.Scope

	// Every run resolves a profile set so it can be validated, persisted, and
	// reported even when the workflow has no agent steps.
	if opts.ProfileStore == nil {
		cfg, err := config.LoadWithProfile(".agent-runner/config.yaml", opts.ProfileOverride)
		if err != nil {
			return nil, fmt.Errorf("loading agent profiles: %w", err)
		}
		opts.ProfileStore = cfg
	}

	workingDir, err := prepareScopeAndParams(workflow, params, opts)
	if err != nil {
		return nil, err
	}
	if opts.RepositoryFrame == nil && opts.Workspace != nil && len(opts.Workspace.Selected) > 0 {
		opts.RepositoryFrame = newRepositoryFrame(opts.Workspace)
	}

	if opts.Engine != nil {
		if err := opts.Engine.ValidateWorkflow(workflow, params, opts.WorkflowFile); err != nil {
			return nil, err
		}
	}

	sessionDir, sessionID, now, err := freshSessionLocation(workflow.Name, workingDir, opts)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(sessionDir, 0o750); err != nil {
		return nil, fmt.Errorf("create session dir: %w", err)
	}
	cleanupSession := newRunSessionCleanup(sessionDir, opts)
	if err := materializeBundledAssets(sessionDir, opts.WorkflowFile); err != nil {
		cleanupSession(nil)
		return nil, err
	}
	if err := writeRepositoryEvidenceIndex(sessionDir, opts.Workspace); err != nil {
		cleanupSession(nil)
		return nil, err
	}

	activePID, lockErr := runlock.Acquire(sessionDir)
	switch {
	case lockErr != nil:
		// Genuine I/O error inspecting or writing the lock — refuse rather
		// than risk a second runner racing the same state file.
		cleanupSession(nil)
		return nil, fmt.Errorf("acquire run lock in %s: %w", sessionDir, lockErr)
	case activePID > 0:
		return nil, fmt.Errorf("run already in progress (PID %d) in %s; wait for it to finish or kill the process before resuming", activePID, sessionDir)
	}
	if err := cleanupCrashedInteractiveAttempt(sessionDir, opts); err != nil {
		cleanupSession(nil)
		return nil, err
	}
	if err := acquireWorkspaceRepositoryLocks(opts.Workspace, sessionID); err != nil {
		cleanupSession(nil)
		return nil, err
	}

	if opts.SessionDir == "" {
		projectDir := filepath.Dir(filepath.Dir(sessionDir)) // parent of runs/
		writeMetaJSON(projectDir, workingDir)
	}

	auditLogger, metricsCollector, ctx, err := buildExecutionContext(workflow, params, opts, sessionDir, sessionID, now)
	if err != nil {
		cleanupSession(auditLogger)
		return nil, err
	}

	log := opts.Log
	if log == nil {
		log = &defaultLogger{}
	}

	// Merge the root workflow's session declarations into NamedSessionDecls.
	// On fresh runs this populates the map from scratch. On resume, previously
	// persisted entries are already present; mergeSessionDecls handles drift.
	if err := exec.MergeSessionDecls(ctx, workflow.Sessions, log); err != nil {
		cleanupSession(auditLogger)
		return nil, err
	}

	return &runState{
		workflow:             *workflow,
		ctx:                  ctx,
		until:                opts.Until,
		sessionDir:           sessionDir,
		sessionID:            sessionID,
		workflowHash:         computeHash(opts.WorkflowFile),
		auditLogger:          auditLogger,
		metricsCollector:     metricsCollector,
		runStartTime:         now,
		log:                  log,
		runner:               opts.ProcessRunner,
		glob:                 opts.GlobExpander,
		repositoryStartIndex: opts.RepositoryStartIndex,
		repositoryStartSet:   opts.RepositoryStartSet,
	}, nil
}

func acquireWorkspaceRepositoryLocks(workspace *model.WorkspaceContext, runID string) error {
	if workspace == nil || len(workspace.Selected) == 0 {
		return nil
	}
	targets := make([]repositorylock.Target, 0, len(workspace.Selected))
	for _, name := range workspace.Selected {
		repository, ok := workspace.Repositories[name]
		if !ok {
			return fmt.Errorf("selected repository %q is no longer configured", name)
		}
		targets = append(targets, repositorylock.Target{Root: repository.Dir, RunID: runID})
	}
	if err := repositorylock.AcquireAll(targets); err != nil {
		return fmt.Errorf("acquire selected repository locks: %w", err)
	}
	return nil
}

func newRepositoryFrame(workspace *model.WorkspaceContext) *model.RepositoryFrame {
	frame := &model.RepositoryFrame{Repositories: make([]model.RepositoryExecutionState, 0, len(workspace.Selected))}
	for _, name := range workspace.Selected {
		repository := workspace.Repositories[name]
		frame.Repositories = append(frame.Repositories, model.RepositoryExecutionState{
			Identity: model.RepositoryIdentity(repository),
			Status:   model.RepositoryPending,
		})
	}
	return frame
}

type repositoryEvidenceIndex struct {
	Repositories []repositoryEvidenceEntry `json:"repositories"`
}

type repositoryEvidenceEntry struct {
	Name      string `json:"name"`
	OutputDir string `json:"output_dir"`
}

// writeRepositoryEvidenceIndex establishes stable, workspace-readable output
// locations before any scoped body runs. The array preserves repository
// selection order; the transparent implicit repository deliberately keeps the
// historical output directory.
func writeRepositoryEvidenceIndex(sessionDir string, workspace *model.WorkspaceContext) error {
	if workspace == nil || len(workspace.Selected) == 0 {
		return nil
	}
	outputDir := filepath.Join(sessionDir, "output")
	index := repositoryEvidenceIndex{Repositories: make([]repositoryEvidenceEntry, 0, len(workspace.Selected))}
	for _, name := range workspace.Selected {
		repository, ok := workspace.Repositories[name]
		if !ok {
			return fmt.Errorf("selected repository %q is no longer configured", name)
		}
		repositoryOutputDir := outputDir
		if repository.Name != "default" {
			repositoryOutputDir = filepath.Join(outputDir, "repositories", repository.Name)
		}
		if err := os.MkdirAll(repositoryOutputDir, 0o750); err != nil {
			return fmt.Errorf("create repository output directory for %q: %w", repository.Name, err)
		}
		index.Repositories = append(index.Repositories, repositoryEvidenceEntry{Name: repository.Name, OutputDir: repositoryOutputDir})
	}
	if err := stateio.WriteJSONAtomic(filepath.Join(outputDir, "repository-evidence-index.json"), index); err != nil {
		return fmt.Errorf("write repository evidence index: %w", err)
	}
	return nil
}

func prepareScopeAndParams(workflow *model.Workflow, params map[string]string, opts *Options) (string, error) {
	workingDir, projectRoot, err := resolveExecutionRoots(opts)
	if err != nil {
		return "", err
	}
	opts.WorkingDir = workingDir
	opts.ProjectRoot = projectRoot
	if workflow.RequiresRepositoryTargets() && opts.Workspace != nil && len(opts.Workspace.Repositories) == 1 {
		if _, implicit := opts.Workspace.Repositories["default"]; implicit {
			if _, supplied := params[model.RepositoriesParam]; !supplied {
				params[model.RepositoriesParam] = "default"
			}
		}
	}
	if err := validateParams(workflow, params); err != nil {
		return "", err
	}
	if workflow.RequiresRepositoryTargets() && opts.Workspace != nil {
		selected, err := model.ParseRepositoryTargets(params[model.RepositoriesParam], opts.Workspace.Repositories)
		if err != nil {
			return "", err
		}
		opts.Workspace.Selected = selected
	}
	return workingDir, nil
}

func freshSessionLocation(workflowName, workingDir string, opts *Options) (sessionDir, sessionID string, now time.Time, err error) {
	now = time.Now()
	safeName := audit.SanitizeWorkflowName(workflowName)
	timestamp := strings.NewReplacer(":", "-", ".", "-").Replace(now.UTC().Format(time.RFC3339Nano))
	sessionID = safeName + "-" + timestamp
	sessionDir = opts.SessionDir
	if sessionDir != "" {
		return sessionDir, filepath.Base(sessionDir), now, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".agent-runner", "projects", audit.EncodePath(workingDir), "runs", sessionID), sessionID, now, nil
}

func newRunSessionCleanup(sessionDir string, opts *Options) func(*audit.Logger) {
	return func(auditLogger *audit.Logger) {
		runlock.Delete(sessionDir)
		if auditLogger != nil {
			auditLogger.Close()
		}
		// A caller-provided session directory may contain pre-existing state (for
		// example a test fixture or a resume-adjacent embedding), so only remove a
		// directory that this fresh run allocated itself.
		if opts.SessionDir == "" {
			_ = os.RemoveAll(sessionDir)
		}
	}
}

func cleanupCrashedInteractiveAttempt(sessionDir string, opts *Options) error {
	if opts.InteractiveAttempt == nil {
		return nil
	}
	metadata := interactive.ProcessMetadata{
		ChildPID: opts.InteractiveAttempt.ChildPID, PGID: opts.InteractiveAttempt.PGID,
		StartTime: opts.InteractiveAttempt.StartTime, Socket: opts.InteractiveAttempt.Socket,
	}
	if err := interactive.CleanupProcess(metadata, interactive.DefaultTerminationGrace); err != nil {
		return fmt.Errorf("clean up crashed interactive attempt: %w", err)
	}
	if metadata.Socket != "" {
		_ = os.Remove(metadata.Socket)
	}
	_ = os.Remove(filepath.Join(sessionDir, control.ControlSocketPointerFile))
	opts.InteractiveAttempt = nil
	return nil
}

func buildExecutionContext(
	workflow *model.Workflow,
	params map[string]string,
	opts *Options,
	sessionDir, sessionID string,
	runStart time.Time,
) (*audit.Logger, *metrics.Collector, *model.ExecutionContext, error) {
	var engineRef interface{}
	if opts.Engine != nil {
		engineRef = opts.Engine
	}

	auditLogger, auditErr := audit.NewLogger(filepath.Join(sessionDir, "audit.log"))
	var auditSink audit.EventLogger
	if auditErr == nil {
		auditSink = auditLogger
	} else {
		log := opts.Log
		if log == nil {
			log = &defaultLogger{}
		}
		log.Printf("agent-runner: warning: audit trail unavailable: %v\n", auditErr)
	}
	metricsCollector := metrics.NewCollector(sessionDir, sessionID, workflow.Name, runStart)
	auditEventLogger := metrics.NewPipeline(metricsCollector, auditSink)

	var profileStore any
	if opts.ProfileStore != nil {
		profileStore = opts.ProfileStore
	}

	settings, err := usersettings.Load()
	if err != nil {
		return auditLogger, metricsCollector, nil, err
	}
	var activeRepository *model.Repository
	var repositoryIndex *int
	if opts.RepositoryStartSet {
		index := opts.RepositoryStartIndex
		repositoryIndex = &index
	} else if workflow.Scope == model.ScopeRepositories && opts.Workspace != nil && len(opts.Workspace.Repositories) == 1 {
		if repository, ok := opts.Workspace.Repositories["default"]; ok {
			activeRepository = &repository
			index := 0
			repositoryIndex = &index
		}
	}

	ctx := model.NewRootContext(&model.RootContextOptions{
		Params:                   params,
		WorkflowFile:             opts.WorkflowFile,
		WorkflowName:             workflow.Name,
		WorkflowDescription:      workflow.Description,
		WorkflowScope:            workflow.Scope,
		ProjectRoot:              opts.ProjectRoot,
		WorkingDir:               opts.WorkingDir,
		Workspace:                opts.Workspace,
		ActiveRepository:         activeRepository,
		RepositoryIndex:          repositoryIndex,
		RepositoryFrame:          opts.RepositoryFrame,
		AutonomousBackend:        string(settings.AutonomousBackend),
		AutonomousPermissionMode: string(usersettings.EffectiveAutonomousPermissionMode(settings.AutonomousPermissionMode)),
		SessionDir:               sessionDir,
		IntakeHandoffContents:    opts.IntakeHandoffContents,
		IntakeParentRunID:        opts.IntakeParentRunID,
		AgentOverride:            opts.AgentOverride,
		EngineRef:                engineRef,
		ProfileStore:             profileStore,
		SessionIDs:               opts.SessionIDs,
		SessionProfiles:          opts.SessionProfiles,
		CapturedVariables:        opts.CapturedVariables,
		AuditLogger:              auditEventLogger,
		NamedSessions:            opts.NamedSessions,
		NamedSessionDecls:        opts.NamedSessionDecls,
		UIStepHandler:            opts.UIStepHandler,
	})
	if opts.ChildState != nil {
		ctx.ResumeChildState = opts.ChildState
	}
	if opts.LastSessionStepID != "" {
		ctx.LastSessionStepID = opts.LastSessionStepID
	}
	ctx.InteractiveAttempt = opts.InteractiveAttempt
	ctx.PullRequestCaptureState.RestorePullRequestURLs(opts.WorkspacePullRequestURL, opts.RepositoryPullRequestURLs)
	if opts.From != "" {
		ctx.WorkflowResumed = true
	}
	return auditLogger, metricsCollector, ctx, nil
}

func emitRunStart(rs *runState, opts *Options) {
	auditData := map[string]any{
		"workflow_file": opts.WorkflowFile,
		"workflow_name": rs.workflow.Name,
		"workflow_hash": rs.workflowHash,
		"context": map[string]any{
			"params":            rs.ctx.Params,
			"capturedVariables": capturedAuditMap(rs.ctx.CapturedVariables),
			"sessionIds":        rs.ctx.SessionIDs,
		},
	}
	if cfg, ok := rs.ctx.ProfileStore.(*config.Config); ok {
		auditData["profile_set"] = cfg.ResolvedProfile
		auditData["profile_source"] = auditProfileSource(cfg)
	}
	if opts.From != "" {
		auditData["resumed"] = true
		auditData["resume_from"] = opts.From
	}
	emitAudit(rs.ctx, audit.Event{
		Timestamp: rs.runStartTime.UTC().Format(time.RFC3339Nano),
		Type:      audit.EventRunStart,
		Data:      auditData,
	})
}

func auditProfileSource(cfg *config.Config) string {
	switch cfg.ProfileSource {
	case config.ProfileSourceConfig:
		return "config"
	case config.ProfileSourceDefault:
		return "default"
	case config.ProfileSourceOverride:
		switch cfg.ProfileOverrideOrigin {
		case config.OriginState:
			return "state"
		case config.OriginFlag:
			return "flag"
		default:
			return "flag"
		}
	default:
		return ""
	}
}

func executeSteps(rs *runState, startIndex int) WorkflowResult {
	for i := startIndex; i < len(rs.workflow.Steps); i++ {
		step := &rs.workflow.Steps[i]

		skip, skipErr := exec.ShouldSkipStep(step.SkipIf, rs.ctx.LastStepOutcome, rs.ctx, step.ID)
		if skipErr != nil {
			rs.log.Printf("\nagent-runner: step %q skip_if evaluation failed: %v\n", step.ID, skipErr)
			return ResultFailed
		}
		if skip {
			emitSkippedStep(rs, step, i)
			if step.ID == rs.until {
				writeStepState(step, rs.ctx, &rs.workflow, rs.workflowHash, rs.sessionDir, nil, true)
			}
			if stopAfterUntil(rs, step.ID, i) {
				return ResultSuccess
			}
			continue
		}

		// Fresh chain for each top-level step; writeStepState intentionally
		// does not clear it so the mid-step and post-step writes can share.
		resumeChild := rs.ctx.ResumeChildState
		rs.ctx.LastSubWorkflowChild = nil

		stepRef := step // capture for closure
		rs.ctx.FlushState = func() {
			writeStepState(stepRef, rs.ctx, &rs.workflow, rs.workflowHash, rs.sessionDir, nil, false)
		}

		outcome, loopResult, stepErr := runStep(step, rs)
		rs.ctx.FlushState = nil

		completed := stepErr == nil && outcome != exec.OutcomeAborted && outcome != exec.OutcomeFailed
		if !completed && rs.ctx.LastSubWorkflowChild == nil && resumeChild != nil {
			// A resume can fail before any child step starts, for example when
			// the persisted child ID no longer exists in a sub-workflow. Keep
			// the prior chain so this failed attempt does not erase the last
			// recoverable resume position.
			rs.ctx.LastSubWorkflowChild = resumeChild
		}
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
				if stopAfterUntil(rs, step.ID, i) {
					return ResultSuccess
				}
				continue
			}
			rs.log.Printf("\nagent-runner: step %q failed. Stopping.\n", step.ID)
			return ResultFailed
		}

		o := "success"
		rs.ctx.LastStepOutcome = &o
		if stopAfterUntil(rs, step.ID, i) {
			return ResultSuccess
		}
	}

	return ResultSuccess
}

func stopAfterUntil(rs *runState, stepID string, stepIndex int) bool {
	if rs.until == "" || stepID != rs.until {
		return false
	}
	rs.untilLeavesRemaining = stepIndex < len(rs.workflow.Steps)-1
	rs.log.Printf("agent-runner: stopped after step %q (--until).\n", stepID)
	return true
}

func emitSkippedStep(rs *runState, step *model.Step, index int) {
	prefix := audit.BuildPrefix(nestingToAuditInfo(rs.ctx), step.ID)
	emitAudit(rs.ctx, audit.Event{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Prefix:    prefix,
		Type:      audit.EventStepStart,
		Data:      map[string]any{"context": contextSnapshot(rs.ctx)},
	})
	emitAudit(rs.ctx, audit.Event{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Prefix:    prefix,
		Type:      audit.EventStepEnd,
		Data: map[string]any{
			"outcome": "skipped", "skip_if": step.SkipIf, "duration_ms": int64(0),
			metrics.DataIdentity:            repositoryExecutionIdentity(rs.ctx, step),
			metrics.DataUsage:               skippedStepUsage(step),
			metrics.DataEstimatedAPICostUSD: (*float64)(nil),
		},
	})
}

func metricsIdentityPrefix(ctx *model.ExecutionContext) string {
	parts := make([]string, 0, len(ctx.NestingPath)*2)
	if ctx.ActiveRepository != nil && ctx.ActiveRepository.Name != "default" {
		parts = append(parts, "repo:"+ctx.ActiveRepository.Name)
	}
	for _, segment := range ctx.NestingPath {
		stepID := segment.StepID
		if segment.Iteration != nil {
			stepID = fmt.Sprintf("%s:%d", stepID, *segment.Iteration)
		}
		if stepID != "" {
			parts = append(parts, stepID)
		}
		if segment.SubWorkflowName != "" {
			parts = append(parts, "sub:"+segment.SubWorkflowName)
		}
	}
	return strings.Join(parts, "/")
}

func repositoryExecutionIdentity(ctx *model.ExecutionContext, step *model.Step) model.ExecutionIdentity {
	identity := model.ExecutionIdentity{
		StepID: step.ID, Prefix: metricsIdentityPrefix(ctx), StepType: step.StepType(), Kind: "step", CLI: step.CLI,
		SessionStrategy: string(step.Session), AgentInvoked: false,
	}
	if ctx.ActiveRepository != nil && ctx.ActiveRepository.Name != "default" {
		identity.RepositoryName = ctx.ActiveRepository.Name
		identity.RepositoryDir = ctx.ActiveRepository.Dir
	}
	return identity
}

func skippedStepUsage(step *model.Step) model.UsageRecord {
	if step.StepType() == "agent" {
		return model.UsageRecord{
			Status: model.UsageUnavailable, Reason: model.UnavailableUnsupportedAdapter, CLI: step.CLI, Source: "agent-runner",
		}
	}
	return model.UsageRecord{
		Status: model.UsageCollected, Tokens: model.TokenCounts{}, Source: "agent-runner", Completeness: model.CompletenessComplete,
	}
}

func runStep(step *model.Step, rs *runState) (exec.StepOutcome, *exec.LoopResult, error) {
	if step.Scope == model.ScopeRepositories && rs.ctx.ActiveRepository == nil {
		var outcome exec.StepOutcome
		var loopResult *exec.LoopResult
		var executionErr error
		result := executeRepositoryFanout(rs, 0, func(_ int) WorkflowResult {
			outcome, loopResult, executionErr = runStepOnce(step, rs)
			if executionErr != nil || outcome == exec.OutcomeFailed || outcome == exec.OutcomeAborted {
				return ResultFailed
			}
			return ResultSuccess
		})
		if result != ResultSuccess && outcome == "" {
			outcome = exec.OutcomeFailed
		}
		return outcome, loopResult, executionErr
	}
	return runStepOnce(step, rs)
}

func runStepOnce(step *model.Step, rs *runState) (exec.StepOutcome, *exec.LoopResult, error) {
	if step.Loop != nil && len(step.Steps) > 0 {
		lr, err := exec.ExecuteLoopStep(step, rs.ctx, rs.runner, rs.glob, rs.log, exec.LoopExecuteOptions{})
		return exec.MapLoopOutcomeForRunner(step, lr.Outcome), &lr, err
	}
	outcome, err := exec.DispatchStep(step, rs.ctx, rs.runner, rs.glob, rs.log)
	return outcome, nil, err
}

func finalizeRun(rs *runState, result WorkflowResult) {
	if closer, ok := rs.ctx.Control.(interface{ Close() error }); ok && closer != nil {
		if err := closer.Close(); err != nil {
			rs.log.Printf("agent-runner: warning: close control endpoint: %v\n", err)
		}
	}
	runlock.Delete(rs.sessionDir)

	switch result {
	case ResultSuccess:
		if !rs.untilLeavesRemaining {
			if err := markStateCompleted(rs.sessionDir); err != nil {
				rs.log.Printf("agent-runner: warning: could not mark state completed: %v\n", err)
			}
		}
	case ResultFailed:
		rs.log.Printf("\nto resume: agent-runner --resume %s\n", rs.sessionID)
	}

	if rs.ctx.AuditLogger != nil {
		totals := rs.metricsCollector.Totals()
		emitAudit(rs.ctx, audit.Event{
			Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
			Type:      audit.EventRunEnd,
			Data: map[string]any{
				"outcome":          string(result),
				"duration_ms":      time.Since(rs.runStartTime).Milliseconds(),
				metrics.DataTotals: totals,
			},
		})
		for _, metricsErr := range rs.metricsCollector.Errors() {
			rs.log.Printf("agent-runner: warning: metrics: %v\n", metricsErr)
		}
	}
	if rs.auditLogger != nil {
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
	if err := validateUntilStep(workflow, opts.Until); err != nil {
		return nil, err
	}

	rs, err := initRunState(workflow, params, opts)
	if err != nil {
		return nil, err
	}
	startIndex, err := resolveStartIndex(workflow, opts.From)
	if err != nil {
		// initRunState already created the session dir, lock file, and audit
		// logger — release them so a failed prepare doesn't leave a ghost run.
		newRunSessionCleanup(rs.sessionDir, opts)(rs.auditLogger)
		return nil, err
	}

	// Seed state.json before execution starts so callers that read the run's
	// state concurrently (live run TUI, --inspect on a freshly-started run)
	// can resolve the workflow file immediately instead of falling back to
	// name-based discovery that does not know about .agent-runner/workflows/.
	if err := stateio.WriteState(initialRunState(workflow, rs, opts), rs.sessionDir); err != nil {
		newRunSessionCleanup(rs.sessionDir, opts)(rs.auditLogger)
		return nil, fmt.Errorf("seed initial state: %w", err)
	}

	emitRunStart(rs, opts)
	if cfg, ok := rs.ctx.ProfileStore.(*config.Config); ok {
		exec.EmitAgentDeprecations(rs.ctx, rs.log, cfg.Deprecations)
	}
	rs.log.Printf("\nagent-runner: running workflow %q\n\n", workflow.Name)

	projectDir := filepath.Dir(filepath.Dir(rs.sessionDir)) // parent of runs/
	return &RunHandle{
		rs:         rs,
		startIndex: startIndex,
		SessionDir: rs.sessionDir,
		ProjectDir: projectDir,
	}, nil
}

func initialRunState(workflow *model.Workflow, rs *runState, opts *Options) *model.RunState {
	stepID := strings.TrimSpace(opts.From)
	if stepID == "" && len(workflow.Steps) > 0 {
		stepID = workflow.Steps[0].ID
	}

	state := &model.RunState{
		RunID:                 rs.sessionID,
		WorkflowFile:          opts.WorkflowFile,
		WorkflowName:          workflow.Name,
		Params:                rs.ctx.Params,
		WorkflowHash:          rs.workflowHash,
		IntakeHandoffContents: rs.ctx.IntakeHandoffContents,
		IntakeParentRunID:     rs.ctx.IntakeParentRunID,
		AgentOverride:         rs.ctx.AgentOverride,
		ProfileSet:            resolvedProfileSet(rs.ctx),
		RepositoryIndex:       rs.ctx.RepositoryIndex,
	}
	persistRepositoryIdentity(state, rs.ctx)
	if stepID == "" {
		return state
	}

	state.CurrentStep = model.CurrentStep{
		Nested: &model.NestedStepState{
			StepID:             stepID,
			SessionIDs:         copyMap(rs.ctx.SessionIDs),
			SessionProfiles:    copyMap(rs.ctx.SessionProfiles),
			CapturedVariables:  copyCapturedMap(rs.ctx.CapturedVariables),
			LastSessionStepID:  rs.ctx.LastSessionStepID,
			NamedSessions:      copyMap(rs.ctx.NamedSessions),
			NamedSessionDecls:  copyMap(rs.ctx.NamedSessionDecls),
			Child:              opts.ChildState,
			InteractiveAttempt: opts.InteractiveAttempt,
		},
	}
	return state
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
		if opts.PrepareStepHook != nil {
			h.rs.ctx.PrepareStepHook = opts.PrepareStepHook
		}
		if opts.UIStepHandler != nil {
			h.rs.ctx.UIStepHandler = opts.UIStepHandler
		}
	}
	result := executeWorkflow(h.rs, h.startIndex)
	finalizeRun(h.rs, result)
	return result
}

func executeWorkflow(rs *runState, startIndex int) WorkflowResult {
	if rs.workflow.Scope == model.ScopeRepositories && rs.ctx.ActiveRepository == nil {
		return executeRepositoryFanout(rs, startIndex, func(repositoryStart int) WorkflowResult {
			return executeSteps(rs, repositoryStart)
		})
	}
	return executeSteps(rs, startIndex)
}

func executeRepositoryFanout(rs *runState, startIndex int, executeBody func(repositoryStart int) WorkflowResult) (result WorkflowResult) {
	if rs.ctx.Workspace == nil || len(rs.ctx.Workspace.Selected) == 0 {
		return ResultFailed
	}
	parent := rs.ctx
	previousRepositoryIndex := parent.RepositoryIndex
	defer func() {
		rs.ctx = parent
		rs.repositoryStartIndex = 0
		rs.repositoryStartSet = false
		if result == ResultSuccess {
			parent.RepositoryIndex = previousRepositoryIndex
		}
	}()
	resumeRepository := 0
	if rs.repositoryStartSet {
		resumeRepository = rs.repositoryStartIndex
	}
	for repositoryIndex, name := range parent.Workspace.Selected {
		if skipRepositoryExecution(parent, repositoryIndex, resumeRepository) {
			continue
		}
		if result := executeRepositoryTarget(rs, parent, repositoryIndex, name, startIndex, resumeRepository, executeBody); result != ResultSuccess {
			return result
		}
	}
	return ResultSuccess
}

func skipRepositoryExecution(parent *model.ExecutionContext, index, resumeIndex int) bool {
	if index < resumeIndex {
		return true
	}
	entry := repositoryEntry(parent.RepositoryFrame, index)
	return entry != nil && entry.Status == model.RepositoryCompleted
}

func executeRepositoryTarget(
	rs *runState,
	parent *model.ExecutionContext,
	index int,
	name string,
	startIndex int,
	resumeIndex int,
	executeBody func(repositoryStart int) WorkflowResult,
) WorkflowResult {
	repository, ok := parent.Workspace.Repositories[name]
	if !ok {
		rs.log.Printf("agent-runner: selected repository %q is no longer configured\n", name)
		return ResultFailed
	}
	activeIndex := index
	parent.RepositoryIndex = &activeIndex
	rs.ctx = repositoryExecutionContext(parent, repository, index)
	started := time.Now()
	if err := markRepositoryActive(rs, parent, index, name); err != nil {
		return ResultFailed
	}
	emitRepositoryStart(rs, repository, index, len(parent.Workspace.Selected), started)
	repositoryStart := 0
	if index == resumeIndex {
		repositoryStart = startIndex
	}
	result := executeBody(repositoryStart)
	emitRepositoryEnd(rs, repository, result, started)
	if result != ResultSuccess {
		markRepositoryFailed(rs, parent, index, name)
		return result
	}
	if err := markRepositoryCompleted(rs, parent, index, name); err != nil {
		return ResultFailed
	}
	return ResultSuccess
}

func markRepositoryActive(rs *runState, parent *model.ExecutionContext, index int, name string) error {
	entry := repositoryEntry(parent.RepositoryFrame, index)
	if entry == nil {
		return nil
	}
	entry.Status = model.RepositoryActive
	if err := persistRepositoryFrame(rs.sessionDir, parent); err != nil {
		rs.log.Printf("agent-runner: persist repository %q active state: %v\n", name, err)
		return err
	}
	return nil
}

func markRepositoryFailed(rs *runState, parent *model.ExecutionContext, index int, name string) {
	entry := repositoryEntry(parent.RepositoryFrame, index)
	if entry == nil {
		return
	}
	entry.Status = model.RepositoryFailed
	if err := persistRepositoryFrame(rs.sessionDir, parent); err != nil {
		rs.log.Printf("agent-runner: persist repository %q failed state: %v\n", name, err)
	}
}

func markRepositoryCompleted(rs *runState, parent *model.ExecutionContext, index int, name string) error {
	entry := repositoryEntry(parent.RepositoryFrame, index)
	if entry == nil {
		return nil
	}
	entry.Status = model.RepositoryCompleted
	if err := persistRepositoryFrame(rs.sessionDir, parent); err != nil {
		rs.log.Printf("agent-runner: persist repository %q completed state: %v\n", name, err)
		return err
	}
	return nil
}

func emitRepositoryStart(rs *runState, repository model.Repository, index, total int, started time.Time) {
	if repository.Name == "default" {
		return
	}
	emitAudit(rs.ctx, audit.Event{
		Timestamp: started.UTC().Format(time.RFC3339Nano), Prefix: "[repo:" + repository.Name + "]", Type: audit.EventRepositoryStart,
		Data: map[string]any{"position": index, "total": total, "context": contextSnapshot(rs.ctx)},
	})
}

func emitRepositoryEnd(rs *runState, repository model.Repository, result WorkflowResult, started time.Time) {
	if repository.Name == "default" {
		return
	}
	emitAudit(rs.ctx, audit.Event{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano), Prefix: "[repo:" + repository.Name + "]", Type: audit.EventRepositoryEnd,
		Data: map[string]any{"outcome": string(result), "duration_ms": time.Since(started).Milliseconds()},
	})
}

func repositoryExecutionContext(parent *model.ExecutionContext, repository model.Repository, index int) *model.ExecutionContext {
	return model.NewRepositoryExecutionContext(parent, repository, index)
}

func repositoryEntry(frame *model.RepositoryFrame, index int) *model.RepositoryExecutionState {
	if frame == nil || index < 0 || index >= len(frame.Repositories) {
		return nil
	}
	return &frame.Repositories[index]
}

func persistRepositoryFrame(stateDir string, ctx *model.ExecutionContext) error {
	state, err := stateio.ReadState(filepath.Join(stateDir, "state.json"))
	if err != nil {
		return fmt.Errorf("read state: %w", err)
	}
	persistRepositoryIdentity(&state, ctx)
	if err := stateio.WriteState(&state, stateDir); err != nil {
		return fmt.Errorf("write state: %w", err)
	}
	return nil
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
	// StepID matches the step we are writing for and iteration metadata is
	// present — otherwise the entry is genuinely a nested step whose ID may
	// happen to match its parent.
	//
	// Note: we intentionally do not clear ctx.LastSubWorkflowChild here. The
	// mid-step FlushState callback and the post-step write both read it, and
	// clearing would make the post-step write see an empty chain after the
	// mid-step flush consumed it. executeSteps resets the chain at the top of
	// the next iteration.
	switch {
	case ctx.LastSubWorkflowChild != nil &&
		ctx.LastSubWorkflowChild.StepID == step.ID &&
		ctx.LastSubWorkflowChild.Iteration != nil:
		iteration = ctx.LastSubWorkflowChild.Iteration
		child = ctx.LastSubWorkflowChild.Child
	case ctx.LastSubWorkflowChild != nil:
		child = ctx.LastSubWorkflowChild
	case loopResult != nil && loopResult.LastIteration >= 0:
		// Fallback: a loop step finished without writing iteration metadata
		// through the new channel (e.g. the mechanism was skipped because the
		// loop ran to exhaustion without any iteration). Record the last
		// completed iteration index so resume can start from the next one.
		next := loopResult.LastIteration + 1
		iteration = &next
	}

	nested := &model.NestedStepState{
		StepID:             step.ID,
		SessionIDs:         copyMap(ctx.SessionIDs),
		SessionProfiles:    copyMap(ctx.SessionProfiles),
		CapturedVariables:  copyCapturedMap(ctx.CapturedVariables),
		LastSessionStepID:  ctx.LastSessionStepID,
		NamedSessions:      copyMap(ctx.NamedSessions),
		NamedSessionDecls:  copyMap(ctx.NamedSessionDecls),
		Completed:          completed,
		Iteration:          iteration,
		Child:              child,
		InteractiveAttempt: ctx.InteractiveAttempt,
	}
	if ctx.ActiveRepository != nil && ctx.RepositoryIndex != nil {
		if entry := repositoryEntry(ctx.RepositoryFrame, *ctx.RepositoryIndex); entry != nil {
			entry.Nested = nested
			if completed {
				entry.Status = model.RepositoryActive
			}
		}
	}

	state := model.RunState{
		RunID:                 filepath.Base(stateDir),
		WorkflowFile:          ctx.WorkflowFile,
		WorkflowName:          workflow.Name,
		CurrentStep:           model.CurrentStep{Nested: nested},
		Params:                ctx.Params,
		WorkflowHash:          workflowHash,
		IntakeHandoffContents: ctx.IntakeHandoffContents,
		IntakeParentRunID:     ctx.IntakeParentRunID,
		AgentOverride:         ctx.AgentOverride,
		ProfileSet:            resolvedProfileSet(ctx),
		RepositoryIndex:       ctx.RepositoryIndex,
	}
	persistRepositoryIdentity(&state, ctx)
	_ = stateio.WriteState(&state, stateDir)
}

func persistRepositoryIdentity(state *model.RunState, ctx *model.ExecutionContext) {
	if ctx.PullRequestCaptureState != nil {
		state.WorkspacePullRequestURL, state.RepositoryPullRequestURLs = ctx.PullRequestCaptureState.PullRequestURLs()
	}
	if ctx.Workspace == nil || len(ctx.Workspace.Selected) == 0 {
		return
	}
	state.WorkspaceNamespace = workspaceNamespaceState(ctx)
	state.WorkspaceDir = ctx.Workspace.Dir
	state.SelectedRepositories = repositoryIdentities(ctx.Workspace)
	state.RepositoryFrame = ctx.RepositoryFrame
}

func workspaceNamespaceState(ctx *model.ExecutionContext) *model.NamespaceState {
	workspace := ctx
	for workspace.ParentContext != nil {
		workspace = workspace.ParentContext
	}
	return &model.NamespaceState{
		SessionIDs: copyMap(workspace.SessionIDs), SessionProfiles: copyMap(workspace.SessionProfiles),
		CapturedVariables: copyCapturedMap(workspace.CapturedVariables), LastSessionStepID: workspace.LastSessionStepID,
		NamedSessions: copyMap(workspace.NamedSessions), NamedSessionDecls: copyMap(workspace.NamedSessionDecls),
	}
}

func repositoryIdentities(workspace *model.WorkspaceContext) []model.RepositoryIdentity {
	identities := make([]model.RepositoryIdentity, 0, len(workspace.Selected))
	for _, name := range workspace.Selected {
		if repository, ok := workspace.Repositories[name]; ok {
			identities = append(identities, model.RepositoryIdentity(repository))
		}
	}
	return identities
}

func resolvedProfileSet(ctx *model.ExecutionContext) string {
	if cfg, ok := ctx.ProfileStore.(*config.Config); ok {
		return cfg.ResolvedProfile
	}
	return ""
}

func contextSnapshot(ctx *model.ExecutionContext) map[string]any {
	params := make(map[string]any)
	for k, v := range ctx.Params {
		params[k] = v
	}
	captured := make(map[string]any)
	for k, v := range ctx.CapturedVariables {
		captured[k] = v.AuditValue()
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

func copyCapturedMap(m map[string]model.CapturedValue) map[string]model.CapturedValue {
	result := make(map[string]model.CapturedValue, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}

func capturedAuditMap(m map[string]model.CapturedValue) map[string]any {
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[k] = v.AuditValue()
	}
	return result
}

func materializeBundledAssets(sessionDir, workflowFile string) error {
	if !builtinworkflows.IsRef(workflowFile) {
		return nil
	}
	rel, err := builtinworkflows.RefPath(workflowFile)
	if err != nil {
		return err
	}
	namespace, _, ok := strings.Cut(rel, "/")
	if !ok || namespace == "" {
		return fmt.Errorf("builtin workflow has no namespace: %s", rel)
	}
	root := filepath.Join(sessionDir, "bundled", namespace)
	marker := filepath.Join(root, ".complete")
	if _, err := os.Stat(marker); err == nil {
		return nil
	}
	assets, err := builtinworkflows.ListAssets(namespace)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return fmt.Errorf("create bundled asset root: %w", err)
	}
	for _, asset := range assets {
		data, err := builtinworkflows.ReadAsset(path.Join(namespace, asset))
		if err != nil {
			return err
		}
		target := filepath.Join(root, filepath.FromSlash(asset))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return fmt.Errorf("create bundled asset directory: %w", err)
		}
		mode := os.FileMode(0o600)
		if strings.HasSuffix(asset, ".sh") {
			mode = 0o700
		}
		if err := os.WriteFile(target, data, mode); err != nil {
			return fmt.Errorf("write bundled asset %s: %w", target, err)
		}
	}
	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		return fmt.Errorf("write bundled asset completion marker: %w", err)
	}
	return nil
}

type defaultLogger struct{}

func (l *defaultLogger) Println(args ...any)               { fmt.Println(args...) }
func (l *defaultLogger) Printf(format string, args ...any) { fmt.Printf(format, args...) }
func (l *defaultLogger) Errorf(format string, args ...any) { fmt.Fprintf(os.Stderr, format, args...) }

// DiscardLogger drops all log output. Used in TUI mode where the TUI surfaces
// workflow status instead of stdout.
type DiscardLogger struct{}

func (l *DiscardLogger) Println(_ ...any)          {}
func (l *DiscardLogger) Printf(_ string, _ ...any) {}
func (l *DiscardLogger) Errorf(_ string, _ ...any) {}

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
