package builtinworkflows

import "testing"

func TestAcceptanceAndFlowTesterScratchPrompts(t *testing.T) {
	tests := []struct {
		ref, stepID, scratch string
	}{
		{"builtin:core/accept-change-v1.0.yaml", "run-reacceptance-testing", "acceptance-test"},
		{"builtin:core/accept-change-v1.0.yaml", "recover-reacceptance-testing", "acceptance-test"},
		{"builtin:core/complete-simple-change-v1.0.yaml", "test", "test-flows"},
		{"builtin:core/complete-simple-change-v1.0.yaml", "review", "test-flows"},
	}
	for _, tt := range tests {
		t.Run(tt.stepID, func(t *testing.T) {
			workflow := readBuiltinWorkflowForTest(t, tt.ref)
			step := findStep(workflow.Steps, tt.stepID)
			if step == nil {
				t.Fatalf("%s step not found", tt.stepID)
			}
			requirePromptContains(t, step.ID, step.Prompt,
				"scratch directory: `{{session_dir}}/scratch/"+tt.scratch+"`",
				"mkdir -p \"{{session_dir}}/scratch/"+tt.scratch+"\"",
				"All temporary files, clones, build outputs, and servers' working directories",
				"Never use `/tmp`, `/private/tmp`, or `$TMPDIR`",
			)
		})
	}
}
