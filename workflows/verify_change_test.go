package builtinworkflows

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"gopkg.in/yaml.v3"
)

type verifyChangeRepair struct {
	Prompt  string `yaml:"prompt"`
	Session string `yaml:"session"`
	Rerun   string `yaml:"rerun"`
}

type verifyChangeLoop struct {
	MaxParam string `yaml:"max_param"`
	AsIndex  string `yaml:"as_index"`
}

type verifyChangeStep struct {
	ID                string              `yaml:"id"`
	Prompt            string              `yaml:"prompt"`
	Command           string              `yaml:"command"`
	Script            string              `yaml:"script"`
	ScriptInputs      map[string]string   `yaml:"script_inputs"`
	Session           string              `yaml:"session"`
	Mode              string              `yaml:"mode"`
	Tools             []string            `yaml:"tools"`
	Workflow          string              `yaml:"workflow"`
	Params            map[string]string   `yaml:"params"`
	SkipIf            string              `yaml:"skip_if"`
	BreakIf           string              `yaml:"break_if"`
	ContinueOnFailure bool                `yaml:"continue_on_failure"`
	Loop              *verifyChangeLoop   `yaml:"loop"`
	Repair            *verifyChangeRepair `yaml:"repair"`
	Steps             []verifyChangeStep  `yaml:"steps"`
}

type verifyChangeSession struct {
	Name  string `yaml:"name"`
	Agent string `yaml:"agent"`
}

type verifyChangeParam struct {
	Name     string `yaml:"name"`
	Required *bool  `yaml:"required"`
	Default  string `yaml:"default"`
}

type verifyChangeWorkflow struct {
	Hidden   bool                  `yaml:"hidden"`
	Params   []verifyChangeParam   `yaml:"params"`
	Sessions []verifyChangeSession `yaml:"sessions"`
	Steps    []verifyChangeStep    `yaml:"steps"`
}

func loadVerifyChangeWorkflow(t *testing.T, ref string) verifyChangeWorkflow {
	t.Helper()
	body, err := ReadFile(ref)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", ref, err)
	}
	var workflow verifyChangeWorkflow
	if err := yaml.Unmarshal(body, &workflow); err != nil {
		t.Fatalf("unmarshal %s: %v", ref, err)
	}
	return workflow
}

func verifyStepIDs(steps []verifyChangeStep) []string {
	ids := make([]string, 0, len(steps))
	for i := range steps {
		ids = append(ids, steps[i].ID)
	}
	return ids
}

func findVerifyChangeStep(steps []verifyChangeStep, id string) *verifyChangeStep {
	for i := range steps {
		if steps[i].ID == id {
			return &steps[i]
		}
		if found := findVerifyChangeStep(steps[i].Steps, id); found != nil {
			return found
		}
	}
	return nil
}

func walkVerifyChangeSteps(steps []verifyChangeStep, visit func(*verifyChangeStep)) {
	for i := range steps {
		visit(&steps[i])
		walkVerifyChangeSteps(steps[i].Steps, visit)
	}
}

func TestCoreVerifyChangeShape(t *testing.T) {
	workflow := loadVerifyChangeWorkflow(t, "builtin:core/verify-change-v1.0.yaml")
	if !workflow.Hidden {
		t.Error("verify-change must be hidden")
	}

	type param struct {
		Name     string
		Required bool
		Default  string
	}
	var params []param
	for _, p := range workflow.Params {
		params = append(params, param{Name: p.Name, Required: p.Required == nil || *p.Required, Default: p.Default})
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
	if diff := cmp.Diff(wantSteps, verifyStepIDs(workflow.Steps)); diff != "" {
		t.Errorf("verify-change steps mismatch (-want +got):\n%s", diff)
	}
}

func TestCoreImplementChangeComposesVerifyChangeWithSharedSessions(t *testing.T) {
	implement := loadVerifyChangeWorkflow(t, "builtin:core/implement-change-v1.0.yaml")
	verify := loadVerifyChangeWorkflow(t, "builtin:core/verify-change-v1.0.yaml")

	wantSessions := []verifyChangeSession{{Name: "lead-agent", Agent: "lead"}, {Name: "acceptance-tester", Agent: "tester"}}
	if diff := cmp.Diff(wantSessions, verify.Sessions); diff != "" {
		t.Errorf("verify-change sessions mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(verify.Sessions, implement.Sessions); diff != "" {
		t.Errorf("implement-change and verify-change must declare identical sessions (-verify +implement):\n%s", diff)
	}

	ids := verifyStepIDs(implement.Steps)
	wantHead := []string{
		"validate-change-name",
		"validate-skip-validator",
		"check-plan",
		"implement-tasks",
		"complete-task-index",
		"verify-task-index",
		"verify-change",
	}
	if diff := cmp.Diff(wantHead, ids); diff != "" {
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
	workflow := loadVerifyChangeWorkflow(t, "builtin:core/verify-change-v1.0.yaml")
	walkVerifyChangeSteps(workflow.Steps, func(step *verifyChangeStep) {
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
	workflow := loadVerifyChangeWorkflow(t, "builtin:core/verify-change-v1.0.yaml")
	loop := findVerifyChangeStep(workflow.Steps, "prepare-acceptance")
	if loop == nil || loop.Loop == nil {
		t.Fatal("prepare-acceptance must be a loop")
	}
	if loop.Loop.MaxParam != "acceptance_rounds" || loop.Loop.AsIndex != "acceptance_round" {
		t.Fatalf("prepare-acceptance loop = %+v, want max_param acceptance_rounds and as_index acceptance_round", loop.Loop)
	}
	if loop.ContinueOnFailure {
		t.Fatal("prepare-acceptance must not hide real step failures behind continue_on_failure")
	}
	wantBody := []string{
		"reset-round-evidence",
		"acceptance-test",
		"acceptance-gate",
		"end-final-round",
		"acceptance-fix",
		"acceptance-validator",
		"acceptance-push",
	}
	if diff := cmp.Diff(wantBody, verifyStepIDs(loop.Steps)); diff != "" {
		t.Fatalf("prepare-acceptance body mismatch (-want +got):\n%s", diff)
	}

	reset := loop.Steps[0]
	for _, file := range []string{"acceptance-round-status.txt", "acceptance-test.md", "acceptance-handoff.md"} {
		if !strings.Contains(reset.Command, "{{session_dir}}/output/"+file) {
			t.Errorf("reset-round-evidence does not clear %s: %q", file, reset.Command)
		}
	}
	if strings.Contains(reset.Command, "acceptance-flow-evidence.md") || strings.Contains(reset.Command, "acceptance-findings.md") {
		t.Errorf("reset-round-evidence must keep the tester baseline: %q", reset.Command)
	}

	tester := loop.Steps[1]
	if tester.Session != "acceptance-tester" || tester.Mode != "autonomous" {
		t.Fatalf("acceptance-test = session:%q mode:%q, want acceptance-tester/autonomous", tester.Session, tester.Mode)
	}
	for _, want := range []string{
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
	} {
		if !strings.Contains(tester.Prompt, want) {
			t.Errorf("acceptance-test prompt missing %q", want)
		}
	}

	gate := loop.Steps[2]
	if gate.Script != "acceptance-gate.sh" || gate.ScriptInputs["action"] != "check" ||
		gate.ScriptInputs["evidence_dir"] != "{{session_dir}}/output" ||
		gate.BreakIf != "success" || !gate.ContinueOnFailure {
		t.Fatalf("acceptance-gate = %+v, want a tolerated acceptance-gate.sh check that breaks on success", gate)
	}

	endFinal := loop.Steps[3]
	if endFinal.BreakIf != "success" || !strings.Contains(endFinal.SkipIf, "{{acceptance_round}}") ||
		!strings.Contains(endFinal.SkipIf, "{{acceptance_rounds}}") {
		t.Fatalf("end-final-round = %+v, want a final-round-only break", endFinal)
	}

	fix := loop.Steps[4]
	if fix.Session != "lead-agent" || fix.Mode != "autonomous" {
		t.Fatalf("acceptance-fix = session:%q mode:%q, want lead-agent/autonomous", fix.Session, fix.Mode)
	}
	for _, want := range []string{
		"codagent:implement-with-tdd",
		"{{artifact_validation_instruction}}",
		"`[{{step_id}}]`",
		"Do not run Agent Validator",
		"do not push",
		"`{{session_dir}}/output/acceptance-impact-scope.md`",
		"`targeted`",
		"`evidence-only`",
	} {
		if !strings.Contains(fix.Prompt, want) {
			t.Errorf("acceptance-fix prompt missing %q", want)
		}
	}

	validator := loop.Steps[5]
	if validator.Workflow != "run-validator-v1.0.yaml" || validator.SkipIf != "sh: test {{skip_validator}} = true" {
		t.Fatalf("acceptance-validator = %+v, want the skip_validator-gated run-validator workflow", validator)
	}

	push := loop.Steps[6]
	if push.Script != "acceptance-push.sh" || push.Repair == nil || push.Repair.Session != "lead-agent" {
		t.Fatalf("acceptance-push = %+v, want acceptance-push.sh with lead-agent repair", push)
	}

	status := findVerifyChangeStep(workflow.Steps, "write-acceptance-status")
	if status == nil || status.Script != "acceptance-gate.sh" || status.ScriptInputs["action"] != "finalize" ||
		status.ScriptInputs["rounds"] != "{{acceptance_rounds}}" {
		t.Fatalf("write-acceptance-status = %+v, want acceptance-gate.sh finalize", status)
	}
}

func TestCoreVerifyChangeOpenDraftPRPushesDirectly(t *testing.T) {
	workflow := loadVerifyChangeWorkflow(t, "builtin:core/verify-change-v1.0.yaml")
	step := findVerifyChangeStep(workflow.Steps, "open-draft-pr")
	if step == nil {
		t.Fatal("open-draft-pr step not found")
	}
	for _, want := range []string{
		"directly with `git` and `gh`",
		"gh pr create --draft",
		"gh pr ready --undo",
		"preserve its base branch",
		"its head SHA equals local `HEAD`",
		"REPAIR_BLOCKED",
	} {
		if !strings.Contains(step.Prompt, want) {
			t.Errorf("open-draft-pr prompt missing %q", want)
		}
	}
}

// gitRepo creates a repository with one commit and returns its directory and
// HEAD SHA.
func gitRepo(t *testing.T) (dir, head string) {
	t.Helper()
	dir = t.TempDir()
	verifyGit(t, dir, "init", "-q", "-b", "feature")
	verifyGit(t, dir, "config", "user.email", "test@example.com")
	verifyGit(t, dir, "config", "user.name", "Test")
	verifyGit(t, dir, "config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("one\n"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	verifyGit(t, dir, "add", "file.txt")
	verifyGit(t, dir, "commit", "-q", "-m", "initial")
	return dir, strings.TrimSpace(verifyGit(t, dir, "rev-parse", "HEAD"))
}

func verifyGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
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

func writeEvidence(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func TestCoreAcceptanceGateScript(t *testing.T) {
	scriptPath := writeAssetScript(t, "core/acceptance-gate.sh")

	run := func(t *testing.T, repo, evidenceDir, action string) (string, error) {
		t.Helper()
		cmd := exec.Command("sh", scriptPath)
		cmd.Dir = repo
		cmd.Stdin = strings.NewReader(`{"evidence_dir":` + strconv.Quote(evidenceDir) + `,"action":` + strconv.Quote(action) + `,"rounds":"3"}`)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	converged := func(t *testing.T) (string, string, string) {
		t.Helper()
		repo, head := gitRepo(t)
		evidence := t.TempDir()
		writeEvidence(t, evidence, "acceptance-round-status.txt", "READY "+head+"\n")
		writeEvidence(t, evidence, "acceptance-test.md", "Tested head: "+head+"\n")
		writeEvidence(t, evidence, "acceptance-handoff.md", "Ready SHA `"+head[:12]+"`\n")
		return repo, head, evidence
	}

	t.Run("converges when the tester and evidence name the current head", func(t *testing.T) {
		repo, _, evidence := converged(t)
		if out, err := run(t, repo, evidence, "check"); err != nil {
			t.Fatalf("gate failed: %v\n%s", err, out)
		}
	})

	t.Run("accepts an uppercase abbreviated SHA", func(t *testing.T) {
		repo, head, evidence := converged(t)
		writeEvidence(t, evidence, "acceptance-test.md", "Head "+strings.ToUpper(head[:7])+"\n")
		if out, err := run(t, repo, evidence, "check"); err != nil {
			t.Fatalf("gate failed: %v\n%s", err, out)
		}
	})

	failing := []struct {
		name   string
		mutate func(t *testing.T, repo, head, evidence string)
		want   string
	}{
		{
			name: "tester reported not ready",
			mutate: func(t *testing.T, _, _, evidence string) {
				writeEvidence(t, evidence, "acceptance-round-status.txt", "NOT_READY\n")
			},
			want: "round status is 'NOT_READY'",
		},
		{
			name: "tester recorded no status",
			mutate: func(t *testing.T, _, _, evidence string) {
				if err := os.Remove(filepath.Join(evidence, "acceptance-round-status.txt")); err != nil {
					t.Fatal(err)
				}
			},
			want: "recorded no round status",
		},
		{
			name: "ready status names another revision",
			mutate: func(t *testing.T, _, _, evidence string) {
				writeEvidence(t, evidence, "acceptance-round-status.txt", "READY 0123456789abcdef0123456789abcdef01234567\n")
			},
			want: "not 'READY",
		},
		{
			name: "acceptance test evidence missing",
			mutate: func(t *testing.T, _, _, evidence string) {
				if err := os.Remove(filepath.Join(evidence, "acceptance-test.md")); err != nil {
					t.Fatal(err)
				}
			},
			want: "acceptance-test.md is missing or empty",
		},
		{
			name: "handoff is stale",
			mutate: func(t *testing.T, _, _, evidence string) {
				writeEvidence(t, evidence, "acceptance-handoff.md", "Ready SHA 0123456789abcdef\n")
			},
			want: "acceptance-handoff.md does not name the current revision",
		},
		{
			name: "head moved after testing",
			mutate: func(t *testing.T, repo, _, _ string) {
				writeEvidence(t, repo, "file.txt", "two\n")
				verifyGit(t, repo, "commit", "-q", "-am", "fix")
			},
			want: "not 'READY",
		},
		{
			name: "tracked changes are uncommitted",
			mutate: func(t *testing.T, repo, _, _ string) {
				writeEvidence(t, repo, "file.txt", "dirty\n")
			},
			want: "tracked files have uncommitted changes",
		},
	}
	for _, tt := range failing {
		t.Run("does not converge when "+tt.name, func(t *testing.T) {
			repo, head, evidence := converged(t)
			tt.mutate(t, repo, head, evidence)
			out, err := run(t, repo, evidence, "check")
			if err == nil {
				t.Fatalf("gate succeeded unexpectedly:\n%s", out)
			}
			if !strings.Contains(out, tt.want) {
				t.Fatalf("output = %q, want %q", out, tt.want)
			}
		})
	}

	t.Run("untracked files do not block convergence", func(t *testing.T) {
		repo, _, evidence := converged(t)
		writeEvidence(t, repo, "build-output.log", "setup\n")
		if out, err := run(t, repo, evidence, "check"); err != nil {
			t.Fatalf("gate failed: %v\n%s", err, out)
		}
	})

	t.Run("finalize records completion and keeps the tester handoff", func(t *testing.T) {
		repo, head, evidence := converged(t)
		if out, err := run(t, repo, evidence, "finalize"); err != nil {
			t.Fatalf("finalize failed: %v\n%s", err, out)
		}
		assertFileContent(t, filepath.Join(evidence, "acceptance-preparation-status.txt"), "ACCEPTANCE_COMPLETE\n")
		assertFileContent(t, filepath.Join(evidence, "acceptance-handoff.md"), "Ready SHA `"+head[:12]+"`\n")
	})

	t.Run("finalize records failure with a handoff listing findings and evidence", func(t *testing.T) {
		repo, _, evidence := converged(t)
		writeEvidence(t, evidence, "acceptance-round-status.txt", "NOT_READY\n")
		writeEvidence(t, evidence, "acceptance-findings.md", "AT-3 fails: export button does nothing\n")
		writeEvidence(t, evidence, "acceptance-flow-evidence.md", "flows\n")
		if out, err := run(t, repo, evidence, "finalize"); err != nil {
			t.Fatalf("finalize failed: %v\n%s", err, out)
		}
		assertFileContent(t, filepath.Join(evidence, "acceptance-preparation-status.txt"), "ACCEPTANCE_FAILED\n")
		handoff := readFile(t, filepath.Join(evidence, "acceptance-handoff.md"))
		for _, want := range []string{
			"did not converge within 3 rounds",
			"round status is 'NOT_READY'",
			"AT-3 fails: export button does nothing",
			filepath.Join(evidence, "acceptance-flow-evidence.md"),
			filepath.Join(evidence, "acceptance-handoff-tester.md"),
			"acceptance-assumptions.md",
		} {
			if !strings.Contains(handoff, want) {
				t.Errorf("failure handoff missing %q:\n%s", want, handoff)
			}
		}
		assertFileContent(t, filepath.Join(evidence, "acceptance-handoff-tester.md"), "Ready SHA `"+readHeadPrefix(t, repo)+"`\n")
	})

	t.Run("finalize writes a handoff when the tester wrote nothing", func(t *testing.T) {
		repo, _ := gitRepo(t)
		evidence := filepath.Join(t.TempDir(), "output")
		if out, err := run(t, repo, evidence, "finalize"); err != nil {
			t.Fatalf("finalize failed: %v\n%s", err, out)
		}
		assertFileContent(t, filepath.Join(evidence, "acceptance-preparation-status.txt"), "ACCEPTANCE_FAILED\n")
		handoff := readFile(t, filepath.Join(evidence, "acceptance-handoff.md"))
		for _, want := range []string{"recorded no findings file", "No acceptance evidence files were found"} {
			if !strings.Contains(handoff, want) {
				t.Errorf("failure handoff missing %q:\n%s", want, handoff)
			}
		}
	})

	t.Run("rejects an unknown action", func(t *testing.T) {
		repo, _, evidence := converged(t)
		out, err := run(t, repo, evidence, "bogus")
		if err == nil || !strings.Contains(out, "unknown action") {
			t.Fatalf("gate = (%q, %v), want unknown-action failure", out, err)
		}
	})
}

func readHeadPrefix(t *testing.T, repo string) string {
	t.Helper()
	return strings.TrimSpace(verifyGit(t, repo, "rev-parse", "HEAD"))[:12]
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	if got := readFile(t, path); got != want {
		t.Fatalf("%s = %q, want %q", filepath.Base(path), got, want)
	}
}

func TestCoreAcceptancePushScript(t *testing.T) {
	scriptPath := writeAssetScript(t, "core/acceptance-push.sh")

	// setup returns a repository whose branch tracks a bare remote, plus a
	// directory holding a fake gh that prints $FAKE_GH_JSON.
	setup := func(t *testing.T) (string, string) {
		t.Helper()
		repo, _ := gitRepo(t)
		remote := t.TempDir()
		verifyGit(t, remote, "init", "-q", "--bare")
		verifyGit(t, repo, "remote", "add", "origin", remote)
		verifyGit(t, repo, "push", "-q", "--set-upstream", "origin", "feature")
		binDir := t.TempDir()
		fakeGH := "#!/bin/sh\nprintf '%s\\n' \"$*\" >>\"$FAKE_GH_LOG\"\nprintf '%s' \"$FAKE_GH_JSON\"\n"
		if err := os.WriteFile(filepath.Join(binDir, "gh"), []byte(fakeGH), 0o700); err != nil {
			t.Fatalf("write fake gh: %v", err)
		}
		return repo, binDir
	}
	run := func(t *testing.T, repo, binDir, prJSON string) (string, error) {
		t.Helper()
		cmd := exec.Command("sh", scriptPath)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(),
			"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
			"FAKE_GH_JSON="+prJSON,
			"FAKE_GH_LOG="+filepath.Join(t.TempDir(), "gh.log"),
		)
		cmd.Stdin = strings.NewReader(`{"attempts":"2","interval_seconds":"0"}`)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	commitFix := func(t *testing.T, repo string) string {
		t.Helper()
		writeEvidence(t, repo, "file.txt", "fixed\n")
		verifyGit(t, repo, "commit", "-q", "-am", "fix")
		return strings.TrimSpace(verifyGit(t, repo, "rev-parse", "HEAD"))
	}
	prJSON := func(head string, draft bool) string {
		return `[{"url":"https://example.test/pr/1","isDraft":` + strconv.FormatBool(draft) + `,"headRefOid":"` + head + `"}]`
	}

	t.Run("pushes the fix and verifies the draft pull request head", func(t *testing.T) {
		repo, binDir := setup(t)
		head := commitFix(t, repo)
		out, err := run(t, repo, binDir, prJSON(head, true))
		if err != nil {
			t.Fatalf("push failed: %v\n%s", err, out)
		}
		remoteHead := strings.TrimSpace(verifyGit(t, repo, "rev-parse", "origin/feature"))
		if remoteHead != head {
			t.Fatalf("remote head = %s, want pushed %s", remoteHead, head)
		}
		if !strings.Contains(out, "https://example.test/pr/1") {
			t.Fatalf("output = %q, want pull request URL", out)
		}
	})

	t.Run("fails when the pull request head does not match", func(t *testing.T) {
		repo, binDir := setup(t)
		commitFix(t, repo)
		out, err := run(t, repo, binDir, prJSON("0123456789abcdef0123456789abcdef01234567", true))
		if err == nil || !strings.Contains(out, "does not equal local HEAD") {
			t.Fatalf("push = (%q, %v), want head mismatch failure", out, err)
		}
	})

	t.Run("fails when the pull request is not a draft", func(t *testing.T) {
		repo, binDir := setup(t)
		head := commitFix(t, repo)
		out, err := run(t, repo, binDir, prJSON(head, false))
		if err == nil || !strings.Contains(out, "is not a draft") {
			t.Fatalf("push = (%q, %v), want draft failure", out, err)
		}
	})

	t.Run("fails when no open pull request exists", func(t *testing.T) {
		repo, binDir := setup(t)
		commitFix(t, repo)
		out, err := run(t, repo, binDir, `[]`)
		if err == nil || !strings.Contains(out, "found 0") {
			t.Fatalf("push = (%q, %v), want missing pull request failure", out, err)
		}
	})

	t.Run("refuses to push uncommitted tracked changes", func(t *testing.T) {
		repo, binDir := setup(t)
		writeEvidence(t, repo, "file.txt", "dirty\n")
		out, err := run(t, repo, binDir, prJSON("x", true))
		if err == nil || !strings.Contains(out, "tracked changes are uncommitted") {
			t.Fatalf("push = (%q, %v), want uncommitted-changes failure", out, err)
		}
	})

	t.Run("sets the upstream when the branch has none", func(t *testing.T) {
		repo, binDir := setup(t)
		verifyGit(t, repo, "branch", "--unset-upstream")
		head := commitFix(t, repo)
		if out, err := run(t, repo, binDir, prJSON(head, true)); err != nil {
			t.Fatalf("push failed: %v\n%s", err, out)
		}
		if upstream := strings.TrimSpace(verifyGit(t, repo, "rev-parse", "--abbrev-ref", "feature@{upstream}")); upstream != "origin/feature" {
			t.Fatalf("upstream = %q, want origin/feature", upstream)
		}
	})
}
