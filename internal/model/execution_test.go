package model

import (
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestScopeIsolationForLastAgentExecution(t *testing.T) {
	parent := NewRootContext(&RootContextOptions{WorkflowFile: "test.yaml"})
	parent.LastAgentExecution = &AgentExecutionRecord{
		Ref:      ExecutionRef{Prefix: "[open-draft-pr]", Attempt: 1},
		Response: "opened the PR",
	}

	t.Run("loop iteration context starts with no guarded execution", func(t *testing.T) {
		child := NewLoopIterationContext(parent, LoopIterationOptions{StepID: "loop", Iteration: 0})
		if child.LastAgentExecution != nil {
			t.Fatalf("expected nil LastAgentExecution, got %+v", child.LastAgentExecution)
		}
	})

	t.Run("sub-workflow context starts with no guarded execution", func(t *testing.T) {
		child := NewSubWorkflowContext(parent, &SubWorkflowContextOptions{StepID: "sub", WorkflowFile: "child.yaml"})
		if child.LastAgentExecution != nil {
			t.Fatalf("expected nil LastAgentExecution, got %+v", child.LastAgentExecution)
		}
	})
}

func TestLastAgentRef(t *testing.T) {
	ctx := NewRootContext(&RootContextOptions{WorkflowFile: "test.yaml"})
	if ctx.LastAgentRef() != nil {
		t.Fatal("expected nil ref before any agent completes")
	}
	ctx.LastAgentExecution = &AgentExecutionRecord{Ref: ExecutionRef{Prefix: "[a]", Attempt: 1}}
	ref := ctx.LastAgentRef()
	if ref == nil || *ref != (ExecutionRef{Prefix: "[a]", Attempt: 1}) {
		t.Fatalf("ref = %+v, want {[a] 1}", ref)
	}
}

func TestAgentExecutionRecordJSONRoundTrip(t *testing.T) {
	record := AgentExecutionRecord{
		Ref:      ExecutionRef{Prefix: "[open-draft-pr]", Attempt: 1},
		Response: "opened the PR",
		CallResponses: []CallResponse{
			{CallID: "call-1", Response: "first child response"},
			{CallID: "call-2", Response: "second child response"},
		},
	}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded AgentExecutionRecord
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if diff := cmp.Diff(record, decoded); diff != "" {
		t.Fatalf("round-trip mismatch (-want +got):\n%s", diff)
	}
}

func TestFailureRecordJSONRoundTrip(t *testing.T) {
	record := FailureRecord{
		StepID: "verify-draft-pr", Prefix: "[verify-draft-pr]", Attempt: 1, ExitCode: 1,
		Stdout: "checked out", Stderr: "expected exactly one open pull request",
		Guarded: &AgentExecutionRecord{
			Ref:      ExecutionRef{Prefix: "[open-draft-pr]", Attempt: 1},
			Response: "opened the PR",
		},
		Blocked: true, BlockedBy: "push rejected: token lacks workflow scope", RepairAttempts: 2,
	}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded FailureRecord
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if diff := cmp.Diff(record, decoded); diff != "" {
		t.Fatalf("round-trip mismatch (-want +got):\n%s", diff)
	}
}

func TestRepairFrameAndPendingRewindJSONRoundTrip(t *testing.T) {
	frame := RepairFrame{
		CheckID: "verify-draft-pr", Form: "rerun", Target: "open-draft-pr", Phase: "repairing",
		Attempts: 1, Budget: 2,
		Guarded:       &ExecutionRef{Prefix: "[open-draft-pr]", Attempt: 1},
		RangeCaptures: []string{"pr_url"},
	}
	data, err := json.Marshal(frame)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded RepairFrame
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if diff := cmp.Diff(frame, decoded); diff != "" {
		t.Fatalf("round-trip mismatch (-want +got):\n%s", diff)
	}

	rewind := RewindRequest{Target: "open-draft-pr", CheckID: "verify-draft-pr"}
	data, err = json.Marshal(rewind)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decodedRewind RewindRequest
	if err := json.Unmarshal(data, &decodedRewind); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if diff := cmp.Diff(rewind, decodedRewind); diff != "" {
		t.Fatalf("round-trip mismatch (-want +got):\n%s", diff)
	}
}

func TestNestedStepStateLastAgentAndRepairJSONRoundTrip(t *testing.T) {
	state := NestedStepState{
		StepID:            "verify-draft-pr",
		SessionIDs:        map[string]string{},
		CapturedVariables: map[string]CapturedValue{},
		LastAgent:         &ExecutionRef{Prefix: "[open-draft-pr]", Attempt: 1},
		Repair: &RepairFrame{
			CheckID: "verify-draft-pr", Form: "rerun", Target: "open-draft-pr", Phase: "checking",
			Attempts: 0, Budget: 1,
		},
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded NestedStepState
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if diff := cmp.Diff(state, decoded); diff != "" {
		t.Fatalf("round-trip mismatch (-want +got):\n%s", diff)
	}
}
