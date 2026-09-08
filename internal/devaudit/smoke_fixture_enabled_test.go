//go:build dev_audit && devaudit_smoke

package devaudit

import (
	"testing"

	"github.com/codagent/agent-runner/internal/loader"
	builtinworkflows "github.com/codagent/agent-runner/workflows"
)

func TestTaggedProviderRegistersHermeticCanonicalSmokeFixture(t *testing.T) {
	ref, err := builtinworkflows.Resolve("openspec:audit-smoke")
	if err != nil {
		t.Fatalf("resolve canonical smoke workflow: %v", err)
	}
	workflow, err := loader.LoadWorkflow(ref, loader.Options{})
	if err != nil {
		t.Fatalf("load canonical smoke workflow: %v", err)
	}
	if !workflow.Hidden || len(workflow.Steps) != 1 || workflow.Steps[0].Agent != "crosscheck" {
		t.Fatalf("canonical smoke workflow = %#v, want hidden one-step crosscheck fixture", workflow)
	}
}
