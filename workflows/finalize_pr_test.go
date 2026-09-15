package builtinworkflows

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestFinalizePRSplitsAssessmentFromMutation(t *testing.T) {
	data, err := ReadFile("builtin:core/finalize-pr-v1.0.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Sessions []struct {
			Name  string `yaml:"name"`
			Agent string `yaml:"agent"`
		} `yaml:"sessions"`
		Steps []struct {
			ID      string `yaml:"id"`
			Session string `yaml:"session"`
			Steps   []struct {
				ID      string `yaml:"id"`
				Session string `yaml:"session"`
			} `yaml:"steps"`
		} `yaml:"steps"`
	}
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatal(err)
	}

	roles := map[string]string{}
	for _, session := range workflow.Sessions {
		roles[session.Name] = session.Agent
	}
	if roles["lead-agent"] != "lead" || roles["implementor-agent"] != "implementor" {
		t.Fatalf("sessions = %#v, want shared lead and implementor sessions", roles)
	}

	owners := map[string]string{}
	for _, step := range workflow.Steps {
		owners[step.ID] = step.Session
		for _, child := range step.Steps {
			owners[child.ID] = child.Session
		}
	}
	for _, id := range []string{"push-pr", "fix-pr"} {
		if owners[id] != "implementor-agent" {
			t.Fatalf("%s session = %q, want implementor-agent", id, owners[id])
		}
	}
	for _, id := range []string{"wait-ci", "verify-final"} {
		if owners[id] != "lead-agent" {
			t.Fatalf("%s session = %q, want lead-agent", id, owners[id])
		}
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
