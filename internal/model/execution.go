package model

// PropagateFailure copies c's failure record onto every ancestor context.
// The top-level runner classifies whatever failure record its root context
// carries, so a nested check's failure must be visible there before its
// scope's blocking return unwinds past the child context that recorded it.
func (c *ExecutionContext) PropagateFailure() {
	for p := c.ParentContext; p != nil; p = p.ParentContext {
		p.LastFailure = c.LastFailure
	}
}

// ClearInheritedFailure removes any failure record inherited from a nested
// scope on every ancestor context. Called when that scope recovers or
// continues past the failure via continue_on_failure/warn_on_failure, so a
// stale record is never classified as the run's terminal failure.
func (c *ExecutionContext) ClearInheritedFailure() {
	for p := c.ParentContext; p != nil; p = p.ParentContext {
		p.LastFailure = nil
	}
}

// LastAgentRef returns a copy of the guarded execution's identity reference
// for this scope, or nil when no agent has completed in it yet. State-chain
// writers use this to persist NestedStepState.LastAgent.
func (c *ExecutionContext) LastAgentRef() *ExecutionRef {
	if c == nil || c.LastAgentExecution == nil {
		return nil
	}
	ref := c.LastAgentExecution.Ref
	return &ref
}

// ExecutionRef identifies one execution by its audit prefix and attempt
// number. It is durable across interruption: the response it names can be
// rebuilt from the audit log by exact prefix and attempt.
type ExecutionRef struct {
	Prefix  string `json:"prefix" yaml:"prefix"`
	Attempt int    `json:"attempt" yaml:"attempt"`
}

// CallResponse is one recorded agent call's final response, labeled by its
// call identity.
type CallResponse struct {
	CallID   string `json:"callId" yaml:"callId"`
	Response string `json:"response" yaml:"response"`
}

// AgentExecutionRecord captures a completed agent step's identity, final
// response, and the final responses of any agent calls it made.
type AgentExecutionRecord struct {
	Ref           ExecutionRef   `json:"ref" yaml:"ref"`
	Response      string         `json:"response" yaml:"response"`
	CallResponses []CallResponse `json:"callResponses,omitempty" yaml:"callResponses,omitempty"`
}

// FailureRecord captures the evidence for one failed shell or script check:
// its own output, the most recent guarded agent execution in scope (if any),
// and any repair/blocked evidence attached by the repair executor.
type FailureRecord struct {
	StepID   string `json:"stepId"`
	Prefix   string `json:"prefix"`
	Attempt  int    `json:"attempt"`
	ExitCode int    `json:"exitCode"`
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
	// Guarded identifies the most recent agent execution earlier in the same
	// sequential scope, if one ran.
	Guarded *AgentExecutionRecord `json:"guarded,omitempty"`
	// Blocked and BlockedBy are set by the repair executor when a declaring
	// response reported REPAIR_BLOCKED; always false/empty here.
	Blocked   bool   `json:"blocked,omitempty"`
	BlockedBy string `json:"blockedBy,omitempty"`
	// RepairAttempts is the number of repair attempts that ran before this
	// failure was recorded terminal; always 0 here.
	RepairAttempts int `json:"repairAttempts,omitempty"`
}
