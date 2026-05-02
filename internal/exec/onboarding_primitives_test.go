package exec

import (
	"os"
	"path/filepath"
	"testing"

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
	outcome, err := ExecuteScriptStep(&step, ctx, &mockRunner{}, &mockLogger{})
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

func TestExecuteUIStepExpandsTypedListOptionsAndCapturesTypedMap(t *testing.T) {
	ctx := model.NewRootContext(&model.RootContextOptions{WorkflowFile: "workflow.yaml"})
	ctx.CapturedVariables["detected"] = model.CapturedValue{Kind: model.CaptureList, List: []string{"claude", "codex"}}
	ctx.UIStepHandler = func(req model.UIStepRequest) (model.UIStepResult, error) {
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
			Kind:    "single-select",
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
