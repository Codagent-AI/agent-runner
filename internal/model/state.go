package model

import (
	"encoding/json"
	"fmt"
)

// NestedStepState tracks execution position within nested workflows/loops.
//
// For a loop step, Iteration is the next iteration index to execute on resume.
// At iteration N start it is set to N; when iteration N completes it advances
// to N+1. When the loop finishes, Iteration equals the total count and
// Completed is true.
type NestedStepState struct {
	StepID            string                   `json:"stepId"`
	SessionIDs        map[string]string        `json:"sessionIds"`
	SessionProfiles   map[string]string        `json:"sessionProfiles,omitempty"`
	CapturedVariables map[string]CapturedValue `json:"capturedVariables"`
	LastSessionStepID string                   `json:"lastSessionStepId,omitempty"`
	// NamedSessions and NamedSessionDecls are only meaningful at the root
	// NestedStepState level (written by runner.writeStepState). Nested entries
	// produced by sub-workflow and loop progress records leave these nil.
	NamedSessions      map[string]string           `json:"namedSessions,omitempty"`
	NamedSessionDecls  map[string]string           `json:"namedSessionDecls,omitempty"`
	Completed          bool                        `json:"completed,omitempty"`
	Iteration          *int                        `json:"iteration,omitempty"`
	Child              *NestedStepState            `json:"child"`
	InteractiveAttempt *InteractiveAttemptMetadata `json:"interactiveAttempt,omitempty"`
}

type InteractiveAttemptMetadata struct {
	ChildPID  int    `json:"child_pid"`
	PGID      int    `json:"pgid"`
	StartTime string `json:"start_time"`
	Socket    string `json:"socket_path"`
}

// CurrentStep can be either a plain string (legacy) or a NestedStepState.
type CurrentStep struct {
	StepID string           // Set when the value is a plain string.
	Nested *NestedStepState // Set when the value is an object.
}

// MarshalJSON encodes CurrentStep as either a string or an object.
func (cs CurrentStep) MarshalJSON() ([]byte, error) {
	if cs.Nested != nil {
		return json.Marshal(cs.Nested)
	}
	return json.Marshal(cs.StepID)
}

// UnmarshalJSON decodes CurrentStep from either a string or an object.
func (cs *CurrentStep) UnmarshalJSON(data []byte) error {
	// Try string first.
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		cs.StepID = s
		cs.Nested = nil
		return nil
	}

	// Try object.
	var ns NestedStepState
	if err := json.Unmarshal(data, &ns); err != nil {
		return err
	}
	cs.Nested = &ns
	cs.StepID = ""
	return nil
}

// RunState is the serialized workflow execution state.
type RunState struct {
	RunID        string            `json:"runId,omitempty"`
	WorkflowFile string            `json:"workflowFile"`
	WorkflowName string            `json:"workflowName"`
	CurrentStep  CurrentStep       `json:"currentStep"`
	Params       map[string]string `json:"params"`
	WorkflowHash string            `json:"workflowHash"`
	ProfileSet   string            `json:"profileSet,omitempty"`
	// IntakeHandoffContents and IntakeParentRunID are immutable provenance seeded
	// from a frozen intake route and restored unchanged on resume.
	IntakeHandoffContents  string         `json:"intakeHandoffContents,omitempty"`
	IntakeHandoffDelivered bool           `json:"intakeHandoffDelivered,omitempty"`
	IntakeParentRunID      string         `json:"intakeParentRunId,omitempty"`
	AgentOverride          *AgentOverride `json:"agentOverride,omitempty"`
	// Completed is set to true when the workflow has finished successfully.
	// The state file is preserved (not deleted) so the TUI can still display
	// the run's metadata after completion.
	Completed bool `json:"completed,omitempty"`
	// WarningCount is the number of explicit terminal warning origins recorded
	// by a successfully completed workflow.
	WarningCount int `json:"warningCount,omitempty"`
	// RunKind identifies special persisted runs without changing how ordinary
	// callers load them. Empty means an ordinary workflow execution.
	RunKind string `json:"runKind,omitempty"`
	// Audit records reciprocal audit linkage. It is intentionally data-only so
	// untagged binaries can safely list and inspect development audit history.
	Audit *AuditMetadata `json:"audit,omitempty"`
}

// AuditMetadata is the persisted, append-only linkage between a source run
// execution session and its separately-owned audit run.
type AuditMetadata struct {
	SourceRunID     string      `json:"sourceRunId,omitempty"`
	SourceSessionID string      `json:"sourceExecutionSessionId,omitempty"`
	Trigger         string      `json:"trigger,omitempty"`
	Links           []AuditLink `json:"links,omitempty"`
	LifecycleState  string      `json:"lifecycleState,omitempty"`
	Warning         string      `json:"warning,omitempty"`
}

// AuditLink is a source-side reservation or a reciprocal audit-side link.
type AuditLink struct {
	AuditRunID         string `json:"auditRunId"`
	ExecutionSessionID string `json:"executionSessionId"`
	Trigger            string `json:"trigger"`
	State              string `json:"state"`
	SnapshotPath       string `json:"snapshotPath,omitempty"`
	RequestedAt        string `json:"requestedAt,omitempty"`
	StartedAt          string `json:"startedAt,omitempty"`
	FailedAt           string `json:"failedAt,omitempty"`
	Warning            string `json:"warning,omitempty"`
}

// ResolveResumeStepResult holds the outcome of resolving which step to resume from.
type ResolveResumeStepResult struct {
	StepID  string // The step ID to resume from (empty if all steps completed).
	AllDone bool   // True when the recorded step was the last step and it completed.
}

// ResolveResumeStep determines which step to actually start executing on resume.
// If the recorded step completed successfully, it advances to the next step.
// If the recorded step did not complete, it returns that step (to re-run it).
func ResolveResumeStep(steps []Step, recordedStepID string, completed bool) (ResolveResumeStepResult, error) {
	for i := range steps {
		if steps[i].ID == recordedStepID {
			if completed {
				if i+1 < len(steps) {
					return ResolveResumeStepResult{StepID: steps[i+1].ID}, nil
				}
				return ResolveResumeStepResult{AllDone: true}, nil
			}
			return ResolveResumeStepResult{StepID: recordedStepID}, nil
		}
	}
	return ResolveResumeStepResult{}, fmt.Errorf("step %q not found", recordedStepID)
}
