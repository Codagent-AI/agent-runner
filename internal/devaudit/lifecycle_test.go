//go:build dev_audit

package devaudit

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/metrics"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/runner"
	"github.com/codagent/agent-runner/internal/stateio"
)

func TestReplayExcludesEvidenceWithoutHistoricalSessionOwnership(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, ".agent-runner"), 0o700); err != nil {
		t.Fatal(err)
	}
	config := "profiles:\n  default:\n    agents:\n      crosscheck:\n        default_mode: autonomous\n        cli: codex\n        model: gpt-5.6-sol\n        effort: low\n"
	if err := os.WriteFile(filepath.Join(project, ".agent-runner", "config.yaml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	projectStateDir := filepath.Join(home, ".agent-runner", "projects", audit.EncodePath(project))
	if err := os.MkdirAll(filepath.Join(projectStateDir, "runs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectStateDir, "meta.json"), []byte(`{"path":`+strconv.Quote(project)+`}`), 0o600); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(projectStateDir, "runs")
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
	t.Chdir(t.TempDir())
	if err := Replay(source, "session-1", func(got Request) error {
		request = got
		return nil
	}); err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
	if request.Project != filepath.Base(project) || request.Crosscheck.CLI != "codex" {
		t.Fatalf("replay source context = project %q crosscheck %#v", request.Project, request.Crosscheck)
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

func TestCopySourceTreeOmitsGitIgnoredArtifactsAndVCSMetadata(t *testing.T) {
	source := t.TempDir()
	for _, args := range [][]string{{"init"}, {"config", "user.email", "audit@example.test"}, {"config", "user.name", "Audit Test"}} {
		if output, err := exec.Command("git", append([]string{"-C", source}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	if err := os.WriteFile(filepath.Join(source, "go.mod"), []byte("module github.com/codagent/agent-runner\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"cmd/agent-runner", "internal/runner", "workflows"} {
		if err := os.MkdirAll(filepath.Join(source, path), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(source, "cmd", "agent-runner", "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "internal", "runner", "runner.go"), []byte("package runner\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "workflows", "example.yaml"), []byte("name: example\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitignore := ".validator/cache/\nbin/\nvalidator_logs/\nartifacts/\nworktrees/\n"
	if err := os.WriteFile(filepath.Join(source, ".gitignore"), []byte(gitignore), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "."}, {"commit", "-m", "tracked source"}} {
		if output, err := exec.Command("git", append([]string{"-C", source}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	if err := os.MkdirAll(filepath.Join(source, ".validator", "cache", "mod"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, ".validator", "cache", "mod", "cache.dat"), []byte("build cache"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "internal", "runner", "untracked.go"), []byte("package runner\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(source, "worktrees", "other"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "worktrees", "other", "file.go"), []byte("package other\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	destination := t.TempDir()
	if err := copySourceTree(source, destination); err != nil {
		t.Fatalf("copySourceTree() error = %v", err)
	}

	for _, path := range []string{"go.mod", "cmd/agent-runner/main.go", "internal/runner/runner.go", "workflows/example.yaml", "internal/runner/untracked.go"} {
		if _, err := os.Stat(filepath.Join(destination, path)); err != nil {
			t.Fatalf("snapshot missing %s: %v", path, err)
		}
	}
	for _, path := range []string{".git", "worktrees", ".validator/cache", ".validator/cache/mod/cache.dat"} {
		if _, err := os.Stat(filepath.Join(destination, path)); !os.IsNotExist(err) {
			t.Fatalf("snapshot includes %s: %v", path, err)
		}
	}
	if !runnerSnapshotComplete(destination) {
		t.Fatal("runnerSnapshotComplete() = false, want true")
	}
}

func TestCopySourceTreeFailsClosedWhenGitListingUnavailable(t *testing.T) {
	source := t.TempDir()
	if err := os.MkdirAll(filepath.Join(source, ".validator", "cache"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, ".validator", "cache", "cache.dat"), []byte("build cache"), 0o600); err != nil {
		t.Fatal(err)
	}
	destination := t.TempDir()
	err := copySourceTree(source, destination)
	if err == nil {
		t.Fatal("copySourceTree() error = nil, want listing failure")
	}
	if !strings.Contains(err.Error(), "build source-tree filters") {
		t.Fatalf("copySourceTree() error = %v, want filter construction failure", err)
	}
	if _, statErr := os.Stat(filepath.Join(destination, ".validator", "cache", "cache.dat")); !os.IsNotExist(statErr) {
		t.Fatalf("snapshot copied ignored cache after listing failure: %v", statErr)
	}
}
