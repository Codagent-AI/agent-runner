package builtinworkflows

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/textfmt"
)

const verifyChangeRef = "builtin:core/verify-change-v1.0.yaml"

// The loop semantics of verify-change (round order, convergence break, the
// final-round break, validator skipping, push, and status writing) are
// exercised end to end in internal/exec/verify_change_workflow_test.go. The
// tests here cover the declarations and prompt contracts that execution with
// stubbed agents cannot see.

func findStep(steps []model.Step, id string) *model.Step {
	for i := range steps {
		if steps[i].ID == id {
			return &steps[i]
		}
		if found := findStep(steps[i].Steps, id); found != nil {
			return found
		}
	}
	return nil
}

func walkSteps(steps []model.Step, visit func(*model.Step)) {
	for i := range steps {
		visit(&steps[i])
		walkSteps(steps[i].Steps, visit)
	}
}

func requirePromptContains(t *testing.T, stepID, prompt string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(prompt, want) {
			t.Errorf("%s prompt missing %q", stepID, want)
		}
	}
}

func TestCoreVerifyChangeShape(t *testing.T) {
	workflow := readBuiltinWorkflowForTest(t, verifyChangeRef)
	if !workflow.Hidden {
		t.Error("verify-change must be hidden")
	}

	type param struct {
		Name     string
		Required bool
		Default  string
	}
	var params []param
	for i := range workflow.Params {
		p := &workflow.Params[i]
		params = append(params, param{Name: p.Name, Required: p.IsRequired(), Default: p.Default})
	}
	wantParams := []param{
		{Name: "change_name", Required: true},
		{Name: "change_dir", Required: true},
		{Name: "change_label", Required: true},
		{Name: "artifact_validation_instruction", Required: true},
		{Name: "skip_validator", Default: "false"},
		{Name: "acceptance_rounds", Default: "3"},
	}
	if diff := cmp.Diff(wantParams, params); diff != "" {
		t.Errorf("verify-change params mismatch (-want +got):\n%s", diff)
	}

	wantSteps := []string{
		"validate-change-name",
		"validate-skip-validator",
		"review-assumptions",
		"verify-assumptions-handoff",
		"simplify",
		"run-validator",
		"verify-validator-result",
		"verify-clean-for-pr",
		"open-draft-pr",
		"verify-draft-pr",
		"prepare-acceptance",
		"write-acceptance-status",
		"verify-acceptance-handoff",
	}
	if diff := cmp.Diff(wantSteps, stepIDs(workflow.Steps)); diff != "" {
		t.Errorf("verify-change steps mismatch (-want +got):\n%s", diff)
	}
}

func TestCoreVerifyChangeSimplifyPass(t *testing.T) {
	workflow := readBuiltinWorkflowForTest(t, verifyChangeRef)
	step := findStep(workflow.Steps, "simplify")
	if step == nil {
		t.Fatal("simplify step not found")
	}
	if step.Agent != "implementor" {
		t.Errorf("simplify agent = %q, want implementor", step.Agent)
	}
	if step.Session != "new" {
		t.Errorf("simplify session = %q, want new", step.Session)
	}
	if step.Mode != "autonomous" {
		t.Errorf("simplify mode = %q, want autonomous", step.Mode)
	}
	requirePromptContains(t, step.ID, step.Prompt,
		"do not use a /simplify skill or reviewer subagents",
		"{{session_dir}}/output/acceptance-assumptions.md",
		"No unresolved assumptions or context gaps.",
		"[{{step_id}}]",
		"Do not run Agent Validator or push",
	)

	pr := findStep(workflow.Steps, "open-draft-pr")
	if pr == nil {
		t.Fatal("open-draft-pr step not found")
	}
	requirePromptContains(t, pr.ID, pr.Prompt,
		"Do not list known defects as Known follow-up items",
		"{{session_dir}}/output/acceptance-assumptions.md",
	)
}

func TestLegacyImplementChangeSimplifyStopsForDecisions(t *testing.T) {
	for _, ref := range []string{
		"builtin:openspec/implement-change-v1.0.yaml",
		"builtin:spec-driven/implement-change-v1.0.yaml",
	} {
		t.Run(ref, func(t *testing.T) {
			workflow := readBuiltinWorkflowForTest(t, ref)
			simplify := findStep(workflow.Steps, "simplify")
			if simplify == nil {
				t.Fatal("simplify step not found")
			}
			requirePromptContains(t, simplify.ID, simplify.Prompt,
				"Fix clear-cut correctness or spec-conformance defects",
				"Do not defer defects to /code-review or a later review",
				"{{session_dir}}/output/acceptance-assumptions.md",
				"[{{step_id}}]",
			)
			gate := findStep(workflow.Steps, "require-simplify-decisions-resolved")
			if gate == nil {
				t.Fatal("unresolved simplify decisions have no gate before finalization")
			}
			if !strings.Contains(gate.Command, "{{session_dir}}/output/acceptance-assumptions.md") ||
				!strings.Contains(gate.Command, "exit 1") {
				t.Errorf("decision gate must stop the workflow on unresolved findings: %q", gate.Command)
			}
			if !strings.Contains(gate.Command, "After resolving each item, remove or empty {{session_dir}}/output/acceptance-assumptions.md, then resume the run.") {
				t.Errorf("decision gate must explain how to resume after resolving decisions: %q", gate.Command)
			}
			for _, tc := range []struct {
				name        string
				ledger      string
				grepFailure bool
				exitCode    int
			}{
				{name: "no findings placeholder", ledger: "No unresolved assumptions or context gaps.\n"},
				{name: "unresolved finding", ledger: "Decision needed: choose a storage format.\n", exitCode: 1},
				{name: "placeholder with finding", ledger: "No unresolved assumptions or context gaps.\nDecision needed: choose a storage format.\n", exitCode: 1},
				{name: "grep read failure", ledger: "No unresolved assumptions or context gaps.\n", grepFailure: true, exitCode: 2},
			} {
				t.Run(tc.name, func(t *testing.T) {
					// A space and a single quote in the session dir prove the gate
					// survives the runner's shell-safe interpolation.
					sessionDir := filepath.Join(t.TempDir(), "session dir's")
					outputDir := filepath.Join(sessionDir, "output")
					if err := os.MkdirAll(outputDir, 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(outputDir, "acceptance-assumptions.md"), []byte(tc.ledger), 0o644); err != nil {
						t.Fatal(err)
					}
					command, err := textfmt.InterpolateShellSafeTyped(gate.Command, nil, nil, map[string]string{"session_dir": sessionDir})
					if err != nil {
						t.Fatalf("interpolate decision gate: %v", err)
					}
					cmd := exec.Command("sh", "-c", command)
					if tc.grepFailure {
						binDir := filepath.Join(sessionDir, "bin")
						if err := os.Mkdir(binDir, 0o755); err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(filepath.Join(binDir, "grep"), []byte("#!/bin/sh\nexit 2\n"), 0o755); err != nil {
							t.Fatal(err)
						}
						cmd.Env = append(os.Environ(), "PATH="+binDir+":"+os.Getenv("PATH"))
					}
					output, err := cmd.CombinedOutput()
					actualExitCode := 0
					if err != nil {
						exitErr, ok := err.(*exec.ExitError)
						if !ok {
							t.Fatalf("decision gate execution failed: %v", err)
						}
						actualExitCode = exitErr.ExitCode()
					}
					if actualExitCode != tc.exitCode {
						t.Errorf("decision gate exit code = %d, want %d; output: %s", actualExitCode, tc.exitCode, output)
					}
				})
			}
			ids := stepIDs(workflow.Steps)
			if !strings.Contains(strings.Join(ids, ","), "simplify,require-simplify-decisions-resolved,run-validator") {
				t.Errorf("decision gate must run immediately after simplify: %v", ids)
			}
		})
	}
}

func TestCoreVerifyChangeValidatorResultStopsBeforePR(t *testing.T) {
	w := readBuiltinWorkflowForTest(t, verifyChangeRef)
	validator := findStep(w.Steps, "run-validator")
	if validator == nil || validator.Params["result_file"] != "{{session_dir}}/output/verify-change-validator-result.txt" {
		t.Fatalf("validator result file is not forwarded: %+v", validator)
	}
	gate := findStep(w.Steps, "verify-validator-result")
	if gate == nil || gate.Script != "validator-pr-gate.sh" || gate.SkipIf != "sh: test {{skip_validator}} = true" {
		t.Fatalf("validator PR gate = %+v", gate)
	}
}

// A red validator after an acceptance fix must not block the loop, but its
// result must be recorded where callers can find it.
func TestCoreVerifyChangeAcceptanceValidatorRecordsResultWithoutGating(t *testing.T) {
	const resultFile = "{{session_dir}}/output/acceptance-validator-result.txt"
	w := readBuiltinWorkflowForTest(t, verifyChangeRef)

	loop := findStep(w.Steps, "prepare-acceptance")
	if loop == nil {
		t.Fatal("prepare-acceptance step not found")
	}
	wantValidator := model.Step{
		ID:       "acceptance-validator",
		Workflow: "run-validator-v1.0.yaml",
		Params:   map[string]string{"result_file": resultFile},
		SkipIf:   "sh: test {{skip_validator}} = true",
	}
	validator := findStep(loop.Steps, "acceptance-validator")
	if validator == nil {
		t.Fatal("acceptance-validator step not found in prepare-acceptance")
	}
	if diff := cmp.Diff(wantValidator, *validator); diff != "" {
		t.Errorf("acceptance-validator mismatch (-want +got):\n%s", diff)
	}

	wantRound := []string{
		"reset-round-evidence",
		"acceptance-test",
		"acceptance-gate",
		"end-final-round",
		"acceptance-fix",
		"acceptance-validator",
		"acceptance-push",
		"verify-acceptance-pr",
	}
	if diff := cmp.Diff(wantRound, stepIDs(loop.Steps)); diff != "" {
		t.Errorf("prepare-acceptance steps mismatch (-want +got):\n%s", diff)
	}
	walkSteps(w.Steps, func(step *model.Step) {
		if step.ID == "acceptance-validator" {
			return
		}
		texts := []string{step.Command, step.Prompt, step.SkipIf, step.BreakIf}
		for _, value := range step.ScriptInputs {
			texts = append(texts, value)
		}
		for _, text := range texts {
			if strings.Contains(text, "acceptance-validator-result.txt") {
				t.Errorf("%s reads the acceptance validator result; it must not gate the workflow", step.ID)
			}
		}
	})
}

func TestValidatorPRGatePushesRedBranchAndReportsFailure(t *testing.T) {
	repo, head := gitRepo(t)
	remote := filepath.Join(t.TempDir(), "remote.git")
	runGit(t, repo, "init", "--bare", "-q", remote)
	runGit(t, repo, "remote", "add", "origin", remote)
	resultFile := filepath.Join(t.TempDir(), "result.txt")
	if err := os.WriteFile(resultFile, []byte("FAIL\ncheck: tests failed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	script := writeAssetScript(t, "core/validator-pr-gate.sh")
	output, err := runScriptIn(t, script, repo, `{"result_file":"`+resultFile+`"}`)
	if err == nil || !strings.Contains(output, "check: tests failed") {
		t.Fatalf("red gate = (%q, %v), want failed checks", output, err)
	}
	got := strings.TrimSpace(string(runGitOutput(t, repo, "--git-dir", remote, "rev-parse", "refs/heads/feature")))
	if got != head {
		t.Fatalf("pushed head = %s, want %s", got, head)
	}
}

func TestRunValidatorRecordsFinalResultForPRGate(t *testing.T) {
	for _, tc := range []struct {
		name, validator, want string
		wantFailure           bool
	}{
		{name: "passing", validator: "echo checks passed", want: "PASS\n"},
		{name: "failing", validator: "echo 'check: tests failed'; exit 1", want: "FAIL\ncheck: tests failed\n", wantFailure: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			resultFile := filepath.Join(dir, "result.txt")
			writeFakeBinary(t, dir, "runner", "#!/bin/sh\ncase \"$3\" in\n  task_file) echo '';;\n  result_file) echo \"$RESULT_FILE\";;\nesac\n")
			writeFakeBinary(t, dir, "validator", "#!/bin/sh\n"+tc.validator+"\n")
			output, err := runScriptIn(t, coreRunValidatorScript(t), dir, "", "AGENT_RUNNER_EXECUTABLE="+filepath.Join(dir, "runner"), "AGENT_RUNNER_VALIDATOR_EXECUTABLE="+filepath.Join(dir, "validator"), "RESULT_FILE="+resultFile)
			if (err != nil) != tc.wantFailure {
				t.Fatalf("validator result = (%q, %v), want failure %t", output, err, tc.wantFailure)
			}
			got, readErr := os.ReadFile(resultFile)
			if readErr != nil || string(got) != tc.want {
				t.Fatalf("result file = (%q, %v), want %q", got, readErr, tc.want)
			}
		})
	}
}

func TestRunValidatorReadsTaskAndResultFileFromSameInput(t *testing.T) {
	dir := t.TempDir()
	resultFile := filepath.Join(dir, "result.txt")
	writeFakeBinary(t, dir, "runner", "#!/bin/sh\njq -r --arg key \"$3\" '.[$key] // \"\"'\n")
	writeFakeBinary(t, dir, "validator", "#!/bin/sh\nprintf '%s\\n' \"$@\" > validator-args\n")
	input := `{"task_file":"task.md","result_file":"` + resultFile + `"}`
	output, err := runScriptIn(t, coreRunValidatorScript(t), dir, input, "AGENT_RUNNER_EXECUTABLE="+filepath.Join(dir, "runner"), "AGENT_RUNNER_VALIDATOR_EXECUTABLE="+filepath.Join(dir, "validator"))
	if err != nil {
		t.Fatalf("run-validator = (%q, %v)", output, err)
	}
	if _, err := os.Stat(resultFile); err != nil {
		t.Fatalf("result file missing: %v", err)
	}
	args, err := os.ReadFile(filepath.Join(dir, "validator-args"))
	if err != nil || !strings.Contains(string(args), "task.md") {
		t.Fatalf("validator args = (%q, %v)", args, err)
	}
}

func TestCoreImplementChangeComposesVerifyChangeWithSharedSessions(t *testing.T) {
	implement := readBuiltinWorkflowForTest(t, "builtin:core/implement-change-v1.0.yaml")
	verify := readBuiltinWorkflowForTest(t, verifyChangeRef)

	wantSessions := []model.SessionDecl{{Name: "lead-agent", Agent: "lead"}, {Name: "acceptance-tester", Agent: "tester"}}
	if diff := cmp.Diff(wantSessions, verify.Sessions); diff != "" {
		t.Errorf("verify-change sessions mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(verify.Sessions, implement.Sessions); diff != "" {
		t.Errorf("implement-change and verify-change must declare identical sessions (-verify +implement):\n%s", diff)
	}

	wantHead := []string{
		"validate-change-name",
		"validate-skip-validator",
		"check-plan",
		"implement-tasks",
		"complete-task-index",
		"verify-task-index",
		"verify-change",
	}
	if diff := cmp.Diff(wantHead, stepIDs(implement.Steps)); diff != "" {
		t.Fatalf("implement-change steps mismatch (-want +got):\n%s", diff)
	}
	call := implement.Steps[len(implement.Steps)-1]
	if call.Workflow != "verify-change-v1.0.yaml" {
		t.Fatalf("verify-change workflow = %q", call.Workflow)
	}
	wantParams := map[string]string{
		"change_name":                     "{{change_name}}",
		"change_dir":                      "{{change_dir}}",
		"change_label":                    "{{change_label}}",
		"artifact_validation_instruction": "{{artifact_validation_instruction}}",
		"skip_validator":                  "{{skip_validator}}",
	}
	if diff := cmp.Diff(wantParams, call.Params); diff != "" {
		t.Errorf("verify-change params mismatch (-want +got):\n%s", diff)
	}
}

func TestCoreVerifyChangeUsesNoAgentCallsOrValidatorSkills(t *testing.T) {
	workflow := readBuiltinWorkflowForTest(t, verifyChangeRef)
	walkSteps(workflow.Steps, func(step *model.Step) {
		if len(step.Tools) != 0 {
			t.Errorf("step %s declares tools %v; a second agent must be a workflow step", step.ID, step.Tools)
		}
		prompts := []string{step.Prompt}
		if step.Repair != nil {
			prompts = append(prompts, step.Repair.Prompt)
		}
		for _, prompt := range prompts {
			for _, forbidden := range []string{"call_agent", "call-agent", "validator-run", "push-pr"} {
				if strings.Contains(prompt, forbidden) {
					t.Errorf("step %s prompt mentions %q", step.ID, forbidden)
				}
			}
		}
	})
}

func TestCoreVerifyChangeAcceptanceRoundsAreWorkflowSteps(t *testing.T) {
	workflow := readBuiltinWorkflowForTest(t, verifyChangeRef)

	reset := findStep(workflow.Steps, "reset-round-evidence")
	if reset == nil {
		t.Fatal("reset-round-evidence step not found")
	}
	for _, file := range []string{"acceptance-round-status.txt", "acceptance-handoff.md"} {
		if !strings.Contains(reset.Command, "{{session_dir}}/output/"+file) {
			t.Errorf("reset-round-evidence does not clear %s: %q", file, reset.Command)
		}
	}
	for _, baseline := range []string{"exploration-log.md", "acceptance-tested-revision.txt", "acceptance-findings.md"} {
		if strings.Contains(reset.Command, baseline) {
			t.Errorf("reset-round-evidence must keep the tester baseline %s: %q", baseline, reset.Command)
		}
	}

	tester := findStep(workflow.Steps, "acceptance-test")
	if tester == nil || tester.Session != "acceptance-tester" || tester.Mode != model.ModeAutonomous {
		t.Fatalf("acceptance-test = %+v, want an autonomous acceptance-tester step", tester)
	}
	requirePromptContains(t, tester.ID, tester.Prompt,
		"codagent:prepare-acceptance",
		"approved artifacts: `{{change_dir}}/`",
		"`{{change_dir}}/test-plan.md`",
		"`{{session_dir}}/output/*session-report*.out` and `{{session_dir}}/audit.log`",
		"evidence directory: `{{session_dir}}/output/`",
		"`{{session_dir}}/output/acceptance-assumptions.md`",
		"`{{session_dir}}/output/acceptance-tested-revision.txt`",
		"Acceptance is exploratory",
		"nobody hands you a list of cases to run",
		"never to decide what correct means",
		"prior exploration log as history rather than as coverage",
		"every user-visible requirement or scenario that the approved specs add or modify at least once",
		"as a limitation with its reason",
		"the coverage floor does not apply again",
		"`{{session_dir}}/output/acceptance-round-status.txt`",
		"`READY <sha>`",
		"`NOT_READY`",
	)

	fix := findStep(workflow.Steps, "acceptance-fix")
	if fix == nil || fix.Session != "lead-agent" || fix.Mode != model.ModeAutonomous {
		t.Fatalf("acceptance-fix = %+v, want an autonomous lead-agent step", fix)
	}
	requirePromptContains(t, fix.ID, fix.Prompt,
		"When fixing an implementation defect found by acceptance testing, add unit regression coverage",
		"{{artifact_validation_instruction}}",
		"`[{{step_id}}]`",
		"Do not run Agent Validator",
		"do not push",
		"Do not name a scope for the next round",
		"may not be dismissed as \"not a defect\"",
		"a decision needed from the user, with your reasoning",
		"Do not audit the tester's evidence files for completeness",
	)
	for _, retired := range []string{"acceptance-impact-scope.md", "acceptance-flow-evidence.md", "`targeted`", "`evidence-only`", "AT-", "implement-with-tdd"} {
		if strings.Contains(fix.Prompt, retired) || strings.Contains(tester.Prompt, retired) {
			t.Errorf("acceptance prompts mention retired machinery %q", retired)
		}
	}

	for _, id := range []string{"acceptance-push", "verify-acceptance-pr"} {
		step := findStep(workflow.Steps, id)
		if step == nil || step.Repair == nil || step.Repair.Session != "lead-agent" {
			t.Fatalf("%s = %+v, want a lead-agent repair", id, step)
		}
	}
}

func TestCoreVerifyChangeAcceptanceScratchDirectory(t *testing.T) {
	workflow := readBuiltinWorkflowForTest(t, verifyChangeRef)
	reset := findStep(workflow.Steps, "reset-round-evidence")
	if reset == nil || !strings.Contains(reset.Command, `mkdir -p "{{session_dir}}/scratch/acceptance-test"`) {
		t.Fatalf("reset-round-evidence must create acceptance scratch directory: %+v", reset)
	}
	tester := findStep(workflow.Steps, "acceptance-test")
	if tester == nil {
		t.Fatal("acceptance-test step not found")
	}
	requirePromptContains(t, tester.ID, tester.Prompt,
		"scratch_dir: `{{session_dir}}/scratch/acceptance-test`",
		"All temporary files, clones, build outputs, and servers' working directories must go under `scratch_dir`",
		"Never use `/tmp`, `/private/tmp`, or `$TMPDIR`",
	)
}

func TestCoreVerifyChangeOpenDraftPRPushesDirectly(t *testing.T) {
	workflow := readBuiltinWorkflowForTest(t, verifyChangeRef)
	step := findStep(workflow.Steps, "open-draft-pr")
	if step == nil {
		t.Fatal("open-draft-pr step not found")
	}
	requirePromptContains(t, step.ID, step.Prompt,
		"directly with `git` and `gh`",
		"gh pr create --draft",
		"gh api --method PATCH",
		"only the first ordered pair is replaced; later pairs are left unchanged",
		"prepend",
		"{{session_dir}}/bundled/core/update-pr-body.sh",
		"--body-file",
		"<!-- agent-runner:generated-body:start -->",
		"<!-- agent-runner:generated-body:end -->",
		"task-delivery/*.accepted",
		"gh pr ready --undo",
		"preserve its base branch",
		"its head SHA equals local `HEAD`",
		"REPAIR_BLOCKED",
	)
	if strings.Contains(step.Prompt, "gh pr edit <number>") {
		t.Error("open-draft-pr must not invoke gh pr edit")
	}

	verify := findStep(workflow.Steps, "verify-draft-pr")
	if verify == nil || verify.Script != "check-draft-pr.sh" || verify.ScriptInputs["attempts"] != "3" || verify.Capture != "pr_url" {
		t.Fatalf("verify-draft-pr = %+v, want check-draft-pr.sh with retries capturing pr_url", verify)
	}
}

func TestCoreUpdatePRBodyScript(t *testing.T) {
	const start = "<!-- agent-runner:generated-body:start -->"
	const end = "<!-- agent-runner:generated-body:end -->"
	const block = start + "\nFresh delivery\n" + end + "\n"
	for _, tc := range []struct {
		name, oldBody, wantBody, failStep string
	}{
		{"marked body", "Intro\n" + start + "\nStale delivery\n" + end + "\nRefs #174\n<!-- agent-factory:claim:abc -->\n", "Intro\n" + block + "Refs #174\n<!-- agent-factory:claim:abc -->\n", ""},
		{"unicode around marked body", "🤖 Intro — note\n" + start + "\nStale delivery\n" + end + "\nRefs #174\n<!-- agent-factory:claim:abc -->\n", "🤖 Intro — note\n" + block + "Refs #174\n<!-- agent-factory:claim:abc -->\n", ""},
		{"unmarked body", "Refs #174\n<!-- agent-factory:claim:abc -->\n", block + "\nRefs #174\n<!-- agent-factory:claim:abc -->\n", ""},
		{"start marker only", start + "\nRefs #174\n", block + "\n" + start + "\nRefs #174\n", ""},
		{"end marker only", end + "\nRefs #174\n", block + "\n" + end + "\nRefs #174\n", ""},
		{"reversed markers", end + "\nOld\n" + start + "\n", block + "\n" + end + "\nOld\n" + start + "\n", ""},
		{"duplicated markers", start + "\nA\n" + end + "\nmid\n" + start + "\nB\n" + end + "\ntail\n", block + "mid\n" + start + "\nB\n" + end + "\ntail\n", ""},
		{"empty body", "", block, ""},
		{"view fails", "", "", "view"},
		{"patch fails", "", "", "api"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			script := writeAssetScript(t, "core/update-pr-body.sh")
			dir := t.TempDir()
			binDir := filepath.Join(dir, "bin")
			if err := os.Mkdir(binDir, 0o700); err != nil {
				t.Fatal(err)
			}
			oldFile := filepath.Join(dir, "old")
			blockFile := filepath.Join(dir, "block")
			argsFile := filepath.Join(dir, "args")
			bodyFile := filepath.Join(dir, "patched")
			mustWriteFile(t, oldFile, tc.oldBody)
			mustWriteFile(t, blockFile, block)
			writeFakeBinary(t, binDir, "gh", `#!/bin/sh
printf '%s\n' "$*" >> "$FAKE_GH_ARGS"
case "$1 $2" in
  'pr view')
    [ "${FAKE_GH_FAIL:-}" != view ] || exit 34
    [ "$3" = 174 ] && [ "$4" = --json ] && [ "$5" = body ] && [ "$6" = -q ] && [ "$7" = .body ] || exit 36
    cat "$FAKE_GH_OLD"
    printf '\n' ;;
  'api --method')
    [ "${FAKE_GH_FAIL:-}" != api ] || exit 35
    [ "$3" = PATCH ] && [ "$4" = 'repos/{owner}/{repo}/pulls/174' ] && [ "$5" = -F ] || exit 31
    case "$6" in body=@*) cat "${6#body=@}" > "$FAKE_GH_PATCHED" ;; *) exit 32 ;; esac
    printf '{"body":"large response"}\n' ;;
  *) exit 33 ;;
esac
`)
			run := func() ([]byte, error) {
				cmd := exec.Command("sh", script, "174", blockFile)
				cmd.Dir = dir
				cmd.Env = append(os.Environ(), "PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"), "FAKE_GH_OLD="+oldFile, "FAKE_GH_ARGS="+argsFile, "FAKE_GH_PATCHED="+bodyFile, "FAKE_GH_FAIL="+tc.failStep)
				return cmd.CombinedOutput()
			}
			out, err := run()
			if tc.failStep != "" {
				if err == nil || !strings.Contains(string(out), "could not") {
					t.Fatalf("gh failure = (%v, %q), want clear error", err, out)
				}
				return
			}
			if err != nil {
				t.Fatalf("update failed: %v\n%s", err, out)
			}
			if diff := cmp.Diff("updated pull request body for #174\n", string(out)); diff != "" {
				t.Errorf("script output (-want +got):\n%s", diff)
			}
			firstBody := readFile(t, bodyFile)
			if diff := cmp.Diff(tc.wantBody, firstBody); diff != "" {
				t.Errorf("body (-want +got):\n%s", diff)
			}
			calls := readFile(t, argsFile)
			if !strings.Contains(calls, "api --method PATCH repos/{owner}/{repo}/pulls/174 -F body=@") || strings.Contains(calls, "pr edit") {
				t.Errorf("gh calls = %q", calls)
			}
			mustWriteFile(t, oldFile, firstBody)
			if err := os.Remove(bodyFile); err != nil {
				t.Fatal(err)
			}
			out, err = run()
			if err != nil {
				t.Fatalf("rerun failed: %v\n%s", err, out)
			}
			if diff := cmp.Diff(firstBody, readFile(t, bodyFile)); diff != "" {
				t.Errorf("rerun body (-first +second):\n%s", diff)
			}
		})
	}
}

// gitRepo creates a repository with one commit and returns its directory and
// HEAD SHA.
func gitRepo(t *testing.T) (dir, head string) {
	t.Helper()
	dir = t.TempDir()
	runGit(t, dir, "init", "-q", "-b", "feature")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")
	runGit(t, dir, "config", "commit.gpgsign", "false")
	mustWriteFile(t, filepath.Join(dir, "file.txt"), "one\n")
	runGit(t, dir, "add", "file.txt")
	runGit(t, dir, "commit", "-q", "-m", "initial")
	return dir, gitHead(t, dir)
}

func gitHead(t *testing.T, dir string) string {
	t.Helper()
	return strings.TrimSpace(string(runGitOutput(t, dir, "rev-parse", "HEAD")))
}

func writeAssetScript(t *testing.T, asset string) string {
	t.Helper()
	script, err := ReadAsset(asset)
	if err != nil {
		t.Fatalf("ReadAsset(%s): %v", asset, err)
	}
	path := filepath.Join(t.TempDir(), filepath.Base(asset))
	if err := os.WriteFile(path, script, 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	return path
}

func runScriptIn(t *testing.T, scriptPath, dir, stdin string, env ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("sh", scriptPath)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestCoreAcceptanceGateScript(t *testing.T) {
	scriptPath := writeAssetScript(t, "core/acceptance-gate.sh")
	run := func(t *testing.T, repo, evidenceDir, action string) (string, error) {
		t.Helper()
		input := `{"evidence_dir":` + strconv.Quote(evidenceDir) + `,"action":` + strconv.Quote(action) + `,"rounds":"3"}`
		return runScriptIn(t, scriptPath, repo, input)
	}
	converged := func(t *testing.T) (repo, evidence string) {
		t.Helper()
		repo, head := gitRepo(t)
		evidence = t.TempDir()
		mustWriteFile(t, filepath.Join(evidence, "acceptance-round-status.txt"), "READY "+head+"\n")
		mustWriteFile(t, filepath.Join(evidence, "acceptance-tested-revision.txt"), head+"\n")
		mustWriteFile(t, filepath.Join(evidence, "exploration-log.md"), "explored\n")
		mustWriteFile(t, filepath.Join(evidence, "acceptance-handoff.md"), "tester handoff\n")
		return repo, evidence
	}

	t.Run("converges when the tester reports the current head ready", func(t *testing.T) {
		repo, evidence := converged(t)
		writeFileIn(t, repo, "build-output.log", "untracked files do not block\n")
		if out, err := run(t, repo, evidence, "check"); err != nil {
			t.Fatalf("gate failed: %v\n%s", err, out)
		}
	})

	failing := []struct {
		name   string
		mutate func(t *testing.T, repo, evidence string)
		want   string
	}{
		{
			name: "tester reported not ready",
			mutate: func(t *testing.T, _, evidence string) {
				mustWriteFile(t, filepath.Join(evidence, "acceptance-round-status.txt"), "NOT_READY\n")
			},
			want: "round status is 'NOT_READY'",
		},
		{
			name: "tester recorded no status",
			mutate: func(t *testing.T, _, evidence string) {
				removeFile(t, filepath.Join(evidence, "acceptance-round-status.txt"))
			},
			want: "recorded no round status",
		},
		{
			name: "head moved after testing",
			mutate: func(t *testing.T, repo, _ string) {
				writeFileIn(t, repo, "file.txt", "two\n")
				runGit(t, repo, "commit", "-q", "-am", "fix")
			},
			want: "not 'READY",
		},
		{
			name: "exploration log missing",
			mutate: func(t *testing.T, _, evidence string) {
				removeFile(t, filepath.Join(evidence, "exploration-log.md"))
			},
			want: "exploration-log.md is missing or empty",
		},
		{
			name: "tester did not record the tested revision",
			mutate: func(t *testing.T, _, evidence string) {
				removeFile(t, filepath.Join(evidence, "acceptance-tested-revision.txt"))
			},
			want: "acceptance-tested-revision.txt names ''",
		},
		{
			name: "tested revision is an earlier pass",
			mutate: func(t *testing.T, _, evidence string) {
				mustWriteFile(t, filepath.Join(evidence, "acceptance-tested-revision.txt"), "0123456789012345678901234567890123456789\n")
			},
			want: "acceptance-tested-revision.txt names '0123456789012345678901234567890123456789'",
		},
		{
			name: "handoff missing",
			mutate: func(t *testing.T, _, evidence string) {
				removeFile(t, filepath.Join(evidence, "acceptance-handoff.md"))
			},
			want: "acceptance-handoff.md is missing or empty",
		},
		{
			name: "tracked changes are uncommitted",
			mutate: func(t *testing.T, repo, _ string) {
				writeFileIn(t, repo, "file.txt", "dirty\n")
			},
			want: "tracked files have uncommitted changes",
		},
	}
	for _, tt := range failing {
		t.Run("does not converge when "+tt.name, func(t *testing.T) {
			repo, evidence := converged(t)
			tt.mutate(t, repo, evidence)
			out, err := run(t, repo, evidence, "check")
			if err == nil {
				t.Fatalf("gate succeeded unexpectedly:\n%s", out)
			}
			if !strings.Contains(out, tt.want) {
				t.Fatalf("output = %q, want %q", out, tt.want)
			}
		})
	}

	t.Run("finalize records completion and keeps the tester handoff", func(t *testing.T) {
		repo, evidence := converged(t)
		if out, err := run(t, repo, evidence, "finalize"); err != nil {
			t.Fatalf("finalize failed: %v\n%s", err, out)
		}
		assertFileContent(t, filepath.Join(evidence, "acceptance-preparation-status.txt"), "ACCEPTANCE_COMPLETE\n")
		assertFileContent(t, filepath.Join(evidence, "acceptance-handoff.md"), "tester handoff\n")
	})

	t.Run("finalize records failure with a short handoff", func(t *testing.T) {
		repo, evidence := converged(t)
		mustWriteFile(t, filepath.Join(evidence, "acceptance-round-status.txt"), "NOT_READY\n")
		if out, err := run(t, repo, evidence, "finalize"); err != nil {
			t.Fatalf("finalize failed: %v\n%s", err, out)
		}
		assertFileContent(t, filepath.Join(evidence, "acceptance-preparation-status.txt"), "ACCEPTANCE_FAILED\n")
		assertFileContent(t, filepath.Join(evidence, "acceptance-handoff-tester.md"), "tester handoff\n")
		handoff := readFile(t, filepath.Join(evidence, "acceptance-handoff.md"))
		for _, want := range []string{
			"did not converge within 3 rounds",
			gitHead(t, repo),
			"round status is 'NOT_READY'",
			filepath.Join(evidence, "acceptance-findings.md"),
			filepath.Join(evidence, "acceptance-assumptions.md"),
			filepath.Join(evidence, "acceptance-handoff-tester.md"),
		} {
			if !strings.Contains(handoff, want) {
				t.Errorf("failure handoff missing %q:\n%s", want, handoff)
			}
		}
	})

	t.Run("finalize reruns keep the tester handoff", func(t *testing.T) {
		repo, evidence := converged(t)
		mustWriteFile(t, filepath.Join(evidence, "acceptance-round-status.txt"), "NOT_READY\n")
		for attempt := 1; attempt <= 2; attempt++ {
			if out, err := run(t, repo, evidence, "finalize"); err != nil {
				t.Fatalf("finalize %d failed: %v\n%s", attempt, err, out)
			}
			assertFileContent(t, filepath.Join(evidence, "acceptance-preparation-status.txt"), "ACCEPTANCE_FAILED\n")
			assertFileContent(t, filepath.Join(evidence, "acceptance-handoff-tester.md"), "tester handoff\n")
		}
		if handoff := readFile(t, filepath.Join(evidence, "acceptance-handoff.md")); !strings.Contains(handoff, "did not converge within 3 rounds") {
			t.Fatalf("rerun handoff = %q, want the generated failure handoff", handoff)
		}
	})

	t.Run("finalize writes a handoff when the tester wrote nothing", func(t *testing.T) {
		repo, _ := gitRepo(t)
		evidence := filepath.Join(t.TempDir(), "output")
		if out, err := run(t, repo, evidence, "finalize"); err != nil {
			t.Fatalf("finalize failed: %v\n%s", err, out)
		}
		assertFileContent(t, filepath.Join(evidence, "acceptance-preparation-status.txt"), "ACCEPTANCE_FAILED\n")
		if handoff := readFile(t, filepath.Join(evidence, "acceptance-handoff.md")); strings.Contains(handoff, "acceptance-handoff-tester.md") {
			t.Errorf("handoff points at a tester handoff that does not exist:\n%s", handoff)
		}
	})
}

func writeFileIn(t *testing.T, dir, name, content string) {
	t.Helper()
	mustWriteFile(t, filepath.Join(dir, name), content)
}

func removeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	if got := readFile(t, path); got != want {
		t.Fatalf("%s = %q, want %q", filepath.Base(path), got, want)
	}
}

// fakeGH installs a gh that prints $FAKE_GH_FIRST_JSON on its first call when
// set, and $FAKE_GH_JSON on every other call.
func fakeGH(t *testing.T) (binDir, countFile string) {
	t.Helper()
	binDir = t.TempDir()
	countFile = filepath.Join(t.TempDir(), "gh-calls")
	script := `#!/bin/sh
n=$(cat "$FAKE_GH_COUNT" 2>/dev/null || echo 0)
n=$((n + 1))
echo "$n" >"$FAKE_GH_COUNT"
if [ "$n" -eq 1 ] && [ -n "${FAKE_GH_FIRST_JSON:-}" ]; then
  printf '%s' "$FAKE_GH_FIRST_JSON"
else
  printf '%s' "$FAKE_GH_JSON"
fi
`
	mustWriteFile(t, filepath.Join(binDir, "gh"), script)
	if err := os.Chmod(filepath.Join(binDir, "gh"), 0o700); err != nil {
		t.Fatal(err)
	}
	return binDir, countFile
}

func prListJSON(prs ...string) string {
	return "[" + strings.Join(prs, ",") + "]"
}

func prJSON(head string, draft bool) string {
	return `{"number":1,"url":"https://example.test/pr/1","state":"OPEN","isDraft":` + strconv.FormatBool(draft) +
		`,"baseRefName":"main","headRefOid":"` + head + `"}`
}

func TestCoreCheckDraftPRScript(t *testing.T) {
	scriptPath := writeAssetScript(t, "core/check-draft-pr.sh")
	run := func(t *testing.T, input, first, rest string) (out, calls string, err error) {
		t.Helper()
		repo, _ := gitRepo(t)
		binDir, countFile := fakeGH(t)
		first = strings.ReplaceAll(first, "HEAD", gitHead(t, repo))
		rest = strings.ReplaceAll(rest, "HEAD", gitHead(t, repo))
		out, err = runScriptIn(t, scriptPath, repo, input,
			"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
			"FAKE_GH_COUNT="+countFile,
			"FAKE_GH_FIRST_JSON="+first,
			"FAKE_GH_JSON="+rest,
		)
		return out, strings.TrimSpace(readFile(t, countFile)), err
	}
	stale := prListJSON(prJSON("0123456789abcdef0123456789abcdef01234567", true))
	ready := prListJSON(prJSON("HEAD", true))

	t.Run("prints only the URL when the draft pull request is at local HEAD", func(t *testing.T) {
		out, _, err := run(t, "", "", ready)
		if err != nil {
			t.Fatalf("check failed: %v\n%s", err, out)
		}
		if out != "https://example.test/pr/1\n" {
			t.Fatalf("output = %q, want only the pull request URL", out)
		}
	})

	t.Run("retries until the pushed head is reported", func(t *testing.T) {
		out, calls, err := run(t, `{"attempts":"3","interval_seconds":"0"}`, stale, ready)
		if err != nil {
			t.Fatalf("check failed: %v\n%s", err, out)
		}
		if calls != "2" {
			t.Fatalf("gh calls = %s, want 2", calls)
		}
	})

	t.Run("checks once by default", func(t *testing.T) {
		out, calls, err := run(t, "", stale, ready)
		if err == nil {
			t.Fatalf("check succeeded unexpectedly:\n%s", out)
		}
		if calls != "1" {
			t.Fatalf("gh calls = %s, want 1", calls)
		}
	})

	failing := []struct {
		name string
		json string
		want string
	}{
		{name: "head mismatch", json: stale, want: "does not match required draft state or local HEAD"},
		{name: "not a draft", json: prListJSON(prJSON("HEAD", false)), want: "draft false"},
		{name: "no pull request", json: prListJSON(), want: "found 0"},
		{name: "two pull requests", json: prListJSON(prJSON("HEAD", true), prJSON("HEAD", true)), want: "found 2"},
	}
	for _, tt := range failing {
		t.Run("fails on "+tt.name+" after all attempts", func(t *testing.T) {
			out, calls, err := run(t, `{"attempts":"2","interval_seconds":"0"}`, "", tt.json)
			if err == nil {
				t.Fatalf("check succeeded unexpectedly:\n%s", out)
			}
			if !strings.Contains(out, tt.want) || strings.Contains(out, "https://") {
				t.Fatalf("output = %q, want %q and no URL", out, tt.want)
			}
			if calls != "2" {
				t.Fatalf("gh calls = %s, want 2", calls)
			}
		})
	}
}

func TestCoreCheckDraftPRScriptRejectsDetachedHead(t *testing.T) {
	scriptPath := writeAssetScript(t, "core/check-draft-pr.sh")
	repo, head := gitRepo(t)
	runGit(t, repo, "checkout", "-q", "--detach", head)
	binDir, countFile := fakeGH(t)
	out, err := runScriptIn(t, scriptPath, repo, "",
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"FAKE_GH_COUNT="+countFile,
		"FAKE_GH_JSON="+prListJSON(prJSON(head, true)),
	)
	if err == nil || !strings.Contains(out, "HEAD is detached") {
		t.Fatalf("check = (%q, %v), want detached-HEAD failure", out, err)
	}
	if _, statErr := os.Stat(countFile); !os.IsNotExist(statErr) {
		t.Fatalf("fake gh was called on a detached HEAD (count file stat err = %v)", statErr)
	}
}

func TestCoreAcceptancePushScript(t *testing.T) {
	scriptPath := writeAssetScript(t, "core/acceptance-push.sh")
	setup := func(t *testing.T) string {
		t.Helper()
		repo, _ := gitRepo(t)
		remote := t.TempDir()
		runGit(t, remote, "init", "-q", "--bare")
		runGit(t, repo, "remote", "add", "origin", remote)
		runGit(t, repo, "push", "-q", "--set-upstream", "origin", "feature")
		return repo
	}
	commitFix := func(t *testing.T, repo string) string {
		t.Helper()
		writeFileIn(t, repo, "file.txt", "fixed\n")
		runGit(t, repo, "commit", "-q", "-am", "fix")
		return gitHead(t, repo)
	}

	t.Run("pushes the fix to the configured upstream", func(t *testing.T) {
		repo := setup(t)
		head := commitFix(t, repo)
		if out, err := runScriptIn(t, scriptPath, repo, ""); err != nil {
			t.Fatalf("push failed: %v\n%s", err, out)
		}
		if remoteHead := strings.TrimSpace(string(runGitOutput(t, repo, "rev-parse", "origin/feature"))); remoteHead != head {
			t.Fatalf("remote head = %s, want pushed %s", remoteHead, head)
		}
	})

	t.Run("sets the upstream when the branch has none", func(t *testing.T) {
		repo := setup(t)
		runGit(t, repo, "branch", "--unset-upstream")
		commitFix(t, repo)
		if out, err := runScriptIn(t, scriptPath, repo, ""); err != nil {
			t.Fatalf("push failed: %v\n%s", err, out)
		}
		if upstream := strings.TrimSpace(string(runGitOutput(t, repo, "rev-parse", "--abbrev-ref", "feature@{upstream}"))); upstream != "origin/feature" {
			t.Fatalf("upstream = %q, want origin/feature", upstream)
		}
	})

	t.Run("refuses to push uncommitted tracked changes", func(t *testing.T) {
		repo := setup(t)
		writeFileIn(t, repo, "file.txt", "dirty\n")
		out, err := runScriptIn(t, scriptPath, repo, "")
		if err == nil || !strings.Contains(out, "tracked changes are uncommitted") {
			t.Fatalf("push = (%q, %v), want uncommitted-changes failure", out, err)
		}
	})
}
