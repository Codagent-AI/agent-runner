package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/config"
	"github.com/codagent/agent-runner/internal/exec"
	"github.com/codagent/agent-runner/internal/loader"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/runlock"
	"github.com/codagent/agent-runner/internal/stateio"
)

// interruptedCall is a sentinel panic value used to simulate a process
// crashing mid-step: the real executor has already flushed whatever state it
// flushes before invoking the process, and the fake CLI/process never
// returns, so no step_end is ever emitted for the in-flight step.
type interruptedCall struct{}

// abortingRunner wraps mockRunner and panics with interruptedCall on the
// callNumber'th process invocation (1-indexed, across RunShell/RunAgent/
// RunScript), simulating an interrupted run so the caller can inspect the
// real state.json/audit.log the executor wrote up to that point.
type abortingRunner struct {
	mockRunner
	abortOnCall int
	calls       int
}

func (m *abortingRunner) maybeAbort() {
	m.calls++
	if m.calls == m.abortOnCall {
		panic(interruptedCall{})
	}
}

func (m *abortingRunner) RunShell(cmd string, capture bool, workdir string) (exec.ProcessResult, error) {
	m.maybeAbort()
	return m.mockRunner.RunShell(cmd, capture, workdir)
}

func (m *abortingRunner) RunAgent(options *exec.AgentProcessOptions) (exec.ProcessResult, error) {
	m.maybeAbort()
	return m.mockRunner.RunAgent(options)
}

func (m *abortingRunner) RunScript(path string, stdin []byte, capture bool, workdir string) (exec.ProcessResult, error) {
	m.maybeAbort()
	return m.mockRunner.RunScript(path, stdin, capture, workdir)
}

// runInterrupted loads workflowPath, runs it with a runner that panics on
// the abortOnCall'th process invocation, recovers the panic, and releases
// the run lock the interrupted process would never have released, leaving
// sessionDir's state.json and audit.log exactly as the real executor wrote
// them mid-flight.
func runInterrupted(t *testing.T, workflowPath, sessionDir string, runner *abortingRunner, opts *Options) {
	t.Helper()
	w, err := loader.LoadWorkflow(workflowPath, loader.Options{})
	if err != nil {
		t.Fatalf("load workflow: %v", err)
	}
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(interruptedCall); !ok {
				panic(r)
			}
		}
		runlock.Delete(sessionDir)
	}()
	opts.WorkflowFile = workflowPath
	opts.SessionDir = sessionDir
	opts.ProcessRunner = runner
	if opts.GlobExpander == nil {
		opts.GlobExpander = &mockGlob{}
	}
	if opts.Log == nil {
		opts.Log = &mockLog{}
	}
	_, _ = RunWorkflow(&w, map[string]string{}, opts)
	t.Fatal("expected the run to be interrupted before completing")
}

func testAgentProfileStore() *config.Config {
	return &config.Config{ActiveAgents: map[string]*config.Agent{
		"test-agent": {CLI: "claude"},
	}}
}

func writeWorkflowFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const inlineRepairYAML = `name: test
steps:
  - id: verify
    command: exit 1
    repair:
      prompt: please fix it
      agent: test-agent
      max: 2
`

const inlineRepairYAMLMax1 = `name: test
steps:
  - id: verify
    command: exit 1
    repair:
      prompt: please fix it
      agent: test-agent
      max: 1
`

const rerunRepairYAML = `name: test
steps:
  - id: act
    mode: autonomous
    prompt: open it
    agent: test-agent
    session: new
  - id: verify
    command: exit 1
    repair:
      rerun: act
      max: 1
`

const rerunRepairYAMLWithMid = `name: test
steps:
  - id: act
    mode: autonomous
    prompt: open it
    agent: test-agent
    session: new
  - id: mid
    command: echo mid
  - id: verify
    command: exit 1
    repair:
      rerun: act
      max: 2
`

const loopInlineRepairYAML = `name: test
steps:
  - id: retry
    loop:
      max: 1
    steps:
      - id: verify
        command: exit 1
        repair:
          prompt: please fix it
          agent: test-agent
          max: 2
`

const loopSubWorkflowRerunRepairParentYAML = `name: parent
steps:
  - id: retry
    loop:
      max: 1
    steps:
      - id: child
        workflow: %s
`

const groupRerunRepairYAML = `name: test
steps:
  - id: g
    steps:
      - id: act
        mode: autonomous
        prompt: open it
        agent: test-agent
        session: new
      - id: verify
        command: exit 1
        repair:
          rerun: act
          max: 2
`

const groupRerunRepairExhaustedYAML = `name: test
steps:
  - id: g
    steps:
      - id: act
        mode: autonomous
        prompt: open it
        agent: test-agent
        session: new
      - id: verify
        command: exit 1
        repair:
          rerun: act
          max: 1
`

const nestedRerunRepairChildYAML = `name: child
steps:
  - id: act
    mode: autonomous
    prompt: open it
    agent: test-agent
    session: new
  - id: verify
    command: exit 1
    repair:
      rerun: act
      max: 2
`

func runWorkflowFile(t *testing.T, workflowPath string, opts *Options) (WorkflowResult, error) {
	t.Helper()
	w, err := loader.LoadWorkflow(workflowPath, loader.Options{})
	if err != nil {
		t.Fatalf("load workflow: %v", err)
	}
	opts.WorkflowFile = workflowPath
	return RunWorkflow(&w, map[string]string{}, opts)
}

func TestResumeRepairInlineDuringAgentResumesSameAttempt(t *testing.T) {
	sessionDir := t.TempDir()
	workflowPath := writeWorkflowFile(t, t.TempDir(), "test-v1.0.yaml", inlineRepairYAML)

	// First call: the check fails. Second call: the inline repair agent for
	// attempt 1 -- this is where we abort.
	runInterrupted(t, workflowPath, sessionDir, &abortingRunner{
		mockRunner:  mockRunner{results: []exec.ProcessResult{{ExitCode: 1, Stderr: "boom"}}},
		abortOnCall: 2,
	}, &Options{ProfileStore: testAgentProfileStore()})

	state, err := stateio.ReadState(filepath.Join(sessionDir, "state.json"))
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	nested := state.CurrentStep.Nested
	if nested == nil || nested.StepID != "verify" {
		t.Fatalf("expected recorded step 'verify', got %+v", nested)
	}
	if nested.Repair == nil || nested.Repair.Phase != model.RepairPhaseRepairing || nested.Repair.Attempts != 0 {
		t.Fatalf("expected open frame phase repairing with 0 attempts, got %+v", nested.Repair)
	}

	// Resume: repair agent succeeds, then the check passes.
	runner2 := &mockRunner{results: []exec.ProcessResult{
		{ExitCode: 0, Stdout: claudeAgentOutput("fixed it")},
		{ExitCode: 0, Stdout: "all good"},
	}}
	result, err := ResumeWorkflow(filepath.Join(sessionDir, "state.json"), &Options{
		ProcessRunner: runner2, GlobExpander: &mockGlob{}, Log: &mockLog{}, ProfileStore: testAgentProfileStore(),
	})
	if err != nil {
		t.Fatalf("resume error: %v", err)
	}
	if result != ResultSuccess {
		t.Fatalf("expected success, got %q", result)
	}
	if len(runner2.calls) != 2 {
		t.Fatalf("expected 2 calls (repair agent, rerun check), got %d: %v", len(runner2.calls), runner2.calls)
	}
}

func TestResumeRepairReplayedIntermediateStepContinues(t *testing.T) {
	sessionDir := t.TempDir()
	workflowPath := writeWorkflowFile(t, t.TempDir(), "test-v1.0.yaml", rerunRepairYAMLWithMid)

	// Calls: act (ok), mid (ok), verify (fails, rewinds), act replay (ok),
	// mid replay -- abort here.
	runInterrupted(t, workflowPath, sessionDir, &abortingRunner{
		mockRunner: mockRunner{results: []exec.ProcessResult{
			{ExitCode: 0, Stdout: claudeAgentOutput("opened (draft)")},
			{ExitCode: 0, Stdout: "mid ran"},
			{ExitCode: 1, Stderr: "not ready"},
			{ExitCode: 0, Stdout: claudeAgentOutput("opened (ready)")},
		}},
		abortOnCall: 5,
	}, &Options{ProfileStore: testAgentProfileStore()})

	state, err := stateio.ReadState(filepath.Join(sessionDir, "state.json"))
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	// "mid" never flushes state before its own process call, so the last
	// state on disk is act's own completed record (with the open replaying
	// frame attached); resume's ordinary completed-advance path is what
	// actually re-enters at "mid" below.
	nested := state.CurrentStep.Nested
	if nested == nil || nested.StepID != "act" || !nested.Completed {
		t.Fatalf("expected recorded step 'act' completed, got %+v", nested)
	}
	if nested.Repair == nil || nested.Repair.Phase != model.RepairPhaseReplaying {
		t.Fatalf("expected open frame phase replaying, got %+v", nested.Repair)
	}

	runner2 := &mockRunner{results: []exec.ProcessResult{
		{ExitCode: 0, Stdout: "mid ran"},
		{ExitCode: 0, Stdout: "pr is open"},
	}}
	result, err := ResumeWorkflow(filepath.Join(sessionDir, "state.json"), &Options{
		ProcessRunner: runner2, GlobExpander: &mockGlob{}, Log: &mockLog{}, ProfileStore: testAgentProfileStore(),
	})
	if err != nil {
		t.Fatalf("resume error: %v", err)
	}
	if result != ResultSuccess {
		t.Fatalf("expected success, got %q", result)
	}
	if len(runner2.calls) != 2 {
		t.Fatalf("expected 2 calls (mid, verify), got %d: %v", len(runner2.calls), runner2.calls)
	}
}

func TestResumeRepairExhaustedRerunFormReentersAtTarget(t *testing.T) {
	sessionDir := t.TempDir()
	workflowPath := writeWorkflowFile(t, t.TempDir(), "test-v1.0.yaml", rerunRepairYAML)

	log := &mockLog{}
	result, err := runWorkflowFile(t, workflowPath, &Options{
		ProcessRunner: &mockRunner{results: []exec.ProcessResult{
			{ExitCode: 0, Stdout: claudeAgentOutput("opened (draft)")},
			{ExitCode: 1, Stderr: "not ready"},
			{ExitCode: 0, Stdout: claudeAgentOutput("still draft")},
			{ExitCode: 1, Stderr: "still not ready"},
		}},
		GlobExpander: &mockGlob{}, Log: log, SessionDir: sessionDir, ProfileStore: testAgentProfileStore(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != ResultFailed {
		t.Fatalf("expected failed, got %q", result)
	}

	state, err := stateio.ReadState(filepath.Join(sessionDir, "state.json"))
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	if state.CurrentStep.Nested.Repair.Phase != model.RepairPhaseFailed {
		t.Fatalf("expected phase failed, got %+v", state.CurrentStep.Nested.Repair)
	}

	// Resume: a human granted whatever was missing, so the target and check
	// both succeed with a fresh budget.
	runner2 := &mockRunner{results: []exec.ProcessResult{
		{ExitCode: 0, Stdout: claudeAgentOutput("opened (ready)")},
		{ExitCode: 0, Stdout: "pr is open"},
	}}
	result, err = ResumeWorkflow(filepath.Join(sessionDir, "state.json"), &Options{
		ProcessRunner: runner2, GlobExpander: &mockGlob{}, Log: &mockLog{}, ProfileStore: testAgentProfileStore(),
	})
	if err != nil {
		t.Fatalf("resume error: %v", err)
	}
	if result != ResultSuccess {
		t.Fatalf("expected success, got %q", result)
	}
	if len(runner2.calls) != 2 {
		t.Fatalf("expected 2 calls (act, verify), got %d: %v", len(runner2.calls), runner2.calls)
	}

	// Audit is append-only: the earlier failed attempt's evidence must
	// remain inspectable after the resumed run completes.
	auditData, err := os.ReadFile(filepath.Join(sessionDir, "audit.log"))
	if err != nil {
		t.Fatalf("read audit.log: %v", err)
	}
	if !strings.Contains(string(auditData), "still not ready") {
		t.Fatalf("earlier failed attempt's evidence missing from audit.log after resume:\n%s", auditData)
	}
}

func TestResumeRepairExhaustedInlineFormRunsCheckFirst(t *testing.T) {
	sessionDir := t.TempDir()
	workflowPath := writeWorkflowFile(t, t.TempDir(), "test-v1.0.yaml", inlineRepairYAMLMax1)

	log := &mockLog{}
	result, err := runWorkflowFile(t, workflowPath, &Options{
		ProcessRunner: &mockRunner{results: []exec.ProcessResult{
			{ExitCode: 1, Stderr: "boom"},
			{ExitCode: 0, Stdout: claudeAgentOutput("fixed it")},
			{ExitCode: 1, Stderr: "still boom"},
		}},
		GlobExpander: &mockGlob{}, Log: log, SessionDir: sessionDir, ProfileStore: testAgentProfileStore(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != ResultFailed {
		t.Fatalf("expected failed, got %q", result)
	}

	// Resume: the check now passes on its own; repair must not run again.
	runner2 := &mockRunner{results: []exec.ProcessResult{{ExitCode: 0, Stdout: "fixed externally"}}}
	result, err = ResumeWorkflow(filepath.Join(sessionDir, "state.json"), &Options{
		ProcessRunner: runner2, GlobExpander: &mockGlob{}, Log: &mockLog{}, ProfileStore: testAgentProfileStore(),
	})
	if err != nil {
		t.Fatalf("resume error: %v", err)
	}
	if result != ResultSuccess {
		t.Fatalf("expected success, got %q", result)
	}
	if len(runner2.calls) != 1 {
		t.Fatalf("expected exactly 1 call (the check, no repair), got %d: %v", len(runner2.calls), runner2.calls)
	}
}

func TestResumeRepairStaleRerunTargetErrors(t *testing.T) {
	sessionDir := t.TempDir()
	workflowDir := t.TempDir()
	workflowPath := writeWorkflowFile(t, workflowDir, "test-v1.0.yaml", rerunRepairYAML)

	log := &mockLog{}
	result, err := runWorkflowFile(t, workflowPath, &Options{
		ProcessRunner: &mockRunner{results: []exec.ProcessResult{
			{ExitCode: 0, Stdout: claudeAgentOutput("opened (draft)")},
			{ExitCode: 1, Stderr: "not ready"},
			{ExitCode: 0, Stdout: claudeAgentOutput("still draft")},
			{ExitCode: 1, Stderr: "still not ready"},
		}},
		GlobExpander: &mockGlob{}, Log: log, SessionDir: sessionDir, ProfileStore: testAgentProfileStore(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != ResultFailed {
		t.Fatalf("expected failed, got %q", result)
	}

	// Simulate the workflow file changing so the rerun target no longer
	// exists. The check's own repair declaration must go too (or the file
	// would fail load-time validateRepairTargets before resume even runs).
	staleYAML := `name: test
steps:
  - id: verify
    command: exit 1
`
	if err := os.WriteFile(workflowPath, []byte(staleYAML), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err = ResumeWorkflow(filepath.Join(sessionDir, "state.json"), &Options{
		ProcessRunner: &mockRunner{}, GlobExpander: &mockGlob{}, Log: &mockLog{}, ProfileStore: testAgentProfileStore(),
	})
	if err == nil || !strings.Contains(err.Error(), "act") {
		t.Fatalf("expected error naming missing target 'act', got %v", err)
	}
}

func TestResumeRepairInsideLoopIterationResumesSameAttempt(t *testing.T) {
	sessionDir := t.TempDir()
	workflowPath := writeWorkflowFile(t, t.TempDir(), "test-v1.0.yaml", loopInlineRepairYAML)

	// First call: the check fails. Second call: the inline repair agent for
	// attempt 1 -- this is where we abort.
	runInterrupted(t, workflowPath, sessionDir, &abortingRunner{
		mockRunner:  mockRunner{results: []exec.ProcessResult{{ExitCode: 1, Stderr: "boom"}}},
		abortOnCall: 2,
	}, &Options{ProfileStore: testAgentProfileStore()})

	state, err := stateio.ReadState(filepath.Join(sessionDir, "state.json"))
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	nested := state.CurrentStep.Nested
	if nested == nil || nested.StepID != "retry" || nested.Iteration == nil {
		t.Fatalf("expected recorded loop step 'retry' mid-iteration, got %+v", nested)
	}
	if *nested.Iteration != 0 {
		t.Fatalf("expected iteration index 0 (the loop's first and only iteration), got %d", *nested.Iteration)
	}
	if nested.Child == nil || nested.Child.StepID != "verify" {
		t.Fatalf("expected iteration body position 'verify', got %+v", nested.Child)
	}
	if nested.Child.Repair == nil || nested.Child.Repair.Phase != model.RepairPhaseRepairing || nested.Child.Repair.Attempts != 0 {
		t.Fatalf("expected open frame phase repairing with 0 attempts, got %+v", nested.Child.Repair)
	}

	runner2 := &mockRunner{results: []exec.ProcessResult{
		{ExitCode: 0, Stdout: claudeAgentOutput("fixed it")},
		{ExitCode: 0, Stdout: "all good"},
	}}
	result, err := ResumeWorkflow(filepath.Join(sessionDir, "state.json"), &Options{
		ProcessRunner: runner2, GlobExpander: &mockGlob{}, Log: &mockLog{}, ProfileStore: testAgentProfileStore(),
	})
	if err != nil {
		t.Fatalf("resume error: %v", err)
	}
	if result != ResultSuccess {
		t.Fatalf("expected success, got %q", result)
	}
	if len(runner2.calls) != 2 {
		t.Fatalf("expected 2 calls (repair agent, rerun check), got %d: %v", len(runner2.calls), runner2.calls)
	}
}

func TestResumeRepairNestedLoopSubWorkflowReplay(t *testing.T) {
	dir := t.TempDir()
	writeWorkflowFile(t, dir, "child-v1.0.yaml", nestedRerunRepairChildYAML)
	parentYAML := fmt.Sprintf(loopSubWorkflowRerunRepairParentYAML, "child-v1.0.yaml")
	sessionDir := t.TempDir()
	workflowPath := writeWorkflowFile(t, dir, "parent-v1.0.yaml", parentYAML)

	// Calls: act (ok), verify (fails, rewinds), act replay -- abort here.
	runInterrupted(t, workflowPath, sessionDir, &abortingRunner{
		mockRunner: mockRunner{results: []exec.ProcessResult{
			{ExitCode: 0, Stdout: claudeAgentOutput("opened (draft)")},
			{ExitCode: 1, Stderr: "not ready"},
		}},
		abortOnCall: 3,
	}, &Options{ProfileStore: testAgentProfileStore()})

	state, err := stateio.ReadState(filepath.Join(sessionDir, "state.json"))
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	nested := state.CurrentStep.Nested
	if nested == nil || nested.StepID != "retry" || nested.Iteration == nil {
		t.Fatalf("expected recorded loop step 'retry' mid-iteration, got %+v", nested)
	}
	child := nested.Child
	if child == nil || child.StepID != "child" {
		t.Fatalf("expected iteration body position 'child', got %+v", child)
	}
	grandchild := child.Child
	if grandchild == nil || grandchild.StepID != "act" {
		t.Fatalf("expected sub-workflow position 'act', got %+v", grandchild)
	}
	if grandchild.Repair == nil || grandchild.Repair.Phase != model.RepairPhaseReplaying {
		t.Fatalf("expected open frame phase replaying, got %+v", grandchild.Repair)
	}

	runner2 := &mockRunner{results: []exec.ProcessResult{
		{ExitCode: 0, Stdout: claudeAgentOutput("opened (ready)")},
		{ExitCode: 0, Stdout: "pr is open"},
	}}
	result, err := ResumeWorkflow(filepath.Join(sessionDir, "state.json"), &Options{
		ProcessRunner: runner2, GlobExpander: &mockGlob{}, Log: &mockLog{}, ProfileStore: testAgentProfileStore(),
	})
	if err != nil {
		t.Fatalf("resume error: %v", err)
	}
	if result != ResultSuccess {
		t.Fatalf("expected success, got %q", result)
	}
	if len(runner2.calls) != 2 {
		t.Fatalf("expected 2 calls (act replay, verify), got %d: %v", len(runner2.calls), runner2.calls)
	}
}

func TestResumeRepairInsideGroupReplaysFromFirstChild(t *testing.T) {
	sessionDir := t.TempDir()
	workflowPath := writeWorkflowFile(t, t.TempDir(), "test-v1.0.yaml", groupRerunRepairYAML)

	// Calls: act (ok), verify (fails, rewinds), act replay -- abort here.
	runInterrupted(t, workflowPath, sessionDir, &abortingRunner{
		mockRunner: mockRunner{results: []exec.ProcessResult{
			{ExitCode: 0, Stdout: claudeAgentOutput("opened (draft)")},
			{ExitCode: 1, Stderr: "not ready"},
		}},
		abortOnCall: 3,
	}, &Options{ProfileStore: testAgentProfileStore()})

	state, err := stateio.ReadState(filepath.Join(sessionDir, "state.json"))
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	// Groups have no nested state entry: the frame lives on the enclosing
	// (here, top-level) scope's own recorded position, naming the group as
	// the recorded step and the check by CheckID.
	nested := state.CurrentStep.Nested
	if nested == nil || nested.StepID != "g" || nested.Completed {
		t.Fatalf("expected recorded step 'g' not completed, got %+v", nested)
	}
	if nested.Repair == nil || nested.Repair.CheckID != "verify" || nested.Repair.Phase != model.RepairPhaseReplaying {
		t.Fatalf("expected open frame for check 'verify' phase replaying, got %+v", nested.Repair)
	}

	runner2 := &mockRunner{results: []exec.ProcessResult{
		{ExitCode: 0, Stdout: claudeAgentOutput("opened (ready)")},
		{ExitCode: 0, Stdout: "pr is open"},
	}}
	result, err := ResumeWorkflow(filepath.Join(sessionDir, "state.json"), &Options{
		ProcessRunner: runner2, GlobExpander: &mockGlob{}, Log: &mockLog{}, ProfileStore: testAgentProfileStore(),
	})
	if err != nil {
		t.Fatalf("resume error: %v", err)
	}
	if result != ResultSuccess {
		t.Fatalf("expected success, got %q", result)
	}
	// The group re-executes from its first child (existing behavior, since
	// groups persist no child position): act runs again, then verify.
	if len(runner2.calls) != 2 {
		t.Fatalf("expected 2 calls (act, verify), got %d: %v", len(runner2.calls), runner2.calls)
	}
}

// TestResumeRepairGroupExhaustedRerunFormReenters is a regression test: a
// rerun-form repair frame owned by a group (recorded on the enclosing
// scope, naming the group as the recorded step) that reaches phase failed
// must not have its target resolved against the enclosing scope's own step
// list, since the target lives inside the group, not beside it.
// ResolveResumeStep previously did exactly that and errored "step ... not
// found" even though the target still existed inside the group.
func TestResumeRepairGroupExhaustedRerunFormReenters(t *testing.T) {
	sessionDir := t.TempDir()
	workflowPath := writeWorkflowFile(t, t.TempDir(), "test-v1.0.yaml", groupRerunRepairExhaustedYAML)

	log := &mockLog{}
	result, err := runWorkflowFile(t, workflowPath, &Options{
		ProcessRunner: &mockRunner{results: []exec.ProcessResult{
			{ExitCode: 0, Stdout: claudeAgentOutput("opened (draft)")},
			{ExitCode: 1, Stderr: "not ready"},
			{ExitCode: 0, Stdout: claudeAgentOutput("still draft")},
			{ExitCode: 1, Stderr: "still not ready"},
		}},
		GlobExpander: &mockGlob{}, Log: log, SessionDir: sessionDir, ProfileStore: testAgentProfileStore(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != ResultFailed {
		t.Fatalf("expected failed, got %q", result)
	}

	state, err := stateio.ReadState(filepath.Join(sessionDir, "state.json"))
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	nested := state.CurrentStep.Nested
	if nested == nil || nested.StepID != "g" || nested.Completed {
		t.Fatalf("expected recorded step 'g' not completed, got %+v", nested)
	}
	if nested.Repair == nil || nested.Repair.CheckID != "verify" || nested.Repair.Phase != model.RepairPhaseFailed {
		t.Fatalf("expected open frame for check 'verify' phase failed, got %+v", nested.Repair)
	}

	// Resume must not error trying to resolve frame.Target ("act") against
	// the top-level step list (which contains only "g"); it must re-enter
	// the group, which replays from its first child with a fresh budget.
	runner2 := &mockRunner{results: []exec.ProcessResult{
		{ExitCode: 0, Stdout: claudeAgentOutput("opened (ready)")},
		{ExitCode: 0, Stdout: "pr is open"},
	}}
	result, err = ResumeWorkflow(filepath.Join(sessionDir, "state.json"), &Options{
		ProcessRunner: runner2, GlobExpander: &mockGlob{}, Log: &mockLog{}, ProfileStore: testAgentProfileStore(),
	})
	if err != nil {
		t.Fatalf("resume error: %v", err)
	}
	if result != ResultSuccess {
		t.Fatalf("expected success, got %q", result)
	}
	if len(runner2.calls) != 2 {
		t.Fatalf("expected 2 calls (act, verify), got %d: %v", len(runner2.calls), runner2.calls)
	}
}

const loopWarnOnFailureRepairYAML = `name: test
steps:
  - id: retry
    loop:
      max: 1
    steps:
      - id: verify
        command: exit 1
        warn_on_failure: true
        repair:
          prompt: please fix it
          agent: test-agent
          max: 1
      - id: after
        command: "true"
`

// TestResumeRepairExhaustedWarnOnFailureInsideLoopAdvances covers the
// INT-002 scenario "immediately after an exhausted warn_on_failure check
// inside a loop": once the check's repair budget is exhausted,
// warn_on_failure lets the loop iteration advance past it (clearing the
// frame), and a resume interrupted while executing the next body step must
// resume there directly, without re-entering repair.
func TestResumeRepairExhaustedWarnOnFailureInsideLoopAdvances(t *testing.T) {
	sessionDir := t.TempDir()
	workflowPath := writeWorkflowFile(t, t.TempDir(), "test-v1.0.yaml", loopWarnOnFailureRepairYAML)

	// Calls: verify (fails), repair agent (fixes, but...), rerun check
	// (fails again, exhausting budget 1), after -- abort here.
	runInterrupted(t, workflowPath, sessionDir, &abortingRunner{
		mockRunner: mockRunner{results: []exec.ProcessResult{
			{ExitCode: 1, Stderr: "boom"},
			{ExitCode: 0, Stdout: claudeAgentOutput("fixed it")},
			{ExitCode: 1, Stderr: "still boom"},
		}},
		abortOnCall: 4,
	}, &Options{ProfileStore: testAgentProfileStore()})

	state, err := stateio.ReadState(filepath.Join(sessionDir, "state.json"))
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	nested := state.CurrentStep.Nested
	if nested == nil || nested.StepID != "retry" || nested.Iteration == nil || *nested.Iteration != 0 {
		t.Fatalf("expected recorded loop step 'retry' at iteration 0, got %+v", nested)
	}
	// "after" never flushes state before its own process call, so the last
	// state on disk is verify's own completed record (warn_on_failure marks
	// it completed despite the exhausted repair, and clears its frame);
	// resume's ordinary completed-advance path is what actually re-enters
	// at "after" below.
	if nested.Child == nil || nested.Child.StepID != "verify" || !nested.Child.Completed {
		t.Fatalf("expected iteration body position 'verify' completed, got %+v", nested.Child)
	}
	if nested.Child.Repair != nil {
		t.Fatalf("expected the exhausted check's frame cleared by warn_on_failure, got %+v", nested.Child.Repair)
	}

	runner2 := &mockRunner{results: []exec.ProcessResult{{ExitCode: 0}}}
	result, err := ResumeWorkflow(filepath.Join(sessionDir, "state.json"), &Options{
		ProcessRunner: runner2, GlobExpander: &mockGlob{}, Log: &mockLog{}, ProfileStore: testAgentProfileStore(),
	})
	if err != nil {
		t.Fatalf("resume error: %v", err)
	}
	if result != ResultSuccess {
		t.Fatalf("expected success, got %q", result)
	}
	if len(runner2.calls) != 1 {
		t.Fatalf("expected exactly 1 call ('after', no repair reentry), got %d: %v", len(runner2.calls), runner2.calls)
	}
}

// TestResumeRepairBlockedCheckReentersWithFreshBudget covers the INT-002
// scenario "check failed as blocked": the guarded agent declares
// REPAIR_BLOCKED on the very first failure (no repair attempt ever starts),
// and resume re-enters the rerun-form cycle at the target with a fresh
// budget once the underlying block is presumed lifted.
func TestResumeRepairBlockedCheckReentersWithFreshBudget(t *testing.T) {
	sessionDir := t.TempDir()
	workflowPath := writeWorkflowFile(t, t.TempDir(), "test-v1.0.yaml", rerunRepairYAML)

	log := &mockLog{}
	result, err := runWorkflowFile(t, workflowPath, &Options{
		ProcessRunner: &mockRunner{results: []exec.ProcessResult{
			{ExitCode: 0, Stdout: claudeAgentOutput("looked into it: missing scope\nREPAIR_BLOCKED")},
			{ExitCode: 1, Stderr: "not ready"},
		}},
		GlobExpander: &mockGlob{}, Log: log, SessionDir: sessionDir, ProfileStore: testAgentProfileStore(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != ResultFailed {
		t.Fatalf("expected failed, got %q", result)
	}

	state, err := stateio.ReadState(filepath.Join(sessionDir, "state.json"))
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	if state.CurrentStep.Nested.Repair == nil || state.CurrentStep.Nested.Repair.Phase != model.RepairPhaseFailed {
		t.Fatalf("expected phase failed, got %+v", state.CurrentStep.Nested.Repair)
	}

	auditData, err := os.ReadFile(filepath.Join(sessionDir, "audit.log"))
	if err != nil {
		t.Fatalf("read audit.log: %v", err)
	}
	if strings.Contains(string(auditData), "repair_attempt_start") {
		t.Fatalf("audit log unexpectedly contains repair_attempt_start for a blocked-on-first-failure check:\n%s", auditData)
	}

	// Resume: whatever blocked act is presumed resolved, so the target and
	// check both succeed with a fresh budget.
	runner2 := &mockRunner{results: []exec.ProcessResult{
		{ExitCode: 0, Stdout: claudeAgentOutput("opened (ready)")},
		{ExitCode: 0, Stdout: "pr is open"},
	}}
	result, err = ResumeWorkflow(filepath.Join(sessionDir, "state.json"), &Options{
		ProcessRunner: runner2, GlobExpander: &mockGlob{}, Log: &mockLog{}, ProfileStore: testAgentProfileStore(),
	})
	if err != nil {
		t.Fatalf("resume error: %v", err)
	}
	if result != ResultSuccess {
		t.Fatalf("expected success, got %q", result)
	}
	if len(runner2.calls) != 2 {
		t.Fatalf("expected 2 calls (act, verify), got %d: %v", len(runner2.calls), runner2.calls)
	}
}

// TestResumeRepairGuardedEvidenceSurvivesInterruptionBetweenActionAndCheck
// covers the INT-002 scenario "between the guarded agent step and the
// check": act completes and the run is interrupted before verify ever
// starts (so no repair frame exists yet). On resume, verify fails fresh and
// opens a new frame; the guarded response it needs for the repair evidence
// is rebuilt from audit by act's persisted identity (LastAgent), since
// ctx.LastAgentExecution restored on resume carries only the identity, not
// the response.
func TestResumeRepairGuardedEvidenceSurvivesInterruptionBetweenActionAndCheck(t *testing.T) {
	sessionDir := t.TempDir()
	workflowPath := writeWorkflowFile(t, t.TempDir(), "test-v1.0.yaml", rerunRepairYAML)

	// Only one call: act. The run is interrupted before verify (call 2)
	// ever starts.
	runInterrupted(t, workflowPath, sessionDir, &abortingRunner{
		mockRunner:  mockRunner{results: []exec.ProcessResult{{ExitCode: 0, Stdout: claudeAgentOutput("opened (draft)")}}},
		abortOnCall: 2,
	}, &Options{ProfileStore: testAgentProfileStore()})

	state, err := stateio.ReadState(filepath.Join(sessionDir, "state.json"))
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	nested := state.CurrentStep.Nested
	if nested == nil || nested.StepID != "act" || !nested.Completed {
		t.Fatalf("expected recorded step 'act' completed, got %+v", nested)
	}
	if nested.Repair != nil {
		t.Fatalf("expected no repair frame yet (verify never ran), got %+v", nested.Repair)
	}
	if nested.LastAgent == nil {
		t.Fatalf("expected act's execution identity persisted as LastAgent")
	}

	// Resume: verify fails fresh, opening a new frame; act's replay
	// succeeds, then verify passes.
	runner2 := &mockRunner{results: []exec.ProcessResult{
		{ExitCode: 1, Stderr: "not ready"},
		{ExitCode: 0, Stdout: claudeAgentOutput("opened (ready)")},
		{ExitCode: 0, Stdout: "pr is open"},
	}}
	result, err := ResumeWorkflow(filepath.Join(sessionDir, "state.json"), &Options{
		ProcessRunner: runner2, GlobExpander: &mockGlob{}, Log: &mockLog{}, ProfileStore: testAgentProfileStore(),
	})
	if err != nil {
		t.Fatalf("resume error: %v", err)
	}
	if result != ResultSuccess {
		t.Fatalf("expected success, got %q", result)
	}
	if len(runner2.calls) != 3 {
		t.Fatalf("expected 3 calls (verify, act replay, verify replay), got %d: %v", len(runner2.calls), runner2.calls)
	}
	replayArgs := fmt.Sprintf("%v", runner2.calls[1])
	if !strings.Contains(replayArgs, "opened (draft)") {
		t.Fatalf("act's replay prompt does not carry act's own rebuilt guarded response:\n%s", replayArgs)
	}
}
