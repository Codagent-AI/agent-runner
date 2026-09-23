package runner

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/config"
	"github.com/codagent/agent-runner/internal/exec"
	"github.com/codagent/agent-runner/internal/loader"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/stateio"
	"github.com/google/go-cmp/cmp"
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

func TestResumeTopLevelForEachRestartsChangedIteration(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	workflowPath := filepath.Join(dir, "tasks-v1.0.yaml")
	source := `name: tasks
steps:
  - id: implement-tasks
    loop:
      over: "tasks/*.md"
      as: task_file
    steps:
      - id: first
        command: echo FIRST={{task_file}}
      - id: gate
        command: echo GATE={{task_file}}
`
	if err := os.WriteFile(workflowPath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	iteration := 0
	state := model.RunState{
		WorkflowFile: workflowPath,
		WorkflowName: "tasks",
		WorkflowHash: stateio.ComputeWorkflowHash(source),
		CurrentStep: model.CurrentStep{Nested: &model.NestedStepState{
			StepID: "implement-tasks", Iteration: &iteration,
			LoopVar: map[string]string{"task_file": "tasks/a.md"},
			Child: &model.NestedStepState{StepID: "gate", CapturedVariables: map[string]model.CapturedValue{
				"task_start_head": model.NewCapturedString("stale"),
			}},
		}},
	}
	if err := stateio.WriteState(&state, dir); err != nil {
		t.Fatal(err)
	}
	runner := &mockRunner{}
	result, err := ResumeWorkflow(filepath.Join(dir, "state.json"), &Options{
		ProcessRunner: runner,
		GlobExpander:  &mockGlob{matches: []string{"tasks/0.md", "tasks/a.md"}},
		Log:           &mockLog{},
	})
	if err != nil || result != ResultSuccess {
		t.Fatalf("resume result = %q, error = %v", result, err)
	}
	if len(runner.calls) != 4 || !strings.Contains(runner.calls[0][2], "FIRST='tasks/0.md'") || !strings.Contains(runner.calls[1][2], "GATE='tasks/0.md'") {
		t.Fatalf("resumed commands = %v, want first then gate for tasks/0.md", runner.calls)
	}
	resumedState, err := stateio.ReadState(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, stale := resumedState.CurrentStep.Nested.CapturedVariables["task_start_head"]; stale {
		t.Fatal("stale iteration capture survived resume")
	}
}

func TestResumeAfterFailedForEachIterationScopesCaptures(t *testing.T) {
	const loop = `    loop:
      over: "tasks/*.md"
      as: task_file
    steps:
      - id: first
        command: echo FIRST={{task_file}}
      - id: record
        command: git rev-parse HEAD
        capture: task_start_head
      - id: gate
        command: echo GATE={{task_start_head}}
`
	layouts := []struct {
		name  string
		files map[string]string
	}{
		{name: "top-level loop", files: map[string]string{
			"tasks-v1.0.yaml": "name: tasks\nsteps:\n  - id: implement-tasks\n" + loop,
		}},
		{name: "loop in sub-workflow", files: map[string]string{
			"tasks-v1.0.yaml": "name: tasks\nsteps:\n  - id: implement\n    workflow: child-v1.0.yaml\n",
			"child-v1.0.yaml": "name: child\nsteps:\n  - id: implement-tasks\n" + loop,
		}},
	}
	cases := []struct {
		name        string
		resumeItems []string
		wantCmds    []string
		// restarted marks a restarted iteration, which must begin without
		// the failed iteration's captures.
		restarted bool
	}{
		{
			name:        "changed item restarts without stale capture",
			resumeItems: []string{"tasks/0.md"},
			wantCmds:    []string{"echo FIRST='tasks/0.md'", "git rev-parse HEAD", "echo GATE='fresh-head'"},
			restarted:   true,
		},
		{
			name:        "same item resumes failed step with its capture",
			resumeItems: []string{"tasks/a.md"},
			wantCmds:    []string{"echo GATE='a-head'"},
		},
	}
	for _, layout := range layouts {
		for _, tc := range cases {
			t.Run(layout.name+"/"+tc.name, func(t *testing.T) {
				t.Setenv("HOME", t.TempDir())
				dir := t.TempDir()
				for name, body := range layout.files {
					if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				workflowPath := filepath.Join(dir, "tasks-v1.0.yaml")
				workflow, err := loader.LoadWorkflow(workflowPath, loader.Options{})
				if err != nil {
					t.Fatal(err)
				}
				sessionDir := t.TempDir()
				handle, err := PrepareRun(&workflow, nil, &Options{
					WorkflowFile: workflowPath,
					SessionDir:   sessionDir,
					ProcessRunner: &mockRunner{results: []exec.ProcessResult{
						{}, {Stdout: "a-head"}, {ExitCode: 1},
					}},
					GlobExpander: &mockGlob{matches: []string{"tasks/a.md"}},
					Log:          &mockLog{},
				})
				if err != nil {
					t.Fatalf("PrepareRun: %v", err)
				}
				if result := ExecuteFromHandle(handle, nil); result != ResultFailed {
					t.Fatalf("initial result = %q, want failure at gate", result)
				}
				auditPath := filepath.Join(sessionDir, "audit.log")
				firstAudit, err := os.ReadFile(auditPath)
				if err != nil {
					t.Fatal(err)
				}

				resumedRunner := &mockRunner{results: []exec.ProcessResult{{}, {Stdout: "fresh-head"}}}
				result, err := ResumeWorkflow(filepath.Join(sessionDir, "state.json"), &Options{
					ProcessRunner: resumedRunner,
					GlobExpander:  &mockGlob{matches: tc.resumeItems},
					Log:           &mockLog{},
				})
				if err != nil || result != ResultSuccess {
					t.Fatalf("resume result = %q, error = %v", result, err)
				}
				gotCmds := make([]string, 0, len(resumedRunner.calls))
				for _, call := range resumedRunner.calls {
					gotCmds = append(gotCmds, call[2])
				}
				if diff := cmp.Diff(tc.wantCmds, gotCmds); diff != "" {
					t.Fatalf("resumed commands mismatch (-want +got):\n%s", diff)
				}

				if !tc.restarted {
					return
				}
				fullAudit, err := os.ReadFile(auditPath)
				if err != nil {
					t.Fatal(err)
				}
				var iterationStarts int
				for _, line := range strings.Split(string(fullAudit[len(firstAudit):]), "\n") {
					if !strings.Contains(line, " iteration_start ") {
						continue
					}
					iterationStarts++
					if strings.Contains(line, `"task_start_head"`) {
						t.Fatalf("restarted iteration sees the failed iteration's task_start_head: %s", line)
					}
				}
				if iterationStarts != 1 {
					t.Fatalf("resumed iteration_start events = %d, want 1:\n%s", iterationStarts, fullAudit[len(firstAudit):])
				}
			})
		}
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

func TestPrepareRunCleansUpFreshRunWhenSessionSetupFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	project := t.TempDir()
	t.Chdir(project)

	workflow := model.Workflow{
		Name:     "target",
		Sessions: []model.SessionDecl{{Name: "lead", Agent: "lead-profile"}},
		Steps:    []model.Step{{ID: "done", Command: "echo done"}},
	}
	workflow.ApplyDefaults()
	_, err := PrepareRun(&workflow, nil, &Options{
		WorkflowFile:      "builtin:core/target-v1.0.yaml",
		NamedSessionDecls: map[string]string{"lead": "different-profile"},
		ProcessRunner:     &mockRunner{},
		GlobExpander:      &mockGlob{},
		Log:               &mockLog{},
	})
	if err == nil || !strings.Contains(err.Error(), "session declaration") {
		t.Fatalf("PrepareRun() error = %v, want session setup error", err)
	}

	runsDir := filepath.Join(home, ".agent-runner", "projects", audit.EncodePath(project), "runs")
	entries, readErr := os.ReadDir(runsDir)
	if readErr == nil && len(entries) != 0 {
		t.Fatalf("fresh preparation left run directories: %v", entries)
	}
	if readErr != nil && !os.IsNotExist(readErr) {
		t.Fatalf("read runs directory: %v", readErr)
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

func TestPrepareRunPersistsIntakeHandoffAndPrepareResumeRestoresProvenance(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	workflowPath, err := filepath.Abs(filepath.Join("..", "..", "testdata", "intake-handoff-reference-v1.0.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	workflow, err := loader.LoadWorkflow(workflowPath, loader.Options{})
	if err != nil {
		t.Fatal(err)
	}
	sessionDir := t.TempDir()
	runner := &mockRunner{}

	handle, err := PrepareRun(&workflow, nil, &Options{
		WorkflowFile:          workflowPath,
		SessionDir:            sessionDir,
		IntakeHandoffContents: "sealed context",
		IntakeParentRunID:     "intake-parent-run",
		ProcessRunner:         runner,
		GlobExpander:          &mockGlob{},
		Log:                   &mockLog{},
	})
	if err != nil {
		t.Fatalf("PrepareRun() error = %v", err)
	}
	if handle.rs.ctx.IntakeHandoffContents != "sealed context" {
		t.Fatalf("prepared handoff contents = %q, want sealed context", handle.rs.ctx.IntakeHandoffContents)
	}
	state, err := stateio.ReadState(filepath.Join(sessionDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if state.IntakeHandoffContents != "sealed context" || state.IntakeParentRunID != "intake-parent-run" || state.RunID != filepath.Base(sessionDir) {
		t.Fatalf("initial state provenance = %#v", state)
	}

	finalizeRun(handle.rs, ResultStopped)
	resumedRunner := &mockRunner{}
	resumed, err := PrepareResume(filepath.Join(sessionDir, "state.json"), &Options{
		ProcessRunner: resumedRunner,
		GlobExpander:  &mockGlob{},
		Log:           &mockLog{},
	})
	if err != nil {
		t.Fatalf("PrepareResume() error = %v", err)
	}
	if resumed.rs.ctx.IntakeParentRunID != "intake-parent-run" {
		t.Fatalf("resumed parent provenance = %q", resumed.rs.ctx.IntakeParentRunID)
	}
	if resumed.rs.ctx.IntakeHandoffContents != "sealed context" {
		t.Fatalf("resumed handoff contents = %q, want sealed context", resumed.rs.ctx.IntakeHandoffContents)
	}
	if result := ExecuteFromHandle(resumed, nil); result != ResultSuccess {
		t.Fatalf("resumed result = %q, want success", result)
	}
	if len(resumedRunner.calls) != 1 || !strings.Contains(resumedRunner.calls[0][2], "sealed context") {
		t.Fatalf("resumed interpolated command = %#v, want handoff contents", resumedRunner.calls)
	}
	state, err = stateio.ReadState(filepath.Join(sessionDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if state.IntakeParentRunID != "intake-parent-run" {
		t.Fatalf("rewritten state parent provenance = %q", state.IntakeParentRunID)
	}
	if state.IntakeHandoffContents != "sealed context" {
		t.Fatalf("rewritten handoff contents = %q, want sealed context", state.IntakeHandoffContents)
	}
}

func TestPrepareResumeInfersDeliveredIntakeHandoffFromLegacyNestedSession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	workflowPath := filepath.Join(dir, "routed-v1.0.yaml")
	workflowSource := `name: routed
steps:
  - id: define
    command: true
  - id: implement
    agent: implementor
    session: new
    mode: autonomous
    prompt: Implement the approved task.
`
	if err := os.WriteFile(workflowPath, []byte(workflowSource), 0o600); err != nil {
		t.Fatal(err)
	}
	sessionDir := t.TempDir()
	legacyState := model.RunState{
		WorkflowFile:          workflowPath,
		WorkflowName:          "routed",
		WorkflowHash:          stateio.ComputeWorkflowHash(workflowSource),
		IntakeHandoffContents: "Goal: add repository selection.",
		IntakeParentRunID:     "intake-parent-run",
		CurrentStep: model.CurrentStep{Nested: &model.NestedStepState{
			StepID: "define", Completed: true,
			Child: &model.NestedStepState{
				StepID: "proposal", SessionIDs: map[string]string{"proposal": "session-id"}, Completed: true,
			},
		}},
	}
	if err := stateio.WriteState(&legacyState, sessionDir); err != nil {
		t.Fatal(err)
	}
	profiles := &config.Config{ActiveAgents: map[string]*config.Agent{
		"implementor": {DefaultMode: "autonomous", CLI: "claude", Model: "sonnet"},
	}}
	handle, err := PrepareResume(filepath.Join(sessionDir, "state.json"), &Options{
		ProfileStore: profiles, ProcessRunner: &mockRunner{}, GlobExpander: &mockGlob{}, Log: &mockLog{},
	})
	if err != nil {
		t.Fatalf("PrepareResume() error = %v", err)
	}
	defer finalizeRun(handle.rs, ResultStopped)
	if !handle.rs.ctx.IntakeHandoffDelivered() {
		t.Fatal("legacy resume did not infer that a nested agent already received the intake handoff")
	}
}

func TestResumeAfterPreAgentFailureDeliversIntakeHandoffToFirstAgent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	workflowPath := filepath.Join(t.TempDir(), "routed-v1.0.yaml")
	workflowSource := `name: routed
steps:
  - id: prerequisite
    command: false
  - id: plan
    agent: lead
    session: new
    mode: autonomous
    prompt: Plan the routed change.
`
	if err := os.WriteFile(workflowPath, []byte(workflowSource), 0o600); err != nil {
		t.Fatal(err)
	}
	workflow, err := loader.LoadWorkflow(workflowPath, loader.Options{})
	if err != nil {
		t.Fatal(err)
	}
	profiles := &config.Config{ActiveAgents: map[string]*config.Agent{
		"lead": {DefaultMode: "autonomous", CLI: "claude", Model: "sonnet", Effort: "high"},
	}}
	sessionDir := t.TempDir()
	firstRunner := &mockRunner{results: []exec.ProcessResult{{ExitCode: 1}}}
	handle, err := PrepareRun(&workflow, nil, &Options{
		WorkflowFile:          workflowPath,
		SessionDir:            sessionDir,
		IntakeHandoffContents: "Goal: preserve this routed context.",
		IntakeParentRunID:     "intake-parent-run",
		ProfileStore:          profiles,
		ProcessRunner:         firstRunner,
		GlobExpander:          &mockGlob{},
		Log:                   &mockLog{},
	})
	if err != nil {
		t.Fatalf("PrepareRun() error = %v", err)
	}
	if result := ExecuteFromHandle(handle, nil); result != ResultFailed {
		t.Fatalf("initial result = %q, want failure before the agent", result)
	}
	if len(firstRunner.calls) != 1 {
		t.Fatalf("initial calls = %#v, want prerequisite only", firstRunner.calls)
	}

	resumedRunner := &mockRunner{results: []exec.ProcessResult{{ExitCode: 0}, {ExitCode: 0}}}
	resumed, err := PrepareResume(filepath.Join(sessionDir, "state.json"), &Options{
		ProfileStore:  profiles,
		ProcessRunner: resumedRunner,
		GlobExpander:  &mockGlob{},
		Log:           &mockLog{},
	})
	if err != nil {
		t.Fatalf("PrepareResume() error = %v", err)
	}
	if result := ExecuteFromHandle(resumed, nil); result != ResultSuccess {
		t.Fatalf("resumed result = %q, want success", result)
	}
	if len(resumedRunner.calls) != 2 {
		t.Fatalf("resumed calls = %#v, want prerequisite and first agent", resumedRunner.calls)
	}

	auditData, err := os.ReadFile(filepath.Join(sessionDir, "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	const handoff = "Goal: preserve this routed context."
	if count := strings.Count(string(auditData), handoff); count != 1 {
		t.Fatalf("handoff occurrence count = %d, want exactly one first-agent delivery; audit:\n%s", count, auditData)
	}
	if !strings.Contains(string(auditData), "Context from the intake conversation") {
		t.Fatalf("resumed first-agent prompt omitted intake framing; audit:\n%s", auditData)
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

// A resumed run must interpolate the value its original invocation had.
func TestResumeKeepsOriginalHandoffContents(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	workflowPath, err := filepath.Abs(filepath.Join("..", "..", "testdata", "intake-handoff-reference-v1.0.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	workflow, err := loader.LoadWorkflow(workflowPath, loader.Options{})
	if err != nil {
		t.Fatal(err)
	}
	sessionDir := t.TempDir()

	handle, err := PrepareRun(&workflow, nil, &Options{
		WorkflowFile:          workflowPath,
		SessionDir:            sessionDir,
		IntakeHandoffContents: "agreed context",
		IntakeParentRunID:     "intake-parent-run",
		ProcessRunner:         &mockRunner{},
		GlobExpander:          &mockGlob{},
		Log:                   &mockLog{},
	})
	if err != nil {
		t.Fatalf("PrepareRun() error = %v", err)
	}
	finalizeRun(handle.rs, ResultStopped)

	resumedRunner := &mockRunner{}
	resumed, err := PrepareResume(filepath.Join(sessionDir, "state.json"), &Options{
		ProcessRunner: resumedRunner,
		GlobExpander:  &mockGlob{},
		Log:           &mockLog{},
	})
	if err != nil {
		t.Fatalf("PrepareResume() error = %v", err)
	}
	if got := resumed.rs.ctx.IntakeHandoffContents; got != "agreed context" {
		t.Fatalf("resumed handoff contents = %q, want the contents the original invocation saw", got)
	}
	if result := ExecuteFromHandle(resumed, nil); result != ResultSuccess {
		t.Fatalf("resumed result = %q, want success", result)
	}
	if len(resumedRunner.calls) != 1 || !strings.Contains(resumedRunner.calls[0][2], "agreed context") {
		t.Fatalf("resumed prompt omitted the persisted handoff: %#v", resumedRunner.calls)
	}
}
