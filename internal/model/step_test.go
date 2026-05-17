package model

import (
	"strings"
	"testing"
)

func intPtr(n int) *int { return &n }

func TestStepSchema(t *testing.T) {
	t.Run("accepts a valid shell step with command", func(t *testing.T) {
		s := Step{ID: "build", Command: "echo hi", Session: SessionNew}
		if err := s.Validate(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if s.Session != SessionNew {
			t.Fatalf("expected session 'new', got %q", s.Session)
		}
	})

	t.Run("accepts a valid agent step with prompt and agent", func(t *testing.T) {
		s := Step{ID: "review", Agent: "planner", Mode: ModeInteractive, Prompt: "Review code", Session: SessionNew}
		if err := s.Validate(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("accepts a headless agent step with resume", func(t *testing.T) {
		s := Step{ID: "impl", Mode: ModeAutonomous, Prompt: "Implement", Session: SessionResume}
		if err := s.Validate(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("workflow defaults first agentic step to session new", func(t *testing.T) {
		w := Workflow{
			Name: "test",
			Steps: []Step{
				{ID: "s1", Command: "echo hi"},
				{ID: "s2", Agent: "implementor", Prompt: "do it"},
				{ID: "s3", Prompt: "continue"},
			},
		}
		w.ApplyDefaults()
		if w.Steps[1].Session != SessionNew {
			t.Fatalf("expected first agentic step session 'new', got %q", w.Steps[1].Session)
		}
		if w.Steps[2].Session != SessionResume {
			t.Fatalf("expected second agentic step session 'resume', got %q", w.Steps[2].Session)
		}
	})

	t.Run("rejects step with no type", func(t *testing.T) {
		s := Step{ID: "bad", Session: SessionNew}
		err := s.Validate(nil)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("rejects agent step without prompt", func(t *testing.T) {
		s := Step{ID: "bad", Agent: "implementor", Session: SessionNew}
		err := s.Validate(nil)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("rejects invalid mode", func(t *testing.T) {
		s := Step{ID: "bad", Mode: "invalid", Prompt: "hi", Agent: "implementor", Session: SessionNew}
		err := s.Validate(nil)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "invalid mode") {
			t.Fatalf("expected 'invalid mode' error, got: %v", err)
		}
	})

	t.Run("rejects headless with migration hint", func(t *testing.T) {
		s := Step{ID: "bad", Mode: "headless", Prompt: "hi", Agent: "implementor", Session: SessionNew}
		err := s.Validate(nil)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "renamed to") {
			t.Fatalf("expected migration hint, got: %v", err)
		}
	})

	t.Run("rejects mode shell as invalid", func(t *testing.T) {
		s := Step{ID: "bad", Mode: "shell", Prompt: "hi", Agent: "implementor", Session: SessionNew}
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
		s := Step{ID: "bad", Workflow: "child.yaml", Prompt: "hi", Agent: "implementor", Session: SessionNew}
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
		s := Step{ID: "s", Mode: ModeAutonomous, Prompt: "p", Session: SessionInherit}
		if err := s.Validate(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("accepts a shell step with capture field", func(t *testing.T) {
		s := Step{ID: "s", Command: "echo hi", Session: SessionNew, Capture: "output"}
		if err := s.Validate(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("accepts interactive mode on a shell step", func(t *testing.T) {
		s := Step{ID: "s", Command: "read name", Session: SessionNew, Mode: ModeInteractive}
		if err := s.Validate(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("accepts autonomous mode on a shell step", func(t *testing.T) {
		s := Step{ID: "s", Command: "echo hi", Session: SessionNew, Mode: ModeAutonomous}
		if err := s.Validate(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("accepts capture on an autonomous step", func(t *testing.T) {
		s := Step{ID: "s", Agent: "implementor", Mode: ModeAutonomous, Prompt: "p", Session: SessionNew, Capture: "output"}
		if err := s.Validate(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("accepts capture on agent step with profile (mode from profile)", func(t *testing.T) {
		s := Step{ID: "s", Agent: "implementor", Prompt: "p", Session: SessionNew, Capture: "output"}
		if err := s.Validate(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects capture on an interactive step", func(t *testing.T) {
		s := Step{ID: "s", Agent: "planner", Mode: ModeInteractive, Prompt: "p", Session: SessionNew, Capture: "output"}
		err := s.Validate(nil)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "capture") {
			t.Fatalf("expected capture error, got: %v", err)
		}
	})

	t.Run("rejects capture on an interactive shell step", func(t *testing.T) {
		s := Step{ID: "s", Command: "read name", Session: SessionNew, Mode: ModeInteractive, Capture: "output"}
		err := s.Validate(nil)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "capture") || !strings.Contains(err.Error(), "interactive") {
			t.Fatalf("expected interactive capture error, got: %v", err)
		}
	})

	t.Run("accepts capture on session resume step without agent", func(t *testing.T) {
		s := Step{ID: "s", Prompt: "p", Session: SessionResume, Capture: "output"}
		if err := s.Validate(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("accepts capture on session inherit step without agent", func(t *testing.T) {
		s := Step{ID: "s", Prompt: "p", Session: SessionInherit, Capture: "output"}
		if err := s.Validate(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("accepts continue_on_failure on a shell step", func(t *testing.T) {
		s := Step{ID: "s", Command: "echo", Session: SessionNew, ContinueOnFailure: true}
		if err := s.Validate(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("accepts skip_if with value previous_success", func(t *testing.T) {
		s := Step{ID: "s", Command: "echo", Session: SessionNew, SkipIf: "previous_success"}
		if err := s.Validate(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects skip_if with invalid value", func(t *testing.T) {
		s := Step{ID: "s", Command: "echo", Session: SessionNew, SkipIf: "always"}
		err := s.Validate(nil)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("accepts skip_if with sh: prefix", func(t *testing.T) {
		s := Step{ID: "s", Command: "echo", Session: SessionNew, SkipIf: "sh: test -z foo"}
		if err := s.Validate(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects skip_if with empty sh: command", func(t *testing.T) {
		s := Step{ID: "s", Command: "echo", Session: SessionNew, SkipIf: "sh:  "}
		err := s.Validate(nil)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("accepts break_if with value success", func(t *testing.T) {
		s := Step{ID: "s", Command: "echo", Session: SessionNew, BreakIf: "success"}
		if err := s.Validate(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("accepts break_if with value failure", func(t *testing.T) {
		s := Step{ID: "s", Command: "echo", Session: SessionNew, BreakIf: "failure"}
		if err := s.Validate(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("accepts model on an agent step", func(t *testing.T) {
		s := Step{ID: "s", Agent: "implementor", Mode: ModeAutonomous, Prompt: "p", Session: SessionNew, Model: "opus"}
		if err := s.Validate(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects model on a shell step", func(t *testing.T) {
		s := Step{ID: "s", Command: "echo", Session: SessionNew, Model: "opus"}
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
				{ID: "a", Command: "echo a", Session: SessionNew},
				{ID: "b", Command: "echo b", Session: SessionNew},
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
			Steps: []Step{{ID: "a", Command: "echo", Session: SessionNew}},
		}
		if err := s.Validate(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("accepts loop with over and as", func(t *testing.T) {
		s := Step{
			ID: "l", Session: SessionNew,
			Loop:  &Loop{Over: "task_files", As: "task_file"},
			Steps: []Step{{ID: "a", Command: "echo", Session: SessionNew}},
		}
		if err := s.Validate(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects loop without max and without over+as", func(t *testing.T) {
		s := Step{
			ID: "l", Session: SessionNew,
			Loop:  &Loop{},
			Steps: []Step{{ID: "a", Command: "echo", Session: SessionNew}},
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
		s := Step{ID: "bad", Command: "echo", Prompt: "hi", Session: SessionNew}
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

func TestStepSchemaOnboardingPrimitives(t *testing.T) {
	validUI := func() Step {
		return Step{
			ID:      "pick",
			Mode:    ModeUI,
			Title:   "Pick",
			Actions: []UIAction{{Label: "Continue", Outcome: "continue"}},
		}
	}

	t.Run("accepts ui step with action", func(t *testing.T) {
		s := validUI()
		if err := s.Validate(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects agent fields on ui step", func(t *testing.T) {
		for field, s := range map[string]Step{
			"agent":   func() Step { s := validUI(); s.Agent = "planner"; return s }(),
			"command": func() Step { s := validUI(); s.Command = "echo hi"; return s }(),
			"prompt":  func() Step { s := validUI(); s.Prompt = "hi"; return s }(),
			"session": func() Step { s := validUI(); s.Session = SessionNew; return s }(),
		} {
			err := s.Validate(nil)
			if err == nil || !strings.Contains(err.Error(), "ui steps") {
				t.Fatalf("%s: expected ui field error, got %v", field, err)
			}
		}
	})

	t.Run("rejects duplicate ui action outcomes", func(t *testing.T) {
		s := validUI()
		s.Actions = append(s.Actions, UIAction{Label: "Again", Outcome: "continue"})
		err := s.Validate(nil)
		if err == nil || !strings.Contains(err.Error(), "duplicate outcome") {
			t.Fatalf("expected duplicate outcome error, got %v", err)
		}
	})

	t.Run("rejects interpolated ui action outcome", func(t *testing.T) {
		s := validUI()
		s.Actions[0].Outcome = "{{choice}}"
		err := s.Validate(nil)
		if err == nil || !strings.Contains(err.Error(), "static identifiers") {
			t.Fatalf("expected static identifier error, got %v", err)
		}
	})

	t.Run("accepts ui capture only with inputs", func(t *testing.T) {
		s := validUI()
		s.Capture = "profile"
		err := s.Validate(nil)
		if err == nil || !strings.Contains(err.Error(), "capture") || !strings.Contains(err.Error(), "inputs") {
			t.Fatalf("expected capture requires inputs error, got %v", err)
		}
		s.Inputs = []UIInput{{Kind: "single_select", ID: "adapter", Prompt: "Adapter", Options: []string{"claude"}}}
		if err := s.Validate(nil); err != nil {
			t.Fatalf("unexpected error after adding inputs: %v", err)
		}
	})

	t.Run("rejects hyphenated single select kind", func(t *testing.T) {
		s := validUI()
		s.Inputs = []UIInput{{Kind: "single-select", ID: "adapter", Prompt: "Adapter", Options: []string{"claude"}}}
		err := s.Validate(nil)
		if err == nil || !strings.Contains(err.Error(), "single_select") {
			t.Fatalf("expected single_select kind error, got %v", err)
		}
	})

	t.Run("rejects duplicate ui input ids", func(t *testing.T) {
		s := validUI()
		s.Inputs = []UIInput{
			{Kind: "single_select", ID: "adapter", Prompt: "Adapter", Options: []string{"claude"}},
			{Kind: "single_select", ID: "adapter", Prompt: "Again", Options: []string{"codex"}},
		}
		err := s.Validate(nil)
		if err == nil || !strings.Contains(err.Error(), "duplicate input id") {
			t.Fatalf("expected duplicate input id error, got %v", err)
		}
	})

	t.Run("rejects unsafe script path", func(t *testing.T) {
		for _, script := range []string{"/tmp/x.sh", "../x.sh", "{{choice}}.sh"} {
			s := Step{ID: "script", Script: script}
			err := s.Validate(nil)
			if err == nil {
				t.Fatalf("expected error for script %q", script)
			}
		}
	})

	t.Run("rejects model and cli on script steps", func(t *testing.T) {
		for _, s := range []Step{
			{ID: "script", Script: "detect.sh", Model: "opus"},
			{ID: "script", Script: "detect.sh", CLI: "codex"},
		} {
			err := s.Validate(nil)
			if err == nil || !strings.Contains(err.Error(), "script steps") {
				t.Fatalf("expected script field error, got %v", err)
			}
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
				{ID: "build", Command: "echo hi", Session: SessionNew},
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
			Steps: []Step{{ID: "s", Command: "echo", Session: SessionNew}},
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
		w := Workflow{Steps: []Step{{ID: "s", Command: "echo", Session: SessionNew}}}
		w.ApplyDefaults()
		err := w.Validate(nil)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("accepts workflow with engine block", func(t *testing.T) {
		w := Workflow{
			Name:   "test",
			Steps:  []Step{{ID: "s", Command: "echo", Session: SessionNew}},
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
			Steps: []Step{{ID: "s", Command: "echo", Session: SessionNew}},
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
			Steps:  []Step{{ID: "s", Command: "echo", Session: SessionNew}},
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
				Steps: []Step{{ID: "a", Command: "echo", Session: SessionNew}},
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
					{ID: "a", Command: "echo a", Session: SessionNew},
					{ID: "b", Command: "echo b", Session: SessionNew},
				},
			}},
		}
		w.ApplyDefaults()
		if err := w.Validate(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestIsNamedSession(t *testing.T) {
	t.Run("returns false for empty string", func(t *testing.T) {
		if IsNamedSession("") {
			t.Fatal("expected false for empty string")
		}
	})
	t.Run("returns false for new", func(t *testing.T) {
		if IsNamedSession(SessionNew) {
			t.Fatal("expected false for 'new'")
		}
	})
	t.Run("returns false for resume", func(t *testing.T) {
		if IsNamedSession(SessionResume) {
			t.Fatal("expected false for 'resume'")
		}
	})
	t.Run("returns false for inherit", func(t *testing.T) {
		if IsNamedSession(SessionInherit) {
			t.Fatal("expected false for 'inherit'")
		}
	})
	t.Run("returns true for a user-defined name", func(t *testing.T) {
		if !IsNamedSession("planner") {
			t.Fatal("expected true for 'planner'")
		}
	})
	t.Run("returns true for another user-defined name", func(t *testing.T) {
		if !IsNamedSession("implementor") {
			t.Fatal("expected true for 'implementor'")
		}
	})
}

func TestNamedSessionStepValidation(t *testing.T) {
	t.Run("accepts a named session step with prompt and no agent", func(t *testing.T) {
		s := Step{ID: "s", Prompt: "do it", Session: "planner"}
		if err := s.Validate(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("accepts capture on a named session step (profile may resolve headless)", func(t *testing.T) {
		s := Step{ID: "s", Prompt: "do it", Session: "planner", Capture: "out"}
		if err := s.Validate(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects named session step with agent field", func(t *testing.T) {
		s := Step{ID: "s", Prompt: "do it", Session: "planner", Agent: "some-agent"}
		err := s.Validate(nil)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "agent") {
			t.Fatalf("expected agent error, got: %v", err)
		}
	})

	t.Run("workflow applies no session default to named session step", func(t *testing.T) {
		w := Workflow{
			Name: "test",
			Steps: []Step{
				{ID: "s1", Prompt: "plan", Session: "planner"},
			},
		}
		w.ApplyDefaults()
		if w.Steps[0].Session != "planner" {
			t.Fatalf("expected session 'planner' unchanged, got %q", w.Steps[0].Session)
		}
	})
}

func TestWorkflowSessions(t *testing.T) {
	t.Run("accepts workflow with sessions block", func(t *testing.T) {
		w := Workflow{
			Name:     "test",
			Sessions: []SessionDecl{{Name: "planner", Agent: "planner-profile"}},
			Steps:    []Step{{ID: "s1", Prompt: "do it", Session: "planner"}},
		}
		w.ApplyDefaults()
		if err := w.Validate(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(w.Sessions) != 1 {
			t.Fatal("expected 1 session declaration")
		}
		if w.Sessions[0].Name != "planner" || w.Sessions[0].Agent != "planner-profile" {
			t.Fatal("session declaration not preserved")
		}
	})
}

func TestCLIValidation(t *testing.T) {
	knownCLIs := []string{"claude", "codex"}

	t.Run("accepts cli on an agent step", func(t *testing.T) {
		s := Step{ID: "s", Agent: "implementor", Mode: ModeAutonomous, Prompt: "p", Session: SessionNew, CLI: "codex"}
		if err := s.Validate(knownCLIs); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("accepts cli on an interactive step", func(t *testing.T) {
		s := Step{ID: "s", Agent: "planner", Mode: ModeInteractive, Prompt: "p", Session: SessionNew, CLI: "claude"}
		if err := s.Validate(knownCLIs); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects cli on a shell step", func(t *testing.T) {
		s := Step{ID: "s", Command: "echo", Session: SessionNew, CLI: "claude"}
		err := s.Validate(knownCLIs)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "cli") {
			t.Fatalf("expected cli error, got: %v", err)
		}
	})

	t.Run("rejects unknown cli value", func(t *testing.T) {
		s := Step{ID: "s", Agent: "implementor", Mode: ModeAutonomous, Prompt: "p", Session: SessionNew, CLI: "unknown-cli"}
		err := s.Validate(knownCLIs)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "unknown cli adapter") {
			t.Fatalf("expected 'unknown cli adapter' error, got: %v", err)
		}
	})

	t.Run("skips cli name validation when knownCLIs is nil", func(t *testing.T) {
		s := Step{ID: "s", Agent: "implementor", Mode: ModeAutonomous, Prompt: "p", Session: SessionNew, CLI: "anything"}
		if err := s.Validate(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("accepts step with no cli field", func(t *testing.T) {
		s := Step{ID: "s", Agent: "implementor", Mode: ModeAutonomous, Prompt: "p", Session: SessionNew}
		if err := s.Validate(knownCLIs); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}
