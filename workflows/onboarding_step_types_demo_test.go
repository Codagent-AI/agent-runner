package builtinworkflows

import (
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/model"
	"github.com/google/go-cmp/cmp"
	"gopkg.in/yaml.v3"
)

func TestStepTypesDemoWorkflowShape(t *testing.T) {
	wf := readBuiltinWorkflowForTest(t, "builtin:onboarding/step-types-demo.yaml")

	wantIDs := []string{
		"intro-ui",
		"explain-interactive",
		"interactive-qa",
		"explain-headless",
		"headless-demo",
		"explain-shell",
		"shell-capture",
		"summary",
		"learn-more-qa",
	}
	gotIDs := stepIDs(wf.Steps)
	if diff := cmp.Diff(wantIDs, gotIDs); diff != "" {
		t.Fatalf("step IDs mismatch (-want +got):\n%s", diff)
	}

	assertUIStep(t, &wf.Steps[0], "UI")
	assertUIStep(t, &wf.Steps[1], "interactive")
	assertAgentStep(t, &wf.Steps[2], "planner", model.ModeInteractive)
	assertUIStep(t, &wf.Steps[3], "headless")
	assertAgentStep(t, &wf.Steps[4], "implementor", model.ModeHeadless)
	assertUIStep(t, &wf.Steps[5], "shell")

	shellCapture := wf.Steps[6]
	if shellCapture.StepType() != "shell" {
		t.Fatalf("shell-capture type = %q, want shell", shellCapture.StepType())
	}
	if shellCapture.Capture != "shell_capture" {
		t.Fatalf("shell-capture capture = %q, want shell_capture", shellCapture.Capture)
	}
	if !strings.Contains(shellCapture.Command, "agent-runner-demo-capture") {
		t.Fatalf("shell-capture command does not emit deterministic demo value: %q", shellCapture.Command)
	}

	summary := wf.Steps[7]
	assertUIStep(t, &summary, "shell_capture")
	if summary.OutcomeCapture != "summary_action" {
		t.Fatalf("summary outcome_capture = %q, want summary_action", summary.OutcomeCapture)
	}
	gotOutcomes := outcomes(summary.Actions)
	if diff := cmp.Diff([]string{"continue", "learn_more"}, gotOutcomes); diff != "" {
		t.Fatalf("summary outcomes mismatch (-want +got):\n%s", diff)
	}

	learnMore := wf.Steps[8]
	assertAgentStep(t, &learnMore, "planner", model.ModeInteractive)
	if learnMore.SkipIf != `sh: [ "x{{summary_action}}" != "xlearn_more" ]` {
		t.Fatalf("learn-more-qa skip_if = %q", learnMore.SkipIf)
	}
}

func TestStepTypesDemoPromptsUsePackagedDocsAndStayNonDestructive(t *testing.T) {
	wf := readBuiltinWorkflowForTest(t, "builtin:onboarding/step-types-demo.yaml")

	for i := range wf.Steps {
		step := &wf.Steps[i]
		if step.Command == "" {
			continue
		}
		lowerCommand := strings.ToLower(step.Command)
		for _, forbidden := range []string{"touch ", "mkdir", "rm ", "write-setting", "config.yaml", "settings.yaml", "mktemp"} {
			if strings.Contains(lowerCommand, forbidden) {
				t.Fatalf("%s command contains destructive operation %q: %q", step.ID, forbidden, step.Command)
			}
		}
	}

	interactivePrompt := stepByID(t, &wf, "interactive-qa").Prompt
	for _, want := range []string{
		"advance",
		"lightweight Agent Runner questions",
		"{{session_dir}}/bundled/onboarding/docs/",
		"complete the step",
	} {
		if !strings.Contains(interactivePrompt, want) {
			t.Fatalf("interactive prompt missing %q:\n%s", want, interactivePrompt)
		}
	}

	headlessPrompt := strings.ToLower(stepByID(t, &wf, "headless-demo").Prompt)
	for _, forbidden := range []string{"touch ", "mkdir", "rm ", "write-setting", "config.yaml", "settings.yaml"} {
		if strings.Contains(headlessPrompt, forbidden) {
			t.Fatalf("headless prompt contains destructive instruction %q:\n%s", forbidden, headlessPrompt)
		}
	}

	learnMorePrompt := stepByID(t, &wf, "learn-more-qa").Prompt
	if !strings.Contains(learnMorePrompt, "{{session_dir}}/bundled/onboarding/docs/") {
		t.Fatalf("learn-more prompt does not reference packaged docs:\n%s", learnMorePrompt)
	}
}

func TestOnboardingRunsStepTypesDemoBeforeCompletion(t *testing.T) {
	wf := readBuiltinWorkflowForTest(t, "builtin:onboarding/onboarding.yaml")

	wantIDs := []string{"intro", "set-dismissed", "step-types-demo", "set-completed"}
	gotIDs := stepIDs(wf.Steps)
	if diff := cmp.Diff(wantIDs, gotIDs); diff != "" {
		t.Fatalf("onboarding step IDs mismatch (-want +got):\n%s", diff)
	}

	intro := stepByID(t, &wf, "intro")
	gotOutcomes := outcomes(intro.Actions)
	if diff := cmp.Diff([]string{"continue", "not_now", "dismiss"}, gotOutcomes); diff != "" {
		t.Fatalf("intro outcomes mismatch (-want +got):\n%s", diff)
	}
	if strings.Contains(strings.ToLower(intro.Body), "setup") {
		t.Fatalf("intro body still presents as setup:\n%s", intro.Body)
	}

	demo := stepByID(t, &wf, "step-types-demo")
	if demo.Workflow != "step-types-demo.yaml" {
		t.Fatalf("step-types-demo workflow = %q", demo.Workflow)
	}
	if demo.SkipIf != `sh: [ {{demo_action}} != continue ]` {
		t.Fatalf("step-types-demo skip_if = %q", demo.SkipIf)
	}

	completed := stepByID(t, &wf, "set-completed")
	if !strings.Contains(completed.Command, "onboarding.completed_at") {
		t.Fatalf("set-completed command = %q", completed.Command)
	}
}

func readBuiltinWorkflowForTest(t *testing.T, ref string) model.Workflow {
	t.Helper()
	data, err := ReadFile(ref)
	if err != nil {
		t.Fatalf("ReadFile(%s) returned error: %v", ref, err)
	}
	var wf model.Workflow
	if err := yaml.Unmarshal(data, &wf); err != nil {
		t.Fatalf("parse %s: %v", ref, err)
	}
	wf.ApplyDefaults()
	if err := wf.Validate(nil); err != nil {
		t.Fatalf("validate %s: %v", ref, err)
	}
	return wf
}

func stepByID(t *testing.T, wf *model.Workflow, id string) *model.Step {
	t.Helper()
	for i := range wf.Steps {
		if wf.Steps[i].ID == id {
			return &wf.Steps[i]
		}
	}
	t.Fatalf("step %q not found", id)
	return nil
}

func assertUIStep(t *testing.T, step *model.Step, bodyContains string) {
	t.Helper()
	if step.StepType() != "ui" {
		t.Fatalf("%s type = %q, want ui", step.ID, step.StepType())
	}
	if !strings.Contains(strings.ToLower(step.Body), strings.ToLower(bodyContains)) {
		t.Fatalf("%s body does not contain %q:\n%s", step.ID, bodyContains, step.Body)
	}
}

func assertAgentStep(t *testing.T, step *model.Step, agent string, mode model.StepMode) {
	t.Helper()
	if step.StepType() != "agent" {
		t.Fatalf("%s type = %q, want agent", step.ID, step.StepType())
	}
	if step.Agent != agent {
		t.Fatalf("%s agent = %q, want %q", step.ID, step.Agent, agent)
	}
	if step.Mode != mode {
		t.Fatalf("%s mode = %q, want %q", step.ID, step.Mode, mode)
	}
}

func outcomes(actions []model.UIAction) []string {
	var out []string
	for _, action := range actions {
		out = append(out, action.Outcome)
	}
	return out
}

func stepIDs(steps []model.Step) []string {
	var ids []string
	for i := range steps {
		ids = append(ids, steps[i].ID)
	}
	return ids
}
