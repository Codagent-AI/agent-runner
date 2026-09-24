package exec

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/codagent/agent-runner/internal/model"
)

func TestExecuteSubWorkflowStepRerunRepairReplaysTarget(t *testing.T) {
	dir := t.TempDir()
	childYAML := `name: child
steps:
  - id: open-draft-pr
    prompt: open it
    mode: autonomous
    agent: test-agent
    session: new
  - id: verify-draft-pr
    command: exit 1
    repair:
      rerun: open-draft-pr
      max: 1
`
	childPath := filepath.Join(dir, "child-v1.0.yaml")
	if err := os.WriteFile(childPath, []byte(childYAML), 0o600); err != nil {
		t.Fatal(err)
	}

	runner := &mockRunner{results: []ProcessResult{
		{ExitCode: 0, Stdout: claudeUsageOutput("opened draft", 0)},
		{ExitCode: 1, Stderr: "not open"},
		{ExitCode: 0, Stdout: claudeUsageOutput("opened ready", 0)},
		{ExitCode: 0, Stdout: "pr open"},
	}}
	ctx := model.NewRootContext(&model.RootContextOptions{
		Params: map[string]string{}, WorkflowFile: filepath.Join(dir, "parent-v1.0.yaml"),
	})
	step := model.Step{ID: "sub", Workflow: "child-v1.0.yaml", Session: model.SessionNew}

	outcome, err := ExecuteSubWorkflowStep(&step, ctx, runner, &mockGlob{}, &mockLogger{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome != OutcomeSuccess {
		t.Fatalf("expected success, got %q", outcome)
	}
	if len(runner.calls) != 4 {
		t.Fatalf("expected 4 process calls, got %d", len(runner.calls))
	}
}
