package builtinworkflows

import "testing"

func TestAcceptanceAndFlowTesterScratchPrompts(t *testing.T) {
	tests := []struct {
		ref, stepID, scratch, input string
	}{
		{"builtin:core/accept-change-v1.0.yaml", "run-reacceptance-testing", "acceptance-test", "scratch_dir"},
		{"builtin:core/accept-change-v1.0.yaml", "recover-reacceptance-testing", "acceptance-test", "scratch_dir"},
		{"builtin:core/complete-simple-change-v1.0.yaml", "test", "test-flows", "`scratch_dir`"},
		{"builtin:core/complete-simple-change-v1.0.yaml", "review", "test-flows", "`scratch_dir`"},
	}
	for _, tt := range tests {
		t.Run(tt.stepID, func(t *testing.T) {
			workflow := readBuiltinWorkflowForTest(t, tt.ref)
			step := findStep(workflow.Steps, tt.stepID)
			if step == nil {
				t.Fatalf("%s step not found", tt.stepID)
			}
			requirePromptContains(t, step.ID, step.Prompt,
				tt.input+": `{{session_dir}}/scratch/"+tt.scratch+"`",
				"mkdir -p \"{{session_dir}}/scratch/"+tt.scratch+"\"",
				"All temporary files, clones, build outputs, and servers' working directories must go under `scratch_dir`",
				"Never use `/tmp`, `/private/tmp`, or `$TMPDIR`",
			)
		})
	}
}
