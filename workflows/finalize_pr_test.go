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
