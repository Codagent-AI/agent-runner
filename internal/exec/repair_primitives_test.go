package exec

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/codagent/agent-runner/internal/model"
)

func TestRunShellCheckEmitsNoAudit(t *testing.T) {
	auditLog := &mockAuditLogger{}
	ctx := makeCtx()
	ctx.AuditLogger = auditLog
	step := model.Step{ID: "s", Command: "echo {{name}}"}
	ctx.Params = map[string]string{"name": "world"}
	runner := &mockRunner{results: []ProcessResult{{ExitCode: 7, Stdout: "out", Stderr: "err"}}}

	run := runShellCheck(&step, ctx, runner)

	if run.RunErr != nil {
		t.Fatalf("unexpected error: %v", run.RunErr)
	}
	if run.Result.ExitCode != 7 || run.Result.Stdout != "out" || run.Result.Stderr != "err" {
		t.Fatalf("unexpected result: %#v", run.Result)
	}
	if run.Command != "echo 'world'" {
		t.Fatalf("expected interpolated command, got %q", run.Command)
	}
	if len(auditLog.events) != 0 {
		t.Fatalf("expected no audit events, got %d", len(auditLog.events))
	}
	if ctx.CapturedVariables != nil {
		if _, ok := ctx.CapturedVariables["out"]; ok {
			t.Fatal("expected no capture write from the primitive")
		}
	}
}

func TestRunShellCheckReturnsInterpolationError(t *testing.T) {
	ctx := makeCtx()
	step := model.Step{ID: "s", Command: "echo {{missing.var}}"}
	runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}

	run := runShellCheck(&step, ctx, runner)

	if run.RunErr == nil {
		t.Fatal("expected interpolation error")
	}
	if run.Metrics != nil {
		t.Fatal("expected no metrics capture on interpolation failure")
	}
}

func TestRunScriptCheckEmitsNoAudit(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "check.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\necho hello\nexit 3\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	workflowPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(workflowPath, []byte("name: test\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	auditLog := &mockAuditLogger{}
	ctx := makeCtx()
	ctx.AuditLogger = auditLog
	ctx.WorkflowFile = workflowPath
	step := model.Step{ID: "s", Script: "check.sh"}
	runner := &mockRunner{results: []ProcessResult{{ExitCode: 3, Stdout: "hello\n"}}}

	run := runScriptCheck(&step, ctx, runner)

	if run.RunErr != nil {
		t.Fatalf("unexpected error: %v", run.RunErr)
	}
	if run.Result.ExitCode != 3 {
		t.Fatalf("expected exit code 3, got %d", run.Result.ExitCode)
	}
	if len(auditLog.events) != 0 {
		t.Fatalf("expected no audit events, got %d", len(auditLog.events))
	}
}
