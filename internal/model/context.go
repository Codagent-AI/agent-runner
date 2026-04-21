// Package model defines the core types for workflow execution.
package model

import (
	"github.com/codagent/agent-runner/internal/audit"
)

// NestingSegment records one level of nesting in the execution path.
type NestingSegment struct {
	StepID          string            `json:"stepId"`
	Iteration       *int              `json:"iteration,omitempty"`
	LoopVar         map[string]string `json:"loopVar,omitempty"`
	SubWorkflowName string            `json:"subWorkflowName,omitempty"`
}

// ExecutionContext carries state through workflow execution.
type ExecutionContext struct {
	Params            map[string]string
	SessionIDs        map[string]string
	SessionProfiles   map[string]string // maps session-originating step ID → profile name
	CapturedVariables map[string]string
	LastStepOutcome   *string // nil, "success", or "failed"

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

	// SessionDir is the absolute path of the run's session directory
	// (e.g. ~/.agent-runner/projects/<encoded-cwd>/runs/<run-id>). Exposed to
	// templates as {{session_dir}} via BuiltinVars so workflows can point
	// agents at per-run output files.
	SessionDir string

	// EngineRef holds the workflow engine implementation (internal/engine.Engine).
	// Stored as interface{} to avoid circular imports.
	// Callers should type-assert to engine.Engine before use.
	EngineRef interface{}

	// ProfileStore holds the agent profile configuration (*config.Config).
	// Stored as interface{} to avoid circular imports (same pattern as EngineRef).
	ProfileStore interface{}

	// AuditLogger writes structured audit events (audit.EventLogger).
	AuditLogger audit.EventLogger

	LastSubWorkflowChild *NestedStepState
	ResumeChildState     *NestedStepState
	FlushState           func()

	// WorkflowResumed is true when the workflow was started via --resume.
	// It is consumed (cleared) after the first agent step uses it.
	WorkflowResumed bool

	// SuspendHook is called just before an interactive step takes over the
	// terminal. Nil in non-TUI callers (tests, library use).
	SuspendHook func()
	// ResumeHook is called immediately after an interactive step exits.
	// Nil in non-TUI callers.
	ResumeHook func()
}

// RootContextOptions configures a new root execution context.
type RootContextOptions struct {
	Params              map[string]string
	WorkflowFile        string
	WorkflowName        string
	WorkflowDescription string
	SessionDir          string
	EngineRef           interface{} // internal/engine.Engine
	ProfileStore        interface{} // *config.Config
	SessionIDs          map[string]string
	SessionProfiles     map[string]string
	CapturedVariables   map[string]string
	AuditLogger         audit.EventLogger
	NamedSessions       map[string]string
	NamedSessionDecls   map[string]string
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

	capturedVars := make(map[string]string)
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
		Params:              params,
		SessionIDs:          sessionIDs,
		SessionProfiles:     sessionProfiles,
		CapturedVariables:   capturedVars,
		LastStepOutcome:     nil,
		NestingPath:         []NestingSegment{},
		ParentContext:       nil,
		WorkflowFile:        opts.WorkflowFile,
		WorkflowName:        opts.WorkflowName,
		WorkflowDescription: opts.WorkflowDescription,
		SessionDir:          opts.SessionDir,
		EngineRef:           opts.EngineRef,
		ProfileStore:        opts.ProfileStore,
		AuditLogger:         opts.AuditLogger,
		NamedSessions:       namedSessions,
		NamedSessionDecls:   namedSessionDecls,
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
	if stepID != "" {
		m["step_id"] = stepID
	}
	if len(m) == 0 {
		return nil
	}
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

	return &ExecutionContext{
		Params:              params,
		SessionIDs:          sessionIDs,
		SessionProfiles:     sessionProfiles,
		CapturedVariables:   make(map[string]string),
		LastStepOutcome:     nil,
		LastSessionStepID:   parent.LastSessionStepID,
		NestingPath:         nestingPath,
		ParentContext:       parent,
		WorkflowFile:        parent.WorkflowFile,
		WorkflowName:        parent.WorkflowName,
		WorkflowDescription: parent.WorkflowDescription,
		SessionDir:          parent.SessionDir,
		EngineRef:           parent.EngineRef,
		ProfileStore:        parent.ProfileStore,
		AuditLogger:         parent.AuditLogger,
		WorkflowResumed:     parent.WorkflowResumed,
		FlushState:          parent.FlushState,
		SuspendHook:         parent.SuspendHook,
		ResumeHook:          parent.ResumeHook,
		// Named session maps are shared by reference so writes from any child
		// are immediately visible to parents and sibling sub-workflows.
		NamedSessions:     parent.NamedSessions,
		NamedSessionDecls: parent.NamedSessionDecls,
	}
}

// SubWorkflowContextOptions configures a new sub-workflow context.
type SubWorkflowContextOptions struct {
	StepID          string
	Params          map[string]string
	WorkflowFile    string
	SubWorkflowName string
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

	return &ExecutionContext{
		Params:              params,
		SessionIDs:          sessionIDs,
		SessionProfiles:     sessionProfiles,
		CapturedVariables:   make(map[string]string),
		LastStepOutcome:     nil,
		LastSessionStepID:   parent.LastSessionStepID,
		NestingPath:         nestingPath,
		ParentContext:       parent,
		WorkflowFile:        opts.WorkflowFile,
		WorkflowName:        parent.WorkflowName,
		WorkflowDescription: parent.WorkflowDescription,
		SessionDir:          parent.SessionDir,
		EngineRef:           engineRef,
		ProfileStore:        parent.ProfileStore,
		AuditLogger:         parent.AuditLogger,
		WorkflowResumed:     parent.WorkflowResumed,
		FlushState:          parent.FlushState,
		SuspendHook:         parent.SuspendHook,
		ResumeHook:          parent.ResumeHook,
		// Named session maps are shared by reference so writes from a child
		// sub-workflow are immediately visible to the parent and later siblings.
		NamedSessions:     parent.NamedSessions,
		NamedSessionDecls: parent.NamedSessionDecls,
	}
}
