package model

import (
	"strings"
	"testing"
)

func intPtr(n int) *int { return &n }

func TestStepSchema(t *testing.T) {
	t.Run("accepts a valid shell step with command", func(t *testing.T) {
		s := Step{ID: "build", Mode: ModeShell, Command: "echo hi", Session: SessionNew}
		if err := s.Validate(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if s.Session != SessionNew {
			t.Fatalf("expected session 'new', got %q", s.Session)
		}
	})

	t.Run("accepts a valid agent step with prompt", func(t *testing.T) {
		s := Step{ID: "review", Mode: ModeInteractive, Prompt: "Review code", Session: SessionNew}
		if err := s.Validate(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("accepts a headless agent step", func(t *testing.T) {
		s := Step{ID: "impl", Mode: ModeHeadless, Prompt: "Implement", Session: SessionResume}
		if err := s.Validate(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("defaults session to new", func(t *testing.T) {
		s := Step{ID: "build", Mode: ModeShell, Command: "echo hi"}
		s.ApplyDefaults()
		if s.Session != SessionNew {
			t.Fatalf("expected session 'new', got %q", s.Session)
		}
	})

	t.Run("rejects shell step without command", func(t *testing.T) {
		s := Step{ID: "bad", Mode: ModeShell, Session: SessionNew}
		err := s.Validate(nil)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("rejects agent step without prompt", func(t *testing.T) {
		s := Step{ID: "bad", Mode: ModeInteractive, Session: SessionNew}
		err := s.Validate(nil)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("rejects invalid mode", func(t *testing.T) {
		s := Step{ID: "bad", Mode: "invalid", Prompt: "hi", Session: SessionNew}
		err := s.Validate(nil)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "invalid mode") {
			t.Fatalf("expected 'invalid mode' error, got: %v", err)
		}
	})

	t.Run("accepts a sub-workflow step with workflow field", func(t *testing.T) {
		s := Step{ID: "sub", Workflow: "child.yaml", Session: SessionNew}
		if err := s.Validate(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("accepts a sub-workflow step with params", func(t *testing.T) {
		s := Step{ID: "sub", Workflow: "child.yaml", Session: SessionNew, Params: map[string]string{"key": "val"}}
		if err := s.Validate(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects workflow step with command", func(t *testing.T) {
		s := Step{ID: "bad", Workflow: "child.yaml", Command: "echo", Session: SessionNew}
		err := s.Validate(nil)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "exactly one") {
			t.Fatalf("expected 'exactly one' error, got: %v", err)
		}
	})

	t.Run("rejects workflow step with prompt", func(t *testing.T) {
		s := Step{ID: "bad", Workflow: "child.yaml", Prompt: "hi", Session: SessionNew}
		err := s.Validate(nil)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "exactly one") {
			t.Fatalf("expected 'exactly one' error, got: %v", err)
		}
	})

	t.Run("rejects workflow step with mode", func(t *testing.T) {
		s := Step{ID: "bad", Workflow: "child.yaml", Mode: ModeHeadless, Session: SessionNew}
		err := s.Validate(nil)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "exactly one") {
			t.Fatalf("expected 'exactly one' error, got: %v", err)
		}
	})
}

func TestStepSchemaExtensions(t *testing.T) {
	t.Run("accepts inherit as a session strategy", func(t *testing.T) {
		s := Step{ID: "s", Mode: ModeHeadless, Prompt: "p", Session: SessionInherit}
		if err := s.Validate(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("accepts a shell step with capture field", func(t *testing.T) {
		s := Step{ID: "s", Mode: ModeShell, Command: "echo hi", Session: SessionNew, Capture: "output"}
		if err := s.Validate(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("accepts capture on a headless step", func(t *testing.T) {
		s := Step{ID: "s", Mode: ModeHeadless, Prompt: "p", Session: SessionNew, Capture: "output"}
		if err := s.Validate(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects capture on an interactive step", func(t *testing.T) {
		s := Step{ID: "s", Mode: ModeInteractive, Prompt: "p", Session: SessionNew, Capture: "output"}
		err := s.Validate(nil)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "capture") {
			t.Fatalf("expected capture error, got: %v", err)
		}
	})

	t.Run("accepts continue_on_failure on a shell step", func(t *testing.T) {
		s := Step{ID: "s", Mode: ModeShell, Command: "echo", Session: SessionNew, ContinueOnFailure: true}
		if err := s.Validate(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("accepts skip_if with value previous_success", func(t *testing.T) {
		s := Step{ID: "s", Mode: ModeShell, Command: "echo", Session: SessionNew, SkipIf: "previous_success"}
		if err := s.Validate(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects skip_if with invalid value", func(t *testing.T) {
		s := Step{ID: "s", Mode: ModeShell, Command: "echo", Session: SessionNew, SkipIf: "always"}
		err := s.Validate(nil)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("accepts break_if with value success", func(t *testing.T) {
		s := Step{ID: "s", Mode: ModeShell, Command: "echo", Session: SessionNew, BreakIf: "success"}
		if err := s.Validate(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("accepts break_if with value failure", func(t *testing.T) {
		s := Step{ID: "s", Mode: ModeShell, Command: "echo", Session: SessionNew, BreakIf: "failure"}
		if err := s.Validate(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("accepts model on an agent step", func(t *testing.T) {
		s := Step{ID: "s", Mode: ModeHeadless, Prompt: "p", Session: SessionNew, Model: "opus"}
		if err := s.Validate(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects model on a shell step", func(t *testing.T) {
		s := Step{ID: "s", Mode: ModeShell, Command: "echo", Session: SessionNew, Model: "opus"}
		err := s.Validate(nil)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "model") {
			t.Fatalf("expected model error, got: %v", err)
		}
	})

	t.Run("accepts steps field for nested steps group", func(t *testing.T) {
		s := Step{
			ID: "g", Session: SessionNew,
			Steps: []Step{
				{ID: "a", Mode: ModeShell, Command: "echo a", Session: SessionNew},
				{ID: "b", Mode: ModeShell, Command: "echo b", Session: SessionNew},
			},
		}
		if err := s.Validate(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("accepts loop with steps", func(t *testing.T) {
		s := Step{
			ID: "l", Session: SessionNew,
			Loop:  &Loop{Max: intPtr(3)},
			Steps: []Step{{ID: "a", Mode: ModeShell, Command: "echo", Session: SessionNew}},
		}
		if err := s.Validate(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("accepts loop with over and as", func(t *testing.T) {
		s := Step{
			ID: "l", Session: SessionNew,
			Loop:  &Loop{Over: "task_files", As: "task_file"},
			Steps: []Step{{ID: "a", Mode: ModeShell, Command: "echo", Session: SessionNew}},
		}
		if err := s.Validate(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects loop without max and without over+as", func(t *testing.T) {
		s := Step{
			ID: "l", Session: SessionNew,
			Loop:  &Loop{},
			Steps: []Step{{ID: "a", Mode: ModeShell, Command: "echo", Session: SessionNew}},
		}
		err := s.Validate(nil)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("accepts workflow field for sub-workflow step", func(t *testing.T) {
		s := Step{ID: "sub", Workflow: "workflows/sub.yaml", Session: SessionNew}
		if err := s.Validate(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("accepts params on a sub-workflow step", func(t *testing.T) {
		s := Step{ID: "sub", Workflow: "sub.yaml", Session: SessionNew, Params: map[string]string{"out": "{{base}}/result.txt"}}
		if err := s.Validate(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects step with both command and prompt", func(t *testing.T) {
		s := Step{ID: "bad", Mode: ModeShell, Command: "echo", Prompt: "hi", Session: SessionNew}
		err := s.Validate(nil)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "exactly one") {
			t.Fatalf("expected 'exactly one' error, got: %v", err)
		}
	})

	t.Run("rejects step with both command and workflow", func(t *testing.T) {
		s := Step{ID: "bad", Command: "echo", Workflow: "sub.yaml", Session: SessionNew}
		err := s.Validate(nil)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "exactly one") {
			t.Fatalf("expected 'exactly one' error, got: %v", err)
		}
	})

	t.Run("rejects step with neither command prompt workflow nor steps", func(t *testing.T) {
		s := Step{ID: "empty", Session: SessionNew}
		err := s.Validate(nil)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "exactly one") {
			t.Fatalf("expected 'exactly one' error, got: %v", err)
		}
	})
}

func TestParamSchema(t *testing.T) {
	t.Run("parses a required param", func(t *testing.T) {
		p := Param{Name: "file"}
		if p.Name != "file" || !p.IsRequired() {
			t.Fatal("param not parsed correctly")
		}
	})

	t.Run("parses an optional param with default", func(t *testing.T) {
		f := false
		p := Param{Name: "env", Required: &f, Default: "dev"}
		if p.IsRequired() || p.Default != "dev" {
			t.Fatal("param not parsed correctly")
		}
	})
}

func TestWorkflowSchema(t *testing.T) {
	t.Run("parses a full workflow", func(t *testing.T) {
		w := Workflow{
			Name:        "test",
			Description: "A test workflow",
			Params:      []Param{{Name: "file"}},
			Steps: []Step{
				{ID: "build", Mode: ModeShell, Command: "echo hi", Session: SessionNew},
			},
		}
		w.ApplyDefaults()
		if err := w.Validate(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("applies defaults and empty params", func(t *testing.T) {
		w := Workflow{
			Name:  "test",
			Steps: []Step{{ID: "s", Mode: ModeShell, Command: "echo", Session: SessionNew}},
		}
		w.ApplyDefaults()
		if w.Params == nil || len(w.Params) != 0 {
			t.Fatal("expected empty params slice")
		}
	})

	t.Run("rejects workflow with no steps", func(t *testing.T) {
		w := Workflow{Name: "test", Steps: []Step{}}
		w.ApplyDefaults()
		err := w.Validate(nil)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("rejects workflow without name", func(t *testing.T) {
		w := Workflow{Steps: []Step{{ID: "s", Mode: ModeShell, Command: "echo", Session: SessionNew}}}
		w.ApplyDefaults()
		err := w.Validate(nil)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("accepts workflow with engine block", func(t *testing.T) {
		w := Workflow{
			Name:   "test",
			Steps:  []Step{{ID: "s", Mode: ModeShell, Command: "echo", Session: SessionNew}},
			Engine: &EngineConfig{Type: "openspec"},
		}
		w.ApplyDefaults()
		if err := w.Validate(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("accepts workflow without engine block", func(t *testing.T) {
		w := Workflow{
			Name:  "test",
			Steps: []Step{{ID: "s", Mode: ModeShell, Command: "echo", Session: SessionNew}},
		}
		w.ApplyDefaults()
		if err := w.Validate(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if w.Engine != nil {
			t.Fatal("expected nil engine")
		}
	})

	t.Run("rejects engine block missing type", func(t *testing.T) {
		w := Workflow{
			Name:   "test",
			Steps:  []Step{{ID: "s", Mode: ModeShell, Command: "echo", Session: SessionNew}},
			Engine: &EngineConfig{},
		}
		w.ApplyDefaults()
		err := w.Validate(nil)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("accepts a workflow with loop steps", func(t *testing.T) {
		w := Workflow{
			Name: "test",
			Steps: []Step{{
				ID: "l", Session: SessionNew,
				Loop:  &Loop{Max: intPtr(3)},
				Steps: []Step{{ID: "a", Mode: ModeShell, Command: "echo", Session: SessionNew}},
			}},
		}
		w.ApplyDefaults()
		if err := w.Validate(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("accepts a workflow with sub-workflow steps", func(t *testing.T) {
		w := Workflow{
			Name:  "test",
			Steps: []Step{{ID: "sub", Workflow: "child.yaml", Session: SessionNew}},
		}
		w.ApplyDefaults()
		if err := w.Validate(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("accepts a workflow with group steps", func(t *testing.T) {
		w := Workflow{
			Name: "test",
			Steps: []Step{{
				ID: "g", Session: SessionNew,
				Steps: []Step{
					{ID: "a", Mode: ModeShell, Command: "echo a", Session: SessionNew},
					{ID: "b", Mode: ModeShell, Command: "echo b", Session: SessionNew},
				},
			}},
		}
		w.ApplyDefaults()
		if err := w.Validate(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestCLIValidation(t *testing.T) {
	knownCLIs := []string{"claude", "codex"}

	t.Run("accepts cli on an agent step", func(t *testing.T) {
		s := Step{ID: "s", Mode: ModeHeadless, Prompt: "p", Session: SessionNew, CLI: "codex"}
		if err := s.Validate(knownCLIs); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("accepts cli on an interactive step", func(t *testing.T) {
		s := Step{ID: "s", Mode: ModeInteractive, Prompt: "p", Session: SessionNew, CLI: "claude"}
		if err := s.Validate(knownCLIs); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects cli on a shell step", func(t *testing.T) {
		s := Step{ID: "s", Mode: ModeShell, Command: "echo", Session: SessionNew, CLI: "claude"}
		err := s.Validate(knownCLIs)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "cli") {
			t.Fatalf("expected cli error, got: %v", err)
		}
	})

	t.Run("rejects unknown cli value", func(t *testing.T) {
		s := Step{ID: "s", Mode: ModeHeadless, Prompt: "p", Session: SessionNew, CLI: "unknown-cli"}
		err := s.Validate(knownCLIs)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "unknown cli adapter") {
			t.Fatalf("expected 'unknown cli adapter' error, got: %v", err)
		}
	})

	t.Run("skips cli name validation when knownCLIs is nil", func(t *testing.T) {
		s := Step{ID: "s", Mode: ModeHeadless, Prompt: "p", Session: SessionNew, CLI: "anything"}
		if err := s.Validate(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("accepts step with no cli field", func(t *testing.T) {
		s := Step{ID: "s", Mode: ModeHeadless, Prompt: "p", Session: SessionNew}
		if err := s.Validate(knownCLIs); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}
