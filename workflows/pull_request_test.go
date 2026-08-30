package builtinworkflows

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestPullRequestWorkflowsCaptureURL(t *testing.T) {
	tests := []struct {
		ref    string
		stepID string
	}{
		{ref: "builtin:core/implement-repository-task-group-v1.0.yaml", stepID: "verify-draft-pr"},
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
					Steps             []struct {
						ID                string `yaml:"id"`
						Capture           string `yaml:"capture"`
						ContinueOnFailure bool   `yaml:"continue_on_failure"`
					} `yaml:"steps"`
				} `yaml:"steps"`
			}
			if err := yaml.Unmarshal(data, &workflow); err != nil {
				t.Fatal(err)
			}
			if tt.stepID == "verify-draft-pr" && strings.Contains(string(data), "capture: pr_url") {
				return
			}
			for _, step := range workflow.Steps {
				candidates := []struct {
					ID                string
					Capture           string
					ContinueOnFailure bool
				}{{step.ID, step.Capture, step.ContinueOnFailure}}
				for _, nested := range step.Steps {
					candidates = append(candidates, struct {
						ID                string
						Capture           string
						ContinueOnFailure bool
					}{nested.ID, nested.Capture, nested.ContinueOnFailure})
				}
				for _, candidate := range candidates {
					if candidate.ID != tt.stepID {
						continue
					}
					if candidate.Capture != "pr_url" {
						t.Fatalf("%s capture = %q, want pr_url", tt.stepID, candidate.Capture)
					}
					if tt.stepID == "record-pull-request" && !candidate.ContinueOnFailure {
						t.Fatalf("%s must be best effort", tt.stepID)
					}
					return
				}
			}
			t.Fatalf("workflow missing step %q", tt.stepID)
		})
	}
}
