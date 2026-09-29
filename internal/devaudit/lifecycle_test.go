//go:build dev_audit

package devaudit

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/metrics"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/runlock"
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
	config := "profiles:\n  default:\n    agents:\n      lead:\n        default_mode: interactive\n        cli: claude\n        model: opus\n        effort: high\n      crosscheck:\n        default_mode: autonomous\n        cli: codex\n        model: gpt-5.6-sol\n        effort: low\n"
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
	workflowRef := filepath.Join(".agent-runner", "workflows", "source-v1.0.yaml")
	workflowPath := filepath.Join(project, workflowRef)
	if err := os.MkdirAll(filepath.Dir(workflowPath), 0o700); err != nil {
		t.Fatal(err)
	}
	workflowYAML := []byte("name: source\nsteps: []\n")
	if err := os.WriteFile(workflowPath, workflowYAML, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(workflowPath), "checkpoint.sh"), []byte("echo checkpoint\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := stateio.WriteState(&model.RunState{RunID: "source-run", WorkflowFile: workflowRef, WorkflowName: "source"}, source); err != nil {
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
	if _, err := Replay(source, "session-1", "", func(got Request) error {
		request = got
		return nil
	}); err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
	if request.Project != filepath.Base(project) {
		t.Fatalf("replay source project = %q", request.Project)
	}
	if want := (AgentProvenance{CLI: "claude", Model: "opus", Effort: "high"}); request.Auditor != want {
		t.Fatalf("replay auditor = %#v, want lead agent %#v", request.Auditor, want)
	}
	if _, err := os.Stat(filepath.Join(request.SnapshotPath, "output", "later-session.out")); !os.IsNotExist(err) {
		t.Fatalf("ambiguous later-session output remains in replay snapshot: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(request.SnapshotPath, "source-workflow.yaml")); err != nil || !bytes.Equal(got, workflowYAML) {
		t.Fatalf("replay workflow snapshot = %q, %v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(request.SnapshotPath, "source-workflow", "checkpoint.sh")); err != nil || string(got) != "echo checkpoint\n" {
		t.Fatalf("replay sibling script = %q, %v", got, err)
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

func TestSnapshotWorkflowDefinitionIncludesSiblingFiles(t *testing.T) {
	project := t.TempDir()
	workflowDir := filepath.Join(project, "workflows")
	if err := os.Mkdir(workflowDir, 0o700); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"main.yaml":     "name: main\nsteps: []\n",
		"child.yaml":    "name: child\nsteps: []\n",
		"checkpoint.sh": "#!/bin/sh\necho checkpoint\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(workflowDir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(workflowDir, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("checkpoint.sh", filepath.Join(workflowDir, "linked.sh")); err != nil {
		t.Fatal(err)
	}
	snapshot := t.TempDir()
	t.Cleanup(func() {
		_ = filepath.WalkDir(snapshot, func(path string, _ os.DirEntry, err error) error {
			if err == nil {
				_ = os.Chmod(path, 0o700)
			}
			return nil
		})
	})
	if err := snapshotWorkflowDefinition(filepath.Join("workflows", "main.yaml"), project, snapshot); err != nil {
		t.Fatal(err)
	}
	if err := sealSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	for name, want := range files {
		path := filepath.Join(snapshot, "source-workflow", name)
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatalf("%s = %q, %v; want %q", name, got, err, want)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o400 {
			t.Fatalf("%s mode = %v; want 0400", name, info.Mode())
		}
	}
	for _, name := range []string{"nested", "linked.sh"} {
		if _, err := os.Lstat(filepath.Join(snapshot, "source-workflow", name)); !os.IsNotExist(err) {
			t.Fatalf("%s was copied: %v", name, err)
		}
	}
}

func TestSnapshotWorkflowDefinitionSkipsOversizedDirectory(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "main.yaml"), []byte("name: main\nsteps: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "large.sh"), make([]byte, 2*1024*1024+1), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot := t.TempDir()
	if err := snapshotWorkflowDefinition("main.yaml", project, snapshot); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(snapshot, "source-workflow"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "SKIPPED.txt" {
		t.Fatalf("oversized directory entries = %v; want only SKIPPED.txt", entries)
	}
	if note, err := os.ReadFile(filepath.Join(snapshot, "source-workflow", "SKIPPED.txt")); err != nil || !strings.Contains(string(note), "2 MiB") {
		t.Fatalf("skip note = %q, %v", note, err)
	}
}

func TestSnapshotWorkflowDefinitionUnreadableDirectoryKeepsWorkflow(t *testing.T) {
	project := t.TempDir()
	workflowDir := filepath.Join(project, "workflows")
	if err := os.Mkdir(workflowDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workflowDir, "main.yaml"), []byte("name: main\nsteps: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(workflowDir, 0o100); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(workflowDir, 0o700) })
	if _, err := os.ReadDir(workflowDir); err == nil {
		t.Skip("directory listing remains available under this account")
	}
	snapshot := t.TempDir()
	if err := snapshotWorkflowDefinition(filepath.Join("workflows", "main.yaml"), project, snapshot); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(snapshot, "source-workflow.yaml")); err != nil || string(got) != "name: main\nsteps: []\n" {
		t.Fatalf("source workflow = %q, %v", got, err)
	}
	if note, err := os.ReadFile(filepath.Join(snapshot, "source-workflow", "SKIPPED.txt")); err != nil || !strings.Contains(string(note), "could not be listed") {
		t.Fatalf("skip note = %q, %v", note, err)
	}
}

func TestSnapshotWorkflowDefinitionBuiltinHasNoSiblingDirectory(t *testing.T) {
	snapshot := t.TempDir()
	if err := snapshotWorkflowDefinition("builtin:openspec/change-v1.0.yaml", t.TempDir(), snapshot); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(snapshot, "source-workflow")); !os.IsNotExist(err) {
		t.Fatalf("builtin source-workflow directory: %v", err)
	}
}

func TestReplayRetentionRace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	runsDir := filepath.Join(home, ".agent-runner", "projects", "p", "runs")
	source := filepath.Join(runsDir, "source")
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := stateio.WriteState(&model.RunState{RunID: "source", Completed: true}, source); err != nil {
		t.Fatal(err)
	}
	release, err := runlock.ClaimLinkage(source)
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { _, err := Replay(source, "session", home, func(Request) error { return nil }); result <- err }()
	// The pruner renames the source while it owns the claim. Replay must
	// validate the source path again after the claim becomes available.
	if err := os.Rename(source, filepath.Join(runsDir, ".pruning-source")); err != nil {
		t.Fatal(err)
	}
	release()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("Replay succeeded after source removal")
		}
	case <-time.After(time.Second):
		t.Fatal("Replay did not finish")
	}
	if err := RecordReportingWarning(&Request{SourceSessionDir: source}, "late"); err == nil {
		t.Fatal("link-state update recreated removed source")
	}
	entries, err := os.ReadDir(runsDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != ".pruning-source" {
			t.Fatalf("orphan audit run: %s", entry.Name())
		}
	}
}

func TestReconcileReservedAutomaticAuditUsesOriginalIdentity(t *testing.T) {
	home := t.TempDir()
	t.Cleanup(func() {
		_ = filepath.WalkDir(home, func(path string, entry os.DirEntry, err error) error {
			if err == nil {
				_ = os.Chmod(path, 0o700)
			}
			return nil
		})
	})
	t.Setenv("HOME", home)
	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, ".agent-runner"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".agent-runner", "config.yaml"), []byte("profiles:\n  default:\n    agents:\n      crosscheck:\n        default_mode: autonomous\n        cli: codex\n        model: gpt-5.6-sol\n        effort: low\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	projectState := filepath.Join(home, ".agent-runner", "projects", audit.EncodePath(project))
	source := filepath.Join(projectState, "runs", "source")
	auditDir := filepath.Join(projectState, "runs", "audit-original")
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(auditDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectState, "meta.json"), []byte(`{"path":`+strconv.Quote(project)+`}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := stateio.WriteState(&model.RunState{RunID: "source", WorkflowFile: "builtin:openspec/change-v1.0.yaml", WorkflowName: "change"}, source); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(source, metrics.FileName), metrics.Artifact{Sessions: []metrics.SessionRecord{{ExecutionSessionID: "session"}}})
	if err := stateio.WriteState(&model.RunState{RunID: "audit-original", Audit: &model.AuditMetadata{}}, auditDir); err != nil {
		t.Fatal(err)
	}
	snapshot := filepath.Join(auditDir, "snapshot")
	if err := os.MkdirAll(snapshot, 0o700); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(source, lifecycleFileName), Lifecycle{Version: 1, SourceRunID: "source", Links: []Link{{AuditRunID: "audit-original", ExecutionSessionID: "session", Trigger: "automatic", State: LaunchReserved, SnapshotPath: snapshot}}})

	var launched Request
	id, err := Reconcile(source, "session", "", func(request Request) error { launched = request; return nil })
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if id != "audit-original" || launched.AuditRunID != "audit-original" {
		t.Fatalf("reconcile identity = %q, launched %#v", id, launched)
	}
	lifecycle, err := ReadLifecycle(filepath.Join(source, lifecycleFileName))
	if err != nil {
		t.Fatal(err)
	}
	if lifecycle.Links[0].State != LaunchStarted {
		t.Fatalf("lifecycle state = %q, want started", lifecycle.Links[0].State)
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
	if data, err := os.ReadFile(filepath.Join(link.SnapshotPath, "source-workflow.yaml")); err != nil || !strings.Contains(string(data), "name: change") {
		t.Fatalf("automatic workflow snapshot = %q, %v", data, err)
	}
}

func TestCoordinatorOnlyAuditsTopLevelCanonicalWorkflowNamespaces(t *testing.T) {
	for _, test := range []struct {
		name    string
		summary runner.PostFinalizationSummary
		want    bool
	}{
		{"openspec", runner.PostFinalizationSummary{WorkflowFile: "builtin:openspec/change-v1.0.yaml", ExecutionSessionID: "session", Result: runner.ResultSuccess, TopLevel: true}, true},
		{"user stopped", runner.PostFinalizationSummary{WorkflowFile: "builtin:openspec/change-v1.0.yaml", ExecutionSessionID: "session", Result: runner.ResultStopped, TopLevel: true}, false},
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

func TestSnapshotRunnerSourceKeepsUnavailableGitMetadataDistinctFromBuildDiagnostics(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"cmd/agent-runner", "internal/runner", "workflows"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module github.com/codagent/agent-runner\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	originalRoot, originalEncoded, originalRevision, originalDirty := BuildRoot, BuildRootEncoded, BuildRevision, BuildDirty
	BuildRoot, BuildRootEncoded, BuildRevision, BuildDirty = root, "", "build-revision", "true"
	t.Cleanup(func() {
		BuildRoot, BuildRootEncoded, BuildRevision, BuildDirty = originalRoot, originalEncoded, originalRevision, originalDirty
	})

	got := snapshotRunnerSource(t.TempDir())
	if !got.Verified || got.Coverage != "complete" {
		t.Fatalf("source coverage = %#v, want complete verified snapshot", got)
	}
	if got.LaunchGitAvailable {
		t.Fatalf("launch Git availability = true, want false for non-Git source: %#v", got)
	}
	if got.LaunchRevision != "" || got.LaunchDirty != "" {
		t.Fatalf("unavailable launch Git metadata = revision %q dirty %q, want empty", got.LaunchRevision, got.LaunchDirty)
	}
	if got.BuildRevision != "build-revision" || got.BuildDirty != "true" {
		t.Fatalf("build diagnostics unexpectedly changed: %#v", got)
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

func TestReplayUsesExplicitProjectForCustomSessionDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, ".agent-runner"), 0o700); err != nil {
		t.Fatal(err)
	}
	config := "profiles:\n  factory:\n    agents:\n      lead:\n        default_mode: autonomous\n        cli: claude\n        model: opus\n        effort: high\n"
	if err := os.WriteFile(filepath.Join(project, ".agent-runner", "config.yaml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	// Factory runs pass --session-dir, so the session is not under a recorded
	// project's runs directory.
	source := filepath.Join(t.TempDir(), "attempt-1", "agent-runner-session")
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = filepath.WalkDir(filepath.Dir(source), func(path string, entry os.DirEntry, err error) error {
			if err == nil {
				_ = os.Chmod(path, 0o700)
			}
			return nil
		})
	})
	if err := stateio.WriteState(&model.RunState{RunID: "agent-runner-session", WorkflowFile: ".agent-runner/workflows/factory-fix-v1.0.yaml", WorkflowName: "factory-fix", ProfileSet: "factory"}, source); err != nil {
		t.Fatal(err)
	}
	artifact := metrics.Artifact{
		SchemaVersion: metrics.SchemaVersion, RunID: "agent-runner-session", Workflow: "factory-fix",
		Sessions: []metrics.SessionRecord{{ExecutionSessionID: "session", Status: metrics.SessionClosed}},
		Steps:    []metrics.StepRecord{{RecordID: "first", ID: "implement", Kind: "step", Type: "agent", ExecutionSessionID: "session"}},
	}
	data, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, metrics.FileName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	launch := func(Request) error { return nil }
	if _, err := Replay(source, "session", "", launch); err == nil || !strings.Contains(err.Error(), "recorded source project is unavailable") {
		t.Fatalf("Replay() without project error = %v", err)
	}
	var request Request
	if _, err := Replay(source, "session", project, func(got Request) error { request = got; return nil }); err != nil {
		t.Fatalf("Replay() with project error = %v", err)
	}
	if want := (AgentProvenance{CLI: "claude", Model: "opus", Effort: "high"}); request.Auditor != want {
		t.Fatalf("replay auditor = %#v, want factory lead %#v", request.Auditor, want)
	}
	if filepath.Dir(request.AuditSessionDir) != filepath.Dir(source) {
		t.Fatalf("audit session %q is not beside the source session", request.AuditSessionDir)
	}
}

func TestReplayArgsAcceptProjectInAnyPosition(t *testing.T) {
	for _, args := range [][]string{
		{"run", "--session", "s", "--project", "/p"},
		{"--project", "/p", "run", "--session", "s"},
	} {
		source, session, project, ok := replayArgs(args)
		if !ok || source != "run" || session != "s" || project != "/p" {
			t.Fatalf("replayArgs(%q) = %q %q %q %v", args, source, session, project, ok)
		}
	}
	if _, _, _, ok := replayArgs([]string{"run"}); ok {
		t.Fatal("replayArgs accepted a missing session")
	}
}
