package runner

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/loader"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/stateio"
)

func TestPrepareResume_LoadsExactRecordedVersion(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	v1Path := filepath.Join(dir, "deploy-v1.0.yaml")
	v1 := `name: deploy
steps:
  - id: ship
    command: echo v1
`
	v2 := `name: deploy
steps:
  - id: ship
    command: echo v2
`
	if err := os.WriteFile(v1Path, []byte(v1), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "deploy-v2.0.yaml"), []byte(v2), 0o600); err != nil {
		t.Fatal(err)
	}
	state := model.RunState{
		WorkflowFile: v1Path,
		WorkflowName: "deploy",
		WorkflowHash: stateio.ComputeWorkflowHash(v1),
		CurrentStep: model.CurrentStep{
			Nested: &model.NestedStepState{StepID: "ship"},
		},
	}
	if err := stateio.WriteState(&state, dir); err != nil {
		t.Fatal(err)
	}

	handle, err := PrepareResume(filepath.Join(dir, "state.json"), &Options{
		ProcessRunner: &mockRunner{},
		GlobExpander:  &mockGlob{},
		Log:           &mockLog{},
	})
	if err != nil {
		t.Fatalf("PrepareResume: %v", err)
	}
	defer finalizeRun(handle.rs, ResultStopped)
	if got := handle.rs.workflow.Steps[0].Command; got != "echo v1" {
		t.Fatalf("resumed command = %q, want exact recorded v1 command", got)
	}
}

func TestPrepareRun_RecordsExactVersionMetadata(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	sourceDir := t.TempDir()
	workflowPath := filepath.Join(sourceDir, "deploy-v2.0.yaml")
	source := `name: deploy
steps:
  - id: ship
    command: echo ship
`
	if err := os.WriteFile(workflowPath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	workflow := model.Workflow{
		Name:  "deploy",
		Steps: []model.Step{{ID: "ship", Command: "echo ship"}},
	}
	workflow.ApplyDefaults()
	sessionDir := t.TempDir()

	handle, err := PrepareRun(&workflow, map[string]string{"env": "staging"}, &Options{
		WorkflowFile:  workflowPath,
		SessionDir:    sessionDir,
		ProcessRunner: &mockRunner{},
		GlobExpander:  &mockGlob{},
		Log:           &mockLog{},
	})
	if err != nil {
		t.Fatalf("PrepareRun: %v", err)
	}
	defer finalizeRun(handle.rs, ResultStopped)

	state, err := stateio.ReadState(filepath.Join(sessionDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if state.WorkflowFile != workflowPath || state.WorkflowName != "deploy" {
		t.Fatalf("state workflow metadata = %q, %q", state.WorkflowFile, state.WorkflowName)
	}
	if state.WorkflowHash != stateio.ComputeWorkflowHash(source) {
		t.Fatalf("state workflow hash = %q, want source hash", state.WorkflowHash)
	}

	auditData, err := os.ReadFile(filepath.Join(sessionDir, "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.SplitN(strings.TrimSpace(string(auditData)), " run_start ", 2)
	if len(parts) != 2 {
		t.Fatalf("audit log missing run_start: %s", auditData)
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(parts[1]), &data); err != nil {
		t.Fatalf("decode run_start: %v", err)
	}
	if data["workflow_file"] != workflowPath || data["workflow_name"] != "deploy" {
		t.Fatalf("run_start workflow metadata = %#v", data)
	}
	if data["workflow_hash"] != stateio.ComputeWorkflowHash(source) {
		t.Fatalf("run_start workflow hash = %#v", data["workflow_hash"])
	}
	context, ok := data["context"].(map[string]any)
	if !ok {
		t.Fatalf("run_start context = %#v", data["context"])
	}
	params, ok := context["params"].(map[string]any)
	if !ok || params["env"] != "staging" {
		t.Fatalf("run_start params = %#v", context["params"])
	}
}

func TestPrepareResume_RestoresAgentOverride(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	workflowPath := filepath.Join(t.TempDir(), "intake-v1.0.yaml")
	workflowSource := "name: intake\nsteps:\n  - id: plan\n    command: echo plan\n"
	if err := os.WriteFile(workflowPath, []byte(workflowSource), 0o600); err != nil {
		t.Fatal(err)
	}
	sessionDir := t.TempDir()
	state := model.RunState{
		WorkflowFile:  workflowPath,
		WorkflowName:  "intake",
		WorkflowHash:  stateio.ComputeWorkflowHash(workflowSource),
		AgentOverride: &model.AgentOverride{CLI: "codex", Model: "gpt-5.2"},
		CurrentStep:   model.CurrentStep{Nested: &model.NestedStepState{StepID: "plan"}},
	}
	if err := stateio.WriteState(&state, sessionDir); err != nil {
		t.Fatal(err)
	}

	handle, err := PrepareResume(filepath.Join(sessionDir, "state.json"), &Options{
		ProcessRunner: &mockRunner{}, GlobExpander: &mockGlob{}, Log: &mockLog{},
	})
	if err != nil {
		t.Fatalf("PrepareResume() error = %v", err)
	}
	defer finalizeRun(handle.rs, ResultStopped)
	if got := handle.rs.ctx.AgentOverride; got == nil || got.CLI != "codex" || got.Model != "gpt-5.2" {
		t.Fatalf("resumed override = %#v, want codex/gpt-5.2", got)
	}
}

func TestPrepareRun_PersistsAgentOverride(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	workflow := model.Workflow{Name: "intake", Steps: []model.Step{{ID: "plan", Command: "echo plan"}}}
	workflow.ApplyDefaults()
	sessionDir := t.TempDir()
	handle, err := PrepareRun(&workflow, map[string]string{}, &Options{
		WorkflowFile: "builtin:core/intake-v1.0.yaml", SessionDir: sessionDir,
		AgentOverride: &model.AgentOverride{CLI: "codex", Model: "gpt-5.2"},
		ProcessRunner: &mockRunner{}, GlobExpander: &mockGlob{}, Log: &mockLog{},
	})
	if err != nil {
		t.Fatalf("PrepareRun() error = %v", err)
	}
	defer finalizeRun(handle.rs, ResultStopped)
	state, err := stateio.ReadState(filepath.Join(sessionDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := state.AgentOverride; got == nil || got.CLI != "codex" || got.Model != "gpt-5.2" {
		t.Fatalf("recorded override = %#v, want codex/gpt-5.2", got)
	}
}

func TestPrepareRunCopiesIntakeHandoffAndPrepareResumeRestoresProvenance(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	sourceDir := t.TempDir()
	workflowPath, err := filepath.Abs(filepath.Join("..", "..", "testdata", "intake-handoff-reference-v1.0.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	workflow, err := loader.LoadWorkflow(workflowPath, loader.Options{})
	if err != nil {
		t.Fatal(err)
	}
	handoffSource := filepath.Join(sourceDir, "sealed-handoff.md")
	if err := os.WriteFile(handoffSource, []byte("sealed context"), 0o600); err != nil {
		t.Fatal(err)
	}
	sessionDir := t.TempDir()
	runner := &mockRunner{}

	handle, err := PrepareRun(&workflow, nil, &Options{
		WorkflowFile:        workflowPath,
		SessionDir:          sessionDir,
		IntakeHandoffSource: handoffSource,
		IntakeParentRunID:   "intake-parent-run",
		ProcessRunner:       runner,
		GlobExpander:        &mockGlob{},
		Log:                 &mockLog{},
	})
	if err != nil {
		t.Fatalf("PrepareRun() error = %v", err)
	}
	handoffCopy := handle.rs.ctx.IntakeHandoff
	if handoffCopy == handoffSource || filepath.Dir(handoffCopy) != sessionDir {
		t.Fatalf("handoff copy = %q, want a distinct path in %q", handoffCopy, sessionDir)
	}
	if contents, err := os.ReadFile(handoffCopy); err != nil || string(contents) != "sealed context" {
		t.Fatalf("copied handoff = %q, %v", contents, err)
	}
	state, err := stateio.ReadState(filepath.Join(sessionDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if state.IntakeHandoff != handoffCopy || state.IntakeParentRunID != "intake-parent-run" || state.RunID != filepath.Base(sessionDir) {
		t.Fatalf("initial state provenance = %#v", state)
	}

	finalizeRun(handle.rs, ResultStopped)
	if err := os.Remove(handoffSource); err != nil {
		t.Fatal(err)
	}
	resumedRunner := &mockRunner{}
	resumed, err := PrepareResume(filepath.Join(sessionDir, "state.json"), &Options{
		ProcessRunner: resumedRunner,
		GlobExpander:  &mockGlob{},
		Log:           &mockLog{},
	})
	if err != nil {
		t.Fatalf("PrepareResume() error = %v", err)
	}
	if resumed.rs.ctx.IntakeHandoff != handoffCopy || resumed.rs.ctx.IntakeParentRunID != "intake-parent-run" {
		t.Fatalf("resumed provenance = (%q, %q)", resumed.rs.ctx.IntakeHandoff, resumed.rs.ctx.IntakeParentRunID)
	}
	if result := ExecuteFromHandle(resumed, nil); result != ResultSuccess {
		t.Fatalf("resumed result = %q, want success", result)
	}
	if len(resumedRunner.calls) != 1 || !strings.Contains(resumedRunner.calls[0][2], handoffCopy) {
		t.Fatalf("resumed interpolated command = %#v, want copied handoff path", resumedRunner.calls)
	}
	state, err = stateio.ReadState(filepath.Join(sessionDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if state.IntakeHandoff != handoffCopy || state.IntakeParentRunID != "intake-parent-run" {
		t.Fatalf("rewritten state provenance = (%q, %q)", state.IntakeHandoff, state.IntakeParentRunID)
	}
}

func TestRunWorkflowInterpolatesEmptyIntakeHandoff(t *testing.T) {
	workflowPath, err := filepath.Abs(filepath.Join("..", "..", "testdata", "intake-handoff-reference-v1.0.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	workflow, err := loader.LoadWorkflow(workflowPath, loader.Options{})
	if err != nil {
		t.Fatal(err)
	}
	processRunner := &mockRunner{}
	result, err := RunWorkflow(&workflow, nil, &Options{
		SessionDir:    t.TempDir(),
		WorkflowFile:  workflowPath,
		ProcessRunner: processRunner,
		GlobExpander:  &mockGlob{},
		Log:           &mockLog{},
	})
	if err != nil || result != ResultSuccess {
		t.Fatalf("RunWorkflow() = (%q, %v), want success", result, err)
	}
	if len(processRunner.calls) != 1 || processRunner.calls[0][2] != `printf "%s" ""` {
		t.Fatalf("interpolated command = %#v, want empty intake handoff", processRunner.calls)
	}
}

func TestPrepareResumeKeepsDirectRunIntakeProvenanceEmpty(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	workflowDir := t.TempDir()
	workflowPath := filepath.Join(workflowDir, "direct-v1.0.yaml")
	workflowSource := `name: direct
steps:
  - id: handoff
    command: 'printf "%s" "{{intake_handoff}}"'
`
	if err := os.WriteFile(workflowPath, []byte(workflowSource), 0o600); err != nil {
		t.Fatal(err)
	}
	workflow := model.Workflow{Name: "direct", Steps: []model.Step{{ID: "handoff", Command: `printf "%s" "{{intake_handoff}}"`}}}
	workflow.ApplyDefaults()
	sessionDir := t.TempDir()
	handle, err := PrepareRun(&workflow, nil, &Options{
		WorkflowFile: workflowPath, SessionDir: sessionDir,
		ProcessRunner: &mockRunner{}, GlobExpander: &mockGlob{}, Log: &mockLog{},
	})
	if err != nil {
		t.Fatal(err)
	}
	finalizeRun(handle.rs, ResultStopped)

	resumed, err := PrepareResume(filepath.Join(sessionDir, "state.json"), &Options{
		ProcessRunner: &mockRunner{}, GlobExpander: &mockGlob{}, Log: &mockLog{},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer finalizeRun(resumed.rs, ResultStopped)
	if resumed.rs.ctx.IntakeParentRunID != "" || resumed.rs.ctx.BuiltinVars()["intake_handoff"] != "" {
		t.Fatalf("resumed direct provenance = (%q, %q)", resumed.rs.ctx.IntakeParentRunID, resumed.rs.ctx.BuiltinVars()["intake_handoff"])
	}
}

func TestPrepareResume_WarnsAndContinuesAfterRecordedVersionEdit(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	workflowPath := filepath.Join(dir, "deploy-v1.0.yaml")
	original := `name: deploy
steps:
  - id: ship
    command: echo original
`
	edited := `name: deploy
steps:
  - id: ship
    command: echo edited
`
	if err := os.WriteFile(workflowPath, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	state := model.RunState{
		WorkflowFile: workflowPath,
		WorkflowName: "deploy",
		WorkflowHash: stateio.ComputeWorkflowHash(original),
		CurrentStep: model.CurrentStep{
			Nested: &model.NestedStepState{StepID: "ship"},
		},
	}
	if err := stateio.WriteState(&state, dir); err != nil {
		t.Fatal(err)
	}
	log := &mockLog{}

	handle, err := PrepareResume(filepath.Join(dir, "state.json"), &Options{
		ProcessRunner: &mockRunner{},
		GlobExpander:  &mockGlob{},
		Log:           log,
	})
	if err != nil {
		t.Fatalf("PrepareResume: %v", err)
	}
	defer finalizeRun(handle.rs, ResultStopped)
	if got := handle.rs.workflow.Steps[0].Command; got != "echo edited" {
		t.Fatalf("resumed command = %q, want edited contents", got)
	}
	if !strings.Contains(strings.Join(log.lines, "\n"), "workflow file has changed") {
		t.Fatalf("log = %v, want changed-file warning", log.lines)
	}
}

func TestPrepareResume_MissingRecordedVersionFailsWithoutSiblingFallback(t *testing.T) {
	dir := t.TempDir()
	v2 := `name: deploy
steps:
  - id: ship
    command: echo v2
`
	if err := os.WriteFile(filepath.Join(dir, "deploy-v2.0.yaml"), []byte(v2), 0o600); err != nil {
		t.Fatal(err)
	}
	missingV1 := filepath.Join(dir, "deploy-v1.0.yaml")
	state := model.RunState{
		WorkflowFile: missingV1,
		WorkflowName: "deploy",
		CurrentStep: model.CurrentStep{
			Nested: &model.NestedStepState{StepID: "ship"},
		},
	}
	if err := stateio.WriteState(&state, dir); err != nil {
		t.Fatal(err)
	}

	_, err := PrepareResume(filepath.Join(dir, "state.json"), &Options{})
	if err == nil {
		t.Fatal("PrepareResume returned nil error")
	}
	if !strings.Contains(err.Error(), "deploy-v1.0.yaml") {
		t.Fatalf("error = %q, want missing recorded version", err)
	}
}

func TestPrepareResume_LegacyDiskStateRequiresVersionedFilename(t *testing.T) {
	dir := t.TempDir()
	newer := `name: deploy
steps:
  - id: ship
    command: echo v1
`
	if err := os.WriteFile(filepath.Join(dir, "deploy-v1.0.yaml"), []byte(newer), 0o600); err != nil {
		t.Fatal(err)
	}
	state := model.RunState{
		WorkflowFile: filepath.Join(dir, "deploy.yaml"),
		WorkflowName: "deploy",
		CurrentStep: model.CurrentStep{
			Nested: &model.NestedStepState{StepID: "ship"},
		},
	}
	if err := stateio.WriteState(&state, dir); err != nil {
		t.Fatal(err)
	}

	_, err := PrepareResume(filepath.Join(dir, "state.json"), &Options{})
	if err == nil {
		t.Fatal("PrepareResume returned nil error")
	}
	message := err.Error()
	if !strings.Contains(message, "deploy.yaml") ||
		!strings.Contains(message, "deploy-v1.0.yaml") ||
		!strings.Contains(strings.ToLower(message), "rename") {
		t.Fatalf("error = %q, want actionable versioned-filename migration guidance", message)
	}
}

func TestPrepareResume_LegacyBuiltinExplainsBinaryIncompatibility(t *testing.T) {
	sessionDir := t.TempDir()
	state := model.RunState{
		WorkflowFile: "builtin:onboarding/onboarding.yaml",
		WorkflowName: "onboarding",
		CurrentStep: model.CurrentStep{
			Nested: &model.NestedStepState{StepID: "start"},
		},
	}
	if err := stateio.WriteState(&state, sessionDir); err != nil {
		t.Fatal(err)
	}

	_, err := PrepareResume(filepath.Join(sessionDir, "state.json"), &Options{})
	if err == nil {
		t.Fatal("PrepareResume returned nil error")
	}
	message := err.Error()
	for _, want := range []string{"predates workflow versioning", "restart", "current binary", "older binary"} {
		if !strings.Contains(message, want) {
			t.Fatalf("error = %q, want %q", message, want)
		}
	}
	if strings.Contains(strings.ToLower(message), "rename") {
		t.Fatalf("error = %q, must not tell users to rename an embedded workflow", message)
	}
}

func TestPrepareResume_CompletedLegacyRunSkipsWorkflowValidation(t *testing.T) {
	dir := t.TempDir()
	state := model.RunState{
		WorkflowFile: filepath.Join(dir, "missing-unversioned.yaml"),
		WorkflowName: "legacy",
		Completed:    true,
	}
	if err := stateio.WriteState(&state, dir); err != nil {
		t.Fatal(err)
	}

	_, err := PrepareResume(filepath.Join(dir, "state.json"), &Options{})
	if !errors.Is(err, ErrAlreadyCompleted) {
		t.Fatalf("PrepareResume error = %v, want ErrAlreadyCompleted before workflow validation", err)
	}
}

func TestPrepareResume_LastRecordedStepCompletedIsAlreadyCompleted(t *testing.T) {
	dir := t.TempDir()
	workflowPath := filepath.Join(dir, "deploy-v1.0.yaml")
	workflow := `name: deploy
steps:
  - id: ship
    command: echo ship
`
	if err := os.WriteFile(workflowPath, []byte(workflow), 0o600); err != nil {
		t.Fatal(err)
	}
	state := model.RunState{
		WorkflowFile: workflowPath,
		WorkflowName: "deploy",
		WorkflowHash: stateio.ComputeWorkflowHash(workflow),
		CurrentStep: model.CurrentStep{
			Nested: &model.NestedStepState{StepID: "ship", Completed: true},
		},
	}
	if err := stateio.WriteState(&state, dir); err != nil {
		t.Fatal(err)
	}

	_, err := PrepareResume(filepath.Join(dir, "state.json"), &Options{})
	if !errors.Is(err, ErrAlreadyCompleted) {
		t.Fatalf("PrepareResume error = %v, want ErrAlreadyCompleted", err)
	}
}

func TestPrepareResume_MissingDefinitionUsesSuccessfulAuditCompletion(t *testing.T) {
	dir := t.TempDir()
	state := model.RunState{
		WorkflowFile: filepath.Join(dir, "missing-v1.0.yaml"),
		WorkflowName: "missing",
		CurrentStep: model.CurrentStep{
			Nested: &model.NestedStepState{StepID: "final-step", Completed: true},
		},
	}
	if err := stateio.WriteState(&state, dir); err != nil {
		t.Fatal(err)
	}
	auditLog := "2026-07-25T00:00:00Z run_end {\"outcome\":\"success\"}\n"
	if err := os.WriteFile(filepath.Join(dir, "audit.log"), []byte(auditLog), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := PrepareResume(filepath.Join(dir, "state.json"), &Options{})
	if !errors.Is(err, ErrAlreadyCompleted) {
		t.Fatalf("PrepareResume error = %v, want ErrAlreadyCompleted from saved audit evidence", err)
	}
}
