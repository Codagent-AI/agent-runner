package runner

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/codagent/agent-runner/internal/loader"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/stateio"
)

func TestResumeUntil(t *testing.T) {
	workflow := &model.Workflow{Steps: []model.Step{{ID: "A"}, {ID: "B"}, {ID: "C"}}}
	for _, target := range []string{"", "B", "C"} {
		if err := validateResumeUntil(workflow, target, "B"); err != nil {
			t.Fatal(err)
		}
	}
	for _, target := range []string{"A", "missing"} {
		if err := validateResumeUntil(workflow, target, "B"); err == nil {
			t.Fatalf("accepted %s", target)
		}
	}
}

func TestCappedRunResumesWithInvocationCap(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	workflowFile := filepath.Join(dir, "resume-cap-v1.0.yaml")
	body := "name: resume-cap\nsteps:\n  - id: A\n    command: echo A\n  - id: B\n    command: echo B\n  - id: C\n    command: echo C\n"
	if err := os.WriteFile(workflowFile, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	workflow, err := loader.LoadWorkflow(workflowFile, loader.Options{})
	if err != nil {
		t.Fatal(err)
	}
	runDir := filepath.Join(dir, "run")
	process := &mockRunner{}
	result, err := RunWorkflow(&workflow, nil, &Options{WorkflowFile: workflowFile, SessionDir: runDir, Until: "A", ProcessRunner: process, Log: &mockLog{}})
	if err != nil || result != ResultSuccess {
		t.Fatalf("result=%s error=%v", result, err)
	}
	stateFile := filepath.Join(runDir, "state.json")
	before, err := os.ReadFile(stateFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, cap := range []string{"A", "missing"} {
		if _, err := PrepareResume(stateFile, &Options{Until: cap}); err == nil {
			t.Fatalf("accepted cap %s", cap)
		}
		after, _ := os.ReadFile(stateFile)
		if !bytes.Equal(after, before) {
			t.Fatal("invalid cap changed state")
		}
	}
	result, err = ResumeWorkflow(stateFile, &Options{Until: "B", ProcessRunner: process, Log: &mockLog{}})
	if err != nil || result != ResultSuccess {
		t.Fatalf("resume result=%s error=%v", result, err)
	}
	state, err := stateio.ReadState(stateFile)
	if err != nil {
		t.Fatal(err)
	}
	if state.Completed || state.CurrentStep.Nested.StepID != "B" || !state.CurrentStep.Nested.Completed {
		t.Fatalf("state=%+v", state)
	}
	result, err = ResumeWorkflow(stateFile, &Options{ProcessRunner: process, Log: &mockLog{}})
	if err != nil || result != ResultSuccess {
		t.Fatalf("final resume result=%s error=%v", result, err)
	}
	if _, err := PrepareResume(stateFile, &Options{Until: "C"}); err == nil || errors.Is(err, ErrAlreadyCompleted) {
		t.Fatalf("completed cap error=%v", err)
	}
	if len(process.calls) != 3 {
		t.Fatalf("process calls=%v", process.calls)
	}
}
