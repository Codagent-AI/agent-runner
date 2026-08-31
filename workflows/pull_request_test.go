package builtinworkflows

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestPullRequestWorkflowsCaptureURL(t *testing.T) {
	tests := []struct {
		ref    string
		stepID string
	}{
		{ref: "builtin:core/implement-change-v1.0.yaml", stepID: "verify-draft-pr"},
		{ref: "builtin:core/finalize-pr-v1.0.yaml", stepID: "record-pull-request"},
	}
	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			data, err := ReadFile(tt.ref)
			if err != nil {
				t.Fatal(err)
			}
			var workflow struct {
				Steps []struct {
					ID                string `yaml:"id"`
					Capture           string `yaml:"capture"`
					ContinueOnFailure bool   `yaml:"continue_on_failure"`
				} `yaml:"steps"`
			}
			if err := yaml.Unmarshal(data, &workflow); err != nil {
				t.Fatal(err)
			}
			for _, step := range workflow.Steps {
				if step.ID != tt.stepID {
					continue
				}
				if step.Capture != "pr_url" {
					t.Fatalf("%s capture = %q, want pr_url", tt.stepID, step.Capture)
				}
				if tt.stepID == "record-pull-request" && !step.ContinueOnFailure {
					t.Fatalf("%s must be best effort", tt.stepID)
				}
				return
			}
			t.Fatalf("workflow missing step %q", tt.stepID)
		})
	}
}
