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
	for _, file := range []string{"acceptance-round-status.txt", "acceptance-test.md", "acceptance-handoff.md"} {
		if !strings.Contains(reset.Command, "{{session_dir}}/output/"+file) {
			t.Errorf("reset-round-evidence does not clear %s: %q", file, reset.Command)
		}
	}
	for _, baseline := range []string{"acceptance-flow-evidence.md", "acceptance-findings.md"} {
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
		"`full` in round index 0",
		"acceptance-impact-scope.md",
		"acceptance-flow-evidence.md",
		"`evidence-only`",
		"test plan was structurally validated before implementation",
		"`{{session_dir}}/output/acceptance-round-status.txt`",
		"`READY <sha>`",
		"`NOT_READY`",
	)

	fix := findStep(workflow.Steps, "acceptance-fix")
	if fix == nil || fix.Session != "lead-agent" || fix.Mode != model.ModeAutonomous {
		t.Fatalf("acceptance-fix = %+v, want an autonomous lead-agent step", fix)
	}
	requirePromptContains(t, fix.ID, fix.Prompt,
		"codagent:implement-with-tdd",
		"{{artifact_validation_instruction}}",
		"`[{{step_id}}]`",
		"Do not run Agent Validator",
		"do not push",
		"`{{session_dir}}/output/acceptance-impact-scope.md`",
		"`targeted`",
		"`evidence-only`",
	)

	for _, id := range []string{"acceptance-push", "verify-acceptance-pr"} {
		step := findStep(workflow.Steps, id)
		if step == nil || step.Repair == nil || step.Repair.Session != "lead-agent" {
			t.Fatalf("%s = %+v, want a lead-agent repair", id, step)
		}
	}
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
		"gh pr ready --undo",
		"preserve its base branch",
		"its head SHA equals local `HEAD`",
		"REPAIR_BLOCKED",
	)
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
		mustWriteFile(t, filepath.Join(evidence, "acceptance-test.md"), "tested\n")
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
			name: "acceptance test evidence missing",
			mutate: func(t *testing.T, _, evidence string) {
				removeFile(t, filepath.Join(evidence, "acceptance-test.md"))
			},
			want: "acceptance-test.md is missing or empty",
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
