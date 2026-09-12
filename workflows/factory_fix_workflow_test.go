package builtinworkflows

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type minimalStep struct {
	ID       string            `yaml:"id"`
	Workflow string            `yaml:"workflow"`
	Params   map[string]string `yaml:"params"`
	Steps    []minimalStep     `yaml:"steps"`
}

func findStep(steps []minimalStep, id string) *minimalStep {
	for i := range steps {
		if steps[i].ID == id {
			return &steps[i]
		}
		if found := findStep(steps[i].Steps, id); found != nil {
			return found
		}
	}
	return nil
}

func TestFactoryFixDeclaresItsContractOnTheFirstLine(t *testing.T) {
	data, err := ReadFile("builtin:core/factory-fix-v1.0.yaml")
	if err != nil {
		t.Fatal(err)
	}
	firstLine, _, _ := strings.Cut(string(data), "\n")
	if firstLine != "# factory-contract: factory-fix/1" {
		t.Fatalf("first line = %q, want %q", firstLine, "# factory-contract: factory-fix/1")
	}
}

func TestFactoryFixHasATriageStepUsableWithUntil(t *testing.T) {
	data, err := ReadFile("builtin:core/factory-fix-v1.0.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Steps []minimalStep `yaml:"steps"`
	}
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, step := range workflow.Steps {
		if step.ID == "triage" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected a top-level step with id \"triage\" so --until triage can run triage alone")
	}
}

func TestFactoryFixCallsFinalizePRWithOneCIFixCycle(t *testing.T) {
	data, err := ReadFile("builtin:core/factory-fix-v1.0.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Steps []minimalStep `yaml:"steps"`
	}
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatal(err)
	}
	step := findStep(workflow.Steps, "finalize-pr")
	if step == nil {
		t.Fatal("expected a finalize-pr step")
	}
	if step.Workflow != "finalize-pr-v1.0.yaml" {
		t.Fatalf("finalize-pr step.workflow = %q, want finalize-pr-v1.0.yaml", step.Workflow)
	}
	if step.Params["ci_fix_cycles"] != "1" {
		t.Fatalf("finalize-pr ci_fix_cycles param = %q, want \"1\"", step.Params["ci_fix_cycles"])
	}
}

func TestFinalizePRCIFixCyclesDefaultsToThree(t *testing.T) {
	data, err := ReadFile("builtin:core/finalize-pr-v1.0.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Params []struct {
			Name    string `yaml:"name"`
			Default string `yaml:"default"`
		} `yaml:"params"`
	}
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatal(err)
	}
	for _, p := range workflow.Params {
		if p.Name == "ci_fix_cycles" {
			if p.Default != "3" {
				t.Fatalf("ci_fix_cycles default = %q, want \"3\"", p.Default)
			}
			return
		}
	}
	t.Fatal("finalize-pr-v1.0.yaml has no ci_fix_cycles param")
}

func TestFactoryFixAnnotatesThePRWithTheIssueReferenceAndClaimMarker(t *testing.T) {
	data, err := ReadFile("builtin:core/factory-fix-v1.0.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Steps []minimalStep `yaml:"steps"`
	}
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatal(err)
	}
	step := findStep(workflow.Steps, "annotate-pr")
	if step == nil {
		t.Fatal("expected an annotate-pr step so the factory can find its PR by marker")
	}
	text := string(data)
	for _, needle := range []string{"Refs #", "agent-factory:claim:", "gh pr edit"} {
		if !strings.Contains(text, needle) {
			t.Fatalf("factory-fix workflow does not contain %q", needle)
		}
	}
}
