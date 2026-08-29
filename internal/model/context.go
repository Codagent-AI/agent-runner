// Package model defines the core types for workflow execution.
package model

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/codagent/agent-runner/internal/audit"
)

// Repository is a stable logical repository identity paired with its canonical
// Git worktree root. It deliberately belongs to model so execution, engines,
// and persistence can share the contract without depending on one another.
type Repository struct {
	Name string `json:"name"`
	Dir  string `json:"dir"`
}

// WorkspaceContext is the immutable coordination-workspace identity for one
// run. Repositories is keyed by stable configured name; Selected retains the
// caller-supplied order and must never be derived by iterating that map.
type WorkspaceContext struct {
	Dir          string                `json:"dir"`
	Repositories map[string]Repository `json:"repositories,omitempty"`
	Selected     []string              `json:"selected,omitempty"`
}

// ParseRepositoryTargets validates the ordered public repositories control
// parameter without deriving any target from declaration order.
func ParseRepositoryTargets(value string, repositories map[string]Repository) ([]string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, fmt.Errorf("repositories parameter must name at least one repository")
	}
	parts := strings.Split(value, ",")
	targets := make([]string, 0, len(parts))
	seen := make(map[string]bool, len(parts))
	for _, part := range parts {
		name := strings.TrimSpace(part)
		if name == "" {
			return nil, fmt.Errorf("repositories parameter contains an empty repository name")
		}
		if seen[name] {
			return nil, fmt.Errorf("repositories parameter names repository %q more than once", name)
		}
		if _, exists := repositories[name]; !exists {
			return nil, fmt.Errorf("repositories parameter names unknown repository %q", name)
		}
		seen[name] = true
		targets = append(targets, name)
	}
	return targets, nil
}

// NestingSegment records one level of nesting in the execution path.
type NestingSegment struct {
	StepID          string            `json:"stepId"`
	Iteration       *int              `json:"iteration,omitempty"`
	LoopVar         map[string]string `json:"loopVar,omitempty"`
	SubWorkflowName string            `json:"subWorkflowName,omitempty"`
}

// AgentDeprecationState is shared by every execution context in one workflow
// run so concurrent branches can deduplicate legacy profile warnings safely.
type AgentDeprecationState struct {
	mu   sync.Mutex
	seen map[string]bool
}

// AgentOverride is a run-scoped CLI and model selection that takes precedence
// over both the workflow step and its resolved agent profile.
type AgentOverride struct {
	CLI   string `json:"cli,omitempty"`
	Model string `json:"model,omitempty"`
}

// NewAgentDeprecationState creates an empty run-scoped deprecation set.
func NewAgentDeprecationState() *AgentDeprecationState {
	return &AgentDeprecationState{seen: make(map[string]bool)}
}

// Mark records alias and reports whether this is its first occurrence.
func (s *AgentDeprecationState) Mark(alias string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seen[alias] {
		return false
	}
	s.seen[alias] = true
	return true
}

// PullRequestCaptureState tracks the most recently observed pull-request URL
// for one workflow run. All nested execution contexts share this state.
type PullRequestCaptureState struct {
	mu      sync.Mutex
	lastURL string
}

// NewPullRequestCaptureState creates empty run-scoped PR capture state.
func NewPullRequestCaptureState() *PullRequestCaptureState {
	return &PullRequestCaptureState{}
}

// Mark records url and reports whether it differs from the last observation.
func (s *PullRequestCaptureState) Mark(url string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if url == s.lastURL {
		return false
	}
	s.lastURL = url
	return true
}

// ExecutionContext carries state through workflow execution.
type ExecutionContext struct {
	Params            map[string]string
	SessionIDs        map[string]string
	SessionProfiles   map[string]string // maps session-originating step ID → profile name
	CapturedVariables map[string]CapturedValue
	LastStepOutcome   *string // nil, "success", or "failed"
	// PullRequestCaptureState suppresses duplicate PR audit observations for
	// the complete run. It is intentionally transient; captured variables
	// remain the durable resume mechanism.
	PullRequestCaptureState *PullRequestCaptureState

	// LastSessionStepID tracks the most recently stored session key
	// (Go maps are unordered, so we can't rely on insertion order).
	LastSessionStepID string

	// NamedSessions maps role name → session ID for named session references.
	// Shared by reference across all contexts in the execution tree so writes
	// from any child are immediately visible to parents and siblings.
	NamedSessions map[string]string
	// NamedSessionDecls maps role name → agent profile name, populated from
	// workflow sessions: declarations. Also shared by reference. On fresh runs
	// it is built from declarations; on --resume it is restored from persistence
	// (preserving the original agent for drift detection).
	NamedSessionDecls map[string]string

	NestingPath   []NestingSegment
	ParentContext *ExecutionContext

	WorkflowFile        string
	WorkflowName        string
	WorkflowDescription string
	WorkflowScope       Scope
	// ProjectRoot is the canonical repository/worktree boundary established
	// when the run starts. WorkingDir is the canonical launch directory used
	// to resolve relative step workdirs.
	ProjectRoot              string
	WorkingDir               string
	Workspace                *WorkspaceContext
	ActiveRepository         *Repository
	RepositoryIndex          *int
	AutonomousBackend        string
	AutonomousPermissionMode string

	// SessionDir is the absolute path of the run's session directory
	// (e.g. ~/.agent-runner/projects/<encoded-cwd>/runs/<run-id>). Exposed to
	// templates as {{session_dir}} via BuiltinVars so workflows can point
	// agents at per-run output files.
	SessionDir string

	// IntakeHandoffContents is the handoff text interpolated into prompts as
	// {{intake_handoff}} and automatically delivered to the first agent prompt.
	// It is sealed in the route request and persisted across resume.
	IntakeHandoffContents string
	// IntakeParentRunID identifies the intake run that launched this run. It is
	// empty for direct runs.
	IntakeParentRunID string
	// AgentOverride applies only within this workflow run. It is intentionally
	// not represented as a workflow parameter so independently launched runs do
	// not inherit it.
	AgentOverride *AgentOverride

	// EngineRef holds the workflow engine implementation (internal/engine.Engine).
	// Stored as interface{} to avoid circular imports.
	// Callers should type-assert to engine.Engine before use.
	EngineRef interface{}

	// ProfileStore holds the agent profile configuration (*config.Config).
	// Stored as interface{} to avoid circular imports (same pattern as EngineRef).
	ProfileStore interface{}

	// AuditLogger writes structured audit events (audit.EventLogger).
	AuditLogger audit.EventLogger
	// AgentDeprecations tracks legacy profile warnings already emitted in this
	// run. Child contexts share the state so aliases warn at most once.
	AgentDeprecations *AgentDeprecationState

	// Control is the lazily-created run-scoped control server. It is
	// intentionally opaque here to keep core model types independent of runtime
	// packages.
	Control            any
	InteractiveAttempt *InteractiveAttemptMetadata

	LastSubWorkflowChild *NestedStepState
	ResumeChildState     *NestedStepState
	FlushState           func()

	// WorkflowResumed is true when the workflow was started via --resume.
	// It is consumed (cleared) after the first agent step uses it.
	WorkflowResumed bool

	// SuspendHook is called just before an interactive step takes over the
	// terminal. Nil in non-TUI callers (tests, library use).
	SuspendHook func() error
	// ResumeHook is called immediately after an interactive step exits.
	// Nil in non-TUI callers.
	ResumeHook func() error

	// PrepareStepHook is called before each leaf step begins. The boolean
	// argument is true when the step will be interactive. Used by the TUI
	// coordinator to defer or suppress terminal restore between consecutive
	// interactive steps.
	PrepareStepHook func(interactive bool)

	UIStepHandler func(*UIStepRequest) (UIStepResult, error)
}

// RootContextOptions configures a new root execution context.
type RootContextOptions struct {
	Params                   map[string]string
	WorkflowFile             string
	WorkflowName             string
	WorkflowDescription      string
	WorkflowScope            Scope
	ProjectRoot              string
	WorkingDir               string
	Workspace                *WorkspaceContext
	ActiveRepository         *Repository
	RepositoryIndex          *int
	AutonomousBackend        string
	AutonomousPermissionMode string
	SessionDir               string
	IntakeHandoffContents    string
	IntakeParentRunID        string
	AgentOverride            *AgentOverride
	EngineRef                interface{} // internal/engine.Engine
	ProfileStore             interface{} // *config.Config
	SessionIDs               map[string]string
	SessionProfiles          map[string]string
	CapturedVariables        map[string]CapturedValue
	AuditLogger              audit.EventLogger
	NamedSessions            map[string]string
	NamedSessionDecls        map[string]string
	UIStepHandler            func(*UIStepRequest) (UIStepResult, error)
}

// NewRootContext creates a top-level execution context.
func NewRootContext(opts *RootContextOptions) *ExecutionContext {
	params := make(map[string]string)
	for k, v := range opts.Params {
		params[k] = v
	}

	sessionIDs := make(map[string]string)
	for k, v := range opts.SessionIDs {
		sessionIDs[k] = v
	}

	capturedVars := make(map[string]CapturedValue)
	for k, v := range opts.CapturedVariables {
		capturedVars[k] = v
	}

	sessionProfiles := make(map[string]string)
	for k, v := range opts.SessionProfiles {
		sessionProfiles[k] = v
	}

	namedSessions := make(map[string]string)
	for k, v := range opts.NamedSessions {
		namedSessions[k] = v
	}

	namedSessionDecls := make(map[string]string)
	for k, v := range opts.NamedSessionDecls {
		namedSessionDecls[k] = v
	}

	return &ExecutionContext{
		Params:                   params,
		SessionIDs:               sessionIDs,
		SessionProfiles:          sessionProfiles,
		CapturedVariables:        capturedVars,
		LastStepOutcome:          nil,
		NestingPath:              []NestingSegment{},
		ParentContext:            nil,
		WorkflowFile:             opts.WorkflowFile,
		WorkflowName:             opts.WorkflowName,
		WorkflowDescription:      opts.WorkflowDescription,
		WorkflowScope:            opts.WorkflowScope,
		ProjectRoot:              opts.ProjectRoot,
		WorkingDir:               opts.WorkingDir,
		Workspace:                opts.Workspace,
		ActiveRepository:         opts.ActiveRepository,
		RepositoryIndex:          opts.RepositoryIndex,
		AutonomousBackend:        opts.AutonomousBackend,
		AutonomousPermissionMode: opts.AutonomousPermissionMode,
		SessionDir:               opts.SessionDir,
		IntakeHandoffContents:    opts.IntakeHandoffContents,
		IntakeParentRunID:        opts.IntakeParentRunID,
		AgentOverride:            opts.AgentOverride,
		EngineRef:                opts.EngineRef,
		ProfileStore:             opts.ProfileStore,
		AuditLogger:              opts.AuditLogger,
		AgentDeprecations:        NewAgentDeprecationState(),
		PullRequestCaptureState:  NewPullRequestCaptureState(),
		NamedSessions:            namedSessions,
		NamedSessionDecls:        namedSessionDecls,
		UIStepHandler:            opts.UIStepHandler,
	}
}

// BuiltinVars returns the map of runner-provided template variables that are
// available in every interpolated string (prompts, commands, sub-workflow
// params, loop patterns). Only non-empty values are included so tests that
// construct contexts without a session dir do not accidentally expose an
// empty {{session_dir}}.
func (c *ExecutionContext) BuiltinVars() map[string]string {
	return c.BuiltinVarsForStep("")
}

// BuiltinVarsForStep returns the builtin template variables for the given step.
// Extends BuiltinVars with {{step_id}} set to the provided step ID.
func (c *ExecutionContext) BuiltinVarsForStep(stepID string) map[string]string {
	m := make(map[string]string)
	if c.SessionDir != "" {
		m["session_dir"] = c.SessionDir
	}
	if c.Workspace != nil && c.Workspace.Dir != "" {
		m["workspace_dir"] = c.Workspace.Dir
	} else if c.WorkingDir != "" {
		// Legacy contexts preserve their pre-scope launch directory.
		m["workspace_dir"] = c.WorkingDir
	}
	if c.ActiveRepository != nil {
		m["repository_name"] = c.ActiveRepository.Name
		m["repository_dir"] = c.ActiveRepository.Dir
		if c.SessionDir != "" {
			if c.ActiveRepository.Name == "default" {
				m["repository_output_dir"] = filepath.Join(c.SessionDir, "output")
			} else {
				m["repository_output_dir"] = filepath.Join(c.SessionDir, "output", "repositories", c.ActiveRepository.Name)
			}
		}
	}
	if stepID != "" {
		m["step_id"] = stepID
	}
	// Unlike the surrounding built-ins, this must be present even when empty:
	// direct runs must resolve {{intake_handoff}} rather than fail interpolation.
	// It carries the handoff text, not its path, so a consumer workflow receives
	// the context in its prompt instead of having to elect to read a file.
	m[IntakeHandoffVar] = c.IntakeHandoffContents
	return m
}

// LoopIterationOptions configures a new loop iteration context.
type LoopIterationOptions struct {
	StepID    string
	Iteration int
	LoopVar   map[string]string
}

// NewLoopIterationContext creates a child context for a loop iteration.
func NewLoopIterationContext(parent *ExecutionContext, opts LoopIterationOptions) *ExecutionContext {
	segment := NestingSegment{
		StepID:    opts.StepID,
		Iteration: &opts.Iteration,
		LoopVar:   opts.LoopVar,
	}

	params := make(map[string]string)
	for k, v := range parent.Params {
		params[k] = v
	}
	for k, v := range opts.LoopVar {
		params[k] = v
	}

	sessionIDs := make(map[string]string)
	if seed, ok := parent.SessionIDs["_seed"]; ok {
		sessionIDs["_seed"] = seed
	}

	nestingPath := make([]NestingSegment, len(parent.NestingPath)+1)
	copy(nestingPath, parent.NestingPath)
	nestingPath[len(parent.NestingPath)] = segment

	sessionProfiles := make(map[string]string)
	for k, v := range parent.SessionProfiles {
		sessionProfiles[k] = v
	}

	capturedVars := make(map[string]CapturedValue)
	for k, v := range parent.CapturedVariables {
		capturedVars[k] = v
	}

	return &ExecutionContext{
		Params:                   params,
		SessionIDs:               sessionIDs,
		SessionProfiles:          sessionProfiles,
		CapturedVariables:        capturedVars,
		LastStepOutcome:          nil,
		LastSessionStepID:        parent.LastSessionStepID,
		NestingPath:              nestingPath,
		ParentContext:            parent,
		WorkflowFile:             parent.WorkflowFile,
		WorkflowName:             parent.WorkflowName,
		WorkflowDescription:      parent.WorkflowDescription,
		WorkflowScope:            parent.WorkflowScope,
		ProjectRoot:              parent.ProjectRoot,
		WorkingDir:               parent.WorkingDir,
		Workspace:                parent.Workspace,
		ActiveRepository:         parent.ActiveRepository,
		RepositoryIndex:          parent.RepositoryIndex,
		AutonomousBackend:        parent.AutonomousBackend,
		AutonomousPermissionMode: parent.AutonomousPermissionMode,
		SessionDir:               parent.SessionDir,
		IntakeHandoffContents:    parent.IntakeHandoffContents,
		IntakeParentRunID:        parent.IntakeParentRunID,
		AgentOverride:            parent.AgentOverride,
		EngineRef:                parent.EngineRef,
		ProfileStore:             parent.ProfileStore,
		AuditLogger:              parent.AuditLogger,
		AgentDeprecations:        parent.AgentDeprecations,
		PullRequestCaptureState:  parent.PullRequestCaptureState,
		Control:                  parent.Control,
		InteractiveAttempt:       parent.InteractiveAttempt,
		WorkflowResumed:          parent.WorkflowResumed,
		FlushState:               parent.FlushState,
		SuspendHook:              parent.SuspendHook,
		ResumeHook:               parent.ResumeHook,
		PrepareStepHook:          parent.PrepareStepHook,
		UIStepHandler:            parent.UIStepHandler,
		// Named session maps are shared by reference so writes from any child
		// are immediately visible to parents and sibling sub-workflows.
		NamedSessions:     parent.NamedSessions,
		NamedSessionDecls: parent.NamedSessionDecls,
	}
}

// NewRepositoryExecutionContext creates the execution context used for one
// repository member of a scoped fan-out. The workspace run identity remains
// shared, while process roots and mutable per-repository execution state are
// isolated from sibling repositories.
func NewRepositoryExecutionContext(parent *ExecutionContext, repository Repository, index int) *ExecutionContext {
	child := *parent
	child.ParentContext = parent
	child.ProjectRoot = repository.Dir
	child.WorkingDir = repository.Dir
	child.ActiveRepository = &repository
	child.RepositoryIndex = &index
	child.SessionIDs = copyStringMap(parent.SessionIDs)
	child.SessionProfiles = copyStringMap(parent.SessionProfiles)
	child.CapturedVariables = copyCapturedValues(parent.CapturedVariables)
	return &child
}

func copyStringMap(source map[string]string) map[string]string {
	copy := make(map[string]string, len(source))
	for key, value := range source {
		copy[key] = value
	}
	return copy
}

func copyCapturedValues(source map[string]CapturedValue) map[string]CapturedValue {
	copy := make(map[string]CapturedValue, len(source))
	for key, value := range source {
		copy[key] = value
	}
	return copy
}

// SubWorkflowContextOptions configures a new sub-workflow context.
type SubWorkflowContextOptions struct {
	StepID          string
	Params          map[string]string
	WorkflowFile    string
	SubWorkflowName string
	WorkflowScope   Scope
	EngineRef       interface{} // internal/engine.Engine
	EngineSet       bool        // true if EngineRef was explicitly provided (even if nil)
}

// NewSubWorkflowContext creates a child context for a sub-workflow.
func NewSubWorkflowContext(parent *ExecutionContext, opts *SubWorkflowContextOptions) *ExecutionContext {
	segment := NestingSegment{
		StepID:          opts.StepID,
		SubWorkflowName: opts.SubWorkflowName,
	}

	params := make(map[string]string)
	for k, v := range opts.Params {
		params[k] = v
	}

	sessionIDs := make(map[string]string)
	if seed, ok := parent.SessionIDs["_seed"]; ok {
		sessionIDs["_seed"] = seed
	}

	nestingPath := make([]NestingSegment, len(parent.NestingPath)+1)
	copy(nestingPath, parent.NestingPath)
	nestingPath[len(parent.NestingPath)] = segment

	engineRef := parent.EngineRef
	if opts.EngineSet {
		engineRef = opts.EngineRef
	}

	sessionProfiles := make(map[string]string)
	for k, v := range parent.SessionProfiles {
		sessionProfiles[k] = v
	}

	capturedVars := make(map[string]CapturedValue)
	for k, v := range parent.CapturedVariables {
		capturedVars[k] = v
	}

	return &ExecutionContext{
		Params:                   params,
		SessionIDs:               sessionIDs,
		SessionProfiles:          sessionProfiles,
		CapturedVariables:        capturedVars,
		LastStepOutcome:          nil,
		LastSessionStepID:        parent.LastSessionStepID,
		NestingPath:              nestingPath,
		ParentContext:            parent,
		WorkflowFile:             opts.WorkflowFile,
		WorkflowName:             parent.WorkflowName,
		WorkflowDescription:      parent.WorkflowDescription,
		WorkflowScope:            opts.WorkflowScope,
		ProjectRoot:              parent.ProjectRoot,
		WorkingDir:               parent.WorkingDir,
		Workspace:                parent.Workspace,
		ActiveRepository:         parent.ActiveRepository,
		RepositoryIndex:          parent.RepositoryIndex,
		AutonomousBackend:        parent.AutonomousBackend,
		AutonomousPermissionMode: parent.AutonomousPermissionMode,
		SessionDir:               parent.SessionDir,
		IntakeHandoffContents:    parent.IntakeHandoffContents,
		IntakeParentRunID:        parent.IntakeParentRunID,
		AgentOverride:            parent.AgentOverride,
		EngineRef:                engineRef,
		ProfileStore:             parent.ProfileStore,
		AuditLogger:              parent.AuditLogger,
		AgentDeprecations:        parent.AgentDeprecations,
		PullRequestCaptureState:  parent.PullRequestCaptureState,
		Control:                  parent.Control,
		InteractiveAttempt:       parent.InteractiveAttempt,
		WorkflowResumed:          parent.WorkflowResumed,
		FlushState:               parent.FlushState,
		SuspendHook:              parent.SuspendHook,
		ResumeHook:               parent.ResumeHook,
		PrepareStepHook:          parent.PrepareStepHook,
		UIStepHandler:            parent.UIStepHandler,
		// Named session maps are shared by reference so writes from a child
		// sub-workflow are immediately visible to the parent and later siblings.
		NamedSessions:     parent.NamedSessions,
		NamedSessionDecls: parent.NamedSessionDecls,
	}
}
