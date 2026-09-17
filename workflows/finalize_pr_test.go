package builtinworkflows

import (
	"testing"

	"gopkg.in/yaml.v3"
)

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

func TestFinalizePRLoopAgentStepsUseLeadAgentSession(t *testing.T) {
	data, err := ReadFile("builtin:core/finalize-pr-v1.0.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Steps []struct {
			ID    string `yaml:"id"`
			Steps []struct {
				ID      string `yaml:"id"`
				Session string `yaml:"session"`
			} `yaml:"steps"`
		} `yaml:"steps"`
	}
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatal(err)
	}

	want := map[string]string{
		"wait-ci": "lead-agent",
		"fix-pr":  "lead-agent",
	}
	found := map[string]string{}
	for _, step := range workflow.Steps {
		if step.ID != "ci-fix-loop" {
			continue
		}
		for _, inner := range step.Steps {
			if _, ok := want[inner.ID]; ok {
				found[inner.ID] = inner.Session
			}
		}
	}
	for id, session := range want {
		got, ok := found[id]
		if !ok {
			t.Errorf("ci-fix-loop has no %s step", id)
			continue
		}
		if got != session {
			t.Errorf("%s session = %q, want %q", id, got, session)
		}
	}
}
