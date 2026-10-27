package prevalidate

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/config"
)

func implementChangeParams(validation string) map[string]string {
	params := map[string]string{
		"change_name":                     "demo",
		"change_dir":                      "openspec/changes/demo",
		"change_label":                    "OpenSpec change",
		"change_kind":                     "openspec",
		"artifact_validation_instruction": "x",
	}
	if validation != "" {
		params["validation_workflow"] = validation
	}
	return params
}

func validationAgentsConfig() *config.Config {
	agent := func() *config.Agent {
		return &config.Agent{DefaultMode: "autonomous", CLI: "claude", Model: "sonnet", Effort: "high"}
	}
	return &config.Config{ActiveAgents: map[string]*config.Agent{
		"implementor": agent(), "lead": agent(), "tester": agent(),
	}}
}

func supplied(params map[string]string) []string {
	var names []string
	for name := range params {
		names = append(names, name)
	}
	return names
}

func TestCallerTargetsValidatesSuppliedSelection(t *testing.T) {
	const root = "builtin:core/implement-change-v3.0.yaml"
	absValid := writeWorkflow(t, t.TempDir(), "mine-v1.0.yaml", `
name: mine
params:
  - name: repair_session
    required: false
  - name: task_file
    required: false
  - name: task_compliance
    required: false
  - name: result_file
    required: false
steps:
  - id: ok
    command: "true"
`)
	absInvalid := writeWorkflow(t, t.TempDir(), "broken-v1.0.yaml", `
name: broken
steps:
  - id: bad
    workflow: nowhere-v1.0.yaml
`)

	for _, tc := range []struct {
		name      string
		selection string
		wantErr   string
	}{
		{"unset", "", ""},
		{"auto", "auto", ""},
		{"none", "none", ""},
		{"agent-validator", "agent-validator", ""},
		{"valid builtin reference", "builtin:core/validation-none-v1.0.yaml", ""},
		{"valid absolute path", absValid, ""},
		{"missing absolute path", filepath.Join(t.TempDir(), "missing-v1.0.yaml"), "missing-v1.0.yaml"},
		{"invalid absolute path", absInvalid, "nowhere-v1.0.yaml"},
		{"unknown name", "foo", "foo"},
		{"incompatible builtin", "builtin:core/verify-change-v1.0.yaml", "verify-change-v1.0.yaml"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts, _, _ := fakeOptions(t, validationAgentsConfig())
			params := implementChangeParams(tc.selection)
			_, err := CallerTargets(root, params, supplied(params), opts)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("CallerTargets: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want one mentioning %q", err, tc.wantErr)
			}
		})
	}
}

func TestCallerTargetsWithoutSuppliedParamsValidatesNothing(t *testing.T) {
	opts, _, _ := fakeOptions(t, validationAgentsConfig())
	if _, err := CallerTargets("builtin:core/implement-change-v3.0.yaml", map[string]string{"validation_workflow": "foo"}, nil, opts); err != nil {
		t.Fatalf("CallerTargets with nothing supplied: %v", err)
	}
}

// Caller-derived provenance flows from change-v3 through implement-change-v3
// to verify-change-v2, where the selection is finally used; alias binding at
// the top does not hide it.
func TestCallerTargetsFollowsProvenanceThroughNestedHelpers(t *testing.T) {
	const root = "builtin:openspec/change-v3.0.yaml"
	for _, tc := range []struct{ name, selection, wantErr string }{
		{"alias", "none", ""},
		{"unknown", "foo", "foo"},
		{"incompatible", "builtin:core/verify-change-v1.0.yaml", "verify-change-v1.0.yaml"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts, _, _ := fakeOptions(t, validationAgentsConfig())
			params := map[string]string{"change_name": "demo", "validation_workflow": tc.selection}
			_, err := CallerTargets(root, params, supplied(params), opts)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("CallerTargets: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want one mentioning %q", err, tc.wantErr)
			}
		})
	}
}

// A custom validation workflow declaring a named session that conflicts with
// the caller's declaration is rejected before the run starts.
func TestCallerTargetsRejectsConflictingNamedSession(t *testing.T) {
	conflicting := writeWorkflow(t, t.TempDir(), "clash-v1.0.yaml", `
name: clash
params:
  - name: repair_session
    required: false
  - name: result_file
    required: false
sessions:
  - name: implementor-agent
    agent: lead
steps:
  - id: ok
    command: "true"
`)
	opts, _, _ := fakeOptions(t, validationAgentsConfig())
	params := implementChangeParams(conflicting)
	_, err := CallerTargets("builtin:core/implement-change-v3.0.yaml", params, supplied(params), opts)
	if err == nil || !strings.Contains(err.Error(), "incompatible named session declaration") {
		t.Fatalf("error = %v, want a session conflict", err)
	}
}
