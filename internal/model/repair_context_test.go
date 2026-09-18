package model

import "testing"

func TestNewRepairAttemptContext(t *testing.T) {
	t.Run("appends a checkID/attempt nesting segment", func(t *testing.T) {
		owner := NewRootContext(&RootContextOptions{WorkflowFile: "test.yaml"})
		owner.NestingPath = []NestingSegment{{StepID: "outer-loop", Iteration: intPtr(1)}}

		attemptCtx := NewRepairAttemptContext(owner, "verify-draft-pr", 1)

		if len(attemptCtx.NestingPath) != 2 {
			t.Fatalf("expected 2 nesting segments, got %d", len(attemptCtx.NestingPath))
		}
		seg := attemptCtx.NestingPath[1]
		if seg.StepID != "verify-draft-pr" {
			t.Fatalf("expected checkID segment, got %q", seg.StepID)
		}
		if seg.RepairAttempt == nil || *seg.RepairAttempt != 1 {
			t.Fatalf("expected repair attempt 1, got %v", seg.RepairAttempt)
		}
	})

	t.Run("shares session and capture state by reference with the owner", func(t *testing.T) {
		owner := NewRootContext(&RootContextOptions{WorkflowFile: "test.yaml"})
		owner.SessionIDs["role"] = "sess-1"
		owner.LastSessionStepID = "open-draft-pr"

		attemptCtx := NewRepairAttemptContext(owner, "verify-draft-pr", 1)

		attemptCtx.SessionIDs["role"] = "sess-2"
		if owner.SessionIDs["role"] != "sess-2" {
			t.Fatal("expected SessionIDs to be shared by reference")
		}
		attemptCtx.CapturedVariables["x"] = NewCapturedString("y")
		if _, ok := owner.CapturedVariables["x"]; !ok {
			t.Fatal("expected CapturedVariables to be shared by reference")
		}
		if attemptCtx.LastSessionStepID != "open-draft-pr" {
			t.Fatal("expected LastSessionStepID inherited from owner")
		}
	})

	t.Run("starts with a nil LastAgentExecution", func(t *testing.T) {
		owner := NewRootContext(&RootContextOptions{WorkflowFile: "test.yaml"})
		owner.LastAgentExecution = &AgentExecutionRecord{Response: "prior response"}

		attemptCtx := NewRepairAttemptContext(owner, "verify-draft-pr", 1)

		if attemptCtx.LastAgentExecution != nil {
			t.Fatal("expected nil LastAgentExecution in a fresh repair attempt context")
		}
	})

	t.Run("sets ParentContext to owner", func(t *testing.T) {
		owner := NewRootContext(&RootContextOptions{WorkflowFile: "test.yaml"})
		attemptCtx := NewRepairAttemptContext(owner, "verify-draft-pr", 1)
		if attemptCtx.ParentContext != owner {
			t.Fatal("expected ParentContext to be owner")
		}
	})
}
