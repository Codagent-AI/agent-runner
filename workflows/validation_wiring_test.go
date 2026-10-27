package builtinworkflows

import (
	"io/fs"
	"testing"

	"github.com/google/go-cmp/cmp"
	"gopkg.in/yaml.v3"
)

type wiringStep struct {
	ID            string            `yaml:"id"`
	Workflow      string            `yaml:"workflow"`
	Script        string            `yaml:"script"`
	Params        map[string]string `yaml:"params"`
	SkipIf        string            `yaml:"skip_if"`
	WarnOnFailure bool              `yaml:"warn_on_failure"`
	Steps         []wiringStep      `yaml:"steps"`
}

func loadWiring(t *testing.T, path string) []wiringStep {
	t.Helper()
	body, err := fs.ReadFile(FS, path)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Steps []wiringStep `yaml:"steps"`
	}
	if err := yaml.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}
	return doc.Steps
}

func findWiring(steps []wiringStep, id string) (wiringStep, bool) {
	for _, s := range steps {
		if s.ID == id {
			return s, true
		}
		if got, ok := findWiring(s.Steps, id); ok {
			return got, true
		}
	}
	return wiringStep{}, false
}

func indexOf(steps []wiringStep, id string) int {
	for i, s := range steps {
		if s.ID == id {
			return i
		}
	}
	return -1
}

// Every validation point runs the caller's selected workflow, repairs in the
// session that owns the code at that point, records its own result file, and
// only warns when it fails.
func TestValidationPointsUseSelectedWorkflow(t *testing.T) {
	const selected = "{{validation_workflow}}"
	out := "{{session_dir}}/output/"
	for _, tc := range []struct {
		file, step, session, result string
	}{
		{"core/verify-change-v2.0.yaml", "validate", "implementor-agent", "validation-after-acceptance.txt"},
		{"core/accept-change-v2.0.yaml", "validate", "lead-agent", "validation-after-review.txt"},
		{"core/finalize-pr-v2.0.yaml", "validate", "lead-agent", "validation-ci-fix.txt"},
		{"core/complete-simple-change-v2.0.yaml", "validate-after-testing", "implementor-agent", "validation-after-flow-testing.txt"},
		{"core/complete-simple-change-v2.0.yaml", "validate-after-review", "lead-agent", "validation-after-review.txt"},
	} {
		t.Run(tc.file+"/"+tc.step, func(t *testing.T) {
			step, ok := findWiring(loadWiring(t, tc.file), tc.step)
			if !ok {
				t.Fatalf("%s has no step %s", tc.file, tc.step)
			}
			want := map[string]string{"repair_session": tc.session, "result_file": out + tc.result}
			if step.Workflow != selected || !step.WarnOnFailure || cmp.Diff(want, step.Params) != "" {
				t.Errorf("step = %+v, want workflow %s, warn_on_failure, params %v", step, selected, want)
			}
		})
	}
}

// After validation each stage pushes, re-checks the pull request, and records
// the commits validation added, in that order.
func TestValidationTailsPushAndRecordInOrder(t *testing.T) {
	for _, tc := range []struct {
		file string
		want []string
	}{
		{"core/verify-change-v2.0.yaml", []string{"record-validation-base", "validate", "validation-push", "verify-validation-pr", "record-validation-commits"}},
		{"core/accept-change-v2.0.yaml", []string{"record-validation-base", "validate", "validation-push", "verify-validation-pr", "retest-validation-fixes", "record-untested-commits"}},
	} {
		t.Run(tc.file, func(t *testing.T) {
			steps := loadWiring(t, tc.file)
			prev := -1
			for _, id := range tc.want {
				i := indexOf(steps, id)
				if i < 0 || i <= prev {
					t.Fatalf("step %s at %d (previous %d); order must be %v", id, i, prev, tc.want)
				}
				prev = i
			}
		})
	}
}

// The CI-fix group is skipped by the CI-fix-needed gate alone; its inner steps
// carry no previous_success condition that would read the fixer's outcome.
func TestFinalizeCIFixGroupOrderAndSkip(t *testing.T) {
	group, ok := findWiring(loadWiring(t, "core/finalize-pr-v2.0.yaml"), "ci-fix")
	if !ok {
		t.Fatal("finalize-pr-v2.0 has no ci-fix group")
	}
	if group.SkipIf != "previous_success" {
		t.Errorf("ci-fix skip_if = %q, want previous_success", group.SkipIf)
	}
	var ids []string
	for _, s := range group.Steps {
		ids = append(ids, s.ID)
		if s.SkipIf != "" {
			t.Errorf("inner step %s has skip_if %q", s.ID, s.SkipIf)
		}
	}
	want := []string{"fix-pr", "validate", "ci-fix-push", "verify-ci-fix-pr"}
	if diff := cmp.Diff(want, ids); diff != "" {
		t.Errorf("ci-fix steps (-want +got):\n%s", diff)
	}
}

func TestValidationPointsAbsentFromEarlierStages(t *testing.T) {
	for _, file := range []string{"core/implement-change-v3.0.yaml", "core/implement-task-v2.0.yaml", "core/verify-change-v2.0.yaml"} {
		steps := loadWiring(t, file)
		for _, id := range []string{"run-validator", "acceptance-validator", "validator-retry"} {
			if _, ok := findWiring(steps, id); ok {
				t.Errorf("%s still has step %s", file, id)
			}
		}
	}
}
