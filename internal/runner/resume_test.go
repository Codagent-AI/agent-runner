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

func TestPrepareResume_RestoresRepositoryFrameAndValidatesIdentity(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	workspace, backend, frontend := t.TempDir(), t.TempDir(), t.TempDir()
	initGitWorktree(t, workspace)
	initGitWorktree(t, backend)
	initGitWorktree(t, frontend)
	workflowPath := filepath.Join(workspace, "repository-resume-v1.0.yaml")
	workflowSource := `name: repository-resume
scope: repositories
params:
  - name: repositories
steps:
  - id: implement
    command: echo implement
`
	if err := os.WriteFile(workflowPath, []byte(workflowSource), 0o600); err != nil {
		t.Fatal(err)
	}
	backendRoot := canonicalTestDir(t, backend)
	frontendRoot := canonicalTestDir(t, frontend)
	active := 1
	state := model.RunState{
		WorkflowFile:    workflowPath,
		WorkflowName:    "repository-resume",
		WorkflowHash:    stateio.ComputeWorkflowHash(workflowSource),
		WorkspaceDir:    canonicalTestDir(t, workspace),
		Params:          map[string]string{model.RepositoriesParam: "backend,frontend"},
		RepositoryIndex: &active,
		SelectedRepositories: []model.RepositoryIdentity{
			{Name: "backend", Dir: backendRoot},
			{Name: "frontend", Dir: frontendRoot},
		},
		RepositoryFrame: &model.RepositoryFrame{Repositories: []model.RepositoryExecutionState{
			{Identity: model.RepositoryIdentity{Name: "backend", Dir: backendRoot}, Status: model.RepositoryCompleted, Nested: &model.NestedStepState{StepID: "implement", CapturedVariables: map[string]model.CapturedValue{"output": model.NewCapturedString("backend")}}},
			{Identity: model.RepositoryIdentity{Name: "frontend", Dir: frontendRoot}, Status: model.RepositoryFailed, Nested: &model.NestedStepState{StepID: "implement", CapturedVariables: map[string]model.CapturedValue{"output": model.NewCapturedString("frontend")}, NamedSessions: map[string]string{"worker": "frontend-worker"}, NamedSessionDecls: map[string]string{"worker": "implementor"}, Child: &model.NestedStepState{StepID: "inner"}}},
		}},
		WorkspaceNamespace:      &model.NamespaceState{NamedSessions: map[string]string{"planner": "workspace-planner"}, NamedSessionDecls: map[string]string{"planner": "lead"}, CapturedVariables: map[string]model.CapturedValue{"shared": model.NewCapturedString("workspace")}},
		WorkspacePullRequestURL: "https://github.com/acme/workspace/pull/1",
		RepositoryPullRequestURLs: map[string]string{
			"backend":  "https://github.com/acme/backend/pull/2",
			"frontend": "https://github.com/acme/frontend/pull/3",
		},
		CurrentStep: model.CurrentStep{Nested: &model.NestedStepState{StepID: "implement"}},
	}
	sessionDir := t.TempDir()
	if err := stateio.WriteState(&state, sessionDir); err != nil {
		t.Fatal(err)
	}
	profiles := &config.Config{Repositories: map[string]config.Repository{
		"backend": {Path: backend}, "frontend": {Path: frontend},
	}}
	handle, err := PrepareResume(filepath.Join(sessionDir, "state.json"), &Options{
		WorkingDir: workspace, ProfileStore: profiles,
		ProcessRunner: &mockRunner{}, GlobExpander: &mockGlob{}, Log: &mockLog{},
	})
	if err != nil {
		t.Fatalf("PrepareResume() error = %v", err)
	}
	defer finalizeRun(handle.rs, ResultStopped)
	if diff := cmp.Diff(state.RepositoryFrame, handle.rs.ctx.RepositoryFrame); diff != "" {
		t.Fatalf("restored repository frame mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(model.NewCapturedString("workspace"), handle.rs.ctx.CapturedVariables["shared"]); diff != "" {
		t.Fatalf("workspace capture mismatch (-want +got):\n%s", diff)
	}
	if got := handle.rs.ctx.ResumeChildState; got == nil || got.StepID != "inner" {
		t.Fatalf("active repository child state = %#v, want inner", got)
	}
	if got := handle.rs.ctx.LookupNamedSession("planner"); got != "workspace-planner" {
		t.Fatalf("workspace named session = %q", got)
	}
	frontendContext := model.NewRepositoryExecutionContext(handle.rs.ctx, model.Repository{Name: "frontend", Dir: frontendRoot}, 1)
	if diff := cmp.Diff(model.NewCapturedString("frontend"), frontendContext.CapturedVariables["output"]); diff != "" {
		t.Fatalf("restored repository capture mismatch (-want +got):\n%s", diff)
	}
	if got := frontendContext.LookupNamedSession("worker"); got != "frontend-worker" {
		t.Fatalf("restored repository-local session = %q", got)
	}
	workspacePR, repositoryPRs := handle.rs.ctx.PullRequestCaptureState.PullRequestURLs()
	if diff := cmp.Diff(state.RepositoryPullRequestURLs, repositoryPRs); workspacePR != state.WorkspacePullRequestURL || diff != "" {
		t.Fatalf("restored pull-request state = (%q, %#v), want (%q, %#v)", workspacePR, repositoryPRs, state.WorkspacePullRequestURL, state.RepositoryPullRequestURLs)
	}

	t.Run("rejects changed root before execution", func(t *testing.T) {
		moved := t.TempDir()
		initGitWorktree(t, moved)
		changed := *profiles
		changed.Repositories = map[string]config.Repository{"backend": {Path: moved}, "frontend": {Path: frontend}}
		_, err := PrepareResume(filepath.Join(sessionDir, "state.json"), &Options{
			WorkingDir: workspace, ProfileStore: &changed,
			ProcessRunner: &mockRunner{}, GlobExpander: &mockGlob{}, Log: &mockLog{},
		})
		if err == nil || !strings.Contains(err.Error(), "backend") || !strings.Contains(err.Error(), "identity") {
			t.Fatalf("PrepareResume() error = %v, want backend identity mismatch", err)
		}
	})

	t.Run("rejects launch outside persisted workspace", func(t *testing.T) {
		otherWorkspace := t.TempDir()
		initGitWorktree(t, otherWorkspace)
		_, err := PrepareResume(filepath.Join(sessionDir, "state.json"), &Options{
			WorkingDir: otherWorkspace, ProfileStore: profiles,
			ProcessRunner: &mockRunner{}, GlobExpander: &mockGlob{}, Log: &mockLog{},
		})
		if err == nil || !strings.Contains(err.Error(), "workspace") || !strings.Contains(err.Error(), "does not match") {
			t.Fatalf("PrepareResume() error = %v, want workspace identity mismatch", err)
		}
	})
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

func TestPrepareResume_RepositoryFanoutSkipsCompletedRepositories(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	workspace, backend, frontend, docs := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	initGitWorktree(t, workspace)
	initGitWorktree(t, backend)
	initGitWorktree(t, frontend)
	initGitWorktree(t, docs)
	workflowPath := filepath.Join(workspace, "resume-repositories-v1.0.yaml")
	workflowYAML := `name: resume-repositories
scope: repositories
params:
  - name: repositories
steps:
  - id: setup
    command: setup {{repository_name}}
  - id: deploy
    command: deploy {{repository_name}}
`
	if err := os.WriteFile(workflowPath, []byte(workflowYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	failedRepository := 1
	sessionDir := t.TempDir()
	state := model.RunState{
		WorkflowFile:    workflowPath,
		WorkflowName:    "resume-repositories",
		Params:          map[string]string{model.RepositoriesParam: "backend,frontend,docs"},
		WorkflowHash:    stateio.ComputeWorkflowHash(workflowYAML),
		RepositoryIndex: &failedRepository,
		CurrentStep: model.CurrentStep{Nested: &model.NestedStepState{
			StepID: "deploy", Completed: false,
			SessionIDs: map[string]string{}, SessionProfiles: map[string]string{}, CapturedVariables: map[string]model.CapturedValue{},
		}},
	}
	if err := stateio.WriteState(&state, sessionDir); err != nil {
		t.Fatal(err)
	}
	spy := &repositorySpyRunner{}
	handle, err := PrepareResume(filepath.Join(sessionDir, "state.json"), &Options{
		WorkingDir: workspace,
		ProfileStore: &config.Config{Repositories: map[string]config.Repository{
			"backend": {Path: backend}, "frontend": {Path: frontend}, "docs": {Path: docs},
		}},
		ProcessRunner: spy,
		GlobExpander:  &mockGlob{},
		Log:           &mockLog{},
	})
	if err != nil {
		t.Fatalf("PrepareResume() error = %v", err)
	}
	if result := ExecuteFromHandle(handle, nil); result != ResultSuccess {
		t.Fatalf("ExecuteFromHandle() = %q, want success", result)
	}
	if diff := cmp.Diff([]string{"deploy 'frontend'", "setup 'docs'", "deploy 'docs'"}, spy.commands); diff != "" {
		t.Fatalf("resumed commands mismatch (-want +got):\n%s", diff)
	}
}

func TestResumeNestedStateReplacesStaleRepositoryDescendantsAtBoundary(t *testing.T) {
	active := 1
	state := model.RunState{
		RepositoryIndex: &active,
		CurrentStep: model.CurrentStep{Nested: &model.NestedStepState{
			StepID: "implement",
			Child: &model.NestedStepState{
				StepID: "implement-task-groups",
				Child: &model.NestedStepState{
					StepID: "implement-repository-task-group",
					Child:  &model.NestedStepState{StepID: "stale-implement-tasks"},
				},
			},
		}},
		RepositoryFrame: &model.RepositoryFrame{
			BoundaryID: "[implement, sub:implement-change, implement-task-groups]",
			Repositories: []model.RepositoryExecutionState{
				{Status: model.RepositoryCompleted},
				{Status: model.RepositoryActive, Nested: &model.NestedStepState{
					StepID: "simplify",
					Child:  &model.NestedStepState{StepID: "run-validator"},
				}},
			},
		},
	}

	nested := resumeNestedState(&state)
	var got []string
	for current := nested; current != nil; current = current.Child {
		got = append(got, current.StepID)
	}
	want := []string{
		"implement",
		"implement-task-groups",
		"implement-repository-task-group",
		"simplify",
		"run-validator",
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("resume chain mismatch (-want +got):\n%s", diff)
	}
}

func TestResumeNestedStateUsesFullBoundaryPathWhenStepIDsRepeat(t *testing.T) {
	active := 0
	atBoundaryChild := false
	state := model.RunState{
		RepositoryIndex: &active,
		CurrentStep: model.CurrentStep{Nested: &model.NestedStepState{
			StepID: "coordinate",
			Child: &model.NestedStepState{
				StepID: "task-groups",
				Child: &model.NestedStepState{
					StepID: "simplify",
					Child:  &model.NestedStepState{StepID: "task-groups"},
				},
			},
		}},
		RepositoryFrame: &model.RepositoryFrame{
			BoundaryID: "[coordinate, sub:workspace-child, task-groups]",
			Repositories: []model.RepositoryExecutionState{{
				Status:                model.RepositoryActive,
				NestedAtBoundaryChild: &atBoundaryChild,
				Nested: &model.NestedStepState{
					StepID: "simplify",
					Child:  &model.NestedStepState{StepID: "run-validator"},
				},
			}},
		},
	}

	nested := resumeNestedState(&state)
	var got []string
	for current := nested; current != nil; current = current.Child {
		got = append(got, current.StepID)
	}
	want := []string{"coordinate", "task-groups", "simplify", "simplify", "run-validator"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("resume chain mismatch (-want +got):\n%s", diff)
	}
}

func TestResumeNestedStateReplacesBoundaryChildWhenRepositoryProgressStartsThere(t *testing.T) {
	active := 0
	atBoundaryChild := true
	state := model.RunState{
		RepositoryIndex: &active,
		CurrentStep: model.CurrentStep{Nested: &model.NestedStepState{
			StepID: "coordinate",
			Child:  &model.NestedStepState{StepID: "stale-repository-step"},
		}},
		RepositoryFrame: &model.RepositoryFrame{
			BoundaryID: "[coordinate]",
			Repositories: []model.RepositoryExecutionState{{
				Status:                model.RepositoryActive,
				NestedAtBoundaryChild: &atBoundaryChild,
				Nested: &model.NestedStepState{
					StepID: "repository-step",
					Child:  &model.NestedStepState{StepID: "repository-child"},
				},
			}},
		},
	}

	nested := resumeNestedState(&state)
	var got []string
	for current := nested; current != nil; current = current.Child {
		got = append(got, current.StepID)
	}
	want := []string{"coordinate", "repository-step", "repository-child"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("resume chain mismatch (-want +got):\n%s", diff)
	}
}

func TestResumeNestedStateDoesNotReattachRepositoryChainAlreadyInRoot(t *testing.T) {
	active := 0
	atBoundaryChild := false
	repositoryNested := &model.NestedStepState{
		StepID: "repository-step",
		Child:  &model.NestedStepState{StepID: "repository-child"},
	}
	state := model.RunState{
		RepositoryIndex: &active,
		CurrentStep: model.CurrentStep{Nested: &model.NestedStepState{
			StepID: "coordinate",
			Child:  repositoryNested,
		}},
		RepositoryFrame: &model.RepositoryFrame{
			BoundaryID: "[coordinate]",
			Repositories: []model.RepositoryExecutionState{{
				Status:                model.RepositoryActive,
				NestedAtBoundaryChild: &atBoundaryChild,
				Nested:                repositoryNested,
			}},
		},
	}

	nested := resumeNestedState(&state)
	var got []string
	for current := nested; current != nil && len(got) < 4; current = current.Child {
		got = append(got, current.StepID)
	}
	want := []string{"coordinate", "repository-step", "repository-child"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("resume chain mismatch (-want +got):\n%s", diff)
	}
}

func TestResolveLegacyRepositoryAttachment(t *testing.T) {
	tests := []struct {
		name         string
		boundary     *model.NestedStepState
		repository   *model.NestedStepState
		wantAttached bool
		wantErr      string
	}{
		{
			name: "direct boundary child",
			boundary: &model.NestedStepState{
				StepID: "boundary",
				Child:  &model.NestedStepState{StepID: "run", Child: &model.NestedStepState{StepID: "validate"}},
			},
			repository:   &model.NestedStepState{StepID: "run", Child: &model.NestedStepState{StepID: "validate"}},
			wantAttached: true,
		},
		{
			name: "repository local wrapper",
			boundary: &model.NestedStepState{
				StepID: "boundary",
				Child:  &model.NestedStepState{StepID: "wrapper", Child: &model.NestedStepState{StepID: "stale"}},
			},
			repository: &model.NestedStepState{StepID: "run", Child: &model.NestedStepState{StepID: "validate"}},
		},
		{
			name: "ambiguous reused step id",
			boundary: &model.NestedStepState{
				StepID: "boundary",
				Child:  &model.NestedStepState{StepID: "run", Child: &model.NestedStepState{StepID: "stale"}},
			},
			repository: &model.NestedStepState{StepID: "run", Child: &model.NestedStepState{StepID: "validate"}},
			wantErr:    "cannot safely resume legacy repository state",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			active := 0
			state := model.RunState{
				RepositoryIndex: &active,
				CurrentStep:     model.CurrentStep{Nested: tt.boundary},
				RepositoryFrame: &model.RepositoryFrame{
					BoundaryID: "[boundary]",
					Repositories: []model.RepositoryExecutionState{{
						Status: model.RepositoryActive,
						Nested: tt.repository,
					}},
				},
			}

			err := resolveLegacyRepositoryAttachment(&state)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("resolveLegacyRepositoryAttachment() error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got := state.RepositoryFrame.Repositories[0].NestedAtBoundaryChild
			if got == nil || *got != tt.wantAttached {
				t.Fatalf("NestedAtBoundaryChild = %v, want %t", got, tt.wantAttached)
			}
		})
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
