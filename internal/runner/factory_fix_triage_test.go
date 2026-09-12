package runner

import (
	"fmt"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/config"
	"github.com/codagent/agent-runner/internal/exec"
	"github.com/codagent/agent-runner/internal/loader"
)

// claudeResultOutput wraps a fixture agent response the way the real Claude
// CLI's result envelope does, so ExecuteAgentStep's response parsing accepts
// it without spawning a live model call. Mirrors internal/exec's unexported
// test helper of the same name.
func claudeResultOutput(result string) string {
	return fmt.Sprintf(`{"type":"result","result":%q}`, result) + "\n"
}

func factoryFixProfiles() *config.Config {
	return &config.Config{ActiveAgents: map[string]*config.Agent{
		"lead":   {DefaultMode: "autonomous", CLI: "claude", Model: "sonnet"},
		"tester": {DefaultMode: "autonomous", CLI: "claude", Model: "sonnet"},
	}}
}

// TestFactoryFixUntilTriageRunsTriageAloneForBothFixableAndDeclinedFixtures
// covers the test plan's fixture-issue "--until triage" obligation using this
// repository's recorded-response mechanism (a canned ProcessResult in place
// of a live model call): the shipped core/factory-fix-v1.0.yaml workflow is
// loaded for real and run only up to its "triage" step for a fixable and a
// declined fixture decision.
func TestFactoryFixUntilTriageRunsTriageAloneForBothFixableAndDeclinedFixtures(t *testing.T) {
	cases := []struct {
		name     string
		decision string
	}{
		{
			name:     "fixable",
			decision: `{"fixable": true, "reasons": [], "plan": "fix the off-by-one and add a regression test"}`,
		},
		{
			name:     "declined",
			decision: `{"fixable": false, "reasons": ["several viable solutions require a human choice"], "plan": ""}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			workflow, err := loader.LoadWorkflow("builtin:core/factory-fix-v1.0.yaml", loader.Options{})
			if err != nil {
				t.Fatalf("load builtin factory-fix workflow: %v", err)
			}

			runner := &mockRunner{results: []exec.ProcessResult{
				{ExitCode: 0}, // check-contract
				{ExitCode: 0}, // check-clean-tree
				{ExitCode: 0, Stdout: claudeResultOutput(tc.decision)}, // triage
			}}
			log := &mockLog{}

			result, err := RunWorkflow(&workflow, map[string]string{
				"issue_file":       "/artifacts/input/issue.json",
				"branch_name":      "factory/fix-212-1a2b3c4d",
				"contract_version": "factory-fix/1",
			}, &Options{
				WorkflowFile:  "builtin:core/factory-fix-v1.0.yaml",
				Until:         "triage",
				SessionDir:    t.TempDir(),
				ProcessRunner: runner,
				GlobExpander:  &mockGlob{},
				ProfileStore:  factoryFixProfiles(),
				Log:           log,
			})
			if err != nil {
				t.Fatalf("RunWorkflow returned error: %v; log:\n%s", err, strings.Join(log.lines, "\n"))
			}
			if result != ResultSuccess {
				t.Fatalf("result = %q, want success; log:\n%s", result, strings.Join(log.lines, "\n"))
			}
			if len(runner.calls) != 3 {
				t.Fatalf("expected exactly 3 calls (check-contract, check-clean-tree, triage), got %d: %v", len(runner.calls), runner.calls)
			}
		})
	}
}
