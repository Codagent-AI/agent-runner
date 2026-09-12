package exec

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/model"
)

func writeAuditLog(t *testing.T, sessionDir string, events []audit.Event) {
	t.Helper()
	logger, err := audit.NewLogger(filepath.Join(sessionDir, "audit.log"))
	if err != nil {
		t.Fatalf("create audit logger: %v", err)
	}
	for _, e := range events {
		logger.Emit(e)
	}
	logger.Close()
}

func TestLoadAgentExecutionRebuildsFromAudit(t *testing.T) {
	t.Run("rebuilds response for a simple agent step", func(t *testing.T) {
		sessionDir := t.TempDir()
		writeAuditLog(t, sessionDir, []audit.Event{
			{Timestamp: "2026-07-17T00:00:00Z", Prefix: "[open-draft-pr]", Type: audit.EventStepEnd, Data: map[string]any{
				"outcome": "success", "stdout": "opened the PR",
				"identity": map[string]any{"step_id": "open-draft-pr", "attempt": 1},
			}},
		})
		record, err := LoadAgentExecution(sessionDir, model.ExecutionRef{Prefix: "[open-draft-pr]", Attempt: 1})
		if err != nil {
			t.Fatalf("LoadAgentExecution: %v", err)
		}
		want := &model.AgentExecutionRecord{
			Ref: model.ExecutionRef{Prefix: "[open-draft-pr]", Attempt: 1}, Response: "opened the PR",
		}
		if diff := cmp.Diff(want, record); diff != "" {
			t.Fatalf("mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("rebuilds both call responses attached to the correct parent", func(t *testing.T) {
		sessionDir := t.TempDir()
		writeAuditLog(t, sessionDir, []audit.Event{
			{Timestamp: "2026-07-17T00:00:00Z", Prefix: "[agent-a]", Type: audit.EventStepEnd, Data: map[string]any{
				"outcome": "success", "stdout": "response from A",
				"identity": map[string]any{"step_id": "agent-a", "attempt": 1},
			}},
			{Timestamp: "2026-07-17T00:00:01Z", Prefix: "[agent-b, call:call-1]", Type: audit.EventAgentCallEnd, Data: map[string]any{
				"call_id": "call-1", "response": "first child response",
			}},
			{Timestamp: "2026-07-17T00:00:02Z", Prefix: "[agent-b, call:call-2]", Type: audit.EventAgentCallEnd, Data: map[string]any{
				"call_id": "call-2", "response": "second child response",
			}},
			{Timestamp: "2026-07-17T00:00:03Z", Prefix: "[agent-b]", Type: audit.EventStepEnd, Data: map[string]any{
				"outcome": "success", "stdout": "response from B",
				"identity": map[string]any{"step_id": "agent-b", "attempt": 1},
			}},
		})
		recordA, err := LoadAgentExecution(sessionDir, model.ExecutionRef{Prefix: "[agent-a]", Attempt: 1})
		if err != nil {
			t.Fatalf("LoadAgentExecution(A): %v", err)
		}
		if len(recordA.CallResponses) != 0 {
			t.Fatalf("expected no call responses attached to A, got %+v", recordA.CallResponses)
		}
		recordB, err := LoadAgentExecution(sessionDir, model.ExecutionRef{Prefix: "[agent-b]", Attempt: 1})
		if err != nil {
			t.Fatalf("LoadAgentExecution(B): %v", err)
		}
		want := []model.CallResponse{
			{CallID: "call-1", Response: "first child response"},
			{CallID: "call-2", Response: "second child response"},
		}
		if diff := cmp.Diff(want, recordB.CallResponses); diff != "" {
			t.Fatalf("mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("returns an error when no matching step_end exists", func(t *testing.T) {
		sessionDir := t.TempDir()
		writeAuditLog(t, sessionDir, nil)
		if _, err := LoadAgentExecution(sessionDir, model.ExecutionRef{Prefix: "[missing]", Attempt: 1}); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestClassifyFailure(t *testing.T) {
	t.Run("plain failure reason unchanged", func(t *testing.T) {
		record := &model.FailureRecord{StepID: "check-tests", Stderr: "assertion failed\nmore detail"}
		got := ClassifyFailure(record)
		want := "check-tests failed: assertion failed"
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("falls back to exit code when stderr is empty", func(t *testing.T) {
		record := &model.FailureRecord{StepID: "check-tests", ExitCode: 2}
		got := ClassifyFailure(record)
		want := "check-tests failed: with exit code 2"
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("blocked failure reason", func(t *testing.T) {
		record := &model.FailureRecord{
			StepID:  "verify-draft-pr",
			Stderr:  "expected exactly one open pull request for branch 'dev', found 0",
			Blocked: true, BlockedBy: "push rejected: token lacks workflow scope\nmore detail",
		}
		got := ClassifyFailure(record)
		want := "verify-draft-pr failed: expected exactly one open pull request for branch 'dev', found 0; blocked: push rejected: token lacks workflow scope"
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("exhausted failure reason ends with repair attempts", func(t *testing.T) {
		record := &model.FailureRecord{StepID: "check-plan", Stderr: "still failing", RepairAttempts: 2}
		got := ClassifyFailure(record)
		want := "check-plan failed: still failing after 2 repair attempts"
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("blocked and repair attempts combine", func(t *testing.T) {
		record := &model.FailureRecord{
			StepID: "check-plan", Stderr: "still failing",
			Blocked: true, BlockedBy: "manual review needed", RepairAttempts: 1,
		}
		got := ClassifyFailure(record)
		want := "check-plan failed: still failing; blocked: manual review needed after 1 repair attempts"
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})
}

func TestExecuteCheckStepBuildsFailureRecordWithGuardedExecution(t *testing.T) {
	t.Run("failed check after an agent step attaches the guarded execution", func(t *testing.T) {
		auditLog := &mockAuditLogger{}
		ctx := makeCtx()
		ctx.AuditLogger = auditLog
		ctx.LastAgentExecution = &model.AgentExecutionRecord{
			Ref: model.ExecutionRef{Prefix: "[open-draft-pr]", Attempt: 1}, Response: "opened the PR",
		}
		step := model.Step{ID: "verify-draft-pr", Command: "exit 1"}
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 1, Stderr: "expected one open PR"}}}

		outcome, err := ExecuteCheckStep(&step, ctx, runner, &mockGlob{}, &mockLogger{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if outcome != OutcomeFailed {
			t.Fatalf("expected failed outcome, got %q", outcome)
		}
		if ctx.LastFailure == nil {
			t.Fatal("expected LastFailure to be set")
		}
		if ctx.LastFailure.StepID != "verify-draft-pr" || ctx.LastFailure.ExitCode != 1 || ctx.LastFailure.Stderr != "expected one open PR" {
			t.Fatalf("failure record = %+v", ctx.LastFailure)
		}
		if ctx.LastFailure.Guarded == nil || ctx.LastFailure.Guarded.Response != "opened the PR" {
			t.Fatalf("expected guarded execution attached, got %+v", ctx.LastFailure.Guarded)
		}

		end := findAuditEvent(auditLog.events, audit.EventStepEnd)
		if end.Data["guarded_prefix"] != "[open-draft-pr]" || end.Data["guarded_attempt"] != 1 {
			t.Fatalf("step_end guarded linkage = %+v", end.Data)
		}
	})

	t.Run("no agent step in scope leaves no guarded execution", func(t *testing.T) {
		auditLog := &mockAuditLogger{}
		ctx := makeCtx()
		ctx.AuditLogger = auditLog
		step := model.Step{ID: "check-clean", Command: "exit 1"}
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 1, Stderr: "not clean"}}}

		outcome, err := ExecuteCheckStep(&step, ctx, runner, &mockGlob{}, &mockLogger{})
		if err != nil || outcome != OutcomeFailed {
			t.Fatalf("ExecuteCheckStep() = (%q, %v)", outcome, err)
		}
		if ctx.LastFailure == nil || ctx.LastFailure.Guarded != nil {
			t.Fatalf("expected failure record without guarded execution, got %+v", ctx.LastFailure)
		}
		end := findAuditEvent(auditLog.events, audit.EventStepEnd)
		if _, ok := end.Data["guarded_prefix"]; ok {
			t.Fatalf("did not expect guarded_prefix, got %+v", end.Data)
		}
	})

	t.Run("passing check clears no prior failure but leaves guarded execution untouched", func(t *testing.T) {
		ctx := makeCtx()
		ctx.LastAgentExecution = &model.AgentExecutionRecord{Ref: model.ExecutionRef{Prefix: "[a]", Attempt: 1}, Response: "r"}
		step := model.Step{ID: "check", Command: "exit 0"}
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}

		outcome, err := ExecuteCheckStep(&step, ctx, runner, &mockGlob{}, &mockLogger{})
		if err != nil || outcome != OutcomeSuccess {
			t.Fatalf("ExecuteCheckStep() = (%q, %v)", outcome, err)
		}
		if ctx.LastFailure != nil {
			t.Fatalf("expected no failure record on success, got %+v", ctx.LastFailure)
		}
		if ctx.LastAgentExecution == nil {
			t.Fatal("guarded execution should remain in scope after a passing check")
		}
	})

	t.Run("dispatches script steps too", func(t *testing.T) {
		ctx := makeCtx()
		script := filepath.Join(t.TempDir(), "check.sh")
		if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 1\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		ctx.WorkflowFile = filepath.Join(filepath.Dir(script), "workflow.yaml")
		step := model.Step{ID: "s", Script: filepath.Base(script)}
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 1, Stderr: "boom"}}}

		outcome, err := ExecuteCheckStep(&step, ctx, runner, &mockGlob{}, &mockLogger{})
		if err != nil || outcome != OutcomeFailed {
			t.Fatalf("ExecuteCheckStep() = (%q, %v)", outcome, err)
		}
		if ctx.LastFailure == nil || ctx.LastFailure.StepID != "s" {
			t.Fatalf("failure record = %+v", ctx.LastFailure)
		}
	})
}
