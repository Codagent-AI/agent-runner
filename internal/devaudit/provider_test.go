//go:build dev_audit

package devaudit

import (
	"testing"

	"github.com/codagent/agent-runner/internal/loader"
	builtinworkflows "github.com/codagent/agent-runner/workflows"
)

func TestTaggedProviderInjectsTheSingleHiddenAuditWorkflow(t *testing.T) {
	ref, err := builtinworkflows.Resolve("audit:run-audit")
	if err != nil {
		t.Fatalf("resolve audit workflow: %v", err)
	}
	if ref != "builtin:audit/run-audit-v1.0.yaml" {
		t.Fatalf("audit ref = %q", ref)
	}
	workflow, err := loader.LoadWorkflow(ref, loader.Options{})
	if err != nil {
		t.Fatalf("load audit workflow: %v", err)
	}
	if !workflow.Hidden || len(workflow.Steps) != 7 {
		t.Fatalf("workflow hidden=%v stages=%d", workflow.Hidden, len(workflow.Steps))
	}
	want := []string{"prepare-evidence", "value-audit", "validate-value", "correctness-audit", "validate-publish-correctness", "assemble-local-report", "report-value-observations"}
	for index, id := range want {
		if workflow.Steps[index].ID != id {
			t.Fatalf("stage %d = %q, want %q", index, workflow.Steps[index].ID, id)
		}
	}
}
