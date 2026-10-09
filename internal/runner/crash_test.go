package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/config"
	"github.com/codagent/agent-runner/internal/exec"
	"github.com/codagent/agent-runner/internal/loader"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/stateio"
)

func TestRunWorkflowAgentCrashEvidence(t *testing.T) {
	w := model.Workflow{Name: "crash", Steps: []model.Step{{ID: "generate-code", Mode: model.ModeAutonomous, Prompt: "work", Agent: "test-agent", Session: model.SessionNew}}}
	w.ApplyDefaults()
	dir := t.TempDir()
	result, err := RunWorkflow(&w, nil, &Options{SessionDir: dir, ProcessRunner: &mockRunner{results: []exec.ProcessResult{{Started: true, ExitCode: 1, Stderr: "Selected model is at capacity\n"}}}, GlobExpander: &mockGlob{}, Log: &mockLog{}, ProfileStore: &config.Config{ActiveAgents: map[string]*config.Agent{"test-agent": {CLI: "claude"}}}})
	if err != nil || result != ResultFailed {
		t.Fatalf("run = %s, %v", result, err)
	}
	state, err := stateio.ReadState(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if state.FailureKind != model.FailureInfrastructure || !state.CrashObserved || len(state.Crashes) != 1 || state.FailureReason != "generate-code failed (infrastructure): Selected model is at capacity" {
		t.Fatalf("state = %+v", state)
	}
	audit, err := os.ReadFile(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(audit), `"failure_kind":"infrastructure"`) || !strings.Contains(string(audit), `"crash_observed":true`) || !strings.Contains(string(audit), `"failure_origin"`) {
		t.Fatalf("missing audit evidence: %s", audit)
	}
}

func TestInlineRepairCrashClassifiesCheck(t *testing.T) {
	maxAttempts := 1
	w := model.Workflow{Name: "repair-crash", Steps: []model.Step{{ID: "check-plan", Command: "exit 1", Repair: &model.Repair{Agent: "test-agent", Prompt: "fix it", Max: &maxAttempts}}}}
	w.ApplyDefaults()
	dir := t.TempDir()
	runner := &mockRunner{results: []exec.ProcessResult{{ExitCode: 1, Stderr: "check red"}, {Started: true, ExitCode: 1, Stderr: "Selected model is at capacity"}}}
	result, err := RunWorkflow(&w, nil, &Options{SessionDir: dir, ProcessRunner: runner, GlobExpander: &mockGlob{}, Log: &mockLog{}, ProfileStore: &config.Config{ActiveAgents: map[string]*config.Agent{"test-agent": {CLI: "claude"}}}})
	if err != nil || result != ResultFailed {
		t.Fatalf("run = %s, %v", result, err)
	}
	state, err := stateio.ReadState(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := "check-plan repair failed (infrastructure): Selected model is at capacity after 1 repair attempts"
	if state.FailureKind != model.FailureInfrastructure || state.FailureReason != want {
		t.Fatalf("state = %+v", state)
	}
}

func TestNestedRepairCrashReasonKeepsAttemptCount(t *testing.T) {
	dir := t.TempDir()
	childPath := writeWorkflowFile(t, dir, "child-v1.0.yaml", `name: child
steps:
  - id: check-plan
    command: "exit 1"
    repair:
      agent: test-agent
      prompt: fix it
      max: 1
`)
	w := model.Workflow{Name: "parent", Steps: []model.Step{{ID: "child", Workflow: childPath}}}
	w.ApplyDefaults()
	runDir := t.TempDir()
	runner := &mockRunner{results: []exec.ProcessResult{{ExitCode: 1, Stderr: "check red"}, {Started: true, ExitCode: 1, Stderr: "capacity"}}}
	result, err := RunWorkflow(&w, nil, &Options{SessionDir: runDir, ProcessRunner: runner, GlobExpander: &mockGlob{}, Log: &mockLog{}, ProfileStore: testAgentProfileStore()})
	if err != nil || result != ResultFailed {
		t.Fatalf("run=%s err=%v", result, err)
	}
	state, err := stateio.ReadState(filepath.Join(runDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if state.FailureReason != "check-plan repair failed (infrastructure): capacity after 1 repair attempts" {
		t.Fatalf("reason=%q", state.FailureReason)
	}
}

func TestInlineRepairCrashThenRecoveryIsObserved(t *testing.T) {
	maxAttempts := 2
	w := model.Workflow{Name: "repair-recovery", Steps: []model.Step{{ID: "check-plan", Command: "exit 1", Repair: &model.Repair{Agent: "test-agent", Prompt: "fix it", Max: &maxAttempts}}}}
	w.ApplyDefaults()
	dir := t.TempDir()
	runner := &mockRunner{results: []exec.ProcessResult{{ExitCode: 1, Stderr: "check red"}, {Started: true, ExitCode: 1, Stderr: "capacity"}, {Started: true, Stdout: claudeAgentOutput("fixed")}, {ExitCode: 0}}}
	result, err := RunWorkflow(&w, nil, &Options{SessionDir: dir, ProcessRunner: runner, GlobExpander: &mockGlob{}, Log: &mockLog{}, ProfileStore: testAgentProfileStore()})
	if err != nil || result != ResultSuccess {
		t.Fatalf("run=%s err=%v", result, err)
	}
	state, err := stateio.ReadState(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !state.Completed || !state.CrashObserved || len(state.Crashes) != 1 || state.FailureKind != "" {
		t.Fatalf("state=%+v", state)
	}
}

func TestFinishedRepairWithRedRecheckIsStepFailure(t *testing.T) {
	maxAttempts := 1
	w := model.Workflow{Name: "repair-red", Steps: []model.Step{{ID: "check-plan", Command: "exit 1", Repair: &model.Repair{Agent: "test-agent", Prompt: "fix it", Max: &maxAttempts}}}}
	w.ApplyDefaults()
	dir := t.TempDir()
	runner := &mockRunner{results: []exec.ProcessResult{{ExitCode: 1, Stderr: "check red"}, {Started: true, Stdout: claudeAgentOutput("fixed")}, {ExitCode: 1, Stderr: "still red"}}}
	result, err := RunWorkflow(&w, nil, &Options{SessionDir: dir, ProcessRunner: runner, GlobExpander: &mockGlob{}, Log: &mockLog{}, ProfileStore: testAgentProfileStore()})
	if err != nil || result != ResultFailed {
		t.Fatalf("run=%s err=%v", result, err)
	}
	state, err := stateio.ReadState(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if state.FailureKind != model.FailureStep || state.CrashObserved || !strings.Contains(state.FailureReason, "check-plan failed: still red") {
		t.Fatalf("state=%+v", state)
	}
}

func TestRerunReplayCrashClassifiesCheck(t *testing.T) {
	maxAttempts := 1
	w := model.Workflow{Name: "replay-crash", Steps: []model.Step{
		{ID: "act", Mode: model.ModeAutonomous, Prompt: "work", Agent: "test-agent", Session: model.SessionNew},
		{ID: "check", Command: "exit 1", Repair: &model.Repair{Rerun: "act", Max: &maxAttempts}},
	}}
	w.ApplyDefaults()
	dir := t.TempDir()
	runner := &mockRunner{results: []exec.ProcessResult{{Started: true, Stdout: claudeAgentOutput("first")}, {ExitCode: 1, Stderr: "check red"}, {Started: true, ExitCode: 1, Stderr: "capacity"}}}
	result, err := RunWorkflow(&w, nil, &Options{SessionDir: dir, ProcessRunner: runner, GlobExpander: &mockGlob{}, Log: &mockLog{}, ProfileStore: testAgentProfileStore()})
	if err != nil || result != ResultFailed {
		t.Fatalf("run=%s err=%v", result, err)
	}
	state, err := stateio.ReadState(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if state.FailureKind != model.FailureInfrastructure || !state.CrashObserved || len(state.Crashes) != 1 {
		t.Fatalf("state=%+v", state)
	}
}

func TestPreviousCrashSignalsSurviveResume(t *testing.T) {
	workflowPath := writeWorkflowFile(t, t.TempDir(), "crash-resume-v1.0.yaml", `name: crash-resume
steps:
  - id: agent
    mode: autonomous
    agent: test-agent
    session: new
    prompt: work
    continue_on_failure: true
  - id: skipped
    command: "true"
    skip_if: "sh: true"
  - id: read
    command: "printf '%s,%s' {{last_step_failure_kind}} {{last_step_crash_observed}}"
    skip_if: previous_success
`)
	dir := t.TempDir()
	first := &abortingRunner{mockRunner: mockRunner{results: []exec.ProcessResult{{Started: true, ExitCode: 1, Stderr: "capacity"}}}, abortOnCall: 2}
	runInterrupted(t, workflowPath, dir, first, &Options{ProfileStore: testAgentProfileStore()})
	state, err := stateio.ReadState(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	previous := state.CurrentStep.Nested.PreviousStep
	if previous == nil || previous.FailureKind != model.FailureInfrastructure || !previous.CrashObserved {
		t.Fatalf("previous = %+v", previous)
	}
	if len(state.Crashes) != 1 || !state.CrashObserved {
		t.Fatalf("interrupted state=%+v", state)
	}
	second := &mockRunner{}
	result, err := ResumeWorkflow(filepath.Join(dir, "state.json"), &Options{ProcessRunner: second, GlobExpander: &mockGlob{}, Log: &mockLog{}, ProfileStore: testAgentProfileStore()})
	if err != nil || result != ResultSuccess {
		t.Fatalf("resume=%s err=%v", result, err)
	}
	if len(second.calls) != 1 || !strings.Contains(second.calls[0][2], "'infrastructure' 'true'") {
		t.Fatalf("calls=%v", second.calls)
	}
	completed, err := stateio.ReadState(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !completed.CrashObserved || len(completed.Crashes) != 1 {
		t.Fatalf("completed state=%+v", completed)
	}
}

func TestTopLevelSkipErrorDoesNotInheritCrashKind(t *testing.T) {
	w := model.Workflow{Name: "stale-kind", Steps: []model.Step{
		{ID: "agent", Mode: model.ModeAutonomous, Prompt: "work", Agent: "test-agent", Session: model.SessionNew, ContinueOnFailure: true},
		{ID: "next", Command: "true", SkipIf: "sh: echo {{missing}}"},
	}}
	w.ApplyDefaults()
	dir := t.TempDir()
	result, err := RunWorkflow(&w, nil, &Options{SessionDir: dir, ProcessRunner: &mockRunner{results: []exec.ProcessResult{{Started: true, ExitCode: 1, Stderr: "capacity"}}}, GlobExpander: &mockGlob{}, Log: &mockLog{}, ProfileStore: testAgentProfileStore()})
	if err != nil || result != ResultFailed {
		t.Fatalf("run=%s err=%v", result, err)
	}
	state, err := stateio.ReadState(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if state.FailureKind != model.FailureStep || !state.CrashObserved || strings.Contains(state.FailureReason, "infrastructure") {
		t.Fatalf("state=%+v", state)
	}
	audit, err := os.ReadFile(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(audit), `"failure_kind":"step"`) || !strings.Contains(string(audit), `"crash_observed":true`) {
		t.Fatalf("audit=%s", audit)
	}
}

func TestSubWorkflowPreviousCrashSignalsSurviveResume(t *testing.T) {
	workflowDir := t.TempDir()
	writeWorkflowFile(t, workflowDir, "child-v1.0.yaml", `name: child
steps:
  - id: agent
    mode: autonomous
    agent: test-agent
    session: new
    prompt: work
    continue_on_failure: true
  - id: read
    command: "printf '%s,%s' {{last_step_failure_kind}} {{last_step_crash_observed}}"
    skip_if: previous_success
`)
	workflowPath := writeWorkflowFile(t, workflowDir, "parent-v1.0.yaml", `name: parent
steps:
  - id: child
    workflow: child-v1.0.yaml
`)
	dir := t.TempDir()
	first := &abortingRunner{mockRunner: mockRunner{results: []exec.ProcessResult{{Started: true, ExitCode: 1, Stderr: "capacity"}}}, abortOnCall: 2}
	runInterrupted(t, workflowPath, dir, first, &Options{ProfileStore: testAgentProfileStore()})
	state, err := stateio.ReadState(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if state.CurrentStep.Nested.Child == nil || state.CurrentStep.Nested.Child.PreviousStep == nil || state.CurrentStep.Nested.Child.PreviousStep.FailureKind != model.FailureInfrastructure {
		t.Fatalf("child state=%+v", state.CurrentStep.Nested.Child)
	}
	second := &mockRunner{}
	result, err := ResumeWorkflow(filepath.Join(dir, "state.json"), &Options{ProcessRunner: second, GlobExpander: &mockGlob{}, Log: &mockLog{}, ProfileStore: testAgentProfileStore()})
	if err != nil || result != ResultSuccess {
		t.Fatalf("resume=%s err=%v", result, err)
	}
	if len(second.calls) != 1 || !strings.Contains(second.calls[0][2], "'infrastructure' 'true'") {
		t.Fatalf("calls=%v", second.calls)
	}
	completed, err := stateio.ReadState(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !completed.CrashObserved || len(completed.Crashes) != 1 {
		t.Fatalf("completed state=%+v", completed)
	}
}

func TestLoopBodyPreviousCrashSignalsSurviveResume(t *testing.T) {
	workflowPath := writeWorkflowFile(t, t.TempDir(), "loop-resume-v1.0.yaml", `name: loop-resume
steps:
  - id: cycle
    loop:
      max: 1
    steps:
      - id: agent
        mode: autonomous
        agent: test-agent
        session: new
        prompt: work
        continue_on_failure: true
      - id: read
        command: "printf '%s,%s' {{last_step_failure_kind}} {{last_step_crash_observed}}"
        skip_if: previous_success
`)
	dir := t.TempDir()
	first := &abortingRunner{mockRunner: mockRunner{results: []exec.ProcessResult{{Started: true, ExitCode: 1, Stderr: "capacity"}}}, abortOnCall: 2}
	runInterrupted(t, workflowPath, dir, first, &Options{ProfileStore: testAgentProfileStore()})
	state, err := stateio.ReadState(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if state.CurrentStep.Nested.Child == nil || state.CurrentStep.Nested.Child.PreviousStep == nil || state.CurrentStep.Nested.Child.PreviousStep.FailureKind != model.FailureInfrastructure {
		t.Fatalf("iteration state=%+v", state.CurrentStep.Nested.Child)
	}
	second := &mockRunner{}
	result, err := ResumeWorkflow(filepath.Join(dir, "state.json"), &Options{ProcessRunner: second, GlobExpander: &mockGlob{}, Log: &mockLog{}, ProfileStore: testAgentProfileStore()})
	if err != nil || result != ResultSuccess {
		t.Fatalf("resume=%s err=%v", result, err)
	}
	if len(second.calls) != 1 || !strings.Contains(second.calls[0][2], "'infrastructure' 'true'") {
		t.Fatalf("calls=%v", second.calls)
	}
}

func TestPreviousSuccessSkipsAfterResume(t *testing.T) {
	workflowPath := writeWorkflowFile(t, t.TempDir(), "success-resume-v1.0.yaml", `name: success-resume
steps:
  - id: agent
    mode: autonomous
    agent: test-agent
    session: new
    prompt: work
  - id: next
    command: "echo should-not-run"
    skip_if: previous_success
`)
	dir := t.TempDir()
	// Stopping at the completed first step leaves the same durable resume boundary.
	w, err := loader.LoadWorkflow(workflowPath, loader.Options{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := RunWorkflow(&w, nil, &Options{WorkflowFile: workflowPath, SessionDir: dir, ProcessRunner: &mockRunner{results: []exec.ProcessResult{{Started: true, Stdout: claudeAgentOutput("done")}}}, GlobExpander: &mockGlob{}, Log: &mockLog{}, ProfileStore: testAgentProfileStore(), Until: "agent"})
	if err != nil || result != ResultSuccess {
		t.Fatalf("first run=%s err=%v", result, err)
	}
	state, err := stateio.ReadState(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if state.CurrentStep.Nested.PreviousStep == nil || state.CurrentStep.Nested.PreviousStep.Outcome != "success" {
		t.Fatalf("previous=%+v", state.CurrentStep.Nested.PreviousStep)
	}
	removeRunEndForInterruptedBoundary(t, dir)
	second := &mockRunner{}
	result, err = ResumeWorkflow(filepath.Join(dir, "state.json"), &Options{ProcessRunner: second, GlobExpander: &mockGlob{}, Log: &mockLog{}, ProfileStore: testAgentProfileStore()})
	if err != nil || result != ResultSuccess || len(second.calls) != 0 {
		t.Fatalf("resume=%s err=%v calls=%v", result, err, second.calls)
	}
}

func TestLegacyRunWithoutPreviousStepResumesWithDefaults(t *testing.T) {
	workflowPath := writeWorkflowFile(t, t.TempDir(), "legacy-resume-v1.0.yaml", `name: legacy-resume
steps:
  - id: first
    command: "true"
  - id: read
    command: "echo {{last_step_failure_kind}} {{last_step_crash_observed}}"
    skip_if: previous_success
`)
	w, err := loader.LoadWorkflow(workflowPath, loader.Options{})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	result, err := RunWorkflow(&w, nil, &Options{WorkflowFile: workflowPath, SessionDir: dir, ProcessRunner: &mockRunner{}, GlobExpander: &mockGlob{}, Log: &mockLog{}, Until: "first"})
	if err != nil || result != ResultSuccess {
		t.Fatalf("first=%s err=%v", result, err)
	}
	state, err := stateio.ReadState(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	state.CurrentStep.Nested.PreviousStep = nil
	if err := stateio.WriteState(&state, dir); err != nil {
		t.Fatal(err)
	}
	removeRunEndForInterruptedBoundary(t, dir)
	second := &mockRunner{}
	result, err = ResumeWorkflow(filepath.Join(dir, "state.json"), &Options{ProcessRunner: second, GlobExpander: &mockGlob{}, Log: &mockLog{}})
	if err != nil || result != ResultSuccess || len(second.calls) != 1 || !strings.Contains(second.calls[0][2], "'' 'false'") {
		t.Fatalf("resume=%s err=%v calls=%v nested=%+v", result, err, second.calls, state.CurrentStep.Nested)
	}
}

func removeRunEndForInterruptedBoundary(t *testing.T, dir string) {
	t.Helper()
	path := filepath.Join(dir, "audit.log")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if !strings.Contains(line, " run_end ") {
			kept = append(kept, line)
		}
	}
	if err := os.WriteFile(path, []byte(strings.Join(kept, "\n")), 0o600); err != nil {
		t.Fatal(err)
	}
}
