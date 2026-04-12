package model

import (
	"testing"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/google/go-cmp/cmp"
)

type stubAuditLogger struct{}

func (s *stubAuditLogger) Emit(event audit.Event) {} // implements audit.EventLogger

func TestCreateRootContext(t *testing.T) {
	t.Run("creates a context with provided params", func(t *testing.T) {
		ctx := NewRootContext(&RootContextOptions{
			Params:       map[string]string{"file": "main.go"},
			WorkflowFile: "test.yaml",
		})

		if ctx.Params["file"] != "main.go" {
			t.Fatalf("expected param 'file'='main.go', got %q", ctx.Params["file"])
		}
		if len(ctx.SessionIDs) != 0 {
			t.Fatal("expected empty sessionIDs")
		}
		if len(ctx.CapturedVariables) != 0 {
			t.Fatal("expected empty capturedVariables")
		}
		if ctx.LastStepOutcome != nil {
			t.Fatal("expected nil lastStepOutcome")
		}
		if ctx.ParentContext != nil {
			t.Fatal("expected nil parentContext")
		}
	})

	t.Run("restores session IDs from options", func(t *testing.T) {
		ctx := NewRootContext(&RootContextOptions{
			Params:       map[string]string{},
			WorkflowFile: "test.yaml",
			SessionIDs:   map[string]string{"step1": "abc123"},
		})

		if ctx.SessionIDs["step1"] != "abc123" {
			t.Fatalf("expected sessionID 'step1'='abc123', got %q", ctx.SessionIDs["step1"])
		}
	})

	t.Run("restores captured variables from options", func(t *testing.T) {
		ctx := NewRootContext(&RootContextOptions{
			Params:            map[string]string{},
			WorkflowFile:      "test.yaml",
			CapturedVariables: map[string]string{"output": "result"},
		})

		if ctx.CapturedVariables["output"] != "result" {
			t.Fatalf("expected capturedVar 'output'='result', got %q", ctx.CapturedVariables["output"])
		}
	})
}

func TestCreateLoopIterationContext(t *testing.T) {
	t.Run("creates child context with loop variable in params", func(t *testing.T) {
		parent := NewRootContext(&RootContextOptions{
			Params:       map[string]string{"base": "/src"},
			WorkflowFile: "test.yaml",
		})

		child := NewLoopIterationContext(parent, LoopIterationOptions{
			StepID:    "task-loop",
			Iteration: 0,
			LoopVar:   map[string]string{"file": "main.go"},
		})

		if child.Params["file"] != "main.go" {
			t.Fatal("expected loop var in child params")
		}
		if child.Params["base"] != "/src" {
			t.Fatal("expected parent params inherited")
		}
		if len(child.NestingPath) != 1 {
			t.Fatal("expected 1 nesting segment")
		}
		if child.NestingPath[0].StepID != "task-loop" {
			t.Fatal("expected stepId in nesting segment")
		}
		if *child.NestingPath[0].Iteration != 0 {
			t.Fatal("expected iteration 0 in nesting segment")
		}
	})

	t.Run("does not mutate parent context", func(t *testing.T) {
		parent := NewRootContext(&RootContextOptions{
			Params:       map[string]string{"base": "/src"},
			WorkflowFile: "test.yaml",
		})

		origParams := make(map[string]string)
		for k, v := range parent.Params {
			origParams[k] = v
		}
		origNestingLen := len(parent.NestingPath)

		NewLoopIterationContext(parent, LoopIterationOptions{
			StepID:    "task-loop",
			Iteration: 0,
			LoopVar:   map[string]string{"file": "main.go"},
		})

		if diff := cmp.Diff(origParams, parent.Params); diff != "" {
			t.Fatalf("parent params mutated: %s", diff)
		}
		if len(parent.NestingPath) != origNestingLen {
			t.Fatal("parent nestingPath mutated")
		}
	})
}

func TestCreateRootContextWithAuditLogger(t *testing.T) {
	t.Run("stores auditLogger when provided", func(t *testing.T) {
		logger := &stubAuditLogger{}
		ctx := NewRootContext(&RootContextOptions{
			Params:       map[string]string{},
			WorkflowFile: "test.yaml",
			AuditLogger:  logger,
		})
		if ctx.AuditLogger != logger {
			t.Fatal("expected auditLogger to be stored")
		}
	})

	t.Run("defaults auditLogger to null when not provided", func(t *testing.T) {
		ctx := NewRootContext(&RootContextOptions{
			Params:       map[string]string{},
			WorkflowFile: "test.yaml",
		})
		if ctx.AuditLogger != nil {
			t.Fatal("expected nil auditLogger")
		}
	})
}

func TestCreateSubWorkflowContext(t *testing.T) {
	t.Run("creates child context with only explicit params", func(t *testing.T) {
		parent := NewRootContext(&RootContextOptions{
			Params:       map[string]string{"parent_param": "val"},
			WorkflowFile: "parent.yaml",
		})

		child := NewSubWorkflowContext(parent, &SubWorkflowContextOptions{
			StepID:       "call-sub",
			Params:       map[string]string{"child_param": "cval"},
			WorkflowFile: "child.yaml",
		})

		if _, ok := child.Params["parent_param"]; ok {
			t.Fatal("parent params should not be inherited")
		}
		if child.Params["child_param"] != "cval" {
			t.Fatal("expected child params")
		}
		if len(child.CapturedVariables) != 0 {
			t.Fatal("expected empty captured vars")
		}
		if len(child.SessionIDs) != 0 {
			t.Fatal("expected empty sessionIDs")
		}
	})

	t.Run("stores subWorkflowName on the nesting segment", func(t *testing.T) {
		parent := NewRootContext(&RootContextOptions{
			Params:       map[string]string{},
			WorkflowFile: "parent.yaml",
		})

		child := NewSubWorkflowContext(parent, &SubWorkflowContextOptions{
			StepID:          "call-sub",
			Params:          map[string]string{},
			WorkflowFile:    "child.yaml",
			SubWorkflowName: "my-sub",
		})

		if len(child.NestingPath) != 1 {
			t.Fatal("expected 1 nesting segment")
		}
		if child.NestingPath[0].SubWorkflowName != "my-sub" {
			t.Fatalf("expected subWorkflowName 'my-sub', got %q", child.NestingPath[0].SubWorkflowName)
		}
	})

	t.Run("inherits auditLogger from parent", func(t *testing.T) {
		logger := &stubAuditLogger{}
		parent := NewRootContext(&RootContextOptions{
			Params:       map[string]string{},
			WorkflowFile: "parent.yaml",
			AuditLogger:  logger,
		})

		child := NewSubWorkflowContext(parent, &SubWorkflowContextOptions{
			StepID:       "call-sub",
			Params:       map[string]string{},
			WorkflowFile: "child.yaml",
		})

		if child.AuditLogger != logger {
			t.Fatal("expected auditLogger inherited from parent")
		}
	})
}

func TestLoopIterationContextAuditLogger(t *testing.T) {
	t.Run("inherits auditLogger from parent", func(t *testing.T) {
		logger := &stubAuditLogger{}
		parent := NewRootContext(&RootContextOptions{
			Params:       map[string]string{},
			WorkflowFile: "test.yaml",
			AuditLogger:  logger,
		})

		child := NewLoopIterationContext(parent, LoopIterationOptions{
			StepID:    "loop",
			Iteration: 0,
		})

		if child.AuditLogger != logger {
			t.Fatal("expected auditLogger inherited from parent")
		}
	})
}

func TestWorkflowResumedPropagation(t *testing.T) {
	t.Run("propagates WorkflowResumed to loop iteration context", func(t *testing.T) {
		parent := NewRootContext(&RootContextOptions{
			Params:       map[string]string{},
			WorkflowFile: "test.yaml",
		})
		parent.WorkflowResumed = true

		child := NewLoopIterationContext(parent, LoopIterationOptions{
			StepID:    "loop",
			Iteration: 0,
		})

		if !child.WorkflowResumed {
			t.Fatal("expected WorkflowResumed propagated to loop iteration context")
		}
	})

	t.Run("propagates WorkflowResumed to sub-workflow context", func(t *testing.T) {
		parent := NewRootContext(&RootContextOptions{
			Params:       map[string]string{},
			WorkflowFile: "parent.yaml",
		})
		parent.WorkflowResumed = true

		child := NewSubWorkflowContext(parent, &SubWorkflowContextOptions{
			StepID:       "sub",
			Params:       map[string]string{},
			WorkflowFile: "child.yaml",
		})

		if !child.WorkflowResumed {
			t.Fatal("expected WorkflowResumed propagated to sub-workflow context")
		}
	})
}

func TestSeedSessionPropagation(t *testing.T) {
	t.Run("propagates _seed from parent to sub-workflow context", func(t *testing.T) {
		parent := NewRootContext(&RootContextOptions{
			Params:       map[string]string{},
			WorkflowFile: "parent.yaml",
			SessionIDs:   map[string]string{"_seed": "seed-123"},
		})

		child := NewSubWorkflowContext(parent, &SubWorkflowContextOptions{
			StepID:       "sub",
			Params:       map[string]string{},
			WorkflowFile: "child.yaml",
		})

		if child.SessionIDs["_seed"] != "seed-123" {
			t.Fatalf("expected _seed propagated, got %q", child.SessionIDs["_seed"])
		}
	})

	t.Run("propagates _seed from parent to loop iteration context", func(t *testing.T) {
		parent := NewRootContext(&RootContextOptions{
			Params:       map[string]string{},
			WorkflowFile: "test.yaml",
			SessionIDs:   map[string]string{"_seed": "seed-456"},
		})

		child := NewLoopIterationContext(parent, LoopIterationOptions{
			StepID:    "loop",
			Iteration: 0,
		})

		if child.SessionIDs["_seed"] != "seed-456" {
			t.Fatalf("expected _seed propagated, got %q", child.SessionIDs["_seed"])
		}
	})

	t.Run("does not propagate non-seed session IDs to sub-workflow", func(t *testing.T) {
		parent := NewRootContext(&RootContextOptions{
			Params:       map[string]string{},
			WorkflowFile: "parent.yaml",
			SessionIDs:   map[string]string{"step1": "abc", "_seed": "seed-789"},
		})

		child := NewSubWorkflowContext(parent, &SubWorkflowContextOptions{
			StepID:       "sub",
			Params:       map[string]string{},
			WorkflowFile: "child.yaml",
		})

		if _, ok := child.SessionIDs["step1"]; ok {
			t.Fatal("non-seed session ID should not propagate")
		}
		if child.SessionIDs["_seed"] != "seed-789" {
			t.Fatal("expected _seed propagated")
		}
	})

	t.Run("does not propagate when no seed exists", func(t *testing.T) {
		parent := NewRootContext(&RootContextOptions{
			Params:       map[string]string{},
			WorkflowFile: "parent.yaml",
			SessionIDs:   map[string]string{"step1": "abc"},
		})

		child := NewSubWorkflowContext(parent, &SubWorkflowContextOptions{
			StepID:       "sub",
			Params:       map[string]string{},
			WorkflowFile: "child.yaml",
		})

		if len(child.SessionIDs) != 0 {
			t.Fatalf("expected empty sessionIDs, got %v", child.SessionIDs)
		}
	})

	t.Run("propagates _seed through nested sub-workflows", func(t *testing.T) {
		root := NewRootContext(&RootContextOptions{
			Params:       map[string]string{},
			WorkflowFile: "root.yaml",
			SessionIDs:   map[string]string{"_seed": "seed-nested"},
		})

		child1 := NewSubWorkflowContext(root, &SubWorkflowContextOptions{
			StepID:       "sub1",
			Params:       map[string]string{},
			WorkflowFile: "child1.yaml",
		})

		child2 := NewSubWorkflowContext(child1, &SubWorkflowContextOptions{
			StepID:       "sub2",
			Params:       map[string]string{},
			WorkflowFile: "child2.yaml",
		})

		if child2.SessionIDs["_seed"] != "seed-nested" {
			t.Fatalf("expected _seed propagated through nesting, got %q", child2.SessionIDs["_seed"])
		}
	})
}
