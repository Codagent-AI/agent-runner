package model

import (
	"strings"
	"testing"
)

func TestRepairFormAndBudget(t *testing.T) {
	t.Run("inline form reports RepairInline", func(t *testing.T) {
		r := &Repair{Prompt: "fix it", Session: "resume"}
		if r.Form() != RepairInline {
			t.Fatalf("expected RepairInline, got %v", r.Form())
		}
	})

	t.Run("rerun form reports RepairRerun", func(t *testing.T) {
		r := &Repair{Rerun: "open-draft-pr"}
		if r.Form() != RepairRerun {
			t.Fatalf("expected RepairRerun, got %v", r.Form())
		}
	})

	t.Run("budget defaults to 1", func(t *testing.T) {
		r := &Repair{Rerun: "open-draft-pr"}
		if r.Budget() != 1 {
			t.Fatalf("expected default budget 1, got %d", r.Budget())
		}
	})

	t.Run("budget honors max", func(t *testing.T) {
		budget := 3
		r := &Repair{Rerun: "open-draft-pr", Max: &budget}
		if r.Budget() != 3 {
			t.Fatalf("expected budget 3, got %d", r.Budget())
		}
	})
}

func TestRepairValidation(t *testing.T) {
	t.Run("inline repair on a script step validates with default budget", func(t *testing.T) {
		s := Step{ID: "s", Script: "check.sh", Repair: &Repair{Prompt: "fix", Session: "planning-agent"}}
		if err := s.Validate(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if s.Repair.Budget() != 1 {
			t.Fatalf("expected default budget of 1")
		}
	})

	t.Run("rerun repair on a shell step validates", func(t *testing.T) {
		w := Workflow{Name: "w", Steps: []Step{
			{ID: "open-draft-pr", Command: "echo open"},
			{ID: "verify-draft-pr", Command: "echo verify", Repair: &Repair{Rerun: "open-draft-pr"}},
		}}
		if err := w.Validate(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("repair on an agent step is rejected", func(t *testing.T) {
		s := Step{ID: "s", Prompt: "do it", Repair: &Repair{Rerun: "earlier"}}
		err := s.Validate(nil)
		if err == nil || !strings.Contains(err.Error(), "shell and script steps") {
			t.Fatalf("expected shell/script-only error, got: %v", err)
		}
	})

	t.Run("both forms rejected", func(t *testing.T) {
		s := Step{ID: "s", Command: "echo hi", Repair: &Repair{Rerun: "earlier", Prompt: "fix"}}
		err := s.Validate(nil)
		if err == nil || !strings.Contains(err.Error(), "exactly one repair form") {
			t.Fatalf("expected exactly-one-form error, got: %v", err)
		}
	})

	t.Run("neither form rejected", func(t *testing.T) {
		s := Step{ID: "s", Command: "echo hi", Repair: &Repair{}}
		err := s.Validate(nil)
		if err == nil || !strings.Contains(err.Error(), "exactly one repair form") {
			t.Fatalf("expected exactly-one-form error, got: %v", err)
		}
	})

	t.Run("inline block without session or agent rejected", func(t *testing.T) {
		s := Step{ID: "s", Command: "echo hi", Repair: &Repair{Prompt: "fix"}}
		err := s.Validate(nil)
		if err == nil || !strings.Contains(err.Error(), `must name "session" or "agent"`) {
			t.Fatalf("expected session/agent error, got: %v", err)
		}
	})

	t.Run("inline block with both session and agent rejected", func(t *testing.T) {
		s := Step{ID: "s", Command: "echo hi", Repair: &Repair{Prompt: "fix", Session: "resume", Agent: "fixer"}}
		err := s.Validate(nil)
		if err == nil || !strings.Contains(err.Error(), `must name "session" or "agent"`) {
			t.Fatalf("expected session/agent error, got: %v", err)
		}
	})

	t.Run("session: new is rejected in favor of agent", func(t *testing.T) {
		s := Step{ID: "s", Command: "echo hi", Repair: &Repair{Prompt: "fix", Session: SessionNew}}
		err := s.Validate(nil)
		if err == nil || !strings.Contains(err.Error(), "fresh session must use") {
			t.Fatalf("expected fresh-session error, got: %v", err)
		}
	})

	t.Run("max less than 1 rejected", func(t *testing.T) {
		invalidMax := 0
		s := Step{ID: "s", Command: "echo hi", Repair: &Repair{Rerun: "earlier", Max: &invalidMax}}
		err := s.Validate(nil)
		if err == nil || !strings.Contains(err.Error(), "max") {
			t.Fatalf("expected max error, got: %v", err)
		}
	})

	t.Run("named session repair target accepted without workflow-level session declaration", func(t *testing.T) {
		s := Step{ID: "s", Command: "echo hi", Repair: &Repair{Prompt: "fix", Session: "planning-agent"}}
		if err := s.Validate(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestValidateRepairTargets(t *testing.T) {
	t.Run("rerun target outside the loop body is rejected", func(t *testing.T) {
		w := Workflow{Name: "w", Steps: []Step{
			{ID: "outer", Command: "echo outer"},
			{ID: "loop", Loop: &Loop{Max: intPtr(3)}, Steps: []Step{
				{ID: "inner-check", Command: "echo check", Repair: &Repair{Rerun: "outer"}},
			}},
		}}
		err := w.Validate(nil)
		if err == nil || !strings.Contains(err.Error(), "outer") {
			t.Fatalf("expected error identifying invalid target, got: %v", err)
		}
	})

	t.Run("rerun target that is a later step is rejected", func(t *testing.T) {
		w := Workflow{Name: "w", Steps: []Step{
			{ID: "check", Command: "echo check", Repair: &Repair{Rerun: "later"}},
			{ID: "later", Command: "echo later"},
		}}
		err := w.Validate(nil)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("rerun target that is the check itself is rejected", func(t *testing.T) {
		w := Workflow{Name: "w", Steps: []Step{
			{ID: "check", Command: "echo check", Repair: &Repair{Rerun: "check"}},
		}}
		err := w.Validate(nil)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("replay range with break_if is rejected", func(t *testing.T) {
		w := Workflow{Name: "w", Steps: []Step{
			{ID: "open", Command: "echo open"},
			{ID: "gate", Command: "echo gate", BreakIf: "failure"},
			{ID: "verify", Command: "echo verify", Repair: &Repair{Rerun: "open"}},
		}}
		err := w.Validate(nil)
		if err == nil || !strings.Contains(err.Error(), "gate") || !strings.Contains(err.Error(), "break_if") {
			t.Fatalf("expected error naming gate and break_if, got: %v", err)
		}
	})

	t.Run("replay range with skip_if previous_success is rejected", func(t *testing.T) {
		w := Workflow{Name: "w", Steps: []Step{
			{ID: "open", Command: "echo open"},
			{ID: "gate", Command: "echo gate", SkipIf: "previous_success"},
			{ID: "verify", Command: "echo verify", Repair: &Repair{Rerun: "open"}},
		}}
		err := w.Validate(nil)
		if err == nil || !strings.Contains(err.Error(), "gate") {
			t.Fatalf("expected error naming gate, got: %v", err)
		}
	})

	t.Run("rerun target inside a group body validates when check is in same group", func(t *testing.T) {
		w := Workflow{Name: "w", Steps: []Step{
			{ID: "group", Steps: []Step{
				{ID: "open", Command: "echo open"},
				{ID: "verify", Command: "echo verify", Repair: &Repair{Rerun: "open"}},
			}},
		}}
		if err := w.Validate(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}
