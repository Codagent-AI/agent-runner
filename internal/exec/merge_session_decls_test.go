package exec

import (
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/model"
)

func TestMergeSessionDecls(t *testing.T) {
	newCtx := func() *model.ExecutionContext {
		return model.NewRootContext(&model.RootContextOptions{
			Params:       map[string]string{},
			WorkflowFile: "test.yaml",
		})
	}

	t.Run("no-op for empty session list", func(t *testing.T) {
		ctx := newCtx()
		log := &mockLogger{}
		if err := MergeSessionDecls(ctx, nil, log); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(log.lines) != 0 {
			t.Errorf("expected no log output, got: %v", log.lines)
		}
	})

	t.Run("adds new declarations", func(t *testing.T) {
		ctx := newCtx()
		log := &mockLogger{}
		decls := []model.SessionDecl{{Name: "planner", Agent: "planner-profile"}}
		if err := MergeSessionDecls(ctx, decls, log); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ctx.NamedSessionDecls["planner"] != "planner-profile" {
			t.Errorf("expected declaration to be added; got %q", ctx.NamedSessionDecls["planner"])
		}
	})

	t.Run("canonicalizes legacy profile in named declaration and warns once", func(t *testing.T) {
		ctx := newCtx()
		auditLog := &recordingAuditLogger{}
		ctx.AuditLogger = auditLog
		log := &mockLogger{}
		decls := []model.SessionDecl{
			{Name: "lead-session", Agent: "planner"},
			{Name: "lead-session", Agent: "planner"},
		}
		if err := MergeSessionDecls(ctx, decls, log); err != nil {
			t.Fatalf("MergeSessionDecls() error = %v", err)
		}
		if got := ctx.NamedSessionDecls["lead-session"]; got != "lead" {
			t.Fatalf("named session agent = %q, want lead", got)
		}
		const warning = `agent profile "planner" is deprecated; use "lead"`
		if count := strings.Count(strings.Join(log.lines, "\n"), warning); count != 1 {
			t.Fatalf("warning count = %d, want 1; log=%v", count, log.lines)
		}
		warningEvents := 0
		for _, event := range auditLog.events {
			if event.Type == audit.EventWarning && event.Data["alias"] == "planner" {
				warningEvents++
			}
		}
		if warningEvents != 1 {
			t.Fatalf("warning audit events = %d, want 1", warningEvents)
		}
	})

	t.Run("canonicalizes persisted legacy declaration before drift comparison", func(t *testing.T) {
		ctx := newCtx()
		ctx.NamedSessionDecls["lead-session"] = "planner"
		ctx.NamedSessions["lead-session"] = "persisted-session"
		log := &mockLogger{}

		if err := MergeSessionDecls(ctx, []model.SessionDecl{{Name: "lead-session", Agent: "lead"}}, log); err != nil {
			t.Fatalf("MergeSessionDecls() error = %v", err)
		}
		if got := ctx.NamedSessionDecls["lead-session"]; got != "lead" {
			t.Fatalf("persisted declaration = %q, want canonical lead", got)
		}
		output := strings.Join(log.lines, "\n")
		if strings.Contains(output, "declared agent changed") {
			t.Fatalf("canonical synonym produced false drift warning: %s", output)
		}
		const deprecation = `agent profile "planner" is deprecated; use "lead"`
		if count := strings.Count(output, deprecation); count != 1 {
			t.Fatalf("deprecation count = %d, want 1; log=%v", count, log.lines)
		}
	})

	t.Run("silently merges compatible duplicates", func(t *testing.T) {
		ctx := newCtx()
		ctx.NamedSessionDecls["planner"] = "planner-profile"
		log := &mockLogger{}
		decls := []model.SessionDecl{{Name: "planner", Agent: "planner-profile"}}
		if err := MergeSessionDecls(ctx, decls, log); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(log.lines) != 0 {
			t.Errorf("expected no log output for compatible duplicate, got: %v", log.lines)
		}
	})

	t.Run("returns error on conflict when no live session exists", func(t *testing.T) {
		ctx := newCtx()
		ctx.NamedSessionDecls["planner"] = "planner-profile"
		log := &mockLogger{}
		decls := []model.SessionDecl{{Name: "planner", Agent: "implementor-profile"}}
		err := MergeSessionDecls(ctx, decls, log)
		if err == nil {
			t.Fatal("expected error when declarations conflict without a live session")
		}
		msg := err.Error()
		if !strings.Contains(msg, "planner") ||
			!strings.Contains(msg, "planner-profile") ||
			!strings.Contains(msg, "implementor-profile") {
			t.Errorf("expected error to name the session and both agents, got: %v", err)
		}
		// Original agent is preserved.
		if ctx.NamedSessionDecls["planner"] != "planner-profile" {
			t.Errorf("expected original agent to be preserved, got %q", ctx.NamedSessionDecls["planner"])
		}
	})

	t.Run("warns but does not error on conflict when live session exists", func(t *testing.T) {
		ctx := newCtx()
		ctx.NamedSessionDecls["planner"] = "planner-profile"
		ctx.NamedSessions["planner"] = "live-session-id"
		log := &mockLogger{}
		decls := []model.SessionDecl{{Name: "planner", Agent: "implementor-profile"}}
		if err := MergeSessionDecls(ctx, decls, log); err != nil {
			t.Fatalf("expected no error when live session exists, got: %v", err)
		}
		if len(log.lines) == 0 {
			t.Fatal("expected a warning to be logged")
		}
		if !strings.Contains(log.lines[0], "planner") {
			t.Errorf("expected warning to name the session, got: %v", log.lines)
		}
		if ctx.NamedSessionDecls["planner"] != "planner-profile" {
			t.Errorf("expected original agent to be preserved, got %q", ctx.NamedSessionDecls["planner"])
		}
	})
}
