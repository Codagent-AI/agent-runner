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
		"back-in-workflow",
		"explain-headless",
		"headless-demo",
		"review-headless",
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
	assertAgentStep(t, &wf.Steps[2], "planner", "", model.ModeInteractive)
	assertUIStep(t, &wf.Steps[3], "TUI")
	assertUIStep(t, &wf.Steps[4], "headless")
	assertAgentStep(t, &wf.Steps[5], "implementor", "", model.ModeHeadless)
	assertUIStep(t, &wf.Steps[6], "Headless")
	assertUIStep(t, &wf.Steps[7], "shell")

	shellCapture := wf.Steps[8]
	if shellCapture.StepType() != "shell" {
		t.Fatalf("shell-capture type = %q, want shell", shellCapture.StepType())
	}
	if shellCapture.Capture != "shell_capture" {
		t.Fatalf("shell-capture capture = %q, want shell_capture", shellCapture.Capture)
	}
	if !strings.Contains(shellCapture.Command, "agent-runner-demo-capture") {
		t.Fatalf("shell-capture command does not emit deterministic demo value: %q", shellCapture.Command)
	}

	summary := wf.Steps[9]
	assertUIStep(t, &summary, "shell_capture")
	if summary.OutcomeCapture != "summary_action" {
		t.Fatalf("summary outcome_capture = %q, want summary_action", summary.OutcomeCapture)
	}
	gotOutcomes := outcomes(summary.Actions)
	if diff := cmp.Diff([]string{"continue", "learn_more"}, gotOutcomes); diff != "" {
		t.Fatalf("summary outcomes mismatch (-want +got):\n%s", diff)
	}

	learnMore := wf.Steps[10]
	assertAgentStep(t, &learnMore, "planner", "", model.ModeInteractive)
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

	wantIDs := []string{"step-types-demo", "guided-workflow", "validator", "advanced", "set-completed"}
	gotIDs := stepIDs(wf.Steps)
	if diff := cmp.Diff(wantIDs, gotIDs); diff != "" {
		t.Fatalf("onboarding step IDs mismatch (-want +got):\n%s", diff)
	}

	wantWorkflows := map[string]string{
		"step-types-demo": "step-types-demo.yaml",
		"guided-workflow": "guided-workflow.yaml",
		"validator":       "validator.yaml",
		"advanced":        "advanced.yaml",
	}
	for id, want := range wantWorkflows {
		step := stepByID(t, &wf, id)
		if step.Workflow != want {
			t.Fatalf("%s workflow = %q, want %q", id, step.Workflow, want)
		}
	}

	completed := stepByID(t, &wf, "set-completed")
	if !strings.Contains(completed.Command, "onboarding.completed_at") {
		t.Fatalf("set-completed command = %q", completed.Command)
	}
}

func TestGuidedWorkflowShape(t *testing.T) {
	wf := readBuiltinWorkflowForTest(t, "builtin:onboarding/guided-workflow.yaml")

	wantSessions := map[string]string{
		"planning-session": "planner",
		"tutor-session":    "planner",
		"impl-session":     "implementor",
	}
	assertSessions(t, wf.Sessions, wantSessions)

	wantIDs := []string{
		"intro-ui",
		"capture-cwd",
		"confirm-cwd",
		"check-git-clean",
		"warn-dirty",
		"create-plan-dir",
		"explain-plan",
		"plan",
		"locate-task",
		"validate-plan",
		"explain-tutor",
		"tutor",
		"explain-impl",
		"implement",
		"summary",
	}
	gotIDs := stepIDs(wf.Steps)
	if diff := cmp.Diff(wantIDs, gotIDs); diff != "" {
		t.Fatalf("guided workflow step IDs mismatch (-want +got):\n%s", diff)
	}

	assertUIStep(t, stepByID(t, &wf, "intro-ui"), "plan a real task")
	if stepByID(t, &wf, "capture-cwd").Capture != "cwd" {
		t.Fatal("capture-cwd should capture cwd")
	}
	assertUIStep(t, stepByID(t, &wf, "confirm-cwd"), "{{cwd}}")
	gitStatus := stepByID(t, &wf, "check-git-clean")
	if gitStatus.Capture != "git_status" || !strings.Contains(gitStatus.Command, "git status --porcelain") || !strings.Contains(gitStatus.Command, "true") {
		t.Fatalf("check-git-clean command/capture mismatch: %#v", gitStatus)
	}
	if stepByID(t, &wf, "warn-dirty").SkipIf != `sh: [ -z "{{git_status}}" ]` {
		t.Fatalf("warn-dirty skip_if = %q", stepByID(t, &wf, "warn-dirty").SkipIf)
	}
	if stepByID(t, &wf, "create-plan-dir").Capture != "plan_dir" {
		t.Fatal("create-plan-dir should capture plan_dir")
	}
	assertAgentStep(t, stepByID(t, &wf, "plan"), "", "planning-session", model.ModeInteractive)
	if !strings.Contains(stepByID(t, &wf, "plan").Prompt, "First, ask the user what the change is about. DO NOT attempt to guess.") {
		t.Fatalf("plan prompt missing explicit first-question instruction:\n%s", stepByID(t, &wf, "plan").Prompt)
	}
	if !strings.Contains(stepByID(t, &wf, "plan").Prompt, "Do not refuse larger tasks solely because they are larger") {
		t.Fatalf("plan prompt should suggest small tasks without enforcing scope:\n%s", stepByID(t, &wf, "plan").Prompt)
	}
	locate := stepByID(t, &wf, "locate-task")
	assertAgentStep(t, locate, "", "planning-session", model.ModeHeadless)
	if locate.Capture != "task_file" {
		t.Fatalf("locate-task capture = %q, want task_file", locate.Capture)
	}
	if !strings.Contains(stepByID(t, &wf, "validate-plan").Command, `test -f "{{task_file}}"`) {
		t.Fatalf("validate-plan command = %q", stepByID(t, &wf, "validate-plan").Command)
	}
	assertAgentStep(t, stepByID(t, &wf, "tutor"), "", "tutor-session", model.ModeInteractive)
	if !strings.Contains(stepByID(t, &wf, "tutor").Prompt, "{{session_dir}}/bundled/onboarding/docs/") {
		t.Fatalf("tutor prompt missing docs reference:\n%s", stepByID(t, &wf, "tutor").Prompt)
	}
	impl := stepByID(t, &wf, "implement")
	assertAgentStep(t, impl, "", "impl-session", model.ModeHeadless)
	for _, want := range []string{"{{task_file}}", "codagent:implement-with-tdd", "git add", "including newly added files", "do not commit"} {
		if !strings.Contains(impl.Prompt, want) {
			t.Fatalf("implement prompt missing %q:\n%s", want, impl.Prompt)
		}
	}
	if !strings.Contains(stepByID(t, &wf, "summary").Body, "do not commit yet") {
		t.Fatalf("summary should tell the user not to commit yet:\n%s", stepByID(t, &wf, "summary").Body)
	}
}

func TestValidatorWorkflowShape(t *testing.T) {
	wf := readBuiltinWorkflowForTest(t, "builtin:onboarding/validator.yaml")

	wantSessions := map[string]string{
		"validator-setup-session": "planner",
		"tutor-session":           "planner",
		"impl-session":            "implementor",
	}
	assertSessions(t, wf.Sessions, wantSessions)

	wantIDs := []string{
		"intro-ui",
		"stash-guided-changes",
		"init",
		"setup",
		"restore-guided-changes",
		"explain-validation",
		"break-it",
		"prepare-fix-context",
		"run-validator",
		"review-validator-status",
		"summary-ui",
	}
	gotIDs := stepIDs(wf.Steps)
	if diff := cmp.Diff(wantIDs, gotIDs); diff != "" {
		t.Fatalf("validator workflow step IDs mismatch (-want +got):\n%s", diff)
	}

	assertUIStep(t, stepByID(t, &wf, "intro-ui"), "Agent Validator")
	stash := stepByID(t, &wf, "stash-guided-changes")
	if stash.StepType() != "shell" {
		t.Fatalf("stash-guided-changes type = %q, want shell", stash.StepType())
	}
	for _, want := range []string{"git status --porcelain", "git stash push -u", "agent-runner onboarding guided changes", "git status --short"} {
		if !strings.Contains(stash.Command, want) {
			t.Fatalf("stash-guided-changes command missing %q:\n%s", want, stash.Command)
		}
	}
	if stepByID(t, &wf, "init").Command != `"$AGENT_RUNNER_EXECUTABLE" internal validator-init` {
		t.Fatalf("init command = %q, want current executable validator init", stepByID(t, &wf, "init").Command)
	}
	setup := stepByID(t, &wf, "setup")
	assertAgentStep(t, setup, "", "validator-setup-session", model.ModeInteractive)
	if !strings.Contains(setup.Prompt, "agent-validator:validator-setup") {
		t.Fatalf("setup prompt missing validator setup skill:\n%s", setup.Prompt)
	}
	restore := stepByID(t, &wf, "restore-guided-changes")
	if restore.StepType() != "shell" {
		t.Fatalf("restore-guided-changes type = %q, want shell", restore.StepType())
	}
	for _, want := range []string{"git stash list", "git stash pop", "agent-runner onboarding guided changes", "git status --porcelain", "git status --short", "git diff --stat"} {
		if !strings.Contains(restore.Command, want) {
			t.Fatalf("restore-guided-changes command missing %q:\n%s", want, restore.Command)
		}
	}
	breakIt := stepByID(t, &wf, "break-it")
	assertAgentStep(t, breakIt, "", "tutor-session", model.ModeHeadless)
	for _, want := range []string{"same tutorial-agent context", "previous guided workflow", "git status --short", "git diff", "git show", "previously implemented code", ".validator/config.yml", "Do not commit"} {
		if !strings.Contains(breakIt.Prompt, want) {
			t.Fatalf("break-it prompt missing %q:\n%s", want, breakIt.Prompt)
		}
	}
	assertAgentStep(t, stepByID(t, &wf, "prepare-fix-context"), "", "impl-session", model.ModeHeadless)
	runValidator := stepByID(t, &wf, "run-validator")
	if runValidator.StepType() != "sub-workflow" || runValidator.Workflow != "../core/run-validator.yaml" {
		t.Fatalf("run-validator = type %q workflow %q, want sub-workflow ../core/run-validator.yaml", runValidator.StepType(), runValidator.Workflow)
	}
	review := stepByID(t, &wf, "review-validator-status")
	assertAgentStep(t, review, "", "tutor-session", model.ModeInteractive)
	for _, want := range []string{"agent-validator:validator-status", "intentional bug", "validator_logs/*.json", "review_*.json", "reviewer reported violations", "fixed, skipped, or absent", "Do not infer that reviews passed merely because JSON or log files exist", "additional issues", "agent-validator:validator-help", "{{session_dir}}/bundled/onboarding/docs/"} {
		if !strings.Contains(review.Prompt, want) {
			t.Fatalf("review-validator-status prompt missing %q:\n%s", want, review.Prompt)
		}
	}
	assertUIStep(t, stepByID(t, &wf, "summary-ui"), "feedback-loop")
}

func TestAdvancedWorkflowShape(t *testing.T) {
	wf := readBuiltinWorkflowForTest(t, "builtin:onboarding/advanced.yaml")

	wantIDs := []string{"concepts-ui", "help"}
	gotIDs := stepIDs(wf.Steps)
	if diff := cmp.Diff(wantIDs, gotIDs); diff != "" {
		t.Fatalf("advanced step IDs mismatch (-want +got):\n%s", diff)
	}

	concepts := stepByID(t, &wf, "concepts-ui")
	assertUIStep(t, concepts, "workflows")
	for _, want := range []string{"sessions", "new", "resume", "inherit", "loops", "validator loops", "sub-workflows"} {
		if !strings.Contains(strings.ToLower(concepts.Body), want) {
			t.Fatalf("concepts-ui body missing %q:\n%s", want, concepts.Body)
		}
	}

	help := stepByID(t, &wf, "help")
	if help.StepType() != "sub-workflow" {
		t.Fatalf("help type = %q, want sub-workflow", help.StepType())
	}
	if help.Workflow != "help.yaml" {
		t.Fatalf("help workflow = %q, want help.yaml", help.Workflow)
	}
}

func TestHelpWorkflowShape(t *testing.T) {
	wf := readBuiltinWorkflowForTest(t, "builtin:onboarding/help.yaml")

	if len(wf.Sessions) != 1 {
		t.Fatalf("sessions len = %d, want 1", len(wf.Sessions))
	}
	session := wf.Sessions[0]
	if session.Name != "help-session" || session.Agent != "planner" {
		t.Fatalf("session = %#v, want help-session/planner", session)
	}

	wantIDs := []string{"help-agent"}
	gotIDs := stepIDs(wf.Steps)
	if diff := cmp.Diff(wantIDs, gotIDs); diff != "" {
		t.Fatalf("help step IDs mismatch (-want +got):\n%s", diff)
	}

	helpAgent := stepByID(t, &wf, "help-agent")
	assertAgentStep(t, helpAgent, "", "help-session", model.ModeInteractive)
	for _, want := range []string{
		"{{session_dir}}/bundled/onboarding/docs/",
		"Agent Runner concepts",
		"workflows",
		"step types",
		"sessions",
		"validation",
	} {
		if !strings.Contains(helpAgent.Prompt, want) {
			t.Fatalf("help-agent prompt missing %q:\n%s", want, helpAgent.Prompt)
		}
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

func assertAgentStep(t *testing.T, step *model.Step, agent, session string, mode model.StepMode) {
	t.Helper()
	if step.StepType() != "agent" {
		t.Fatalf("%s type = %q, want agent", step.ID, step.StepType())
	}
	if agent != "" && step.Agent != agent {
		t.Fatalf("%s agent = %q, want %q", step.ID, step.Agent, agent)
	}
	if session != "" && step.Session != model.SessionStrategy(session) {
		t.Fatalf("%s session = %q, want %q", step.ID, step.Session, session)
	}
	if step.Mode != mode {
		t.Fatalf("%s mode = %q, want %q", step.ID, step.Mode, mode)
	}
}

func assertSessions(t *testing.T, got []model.SessionDecl, want map[string]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("sessions len = %d, want %d: %#v", len(got), len(want), got)
	}
	gotMap := make(map[string]string, len(got))
	for _, session := range got {
		gotMap[session.Name] = session.Agent
	}
	if diff := cmp.Diff(want, gotMap); diff != "" {
		t.Fatalf("sessions mismatch (-want +got):\n%s", diff)
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
