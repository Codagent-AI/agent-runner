package builtinworkflows

import (
	"slices"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestOnboardingWorkflowsResolveAndAssetsList(t *testing.T) {
	welcome, err := Resolve("onboarding:welcome")
	if err != nil {
		t.Fatalf("Resolve(onboarding:welcome) returned error: %v", err)
	}
	if welcome != "builtin:onboarding/welcome.yaml" {
		t.Fatalf("welcome ref = %q", welcome)
	}
	setup, err := Resolve("onboarding:setup-agent-profile")
	if err != nil {
		t.Fatalf("Resolve(onboarding:setup-agent-profile) returned error: %v", err)
	}
	if setup != "builtin:onboarding/setup-agent-profile.yaml" {
		t.Fatalf("setup ref = %q", setup)
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
	for _, want := range []string{"detect-adapters.sh", "models-for-cli.sh", "check-collisions.sh", "write-profile.sh", "docs/agent-runner-basics.md"} {
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
