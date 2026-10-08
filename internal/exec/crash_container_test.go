package exec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/config"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/runlock"
)

type crashContainerRunner struct {
	agentCode int
	shellCode int
	commands  []string
}

func TestSubWorkflowCrashClassification(t *testing.T) {
	for _, tt := range []struct {
		name              string
		continueOnFailure bool
		shellCode         int
		wantOutcome       StepOutcome
		wantKind          model.FailureKind
	}{
		{"blocking", false, 0, OutcomeFailed, model.FailureInfrastructure},
		{"absorbed then ordinary failure", true, 1, OutcomeFailed, model.FailureStep},
		{"absorbed then success", true, 0, OutcomeSuccess, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			continuation := ""
			if tt.continueOnFailure {
				continuation = "    continue_on_failure: true\n"
			}
			child := `name: child
steps:
  - id: agent
    agent: test-agent
    session: new
    mode: autonomous
    prompt: work
` + continuation + `  - id: check
    command: true
`
			if err := os.WriteFile(filepath.Join(dir, "child-v1.0.yaml"), []byte(child), 0o600); err != nil {
				t.Fatal(err)
			}
			ctx := crashContainerContext()
			ctx.WorkflowFile = filepath.Join(dir, "parent-v1.0.yaml")
			runner := &crashContainerRunner{agentCode: 1, shellCode: tt.shellCode}
			step := &model.Step{ID: "sub", Workflow: "child-v1.0.yaml"}
			outcome, err := DispatchStep(step, ctx, runner, &mockGlob{}, &mockLogger{})
			if err != nil || outcome != tt.wantOutcome || ctx.StepFailure.Kind != tt.wantKind || !ctx.Crashes.ObservedUnder([]model.NestingSegment{{StepID: "sub"}}) {
				t.Fatalf("outcome=%q err=%v failure=%+v", outcome, err, ctx.StepFailure)
			}
		})
	}
}

func TestSubWorkflowSkipKeepsPrecedingCrashSignal(t *testing.T) {
	dir := t.TempDir()
	child := `name: child
steps:
  - id: agent
    agent: test-agent
    session: new
    mode: autonomous
    prompt: work
    continue_on_failure: true
  - id: skip
    command: true
    skip_if: "sh: true"
  - id: reader
    command: "echo {{last_step_failure_kind}} {{last_step_crash_observed}}"
`
	if err := os.WriteFile(filepath.Join(dir, "child-v1.0.yaml"), []byte(child), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := crashContainerContext()
	ctx.WorkflowFile = filepath.Join(dir, "parent-v1.0.yaml")
	runner := &crashContainerRunner{agentCode: 1}
	step := &model.Step{ID: "sub", Workflow: "child-v1.0.yaml"}
	outcome, err := DispatchStep(step, ctx, runner, &mockGlob{}, &mockLogger{})
	if err != nil || outcome != OutcomeSuccess || len(runner.commands) != 1 || !strings.Contains(runner.commands[0], "'infrastructure' 'true'") {
		t.Fatalf("outcome=%s err=%v commands=%v", outcome, err, runner.commands)
	}
}
func (r *crashContainerRunner) RunAgent(*AgentProcessOptions) (ProcessResult, error) {
	return ProcessResult{Started: true, ExitCode: r.agentCode, Stderr: "capacity"}, nil
}
func (r *crashContainerRunner) RunShell(command string, _ bool, _ string) (ProcessResult, error) {
	r.commands = append(r.commands, command)
	return ProcessResult{Started: true, ExitCode: r.shellCode}, nil
}
func (r *crashContainerRunner) RunScript(string, []byte, bool, string) (ProcessResult, error) {
	return ProcessResult{Started: true}, nil
}

func crashContainerContext() *model.ExecutionContext {
	return model.NewRootContext(&model.RootContextOptions{ProfileStore: &config.Config{ActiveAgents: map[string]*config.Agent{"test-agent": {CLI: "claude", DefaultMode: "autonomous"}}}})
}

func crashChild(continueOnFailure bool) model.Step {
	return model.Step{ID: "agent", Agent: "test-agent", Session: model.SessionNew, Prompt: "work", Mode: model.ModeAutonomous, ContinueOnFailure: continueOnFailure}
}

func TestCrashedAgentWarnOnFailureKeepsInfrastructureKind(t *testing.T) {
	ctx := crashContainerContext()
	recorder := &recordingAuditLogger{}
	ctx.AuditLogger = recorder
	step := crashChild(false)
	step.WarnOnFailure = true
	outcome, err := DispatchStep(&step, ctx, &crashContainerRunner{agentCode: 1}, &mockGlob{}, &mockLogger{})
	if err != nil || outcome != OutcomeFailed || ctx.StepFailure.Kind != model.FailureInfrastructure {
		t.Fatalf("outcome=%q err=%v failure=%+v", outcome, err, ctx.StepFailure)
	}
	if ctx.WarningOrigins.Count() != 1 {
		t.Fatalf("warning count=%d", ctx.WarningOrigins.Count())
	}
	for _, event := range recorder.events {
		if event.Type == audit.EventStepEnd && event.Data["outcome"] == string(OutcomeFailed) && event.Data["failure_kind"] == model.FailureInfrastructure && event.Data["status"] == "warning" {
			return
		}
	}
	t.Fatalf("missing failed infrastructure warning event: %+v", recorder.events)
}

func TestAgentPreStartAndControlSetupClassification(t *testing.T) {
	t.Run("prestart", func(t *testing.T) {
		ctx := crashContainerContext()
		step := &model.Step{ID: "agent", Agent: "test-agent", Prompt: "{{missing}}", Session: model.SessionNew, Mode: model.ModeAutonomous}
		outcome, _ := DispatchStep(step, ctx, &crashContainerRunner{}, &mockGlob{}, &mockLogger{})
		if outcome != OutcomeFailed || ctx.StepFailure.Kind != model.FailureStep || ctx.Crashes.ObservedUnder(nil) {
			t.Fatalf("outcome=%s failure=%+v", outcome, ctx.StepFailure)
		}
	})
	t.Run("control setup", func(t *testing.T) {
		ctx := crashContainerContext()
		step := &model.Step{ID: "agent", Agent: "test-agent", Prompt: "work", Session: model.SessionNew, Mode: model.ModeAutonomous, Tools: []model.RunnerTool{model.RunnerToolCallAgent}}
		outcome, _ := DispatchStep(step, ctx, &crashContainerRunner{}, &mockGlob{}, &mockLogger{})
		if outcome != OutcomeFailed || ctx.StepFailure.Kind != model.FailureInfrastructure || !ctx.Crashes.ObservedUnder([]model.NestingSegment{{StepID: "agent"}}) {
			t.Fatalf("outcome=%s failure=%+v", outcome, ctx.StepFailure)
		}
	})
	t.Run("agent call runtime", func(t *testing.T) {
		ctx := crashContainerContext()
		ctx.SessionDir = t.TempDir()
		pid, err := runlock.Acquire(ctx.SessionDir)
		if err != nil || pid != 0 {
			t.Fatalf("run lock pid=%d err=%v", pid, err)
		}
		t.Cleanup(func() { runlock.Delete(ctx.SessionDir) })
		step := &model.Step{ID: "agent", Agent: "test-agent", Prompt: "work", Session: model.SessionNew, Mode: model.ModeAutonomous, Tools: []model.RunnerTool{model.RunnerToolCallAgent}}
		outcome, _ := DispatchStep(step, ctx, &crashContainerRunner{}, &mockGlob{}, &mockLogger{})
		if outcome != OutcomeFailed || ctx.StepFailure.Kind != model.FailureInfrastructure || !ctx.Crashes.ObservedUnder([]model.NestingSegment{{StepID: "agent"}}) {
			t.Fatalf("outcome=%s failure=%+v", outcome, ctx.StepFailure)
		}
	})
}

func TestUncollectedCallFailureDoesNotBecomeCrash(t *testing.T) {
	result := &AgentInvocationResult{Outcome: OutcomeSuccess}
	failAgentStepForUncollectedCalls(result, []string{"child"})
	if result.Outcome != OutcomeFailed || result.Crashed {
		t.Fatalf("result=%+v", result)
	}
}

func TestGroupCrashClassification(t *testing.T) {
	for _, tt := range []struct {
		name              string
		continueOnFailure bool
		shellCode         int
		wantOutcome       StepOutcome
		wantKind          model.FailureKind
	}{
		{"blocking", false, 0, OutcomeFailed, model.FailureInfrastructure},
		{"absorbed then shell failure", true, 1, OutcomeFailed, model.FailureStep},
		{"absorbed then success", true, 0, OutcomeSuccess, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := crashContainerContext()
			runner := &crashContainerRunner{agentCode: 1, shellCode: tt.shellCode}
			group := &model.Step{ID: "group", Steps: []model.Step{crashChild(tt.continueOnFailure), {ID: "check", Command: "true"}}}
			outcome, err := DispatchStep(group, ctx, runner, &mockGlob{}, &mockLogger{})
			if err != nil || outcome != tt.wantOutcome || ctx.StepFailure.Kind != tt.wantKind || !ctx.Crashes.ObservedUnder([]model.NestingSegment{{StepID: "group"}}) {
				t.Fatalf("outcome=%q err=%v failure=%+v", outcome, err, ctx.StepFailure)
			}
		})
	}
}

func TestGroupSkippedStepClearsPrecedingCrashSignal(t *testing.T) {
	ctx := crashContainerContext()
	runner := &crashContainerRunner{agentCode: 1}
	group := &model.Step{ID: "group", Steps: []model.Step{
		crashChild(true),
		{ID: "skip", Command: "true", SkipIf: "sh: true"},
		{ID: "reader", Command: "echo {{last_step_failure_kind}} {{last_step_crash_observed}}"},
	}}
	outcome, err := DispatchStep(group, ctx, runner, &mockGlob{}, &mockLogger{})
	if err != nil || outcome != OutcomeSuccess || len(runner.commands) != 1 || !strings.Contains(runner.commands[0], "'' 'false'") {
		t.Fatalf("outcome=%s err=%v commands=%v", outcome, err, runner.commands)
	}
}

func TestLoopCrashClassification(t *testing.T) {
	maxIterations := 1
	for _, tt := range []struct {
		name              string
		continueOnFailure bool
		shellCode         int
		wantOutcome       StepOutcome
		wantKind          model.FailureKind
	}{
		{"blocking", false, 0, OutcomeFailed, model.FailureInfrastructure},
		{"absorbed then ordinary failure", true, 1, OutcomeFailed, model.FailureStep},
		{"absorbed then success", true, 0, OutcomeSuccess, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := crashContainerContext()
			runner := &crashContainerRunner{agentCode: 1, shellCode: tt.shellCode}
			loop := &model.Step{ID: "loop", Loop: &model.Loop{Max: &maxIterations}, Steps: []model.Step{crashChild(tt.continueOnFailure), {ID: "check", Command: "true"}}}
			outcome, err := DispatchStep(loop, ctx, runner, &mockGlob{}, &mockLogger{})
			if err != nil || outcome != tt.wantOutcome || ctx.StepFailure.Kind != tt.wantKind || !ctx.Crashes.ObservedUnder([]model.NestingSegment{{StepID: "loop"}}) {
				t.Fatalf("outcome=%q err=%v failure=%+v", outcome, err, ctx.StepFailure)
			}
		})
	}
}

func TestLoopSkippedStepClearsPrecedingCrashSignal(t *testing.T) {
	maxIterations := 1
	ctx := crashContainerContext()
	runner := &crashContainerRunner{agentCode: 1}
	loop := &model.Step{ID: "loop", Loop: &model.Loop{Max: &maxIterations}, Steps: []model.Step{
		crashChild(true),
		{ID: "skip", Command: "true", SkipIf: "sh: true"},
		{ID: "reader", Command: "echo {{last_step_failure_kind}} {{last_step_crash_observed}}"},
	}}
	outcome, err := DispatchStep(loop, ctx, runner, &mockGlob{}, &mockLogger{})
	if err != nil || outcome != OutcomeSuccess || len(runner.commands) != 1 || !strings.Contains(runner.commands[0], "'' 'false'") {
		t.Fatalf("outcome=%s err=%v commands=%v", outcome, err, runner.commands)
	}
}

func TestNestedSubWorkflowGroupLoopCrash(t *testing.T) {
	dir := t.TempDir()
	child := `name: child
steps:
  - id: agent
    agent: test-agent
    session: new
    mode: autonomous
    prompt: work
`
	if err := os.WriteFile(filepath.Join(dir, "child-v1.0.yaml"), []byte(child), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := crashContainerContext()
	ctx.WorkflowFile = filepath.Join(dir, "parent-v1.0.yaml")
	runner := &crashContainerRunner{agentCode: 1}
	maxIterations := 1
	loop := &model.Step{ID: "loop", Loop: &model.Loop{Max: &maxIterations}, Steps: []model.Step{{ID: "group", Steps: []model.Step{{ID: "sub", Workflow: "child-v1.0.yaml"}}}}}
	outcome, err := DispatchStep(loop, ctx, runner, &mockGlob{}, &mockLogger{})
	if err != nil || outcome != OutcomeFailed || ctx.StepFailure.Kind != model.FailureInfrastructure {
		t.Fatalf("outcome=%s err=%v failure=%+v", outcome, err, ctx.StepFailure)
	}
	origin := ctx.StepFailure.Origin
	if origin == nil || len(origin.Path) != 4 || origin.Path[0].StepID != "loop" || origin.Path[1].StepID != "group" || origin.Path[2].StepID != "sub" || origin.Path[3].StepID != "agent" {
		t.Fatalf("origin=%+v", origin)
	}
}

func TestContainerErrorsReplaceAbsorbedCrashKind(t *testing.T) {
	maxIterations := 1
	for _, tt := range []struct {
		name string
		step model.Step
	}{
		{"group", model.Step{ID: "group", Steps: []model.Step{crashChild(true), {ID: "bad", Command: "true", SkipIf: "sh: echo {{missing}}"}}}},
		{"loop", model.Step{ID: "loop", Loop: &model.Loop{Max: &maxIterations}, Steps: []model.Step{crashChild(true), {ID: "bad", Command: "true", SkipIf: "sh: echo {{missing}}"}}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := crashContainerContext()
			outcome, err := DispatchStep(&tt.step, ctx, &crashContainerRunner{agentCode: 1}, &mockGlob{}, &mockLogger{})
			if outcome != OutcomeFailed || err == nil || ctx.StepFailure.Kind != model.FailureStep || ctx.StepFailure.Origin != nil || !ctx.Crashes.ObservedUnder([]model.NestingSegment{{StepID: tt.step.ID}}) {
				t.Fatalf("outcome=%s err=%v failure=%+v", outcome, err, ctx.StepFailure)
			}
		})
	}
	// The sub-workflow has its own context, so its orchestration error must
	// replace the child crash before the parent copies its failure.
	dir := t.TempDir()
	child := `name: child
steps:
  - id: agent
    agent: test-agent
    session: new
    mode: autonomous
    prompt: work
    continue_on_failure: true
  - id: bad
    command: true
    skip_if: "sh: echo {{missing}}"
`
	if err := os.WriteFile(filepath.Join(dir, "child-v1.0.yaml"), []byte(child), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := crashContainerContext()
	ctx.WorkflowFile = filepath.Join(dir, "parent-v1.0.yaml")
	step := &model.Step{ID: "sub", Workflow: "child-v1.0.yaml"}
	outcome, err := DispatchStep(step, ctx, &crashContainerRunner{agentCode: 1}, &mockGlob{}, &mockLogger{})
	if outcome != OutcomeFailed || err == nil || ctx.StepFailure.Kind != model.FailureStep || ctx.StepFailure.Origin != nil || !ctx.Crashes.ObservedUnder([]model.NestingSegment{{StepID: "sub"}}) {
		t.Fatalf("outcome=%s err=%v failure=%+v", outcome, err, ctx.StepFailure)
	}
}
