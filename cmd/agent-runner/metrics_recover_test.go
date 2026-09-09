package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/runlock"
	"github.com/codagent/agent-runner/internal/stateio"
)

func TestMetricsRecoverCompletedRunDoesNotExecuteWorkflow(t *testing.T) {
	// Use the existing home in read-only fashion; run discovery gets an isolated
	// project because changing HOME would alter unrelated tool resolution.
	project := t.TempDir()
	t.Chdir(project)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	projectDir := filepath.Join(home, ".agent-runner", "projects", audit.EncodePath(project))
	t.Cleanup(func() { _ = os.RemoveAll(projectDir) })
	dir := filepath.Join(projectDir, "runs", "recovery-test")
	if err = stateio.WriteState(&model.RunState{RunID: "recovery-test", WorkflowName: "missing-workflow", Completed: true}, dir); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(dir, "state.json"))
	if code := dispatchRunCommand([]string{"metrics", "recover", "recovery-test"}, &commandFlags{}); code != 0 {
		t.Fatalf("recovery command failed: %d", code)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "state.json"))
	if !bytes.Equal(before, after) {
		t.Fatal("recovery mutated workflow state")
	}
	if _, err := runlock.Acquire(dir); err != nil {
		t.Fatal(err)
	}
	defer runlock.Delete(dir)
	if code := dispatchRunCommand([]string{"metrics", "recover", "recovery-test"}, &commandFlags{}); code == 0 {
		t.Fatal("recovery ignored active run lock")
	}
}
