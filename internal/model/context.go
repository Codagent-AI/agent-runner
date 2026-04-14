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

// SubWorkflowChildState tracks child step progress for state persistence.
//
// For a loop step, Iteration carries the next iteration index to execute on
// resume (semantics match NestedStepState.Iteration).
type SubWorkflowChildState struct {
	StepID            string                 `json:"stepId"`
	SessionIDs        map[string]string      `json:"sessionIds"`
	SessionProfiles   map[string]string      `json:"sessionProfiles,omitempty"`
	CapturedVariables map[string]string      `json:"capturedVariables"`
	Completed         bool                   `json:"completed,omitempty"`
	Iteration         *int                   `json:"iteration,omitempty"`
	Child             *SubWorkflowChildState `json:"child,omitempty"`
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

	NestingPath   []NestingSegment
	ParentContext *ExecutionContext

	WorkflowFile        string
	WorkflowName        string
	WorkflowDescription string

	// EngineRef holds the workflow engine implementation (internal/engine.Engine).
	// Stored as interface{} to avoid circular imports.
	// Callers should type-assert to engine.Engine before use.
	EngineRef interface{}

	// ProfileStore holds the agent profile configuration (*config.Config).
	// Stored as interface{} to avoid circular imports (same pattern as EngineRef).
	ProfileStore interface{}

	// AuditLogger writes structured audit events (audit.EventLogger).
	AuditLogger audit.EventLogger

	LastSubWorkflowChild *SubWorkflowChildState
	ResumeChildState     *SubWorkflowChildState
	FlushState           func()

	// WorkflowResumed is true when the workflow was started via --resume.
	// It is consumed (cleared) after the first agent step uses it.
	WorkflowResumed bool
}

// RootContextOptions configures a new root execution context.
type RootContextOptions struct {
	Params              map[string]string
	WorkflowFile        string
	WorkflowName        string
	WorkflowDescription string
	EngineRef           interface{} // internal/engine.Engine
	ProfileStore        interface{} // *config.Config
	SessionIDs          map[string]string
	SessionProfiles     map[string]string
	CapturedVariables   map[string]string
	AuditLogger         audit.EventLogger
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
		EngineRef:           opts.EngineRef,
		ProfileStore:        opts.ProfileStore,
		AuditLogger:         opts.AuditLogger,
	}
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
		EngineRef:           parent.EngineRef,
		ProfileStore:        parent.ProfileStore,
		AuditLogger:         parent.AuditLogger,
		WorkflowResumed:     parent.WorkflowResumed,
		FlushState:          parent.FlushState,
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
		EngineRef:           engineRef,
		ProfileStore:        parent.ProfileStore,
		AuditLogger:         parent.AuditLogger,
		WorkflowResumed:     parent.WorkflowResumed,
		FlushState:          parent.FlushState,
	}
}
