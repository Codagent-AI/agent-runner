package loader

import (
	"testing"

	builtinworkflows "github.com/codagent/agent-runner/workflows"
)

func TestEmbeddedOnboardingWorkflowsLoad(t *testing.T) {
	for _, name := range []string{"onboarding:welcome", "onboarding:setup-agent-profile", "onboarding:step-types-demo"} {
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
