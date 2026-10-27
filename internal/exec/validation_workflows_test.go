package exec

import (
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/codagent/agent-runner/internal/loader"
	"github.com/codagent/agent-runner/internal/model"
)

// validationRunner stands in for the validator CLI and the repair agents of the
// validation built-ins. Shell steps and the compliance check run for real in a
// temporary repository; run-validator.sh fails failures times, then passes.
type validationRunner struct {
	t        *testing.T
	repo     string
	failures int
	runs     int
	events   []string
	agents   []AgentProcessOptions
}

func (r *validationRunner) RunShell(cmd string, _ bool, _ string) (ProcessResult, error) {
	return runProcess(osexec.Command("sh", "-c", cmd), r.repo, nil)
}

func (r *validationRunner) RunScript(path string, stdin []byte, _ bool, _ string) (ProcessResult, error) {
	name := filepath.Base(path)
	r.events = append(r.events, name)
	switch name {
	case "check-task-compliance.sh":
		return runProcess(osexec.Command("sh", path), r.repo, stdin)
	case "run-validator.sh":
		r.runs++
		exitCode := 0
		if r.runs <= r.failures {
			exitCode = 1
		}
		return ProcessResult{Started: true, ExitCode: exitCode, Stdout: "validator output"}, nil
	default:
		r.t.Fatalf("unexpected script %s", name)
		return ProcessResult{}, nil
	}
}

func (r *validationRunner) RunAgent(options *AgentProcessOptions) (ProcessResult, error) {
	r.events = append(r.events, "agent:"+options.Prefix)
	recorded := *options
	recorded.Args = append([]string(nil), options.Args...)
	r.agents = append(r.agents, recorded)
	return ProcessResult{Started: true, ExitCode: 0, Stdout: claudeUsageOutput("done", 0)}, nil
}

type validationRun struct {
	outcome StepOutcome
	err     error
	events  []string
	result  string
	agents  []AgentProcessOptions
}

// runValidationWorkflow dispatches a validation built-in against a repository
// with or without validator config and returns what ran.
const (
	existingImplementorSession = "11111111-1111-4111-8111-111111111111"
	existingLeadSession        = "22222222-2222-4222-8222-222222222222"
)

func runValidationWorkflow(t *testing.T, name string, withConfig bool, failures int, params map[string]string) validationRun {
	t.Helper()
	return runValidationWorkflowWith(t, name, withConfig, failures, params, validationOptions{executable: "sh"})
}

// validationOptions varies the environment around a validation built-in.
type validationOptions struct {
	executable string       // AGENT_RUNNER_VALIDATOR_EXECUTABLE
	before     []model.Step // steps run before the built-in's own steps
}

func runValidationWorkflowWith(t *testing.T, name string, withConfig bool, failures int, params map[string]string, opts validationOptions) validationRun {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("AGENT_RUNNER_VALIDATOR_EXECUTABLE", opts.executable)

	ref := "builtin:core/" + name + "-v1.0.yaml"
	workflow, err := loader.LoadWorkflow(ref, loader.Options{})
	if err != nil {
		t.Fatalf("LoadWorkflow(%s): %v", ref, err)
	}
	repo := t.TempDir()
	if withConfig {
		writeValidatorConfig(t, repo)
	}
	t.Chdir(repo)

	resultFile := filepath.Join(t.TempDir(), "result.txt")
	if params == nil {
		params = map[string]string{}
	}
	params["result_file"] = resultFile
	for _, p := range workflow.Params {
		if _, ok := params[p.Name]; !ok {
			params[p.Name] = p.Default
		}
	}
	runner := &validationRunner{t: t, repo: repo, failures: failures}
	ctx := model.NewRootContext(&model.RootContextOptions{
		Params:            params,
		WorkflowFile:      ref,
		SessionDir:        t.TempDir(),
		NamedSessionDecls: map[string]string{"implementor-agent": "implementor", "lead-agent": "lead"},
		NamedSessions:     map[string]string{"implementor-agent": existingImplementorSession, "lead-agent": existingLeadSession},
	})
	group := model.Step{ID: "validation", Steps: append(append([]model.Step{}, opts.before...), workflow.Steps...)}
	outcome, err := DispatchStep(&group, ctx, runner, &mockGlob{}, &mockLogger{})
	body, _ := os.ReadFile(resultFile)
	return validationRun{outcome: outcome, err: err, events: runner.events, result: string(body), agents: runner.agents}
}

func TestValidationNoneRunsNothingAndRecordsSkipped(t *testing.T) {
	got := runValidationWorkflow(t, "validation-none", true, 0, nil)
	if got.err != nil || got.outcome != OutcomeSuccess {
		t.Fatalf("outcome = %q, err = %v", got.outcome, got.err)
	}
	if len(got.events) != 0 {
		t.Errorf("none ran %v, want nothing", got.events)
	}
	if !strings.HasPrefix(got.result, "SKIPPED\n") {
		t.Errorf("result = %q, want SKIPPED", got.result)
	}
}

func TestValidationAutoWithoutConfigSkips(t *testing.T) {
	got := runValidationWorkflow(t, "validation-auto", false, 0, nil)
	if got.err != nil || got.outcome != OutcomeSuccess {
		t.Fatalf("outcome = %q, err = %v", got.outcome, got.err)
	}
	if len(got.events) != 0 {
		t.Errorf("auto without config ran %v, want nothing", got.events)
	}
	if !strings.HasPrefix(got.result, "SKIPPED\n") {
		t.Errorf("result = %q, want SKIPPED", got.result)
	}
}

func TestValidationAutoWithConfigRunsValidator(t *testing.T) {
	got := runValidationWorkflow(t, "validation-auto", true, 0, nil)
	if got.err != nil || got.outcome != OutcomeSuccess {
		t.Fatalf("outcome = %q, err = %v", got.outcome, got.err)
	}
	want := []string{"check-task-compliance.sh", "run-validator.sh"}
	if diff := cmp.Diff(want, got.events); diff != "" {
		t.Errorf("events mismatch (-want +got):\n%s", diff)
	}
}

func TestValidationAgentValidatorRepairsInNamedSession(t *testing.T) {
	for _, tc := range []struct{ session, step, id string }{
		{"implementor-agent", "fix-violations-implementor", existingImplementorSession},
		{"lead-agent", "fix-violations-lead", existingLeadSession},
	} {
		t.Run(tc.session, func(t *testing.T) {
			got := runValidationWorkflow(t, "validation-agent-validator", true, 1, map[string]string{"repair_session": tc.session})
			if got.err != nil || got.outcome != OutcomeSuccess {
				t.Fatalf("outcome = %q, err = %v", got.outcome, got.err)
			}
			var agents []string
			for _, e := range got.events {
				if strings.HasPrefix(e, "agent:") {
					agents = append(agents, e)
				}
			}
			if len(agents) != 1 || !strings.Contains(agents[0], tc.step) {
				t.Errorf("agents = %v, want one repair by %s", agents, tc.step)
			}
			if len(got.agents) != 1 {
				t.Fatalf("agent invocations = %d, want 1", len(got.agents))
			}
			args := strings.Join(got.agents[0].Args, " ")
			if !strings.Contains(args, "--resume "+tc.id) || strings.Contains(args, "--session-id") {
				t.Errorf("repair args %q do not resume session %s", args, tc.id)
			}
		})
	}
}

func TestValidationAgentValidatorExhaustedRepairsWarn(t *testing.T) {
	got := runValidationWorkflow(t, "validation-agent-validator", true, 100, nil)
	if got.err != nil {
		t.Fatalf("err = %v", got.err)
	}
	if got.outcome == OutcomeFailed {
		t.Errorf("outcome = %q, want a warning-completed success", got.outcome)
	}
}

func TestValidationAgentValidatorRejectsBadSetup(t *testing.T) {
	t.Run("no config", func(t *testing.T) {
		got := runValidationWorkflow(t, "validation-agent-validator", false, 0, nil)
		if got.outcome != OutcomeFailed {
			t.Errorf("outcome = %q, want failed", got.outcome)
		}
		if len(got.events) != 0 {
			t.Errorf("ran %v after the missing config", got.events)
		}
	})
	t.Run("bad repair session", func(t *testing.T) {
		got := runValidationWorkflow(t, "validation-agent-validator", true, 0, map[string]string{"repair_session": "acceptance-tester"})
		if got.outcome != OutcomeFailed {
			t.Errorf("outcome = %q, want failed", got.outcome)
		}
	})
}

type commandRecorder struct{ commands []string }

func (r *commandRecorder) RunShell(cmd string, _ bool, _ string) (ProcessResult, error) {
	r.commands = append(r.commands, cmd)
	return ProcessResult{Started: true}, nil
}

func (r *commandRecorder) RunScript(string, []byte, bool, string) (ProcessResult, error) {
	return ProcessResult{Started: true}, nil
}

func (r *commandRecorder) RunAgent(*AgentProcessOptions) (ProcessResult, error) {
	return ProcessResult{Started: true}, nil
}

// A sub-workflow binds an aliased param after defaults, whether the caller
// passes an alias, a literal, or nothing.
func TestSubWorkflowBindsAliasedParam(t *testing.T) {
	dir := t.TempDir()
	child := "name: child\nparams:\n  - name: mode\n    required: false\n    default: auto\n    aliases:\n      auto: builtin:core/validation-auto-v1.0.yaml\n      none: builtin:core/validation-none-v1.0.yaml\nsteps:\n  - id: show\n    command: echo {{mode}}\n"
	if err := os.WriteFile(filepath.Join(dir, "child-v1.0.yaml"), []byte(child), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, passed, want string }{
		{"alias", "none", "builtin:core/validation-none-v1.0.yaml"},
		{"default", "", "builtin:core/validation-auto-v1.0.yaml"},
		{"literal", "/abs/mine-v1.0.yaml", "/abs/mine-v1.0.yaml"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			params := map[string]string{}
			if tc.passed != "" {
				params["mode"] = tc.passed
			}
			step := &model.Step{ID: "call", Workflow: "child-v1.0.yaml", Params: params}
			ctx := model.NewRootContext(&model.RootContextOptions{WorkflowFile: filepath.Join(dir, "parent-v1.0.yaml"), SessionDir: t.TempDir()})
			runner := &commandRecorder{}
			if outcome, err := DispatchStep(step, ctx, runner, &mockGlob{}, &mockLogger{}); err != nil || outcome != OutcomeSuccess {
				t.Fatalf("outcome = %q, err = %v", outcome, err)
			}
			if len(runner.commands) != 1 || !strings.Contains(runner.commands[0], tc.want) {
				t.Errorf("commands = %v, want one containing %q", runner.commands, tc.want)
			}
		})
	}
}

func TestValidationAgentValidatorNamesMissingExecutable(t *testing.T) {
	got := runValidationWorkflowWith(t, "validation-agent-validator", true, 0, nil, validationOptions{executable: "no-such-validator-binary"})
	if got.outcome != OutcomeFailed {
		t.Errorf("outcome = %q, want failed", got.outcome)
	}
	if len(got.events) != 0 {
		t.Errorf("ran %v after the missing executable", got.events)
	}
}

// auto detects configuration on every invocation, so configuration created by
// an earlier step of the same run is picked up.
func TestValidationAutoPicksUpConfigCreatedDuringRun(t *testing.T) {
	create := model.Step{ID: "create-config", Command: "mkdir -p .validator && : > .validator/config.yml"}
	got := runValidationWorkflowWith(t, "validation-auto", false, 0, nil, validationOptions{executable: "sh", before: []model.Step{create}})
	if got.outcome != OutcomeSuccess {
		t.Fatalf("outcome = %q", got.outcome)
	}
	if !strings.Contains(strings.Join(got.events, " "), "run-validator.sh") {
		t.Errorf("events %v: validator did not run after config appeared", got.events)
	}
}
