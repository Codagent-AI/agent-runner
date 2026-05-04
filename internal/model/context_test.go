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
			CapturedVariables: map[string]CapturedValue{"output": NewCapturedString("result")},
		})

		if diff := cmp.Diff(NewCapturedString("result"), ctx.CapturedVariables["output"]); diff != "" {
			t.Fatalf("capturedVar mismatch (-want +got):\n%s", diff)
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

	t.Run("inherits captured variables from parent", func(t *testing.T) {
		parent := NewRootContext(&RootContextOptions{
			Params:       map[string]string{"base": "/src"},
			WorkflowFile: "test.yaml",
			CapturedVariables: map[string]CapturedValue{
				"review_feedback": NewCapturedString("looks good"),
			},
		})

		child := NewLoopIterationContext(parent, LoopIterationOptions{
			StepID:    "discussion",
			Iteration: 0,
		})

		if diff := cmp.Diff(NewCapturedString("looks good"), child.CapturedVariables["review_feedback"]); diff != "" {
			t.Fatalf("captured variable mismatch (-want +got):\n%s", diff)
		}
	})
}

func TestBuiltinVarsForStep(t *testing.T) {
	t.Run("includes step_id when provided", func(t *testing.T) {
		ctx := NewRootContext(&RootContextOptions{
			WorkflowFile: "test.yaml",
			SessionDir:   "/tmp/runs/abc",
		})
		vars := ctx.BuiltinVarsForStep("my-step")
		if vars["step_id"] != "my-step" {
			t.Fatalf("expected step_id='my-step', got %q", vars["step_id"])
		}
		if vars["session_dir"] != "/tmp/runs/abc" {
			t.Fatalf("expected session_dir='/tmp/runs/abc', got %q", vars["session_dir"])
		}
	})

	t.Run("omits step_id when empty", func(t *testing.T) {
		ctx := NewRootContext(&RootContextOptions{WorkflowFile: "test.yaml", SessionDir: "/tmp/runs/abc"})
		vars := ctx.BuiltinVarsForStep("")
		if _, ok := vars["step_id"]; ok {
			t.Fatal("expected step_id to be omitted when empty")
		}
	})

	t.Run("returns nil when no builtins available", func(t *testing.T) {
		ctx := NewRootContext(&RootContextOptions{WorkflowFile: "test.yaml"})
		if ctx.BuiltinVarsForStep("") != nil {
			t.Fatal("expected nil when no builtins available")
		}
	})

	t.Run("includes step_id without session_dir", func(t *testing.T) {
		ctx := NewRootContext(&RootContextOptions{WorkflowFile: "test.yaml"})
		vars := ctx.BuiltinVarsForStep("lone-step")
		if vars["step_id"] != "lone-step" {
			t.Fatalf("expected step_id='lone-step', got %q", vars["step_id"])
		}
		if _, ok := vars["session_dir"]; ok {
			t.Fatal("expected session_dir to be absent")
		}
	})
}

func TestSessionDirBuiltin(t *testing.T) {
	t.Run("BuiltinVars exposes session_dir when set", func(t *testing.T) {
		ctx := NewRootContext(&RootContextOptions{
			WorkflowFile: "test.yaml",
			SessionDir:   "/tmp/runs/abc",
		})
		vars := ctx.BuiltinVars()
		if vars["session_dir"] != "/tmp/runs/abc" {
			t.Fatalf("expected session_dir='/tmp/runs/abc', got %q", vars["session_dir"])
		}
	})

	t.Run("BuiltinVars omits session_dir when empty", func(t *testing.T) {
		ctx := NewRootContext(&RootContextOptions{WorkflowFile: "test.yaml"})
		if _, ok := ctx.BuiltinVars()["session_dir"]; ok {
			t.Fatal("expected session_dir to be omitted when empty")
		}
	})

	t.Run("propagates through loop and sub-workflow contexts", func(t *testing.T) {
		root := NewRootContext(&RootContextOptions{
			WorkflowFile: "root.yaml",
			SessionDir:   "/tmp/runs/xyz",
		})
		loop := NewLoopIterationContext(root, LoopIterationOptions{
			StepID: "loop", Iteration: 0,
		})
		if loop.SessionDir != "/tmp/runs/xyz" {
			t.Fatalf("loop ctx missing session dir: got %q", loop.SessionDir)
		}
		sub := NewSubWorkflowContext(root, &SubWorkflowContextOptions{
			StepID: "call", WorkflowFile: "sub.yaml", SubWorkflowName: "sub",
		})
		if sub.SessionDir != "/tmp/runs/xyz" {
			t.Fatalf("sub ctx missing session dir: got %q", sub.SessionDir)
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

	t.Run("inherits captured variables without sharing writes", func(t *testing.T) {
		parent := NewRootContext(&RootContextOptions{
			Params:       map[string]string{},
			WorkflowFile: "parent.yaml",
			CapturedVariables: map[string]CapturedValue{
				"summary_action": NewCapturedString("learn_more"),
			},
		})

		child := NewSubWorkflowContext(parent, &SubWorkflowContextOptions{
			StepID:       "call-sub",
			Params:       map[string]string{},
			WorkflowFile: "child.yaml",
		})

		if diff := cmp.Diff(NewCapturedString("learn_more"), child.CapturedVariables["summary_action"]); diff != "" {
			t.Fatalf("captured variable mismatch (-want +got):\n%s", diff)
		}
		child.CapturedVariables["summary_action"] = NewCapturedString("continue")
		child.CapturedVariables["child_only"] = NewCapturedString("value")
		if diff := cmp.Diff(NewCapturedString("learn_more"), parent.CapturedVariables["summary_action"]); diff != "" {
			t.Fatalf("parent captured variable mutated (-want +got):\n%s", diff)
		}
		if _, ok := parent.CapturedVariables["child_only"]; ok {
			t.Fatal("child capture write should not mutate parent")
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

func TestNamedSessionSharing(t *testing.T) {
	t.Run("sub-workflow context shares NamedSessions pointer with parent", func(t *testing.T) {
		parent := NewRootContext(&RootContextOptions{
			Params:       map[string]string{},
			WorkflowFile: "parent.yaml",
		})

		child := NewSubWorkflowContext(parent, &SubWorkflowContextOptions{
			StepID:       "sub",
			Params:       map[string]string{},
			WorkflowFile: "child.yaml",
		})

		// Writing in child is visible in parent (same pointer).
		child.NamedSessions["planner"] = "session-xyz"
		if parent.NamedSessions["planner"] != "session-xyz" {
			t.Fatal("expected named session to be visible in parent context")
		}
	})

	t.Run("loop iteration context shares NamedSessions pointer with parent", func(t *testing.T) {
		parent := NewRootContext(&RootContextOptions{
			Params:       map[string]string{},
			WorkflowFile: "test.yaml",
		})

		iter := NewLoopIterationContext(parent, LoopIterationOptions{
			StepID:    "loop",
			Iteration: 0,
		})

		iter.NamedSessions["planner"] = "iter-session"
		if parent.NamedSessions["planner"] != "iter-session" {
			t.Fatal("expected named session to be visible in parent context after loop iteration write")
		}
	})

	t.Run("NamedSessionDecls restored from options in root context", func(t *testing.T) {
		ctx := NewRootContext(&RootContextOptions{
			Params:            map[string]string{},
			WorkflowFile:      "test.yaml",
			NamedSessionDecls: map[string]string{"planner": "planner-profile"},
		})

		if ctx.NamedSessionDecls["planner"] != "planner-profile" {
			t.Fatalf("expected NamedSessionDecls to be restored, got %q", ctx.NamedSessionDecls["planner"])
		}
	})

	t.Run("NamedSessions restored from options in root context", func(t *testing.T) {
		ctx := NewRootContext(&RootContextOptions{
			Params:        map[string]string{},
			WorkflowFile:  "test.yaml",
			NamedSessions: map[string]string{"planner": "persisted-id"},
		})

		if ctx.NamedSessions["planner"] != "persisted-id" {
			t.Fatalf("expected NamedSessions to be restored, got %q", ctx.NamedSessions["planner"])
		}
	})

	t.Run("NamedSessionDecls shared between sub-workflow and parent", func(t *testing.T) {
		parent := NewRootContext(&RootContextOptions{
			Params:       map[string]string{},
			WorkflowFile: "parent.yaml",
		})

		child := NewSubWorkflowContext(parent, &SubWorkflowContextOptions{
			StepID:       "sub",
			Params:       map[string]string{},
			WorkflowFile: "child.yaml",
		})

		child.NamedSessionDecls["implementor"] = "impl-profile"
		if parent.NamedSessionDecls["implementor"] != "impl-profile" {
			t.Fatal("expected NamedSessionDecls to be shared with parent")
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
