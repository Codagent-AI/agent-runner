//go:build dev_audit

package devaudit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/runner"
	"github.com/codagent/agent-runner/internal/stateio"
)

// E2E-002 covers the durable automatic-audit identity that resumes/replays
// build on: repeated finalization for one execution session never launches a
// second audit.
func TestE2E002CoordinatorReservesOneEligibleAuditPerExecutionSession(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(func() {
		_ = filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
			if err == nil {
				_ = os.Chmod(path, 0o700)
			}
			return nil
		})
	})
	if err := stateio.WriteState(&model.RunState{RunID: "source-run", Completed: true}, dir); err != nil {
		t.Fatalf("seed state: %v", err)
	}
	coordinator := Coordinator{Launcher: func(Request) error { return nil }}
	summary := runner.PostFinalizationSummary{
		RunID: "source-run", ExecutionSessionID: "execution-1", SessionDir: dir,
		WorkflowFile: "builtin:openspec/change-v1.0.yaml", WorkflowName: "change",
		Result: runner.ResultSuccess, TopLevel: true,
	}

	if err := coordinator.AfterFinalization(summary); err != nil {
		t.Fatalf("first post-finalization: %v", err)
	}
	if err := coordinator.AfterFinalization(summary); err != nil {
		t.Fatalf("duplicate post-finalization: %v", err)
	}

	state, err := ReadLifecycle(filepath.Join(dir, lifecycleFileName))
	if err != nil {
		t.Fatalf("read lifecycle: %v", err)
	}
	if len(state.Links) != 1 {
		t.Fatalf("reservation count = %d, want 1", len(state.Links))
	}
	link := state.Links[0]
	if link.State != LaunchStarted || link.AuditRunID == "" || link.SnapshotPath == "" {
		t.Fatalf("link = %#v, want started audit with ID and snapshot", link)
	}
}

func TestCoordinatorOnlyAuditsTopLevelCanonicalWorkflowNamespaces(t *testing.T) {
	for _, test := range []struct {
		name    string
		summary runner.PostFinalizationSummary
		want    bool
	}{
		{"openspec", runner.PostFinalizationSummary{WorkflowFile: "builtin:openspec/change-v1.0.yaml", ExecutionSessionID: "session", Result: runner.ResultStopped, TopLevel: true}, true},
		{"spec driven", runner.PostFinalizationSummary{WorkflowFile: "builtin:spec-driven/change-v1.0.yaml", ExecutionSessionID: "session", Result: runner.ResultFailed, TopLevel: true}, true},
		{"nested", runner.PostFinalizationSummary{WorkflowFile: "builtin:openspec/change-v1.0.yaml", Result: runner.ResultSuccess}, false},
		{"unrelated", runner.PostFinalizationSummary{WorkflowFile: "builtin:core/intake-v1.0.yaml", Result: runner.ResultSuccess, TopLevel: true}, false},
		{"audit", runner.PostFinalizationSummary{WorkflowFile: "builtin:audit/run-audit-v1.0.yaml", Result: runner.ResultSuccess, TopLevel: true}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := Eligible(test.summary); got != test.want {
				t.Fatalf("Eligible() = %v, want %v", got, test.want)
			}
		})
	}
}
