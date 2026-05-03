package exec

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/google/go-cmp/cmp"
)

func TestExecuteScriptStepCapturesJSONArrayAsTypedList(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "emit.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '[\"claude\",\"codex\"]\\n'\n"), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	ctx := model.NewRootContext(&model.RootContextOptions{WorkflowFile: filepath.Join(dir, "workflow.yaml")})
	step := model.Step{ID: "detect", Script: "emit.sh", Capture: "detected", CaptureFormat: "json"}
	runner := &mockRunner{results: []ProcessResult{{ExitCode: 0, Stdout: "[\"claude\",\"codex\"]\n"}}}
	outcome, err := ExecuteScriptStep(&step, ctx, runner, &mockLogger{})
	if err != nil {
		t.Fatalf("ExecuteScriptStep() returned error: %v", err)
	}
	if outcome != OutcomeSuccess {
		t.Fatalf("outcome = %s, want success", outcome)
	}
	got := ctx.CapturedVariables["detected"]
	want := model.CapturedValue{Kind: model.CaptureList, List: []string{"claude", "codex"}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("captured mismatch (-want +got):\n%s", diff)
	}
}

func TestExecuteScriptStepEmitsAuditEvents(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "emit.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf ok\n"), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	recorder := &mockAuditLogger{}
	ctx := model.NewRootContext(&model.RootContextOptions{WorkflowFile: filepath.Join(dir, "workflow.yaml")})
	ctx.AuditLogger = recorder
	step := model.Step{ID: "detect", Script: "emit.sh", Capture: "detected"}
	runner := &mockRunner{results: []ProcessResult{{ExitCode: 0, Stdout: "ok"}}}

	outcome, err := ExecuteScriptStep(&step, ctx, runner, &mockLogger{})
	if err != nil {
		t.Fatalf("ExecuteScriptStep() returned error: %v", err)
	}
	if outcome != OutcomeSuccess {
		t.Fatalf("outcome = %s, want success", outcome)
	}
	start := findAuditEvent(recorder.events, audit.EventStepStart)
	if start == nil || start.Prefix != "[detect]" {
		t.Fatalf("expected detect step_start, got %+v", recorder.events)
	}
	end := findAuditEvent(recorder.events, audit.EventStepEnd)
	if end == nil || end.Prefix != "[detect]" || end.Data["outcome"] != "success" {
		t.Fatalf("expected successful detect step_end, got %+v", recorder.events)
	}
}

func TestExecuteScriptStepUsesDelayedPrefixForTUIRunners(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "models.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf ok\n"), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	ctx := model.NewRootContext(&model.RootContextOptions{WorkflowFile: filepath.Join(dir, "workflow.yaml")})
	step := model.Step{ID: "models", Script: "models.sh"}
	runner := &scriptPrefixRunner{mockRunner: mockRunner{results: []ProcessResult{{ExitCode: 0}}}}

	outcome, err := ExecuteScriptStep(&step, ctx, runner, &mockLogger{})
	if err != nil {
		t.Fatalf("ExecuteScriptStep() returned error: %v", err)
	}
	if outcome != OutcomeSuccess {
		t.Fatalf("outcome = %s, want success", outcome)
	}
	if runner.immediatePrefix != "" {
		t.Fatalf("script step should not use immediate prefix notification, got %q", runner.immediatePrefix)
	}
	if runner.delayedPrefix != "[models]" {
		t.Fatalf("delayedPrefix = %q, want [models]", runner.delayedPrefix)
	}
	if runner.delay < 2*time.Second {
		t.Fatalf("delay = %s, want at least 2s", runner.delay)
	}
}

type scriptPrefixRunner struct {
	mockRunner
	immediatePrefix string
	delayedPrefix   string
	delay           time.Duration
}

func (r *scriptPrefixRunner) SetPrefix(prefix string) {
	r.immediatePrefix = prefix
}

func (r *scriptPrefixRunner) SetScriptPrefix(prefix string, delay time.Duration) {
	r.delayedPrefix = prefix
	r.delay = delay
}

func TestExecuteUIStepExpandsTypedListOptionsAndCapturesTypedMap(t *testing.T) {
	ctx := model.NewRootContext(&model.RootContextOptions{WorkflowFile: "workflow.yaml"})
	ctx.CapturedVariables["detected"] = model.CapturedValue{Kind: model.CaptureList, List: []string{"claude", "codex"}}
	ctx.UIStepHandler = func(req *model.UIStepRequest) (model.UIStepResult, error) {
		if len(req.Inputs) != 1 || len(req.Inputs[0].Options) != 2 || req.Inputs[0].Options[1] != "codex" {
			t.Fatalf("resolved inputs = %#v", req.Inputs)
		}
		return model.UIStepResult{Outcome: "continue", Inputs: map[string]string{"cli": "codex"}}, nil
	}

	step := model.Step{
		ID:      "pick",
		Mode:    model.ModeUI,
		Title:   "Pick",
		Actions: []model.UIAction{{Label: "Continue", Outcome: "continue"}},
		Inputs: []model.UIInput{{
			Kind:    "single_select",
			ID:      "cli",
			Prompt:  "CLI",
			Options: []string{"{{detected}}"},
		}},
		Capture:        "choice",
		OutcomeCapture: "action",
	}
	outcome, err := ExecuteUIStep(&step, ctx, &mockLogger{})
	if err != nil {
		t.Fatalf("ExecuteUIStep() returned error: %v", err)
	}
	if outcome != OutcomeSuccess {
		t.Fatalf("outcome = %s, want success", outcome)
	}
	if got, want := ctx.CapturedVariables["choice"], (model.CapturedValue{Kind: model.CaptureMap, Map: map[string]string{"cli": "codex"}}); !cmp.Equal(want, got) {
		t.Fatalf("choice capture mismatch (-want +got):\n%s", cmp.Diff(want, got))
	}
	if got, want := ctx.CapturedVariables["action"], (model.CapturedValue{Kind: model.CaptureString, Str: "continue"}); !cmp.Equal(want, got) {
		t.Fatalf("action capture mismatch (-want +got):\n%s", cmp.Diff(want, got))
	}
}

func TestExecuteUIStepEmitsAuditEvents(t *testing.T) {
	recorder := &mockAuditLogger{}
	ctx := model.NewRootContext(&model.RootContextOptions{WorkflowFile: "workflow.yaml"})
	ctx.AuditLogger = recorder
	ctx.UIStepHandler = func(req *model.UIStepRequest) (model.UIStepResult, error) {
		return model.UIStepResult{Outcome: "continue"}, nil
	}

	step := model.Step{
		ID:             "pick",
		Mode:           model.ModeUI,
		Title:          "Pick",
		Actions:        []model.UIAction{{Label: "Continue", Outcome: "continue"}},
		OutcomeCapture: "action",
	}
	outcome, err := ExecuteUIStep(&step, ctx, &mockLogger{})
	if err != nil {
		t.Fatalf("ExecuteUIStep() returned error: %v", err)
	}
	if outcome != OutcomeSuccess {
		t.Fatalf("outcome = %s, want success", outcome)
	}
	start := findAuditEvent(recorder.events, audit.EventStepStart)
	if start == nil || start.Prefix != "[pick]" {
		t.Fatalf("expected pick step_start, got %+v", recorder.events)
	}
	end := findAuditEvent(recorder.events, audit.EventStepEnd)
	if end == nil || end.Prefix != "[pick]" || end.Data["outcome"] != "success" {
		t.Fatalf("expected successful pick step_end, got %+v", recorder.events)
	}
}
