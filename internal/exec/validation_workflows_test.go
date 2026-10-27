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
	return ProcessResult{Started: true, ExitCode: 0, Stdout: claudeUsageOutput("done", 0)}, nil
}

type validationRun struct {
	outcome StepOutcome
	err     error
	events  []string
	result  string
}

// runValidationWorkflow dispatches a validation built-in against a repository
// with or without validator config and returns what ran.
func runValidationWorkflow(t *testing.T, name string, withConfig bool, failures int, params map[string]string) validationRun {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("AGENT_RUNNER_VALIDATOR_EXECUTABLE", "sh")

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
	})
	group := model.Step{ID: "validation", Steps: workflow.Steps}
	outcome, err := DispatchStep(&group, ctx, runner, &mockGlob{}, &mockLogger{})
	body, _ := os.ReadFile(resultFile)
	return validationRun{outcome: outcome, err: err, events: runner.events, result: string(body)}
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
	for _, tc := range []struct{ session, step string }{
		{"implementor-agent", "fix-violations-implementor"},
		{"lead-agent", "fix-violations-lead"},
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
