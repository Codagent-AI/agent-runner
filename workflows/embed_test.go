package builtinworkflows

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestOnboardingWorkflowsResolveAndAssetsList(t *testing.T) {
	onboarding, err := Resolve("onboarding:onboarding")
	if err != nil {
		t.Fatalf("Resolve(onboarding:onboarding) returned error: %v", err)
	}
	if onboarding != "builtin:onboarding/onboarding.yaml" {
		t.Fatalf("onboarding ref = %q", onboarding)
	}
	demo, err := Resolve("onboarding:step-types-demo")
	if err != nil {
		t.Fatalf("Resolve(onboarding:step-types-demo) returned error: %v", err)
	}
	if demo != "builtin:onboarding/step-types-demo.yaml" {
		t.Fatalf("demo ref = %q", demo)
	}

	assets, err := ListAssets("onboarding")
	if err != nil {
		t.Fatalf("ListAssets(onboarding) returned error: %v", err)
	}
	for _, removed := range []string{"onboarding:welcome", "onboarding:setup-agent-profile"} {
		if ref, err := Resolve(removed); err == nil {
			t.Fatalf("Resolve(%s) = %q, want not found", removed, ref)
		}
	}
	for _, want := range []string{"docs/agent-runner-basics.md"} {
		if !slices.Contains(assets, want) {
			t.Fatalf("asset %q not found in %v", want, assets)
		}
		body, err := ReadAsset("onboarding/" + want)
		if err != nil {
			t.Fatalf("ReadAsset(%s) returned error: %v", want, err)
		}
		if len(body) == 0 {
			t.Fatalf("asset %s is empty", want)
		}
	}
	for _, removed := range []string{
		"check-collisions.sh",
		"count-list.sh",
		"detect-adapters.sh",
		"echo-value.sh",
		"format-list.sh",
		"models-for-cli.sh",
		"write-profile.sh",
	} {
		if slices.Contains(assets, removed) {
			t.Fatalf("removed setup asset %q still embedded in onboarding namespace assets %v", removed, assets)
		}
	}
}

func TestOpenSpecPlanningWorkflowsUseSharedCreateScript(t *testing.T) {
	for _, ref := range []string{"builtin:openspec/plan-change.yaml", "builtin:openspec/simple-change.yaml"} {
		t.Run(ref, func(t *testing.T) {
			data, err := ReadFile(ref)
			if err != nil {
				t.Fatalf("read %s: %v", ref, err)
			}

			var workflow struct {
				Steps []struct {
					ID           string            `yaml:"id"`
					Command      string            `yaml:"command"`
					Script       string            `yaml:"script"`
					ScriptInputs map[string]string `yaml:"script_inputs"`
				} `yaml:"steps"`
			}
			if err := yaml.Unmarshal(data, &workflow); err != nil {
				t.Fatalf("parse %s: %v", ref, err)
			}
			if len(workflow.Steps) == 0 {
				t.Fatalf("%s has no steps", ref)
			}
			create := workflow.Steps[0]
			if create.ID != "create" {
				t.Fatalf("first step id = %q, want create", create.ID)
			}
			if create.Script != "create-change.sh" {
				t.Fatalf("create script = %q, want create-change.sh", create.Script)
			}
			if create.Command != "" {
				t.Fatalf("create should not duplicate shell command, got %q", create.Command)
			}
			if got := create.ScriptInputs["change_name"]; got != "{{change_name}}" {
				t.Fatalf("script input change_name = %q, want {{change_name}}", got)
			}
		})
	}
}

func TestCoreFinalizePRUsesCIStatusGate(t *testing.T) {
	data, err := ReadFile("builtin:core/finalize-pr.yaml")
	if err != nil {
		t.Fatalf("ReadFile(core/finalize-pr): %v", err)
	}

	var workflow struct {
		Steps []struct {
			ID    string `yaml:"id"`
			Steps []struct {
				ID                string            `yaml:"id"`
				Script            string            `yaml:"script"`
				ScriptInputs      map[string]string `yaml:"script_inputs"`
				Capture           string            `yaml:"capture"`
				ContinueOnFailure bool              `yaml:"continue_on_failure"`
				BreakIf           string            `yaml:"break_if"`
				SkipIf            string            `yaml:"skip_if"`
			} `yaml:"steps"`
		} `yaml:"steps"`
	}
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatalf("unmarshal workflow: %v", err)
	}

	var loopSteps []struct {
		ID                string            `yaml:"id"`
		Script            string            `yaml:"script"`
		ScriptInputs      map[string]string `yaml:"script_inputs"`
		Capture           string            `yaml:"capture"`
		ContinueOnFailure bool              `yaml:"continue_on_failure"`
		BreakIf           string            `yaml:"break_if"`
		SkipIf            string            `yaml:"skip_if"`
	}
	for _, step := range workflow.Steps {
		if step.ID == "ci-fix-loop" {
			loopSteps = step.Steps
			break
		}
	}
	if len(loopSteps) != 4 {
		t.Fatalf("ci-fix-loop body has %d steps, want 4", len(loopSteps))
	}

	waitCI := loopSteps[0]
	if waitCI.ID != "wait-ci" {
		t.Fatalf("first loop step = %q, want wait-ci", waitCI.ID)
	}
	if waitCI.BreakIf != "" {
		t.Fatalf("wait-ci break_if = %q, want empty because agent exit success is not CI success", waitCI.BreakIf)
	}
	if waitCI.Capture != "ci_report" {
		t.Fatalf("wait-ci capture = %q, want ci_report", waitCI.Capture)
	}

	gate := loopSteps[1]
	if gate.ID != "ci-status-gate" {
		t.Fatalf("second loop step = %q, want ci-status-gate", gate.ID)
	}
	if gate.Script != "ci-status-gate.sh" {
		t.Fatalf("gate script = %q, want ci-status-gate.sh", gate.Script)
	}
	if got := gate.ScriptInputs["report"]; got != "{{ci_report}}" {
		t.Fatalf("gate report input = %q, want {{ci_report}}", got)
	}
	if !gate.ContinueOnFailure {
		t.Fatal("gate should continue_on_failure so fix-pr can run after failed CI")
	}
	if gate.BreakIf != "success" {
		t.Fatalf("gate break_if = %q, want success", gate.BreakIf)
	}

	fixNeeded := loopSteps[2]
	if fixNeeded.ID != "ci-fix-needed-gate" {
		t.Fatalf("third loop step = %q, want ci-fix-needed-gate", fixNeeded.ID)
	}
	if fixNeeded.Script != "ci-fix-needed-gate.sh" {
		t.Fatalf("fix-needed script = %q, want ci-fix-needed-gate.sh", fixNeeded.Script)
	}
	if got := fixNeeded.ScriptInputs["report"]; got != "{{ci_report}}" {
		t.Fatalf("fix-needed report input = %q, want {{ci_report}}", got)
	}
	if !fixNeeded.ContinueOnFailure {
		t.Fatal("fix-needed gate should continue_on_failure so fix-pr can run after failed CI")
	}

	fixPR := loopSteps[3]
	if fixPR.ID != "fix-pr" {
		t.Fatalf("fourth loop step = %q, want fix-pr", fixPR.ID)
	}
	if fixPR.SkipIf != "previous_success" {
		t.Fatalf("fix-pr skip_if = %q, want previous_success", fixPR.SkipIf)
	}
}

func TestCoreCIStatusGateScript(t *testing.T) {
	script, err := ReadAsset("core/ci-status-gate.sh")
	if err != nil {
		t.Fatalf("ReadAsset(core/ci-status-gate.sh): %v", err)
	}

	scriptPath := filepath.Join(t.TempDir(), "ci-status-gate.sh")
	if err := os.WriteFile(scriptPath, script, 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	tests := []struct {
		name     string
		report   string
		wantCode int
	}{
		{name: "passed exits success", report: "## CI Status: passed\n", wantCode: 0},
		{name: "failed exits fixable failure", report: "## CI Status: failed\n", wantCode: 1},
		{name: "comments exits fixable failure", report: "## CI Status: comments\n", wantCode: 1},
		{name: "pending exits failure to keep polling", report: "## CI Status: pending\n", wantCode: 1},
		{name: "unknown exits failure to keep polling", report: "wait-ci did not produce a status\n", wantCode: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command("sh", scriptPath)
			cmd.Stdin = strings.NewReader(`{"report":` + strconv.Quote(tt.report) + `}`)
			err := cmd.Run()
			gotCode := 0
			if err != nil {
				exitErr, ok := err.(*exec.ExitError)
				if !ok {
					t.Fatalf("run script: %v", err)
				}
				gotCode = exitErr.ExitCode()
			}
			if gotCode != tt.wantCode {
				t.Fatalf("exit code = %d, want %d", gotCode, tt.wantCode)
			}
		})
	}
}

func TestCoreCIFixNeededGateScript(t *testing.T) {
	script, err := ReadAsset("core/ci-fix-needed-gate.sh")
	if err != nil {
		t.Fatalf("ReadAsset(core/ci-fix-needed-gate.sh): %v", err)
	}

	scriptPath := filepath.Join(t.TempDir(), "ci-fix-needed-gate.sh")
	if err := os.WriteFile(scriptPath, script, 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	tests := []struct {
		name     string
		report   string
		wantCode int
	}{
		{name: "failed exits failure so fix-pr runs", report: "## CI Status: failed\n", wantCode: 1},
		{name: "comments exits failure so fix-pr runs", report: "## CI Status: comments\n", wantCode: 1},
		{name: "passed exits success so fix-pr skips", report: "## CI Status: passed\n", wantCode: 0},
		{name: "pending exits success so fix-pr skips", report: "## CI Status: pending\n", wantCode: 0},
		{name: "unknown exits success so fix-pr skips", report: "wait-ci did not produce a status\n", wantCode: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command("sh", scriptPath)
			cmd.Stdin = strings.NewReader(`{"report":` + strconv.Quote(tt.report) + `}`)
			err := cmd.Run()
			gotCode := 0
			if err != nil {
				exitErr, ok := err.(*exec.ExitError)
				if !ok {
					t.Fatalf("run script: %v", err)
				}
				gotCode = exitErr.ExitCode()
			}
			if gotCode != tt.wantCode {
				t.Fatalf("exit code = %d, want %d", gotCode, tt.wantCode)
			}
		})
	}

	t.Run("python fallback parses escaped JSON report without jq", func(t *testing.T) {
		pythonPath, err := exec.LookPath("python3")
		if err != nil {
			t.Skip("python3 not available")
		}
		binDir := t.TempDir()
		if err := os.Symlink(pythonPath, filepath.Join(binDir, "python3")); err != nil {
			t.Fatalf("symlink python3: %v", err)
		}

		cmd := exec.Command("sh", scriptPath)
		cmd.Env = append(os.Environ(), "PATH="+binDir+":/usr/bin:/bin")
		cmd.Stdin = strings.NewReader(`{"report":"intro \"quoted\"\n## CI Status: comments\n"}`)
		err = cmd.Run()
		if err == nil {
			t.Fatal("fallback script exit code = 0, want fix-needed failure")
		}
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("run script: %v", err)
		}
		if exitErr.ExitCode() != 1 {
			t.Fatalf("exit code = %d, want 1", exitErr.ExitCode())
		}
	})
}
