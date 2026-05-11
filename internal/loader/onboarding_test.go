package loader

import (
	"testing"

	builtinworkflows "github.com/codagent/agent-runner/workflows"
)

func TestEmbeddedOnboardingWorkflowsLoad(t *testing.T) {
	for _, name := range []string{
		"onboarding:onboarding",
		"onboarding:step-types-demo",
		"onboarding:guided-workflow",
		"onboarding:validator",
		"onboarding:advanced",
		"onboarding:help",
	} {
		t.Run(name, func(t *testing.T) {
			ref, err := builtinworkflows.Resolve(name)
			if err != nil {
				t.Fatalf("Resolve(%s) returned error: %v", name, err)
			}
			wf, err := LoadWorkflow(ref, Options{})
			if err != nil {
				t.Fatalf("LoadWorkflow(%s) returned error: %v", ref, err)
			}
			if len(wf.Steps) == 0 {
				t.Fatalf("workflow %s loaded with no steps", name)
			}
		})
	}
}

func TestRemovedOnboardingSetupWorkflowsDoNotResolve(t *testing.T) {
	for _, name := range []string{"onboarding:welcome", "onboarding:setup-agent-profile"} {
		t.Run(name, func(t *testing.T) {
			if ref, err := builtinworkflows.Resolve(name); err == nil {
				t.Fatalf("Resolve(%s) = %q, want not found", name, ref)
			}
		})
	}
}
