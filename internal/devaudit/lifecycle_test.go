//go:build dev_audit

package devaudit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/codagent/agent-runner/internal/metrics"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/runner"
	"github.com/codagent/agent-runner/internal/stateio"
)

func TestReplayExcludesEvidenceWithoutHistoricalSessionOwnership(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	t.Cleanup(func() {
		_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err == nil {
				_ = os.Chmod(path, 0o700)
			}
			return nil
		})
	})
	source := filepath.Join(root, "source-run")
	if err := os.MkdirAll(filepath.Join(source, "output"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := stateio.WriteState(&model.RunState{RunID: "source-run", WorkflowFile: "builtin:openspec/change-v1.0.yaml", WorkflowName: "change"}, source); err != nil {
		t.Fatal(err)
	}
	artifact := metrics.Artifact{
		SchemaVersion: metrics.SchemaVersion,
		RunID:         "source-run",
		Workflow:      "builtin:openspec/change-v1.0.yaml",
		Sessions: []metrics.SessionRecord{
			{ExecutionSessionID: "session-1", Status: metrics.SessionClosed},
			{ExecutionSessionID: "session-2", Status: metrics.SessionClosed},
		},
		Steps: []metrics.StepRecord{
			{RecordID: "first", ID: "implement", Kind: "step", Type: "agent", ExecutionSessionID: "session-1"},
			{RecordID: "later", ID: "verify", Kind: "step", Type: "agent", ExecutionSessionID: "session-2"},
		},
		SessionRollups: []metrics.SessionRollup{
			{ExecutionSessionID: "session-1"},
			{ExecutionSessionID: "session-2"},
		},
	}
	data, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, metrics.FileName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "output", "later-session.out"), []byte("later session detail"), 0o600); err != nil {
		t.Fatal(err)
	}

	var request Request
	if err := Replay(source, "session-1", func(got Request) error {
		request = got
		return nil
	}); err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(request.SnapshotPath, "output", "later-session.out")); !os.IsNotExist(err) {
		t.Fatalf("ambiguous later-session output remains in replay snapshot: %v", err)
	}
	projected, err := readMetrics(filepath.Join(request.SnapshotPath, metrics.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if len(projected.Sessions) != 1 || projected.Sessions[0].ExecutionSessionID != "session-1" {
		t.Fatalf("projected sessions = %#v, want selected historical session only", projected.Sessions)
	}
	if len(projected.Steps) != 1 || projected.Steps[0].ExecutionSessionID != "session-1" {
		t.Fatalf("projected steps = %#v, want selected historical session only", projected.Steps)
	}
	prepared, err := PrepareEvidence(request)
	if err != nil {
		t.Fatal(err)
	}
	for _, category := range []string{"audit_log", "validation", "artifact", "narrative", "native_session"} {
		count := 0
		for _, reference := range prepared.Index.References {
			if reference.Category == category {
				count++
				if reference.Status != "unavailable" {
					t.Fatalf("replay %s reference = %#v, want unavailable", category, reference)
				}
			}
		}
		if count != 1 {
			t.Fatalf("replay %s reference count = %d, want 1", category, count)
		}
	}
}

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
			if got := Eligible(&test.summary); got != test.want {
				t.Fatalf("Eligible() = %v, want %v", got, test.want)
			}
		})
	}
}
