package model

// NestingSegment records one level of nesting in the execution path.
type NestingSegment struct {
	StepID          string            `json:"stepId"`
	Iteration       *int              `json:"iteration,omitempty"`
	LoopVar         map[string]string `json:"loopVar,omitempty"`
	SubWorkflowName string            `json:"subWorkflowName,omitempty"`
}

// SubWorkflowChildState tracks child step progress for state persistence.
type SubWorkflowChildState struct {
	StepID            string                 `json:"stepId"`
	SessionIDs        map[string]string      `json:"sessionIds"`
	CapturedVariables map[string]string      `json:"capturedVariables"`
	Child             *SubWorkflowChildState `json:"child,omitempty"`
}

// AuditEmitter is satisfied by the audit logger (avoids import cycle with audit package).
type AuditEmitter interface{}

// Engine is satisfied by an engine implementation (avoids import cycle with engine package).
type Engine interface{}

// ExecutionContext carries state through workflow execution.
type ExecutionContext struct {
	Params            map[string]string
	SessionIDs        map[string]string
	CapturedVariables map[string]string
	LastStepOutcome   *string // nil, "success", or "failed"

	// LastSessionStepID tracks the most recently stored session key
	// (Go maps are unordered, so we can't rely on insertion order).
	LastSessionStepID string

	NestingPath   []NestingSegment
	ParentContext *ExecutionContext

	WorkflowFile string
	EngineRef    Engine
	AuditLogger  AuditEmitter

	LastSubWorkflowChild *SubWorkflowChildState
	ResumeChildState     *SubWorkflowChildState
	FlushState           func()
}

// RootContextOptions configures a new root execution context.
type RootContextOptions struct {
	Params            map[string]string
	WorkflowFile      string
	EngineRef         Engine
	SessionIDs        map[string]string
	CapturedVariables map[string]string
	AuditLogger       AuditEmitter
}

// NewRootContext creates a top-level execution context.
func NewRootContext(opts RootContextOptions) *ExecutionContext {
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

	return &ExecutionContext{
		Params:            params,
		SessionIDs:        sessionIDs,
		CapturedVariables: capturedVars,
		LastStepOutcome:   nil,
		NestingPath:       []NestingSegment{},
		ParentContext:     nil,
		WorkflowFile:      opts.WorkflowFile,
		EngineRef:         opts.EngineRef,
		AuditLogger:       opts.AuditLogger,
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

	return &ExecutionContext{
		Params:            params,
		SessionIDs:        sessionIDs,
		CapturedVariables: make(map[string]string),
		LastStepOutcome:   nil,
		NestingPath:       nestingPath,
		ParentContext:     parent,
		WorkflowFile:      parent.WorkflowFile,
		EngineRef:         parent.EngineRef,
		AuditLogger:       parent.AuditLogger,
	}
}

// SubWorkflowContextOptions configures a new sub-workflow context.
type SubWorkflowContextOptions struct {
	StepID          string
	Params          map[string]string
	WorkflowFile    string
	SubWorkflowName string
	EngineRef       Engine
	EngineSet       bool // true if EngineRef was explicitly provided (even if nil)
}

// NewSubWorkflowContext creates a child context for a sub-workflow.
func NewSubWorkflowContext(parent *ExecutionContext, opts SubWorkflowContextOptions) *ExecutionContext {
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

	return &ExecutionContext{
		Params:            params,
		SessionIDs:        sessionIDs,
		CapturedVariables: make(map[string]string),
		LastStepOutcome:   nil,
		NestingPath:       nestingPath,
		ParentContext:     parent,
		WorkflowFile:      opts.WorkflowFile,
		EngineRef:         engineRef,
		AuditLogger:       parent.AuditLogger,
	}
}
