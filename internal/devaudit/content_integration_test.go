//go:build dev_audit

package devaudit

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codagent/agent-runner/internal/audit"
	iexec "github.com/codagent/agent-runner/internal/exec"
	"github.com/codagent/agent-runner/internal/metrics"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/runner"
)

func contentGit(t *testing.T, repo string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	data, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, data)
	}
	return strings.TrimSpace(string(data))
}
func contentRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	contentGit(t, repo, "init")
	contentGit(t, repo, "config", "user.email", "test@example.com")
	contentGit(t, repo, "config", "user.name", "Test")
	_ = os.WriteFile(filepath.Join(repo, "file"), []byte("base\n"), 0o600)
	contentGit(t, repo, "add", ".")
	contentGit(t, repo, "commit", "-m", "initial")
	return repo
}

func TestINT003ReplayContentOwnershipAndDeferredPatches(t *testing.T) {
	repo := contentRepo(t)
	source := t.TempDir()
	_ = os.Mkdir(filepath.Join(source, "output"), 0o700)
	logger, err := audit.NewLogger(filepath.Join(source, "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()
	logger.Emit(audit.Event{Timestamp: "now", Type: audit.EventStepEnd, Data: map[string]any{"stdout": "unowned should be dropped"}})
	collector := metrics.NewCollector(source, "source", "content", time.Now())
	run := func(session string, body func(func(string, string, string, func()))) {
		pipeline := metrics.NewExecutionPipeline(collector, logger, repo, session, source)
		pipeline.Emit(audit.Event{Timestamp: "2026-10-01T00:00:00Z", Type: audit.EventRunStart})
		step := func(id, mode, stdout string, action func()) {
			identity := model.ExecutionIdentity{StepID: id, StepType: "agent", Kind: "step"}
			prefix := "[" + id + "]"
			command := "test"
			if id == "validator" {
				command = "agent-validator run"
				identity.StepType = "script"
			}
			pipeline.Emit(audit.Event{Timestamp: "2026-10-01T00:00:01Z", Type: audit.EventStepStart, Prefix: prefix, Data: map[string]any{"mode": mode, "script": command, "identity": identity}})
			action()
			pipeline.Emit(audit.Event{Timestamp: "2026-10-01T00:00:02Z", Type: audit.EventStepEnd, Prefix: prefix, Data: map[string]any{"stdout": stdout, "outcome": "success", "identity": identity}})
		}
		body(step)
		pipeline.Emit(audit.Event{Timestamp: "2026-10-01T00:00:03Z", Type: audit.EventRunEnd, Data: map[string]any{"outcome": "success"}})
	}
	ledger := filepath.Join(source, "output", "ledger.md")
	run("one", func(step func(string, string, string, func())) {
		step("audit", "headless", "session one finding", func() { _ = os.WriteFile(ledger, []byte("session one ledger\n"), 0o600) })
		step("audit-fix", "headless", "fixed finding", func() {
			_ = os.WriteFile(filepath.Join(repo, "file"), []byte("fixed\n"), 0o600)
			contentGit(t, repo, "add", ".")
			contentGit(t, repo, "commit", "-m", "[audit-fix] fix: repair")
		})
		step("implement", "headless", "implemented change", func() { _ = os.WriteFile(filepath.Join(repo, "file"), []byte("implemented\n"), 0o600) })
		step("commit-all", "headless", "committed", func() {
			_ = os.WriteFile(filepath.Join(repo, "own"), []byte("own change\n"), 0o600)
			contentGit(t, repo, "add", ".")
			contentGit(t, repo, "commit", "-m", "[commit-all] feat: commit changes")
		})
		step("interactive", "interactive", "", func() {})
		step("validator", "headless", "validation complete", func() {})
	})
	run("two", func(step func(string, string, string, func())) {
		step("audit", "headless", "wrong session two", func() { _ = os.WriteFile(ledger, []byte("wrong session ledger"), 0o600) })
	})
	run("three", func(step func(string, string, string, func())) {
		step("unrelated", "headless", "wrong session three", func() {})
	})
	snapshot, err := snapshotReplayEvidenceAt(source, filepath.Join(t.TempDir(), "snapshot"), "one")
	if err != nil {
		t.Fatal(err)
	}
	if err = exportGitEvidence(repo, snapshot); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = filepath.WalkDir(snapshot, func(path string, entry os.DirEntry, err error) error {
			if err == nil && entry.IsDir() {
				_ = os.Chmod(path, 0o700)
			}
			return nil
		})
	})
	if err = sealSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	log, _ := os.ReadFile(filepath.Join(snapshot, "audit.log"))
	if strings.Contains(string(log), "wrong session") || strings.Contains(string(log), "unowned should be dropped") {
		t.Fatal(string(log))
	}
	request := Request{AuditRunID: "audit", AuditSessionDir: t.TempDir(), SnapshotPath: snapshot, SourceRunID: "source", ExecutionSessionID: "one", SourceWorkflow: "content", Trigger: "replay"}
	prepared, err := PrepareEvidence(request)
	if err != nil {
		t.Fatal(err)
	}
	leaves := map[string]LeafEvidence{}
	for _, leaf := range prepared.Index.Leaves {
		leaves[leaf.Skeleton.StepID] = leaf
	}
	has := func(id, kind, text string) {
		t.Helper()
		for _, r := range leaves[id].Evidence {
			if (r.Contribution == kind || r.Category == kind) && strings.Contains(r.Detail, text) {
				return
			}
		}
		t.Fatalf("%s missing %s %q: %+v", id, kind, text, leaves[id])
	}
	has("audit", "narrative", "session one finding")
	has("audit", "created", "session one ledger")
	has("audit-fix", "commit_patch", "fixed")
	has("implement", "diff", "+implemented")
	has("implement", "commit_patch_confirmation", "+implemented")
	has("commit-all", "commit_patch", "own change")
	has("interactive", "narrative", "interactive step")
	has("validator", "validation", "validation complete")
	for _, ref := range leaves["commit-all"].Evidence {
		if ref.Contribution == "commit_patch" && strings.Contains(ref.Detail, "+implemented") {
			t.Fatal("deferred content credited to committer")
		}
	}
	blobs, _ := filepath.Glob(filepath.Join(snapshot, "step-evidence", "blobs", "sha256", "*"))
	for _, path := range blobs {
		data, _ := os.ReadFile(path)
		if strings.Contains(string(data), "wrong session") {
			t.Fatal("later blob copied")
		}
	}
}

func TestINT004AutomaticSnapshotRetainsContentAndBounds(t *testing.T) {
	repo := contentRepo(t)
	source := t.TempDir()
	_ = os.Mkdir(filepath.Join(source, "output"), 0o700)
	logger, _ := audit.NewLogger(filepath.Join(source, "audit.log"))
	defer logger.Close()
	collector := metrics.NewCollector(source, "source", "content", time.Now())
	pipeline := metrics.NewExecutionPipeline(collector, logger, repo, "one", source)
	pipeline.Emit(audit.Event{Timestamp: "2026-10-01T00:00:00Z", Type: audit.EventRunStart})
	for attempt := 0; attempt < 50; attempt++ {
		identity := model.ExecutionIdentity{StepID: "heavy", StepType: "agent", Kind: "step"}
		pipeline.Emit(audit.Event{Type: audit.EventStepStart, Timestamp: "now", Prefix: "[heavy]", Data: map[string]any{"mode": "headless", "identity": identity}})
		if attempt == 0 {
			for i := 0; i < 2000; i++ {
				_ = os.WriteFile(filepath.Join(source, "output", fmt.Sprintf("%04d.md", i)), []byte("own contribution"), 0o600)
			}
		}
		pipeline.Emit(audit.Event{Type: audit.EventStepEnd, Timestamp: "now", Prefix: "[heavy]", Data: map[string]any{"identity": identity, "stdout": "short final response", "outcome": "success"}})
	}

	step := func(id, output string, action func()) {
		identity := model.ExecutionIdentity{StepID: id, StepType: "agent", Kind: "step"}
		pipeline.Emit(audit.Event{Type: audit.EventStepStart, Timestamp: "now", Prefix: "[" + id + "]", Data: map[string]any{"mode": "autonomous", "identity": identity}})
		action()
		pipeline.Emit(audit.Event{Type: audit.EventStepEnd, Timestamp: "now", Prefix: "[" + id + "]", Data: map[string]any{"identity": identity, "stdout": output, "outcome": "success"}})
	}
	step("large-output", strings.Repeat("large output é ", 10000), func() {})
	step("detector", "detected defect", func() {
		f, e := os.OpenFile(filepath.Join(source, "output", "0000.md"), os.O_WRONLY|os.O_APPEND, 0o600)
		if e != nil {
			t.Fatal(e)
		}
		_, _ = f.WriteString("detector contribution")
		_ = f.Close()
	})
	step("fixer", "corrected defect", func() {
		_ = os.WriteFile(filepath.Join(repo, "file"), []byte("corrected\n"), 0o600)
		contentGit(t, repo, "add", ".")
		contentGit(t, repo, "commit", "-m", "[fixer] fix: correct defect")
	})
	pipeline.Emit(audit.Event{Timestamp: "now", Type: audit.EventRunEnd, Data: map[string]any{"outcome": "success"}})
	snapshot, err := snapshotEvidenceForProject(source, "automatic-content", repo)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = filepath.WalkDir(snapshot, func(path string, entry os.DirEntry, err error) error {
			if err == nil && entry.IsDir() {
				_ = os.Chmod(path, 0o700)
			}
			return nil
		})
	})
	if err = sealSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(source, "output", "0000.md"), []byte("mutated"), 0o600)
	prepared, err := PrepareEvidence(Request{AuditRunID: "audit", AuditSessionDir: t.TempDir(), SnapshotPath: snapshot, SourceRunID: "source", ExecutionSessionID: "one", Trigger: "automatic"})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Index.SchemaVersion != 2 {
		t.Fatal(prepared.Index.SchemaVersion)
	}
	for _, pkg := range prepared.Packages {
		if encodedJSONBytes(pkg) > defaultPackageBytes || encodedJSONBytes(valueOutputSchema(pkg)) > defaultSchemaBytes {
			t.Fatal("package bounds")
		}
		for _, leaf := range pkg.Leaves {
			if encodedJSONBytes(leaf) > defaultLeafDetailBytes {
				t.Fatal("leaf bound")
			}
			data, _ := json.Marshal(leaf)

			switch leaf.Skeleton.StepID {
			case "heavy":
				if !strings.Contains(string(data), "short final response") || !strings.Contains(string(data), "own contribution") {
					t.Fatal("reserved content missing")
				}
				counts := map[string]int{}
				summary := false
				for _, ref := range leaf.Evidence {
					if ref.Status == "summarized" {
						summary = true
						if ref.Omitted == 0 || !strings.Contains(ref.Ref, "evidence-index.json#leaf=") {
							t.Fatal(ref)
						}
					} else {
						counts[ref.Category]++
					}
				}
				if !summary || counts["artifact"] > 8 || counts["narrative"] > 8 {
					t.Fatal("shell bounds")
				}
			case "large-output":
				found := false
				for _, ref := range leaf.Evidence {
					if ref.Category == "narrative" && ref.Truncated && ref.Ref != "" && strings.Contains(ref.Detail, "large output") {
						found = true
					}
				}
				if !found {
					t.Fatal("large output not excerpted")
				}
			case "detector":
				if !strings.Contains(string(data), "detected defect") || !strings.Contains(string(data), "detector contribution") {
					t.Fatal("detector evidence missing")
				}
			case "fixer":
				if !strings.Contains(string(data), "commit_patch") || !strings.Contains(string(data), "+corrected") {
					t.Fatal("fixer evidence missing")
				}
			}
		}
	}
}

// Observation failure is checked through the real Runner as well as preparation:
// unavailable empty observations must never become evidence of no artifacts.
func TestINT002RunnerObservationFailuresRemainIncomplete(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission failures require an unprivileged user")
	}
	for _, condition := range []string{"enumeration", "read", "depth", "retention", "empty unavailable"} {
		t.Run(condition, func(t *testing.T) {
			repo := contentRepo(t)
			source := t.TempDir()
			output := filepath.Join(source, "output")
			_ = os.Mkdir(output, 0o700)
			switch condition {
			case "enumeration":
				for i := 0; i < 2001; i++ {
					_ = os.WriteFile(filepath.Join(output, fmt.Sprintf("%04d", i)), []byte("x"), 0o600)
				}
			case "read":
				for i := 0; i < 5; i++ {
					f, err := os.Create(filepath.Join(output, fmt.Sprintf("%d", i)))
					if err != nil {
						t.Fatal(err)
					}
					_ = f.Truncate(16 << 20)
					_ = f.Close()
				}
			case "depth":
				_ = os.MkdirAll(filepath.Join(output, strings.Repeat("nested/", 18)), 0o700)
			case "retention":
				_ = os.Mkdir(filepath.Join(source, "step-evidence"), 0o500)
				t.Cleanup(func() { _ = os.Chmod(filepath.Join(source, "step-evidence"), 0o700) })
			case "empty unavailable":
				_ = os.Chmod(output, 0o000)
				t.Cleanup(func() { _ = os.Chmod(output, 0o700) })
			}
			if condition != "empty unavailable" {
				_ = os.Symlink("/etc/passwd", filepath.Join(output, "outside"))
			}
			first := "true"
			if condition == "retention" {
				first = "printf contribution > \"" + filepath.Join(output, "new") + "\""
			}
			workflow := model.Workflow{Name: "failures", Steps: []model.Step{{ID: "first", Command: first}, {ID: "later", Command: "true"}}}
			workflow.ApplyDefaults()
			result, err := runner.RunWorkflow(&workflow, nil, &runner.Options{ProjectRoot: repo, WorkingDir: repo, SessionDir: source, ProcessRunner: contentShellRunner{root: repo}, Log: contentSilentLog{}})
			if err != nil || result != runner.ResultSuccess {
				t.Fatalf("%s %v", result, err)
			}
			events, err := readStepEvents(source)
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, attempts := range events.events {
				for _, event := range attempts {
					if event.outputWrites == nil || event.outputWrites.Status == "observed" {
						t.Fatalf("failure not recorded: %+v", event)
					}
					for _, file := range event.outputWrites.Files {
						if file.Path == "outside" {
							t.Fatal("followed symlink")
						}
					}
					count++
				}
			}
			if count != 2 {
				t.Fatalf("steps completed: %d", count)
			}
			// Restore traversal permissions only after Runner has recorded the failure.
			_ = os.Chmod(output, 0o700)
			snapshot, err := snapshotEvidenceForProject(source, "failures", repo)
			if err != nil {
				t.Fatal(err)
			}
			artifact, err := readMetrics(filepath.Join(source, metrics.FileName))
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := PrepareEvidence(Request{AuditRunID: "audit", AuditSessionDir: t.TempDir(), SnapshotPath: snapshot, SourceRunID: "source", ExecutionSessionID: artifact.Sessions[0].ExecutionSessionID, Trigger: "automatic"})
			if err != nil {
				t.Fatal(err)
			}
			for _, leaf := range prepared.Index.Leaves {
				for _, ref := range leaf.Evidence {
					if ref.Category == "artifact" && (ref.Status == "none_produced" || ref.Status == "available") {
						t.Fatalf("false coverage %+v", ref)
					}
				}
				if !incompleteLeafEvidence(&leaf) {
					t.Fatal("complete coverage despite observation failure")
				}
			}
		})
	}
}

type contentShellRunner struct{ root string }

func (r contentShellRunner) RunShell(command string, _ bool, workdir string) (iexec.ProcessResult, error) {
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = workdir
	if cmd.Dir == "" {
		cmd.Dir = r.root
	}
	data, err := cmd.CombinedOutput()
	if exit, ok := err.(*exec.ExitError); ok {
		return iexec.ProcessResult{ExitCode: exit.ExitCode(), Stdout: string(data)}, nil
	}
	return iexec.ProcessResult{Stdout: string(data)}, err
}
func (r contentShellRunner) RunScript(path string, _ []byte, capture bool, workdir string) (iexec.ProcessResult, error) {
	return r.RunShell(path, capture, workdir)
}
func (contentShellRunner) RunAgent(*iexec.AgentProcessOptions) (iexec.ProcessResult, error) {
	return iexec.ProcessResult{}, fmt.Errorf("unexpected agent invocation")
}

type contentSilentLog struct{}

func (contentSilentLog) Printf(string, ...any) {}
func (contentSilentLog) Errorf(string, ...any) {}
func (contentSilentLog) Println(...any)        {}
