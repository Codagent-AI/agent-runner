package builtinworkflows

import "testing"

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

	assets, err := ListAssets("onboarding")
	if err != nil {
		t.Fatalf("ListAssets(onboarding) returned error: %v", err)
	}
	for _, want := range []string{"detect-adapters.sh", "models-for-cli.sh", "check-collisions.sh", "write-profile.sh"} {
		found := false
		for _, got := range assets {
			if got == want {
				found = true
				break
			}
		}
		if !found {
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
