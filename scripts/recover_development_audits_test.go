package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecoveryScriptDoesNotHideUndeliveredAttemptBehindDeliveredSibling(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("git", "init", project).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, output)
	}
	dataRoot := filepath.Join(root, "state")
	projectState := filepath.Join(dataRoot, "projects", "project")
	source := filepath.Join(projectState, "runs", "source")
	for _, dir := range []string{source, filepath.Join(projectState, "runs", "delivered")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(projectState, "meta.json"), []byte(`{"path":`+strconvQuote(project)+`}`), 0o600); err != nil {
		t.Fatal(err)
	}
	lifecycle := `{"version":1,"source_run_id":"source","links":[{"audit_run_id":"delivered","execution_session_id":"old","trigger":"automatic","state":"completed"},{"audit_run_id":"missing","execution_session_id":"recover","trigger":"automatic","state":"completed"}]}`
	if err := os.WriteFile(filepath.Join(source, "audit-lifecycle.json"), []byte(lifecycle), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "run-metrics.json"), []byte(`{"sessions":[{"execution_session_id":"recover"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectState, "runs", "delivered", "local-report.json"), []byte(`{"delivery_state":"delivered"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", filepath.Join(repoRoot(t), "scripts", "recover-development-audits.sh"), "--data-root", dataRoot)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("recovery dry run: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "REPLAY  source  recover") {
		t.Fatalf("dry run did not select undelivered sibling:\n%s", output)
	}
}

func TestRecoveryScriptSkipsLifecycleSessionMissingFromMetrics(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("git", "init", project).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, output)
	}
	dataRoot := filepath.Join(root, "state")
	source := filepath.Join(dataRoot, "projects", "project", "runs", "source")
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	projectState := filepath.Dir(filepath.Dir(source))
	if err := os.WriteFile(filepath.Join(projectState, "meta.json"), []byte(`{"path":`+strconvQuote(project)+`}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "audit-lifecycle.json"), []byte(`{"links":[{"audit_run_id":"missing","execution_session_id":"gone","trigger":"automatic","state":"completed"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "run-metrics.json"), []byte(`{"sessions":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command("bash", filepath.Join(repoRoot(t), "scripts", "recover-development-audits.sh"), "--data-root", dataRoot).CombinedOutput()
	if err != nil {
		t.Fatalf("recovery dry run: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "SKIP unavailable") {
		t.Fatalf("dry run did not skip stale session:\n%s", output)
	}
}

func TestRecoveryScriptSkipsOlderAttemptAfterSameSessionDelivered(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("git", "init", project).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, output)
	}
	dataRoot := filepath.Join(root, "state")
	projectState := filepath.Join(dataRoot, "projects", "project")
	source := filepath.Join(projectState, "runs", "source")
	if err := os.MkdirAll(filepath.Join(projectState, "runs", "delivered"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectState, "meta.json"), []byte(`{"path":`+strconvQuote(project)+`}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "run-metrics.json"), []byte(`{"sessions":[{"execution_session_id":"same"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	lifecycle := `{"links":[{"audit_run_id":"delivered","execution_session_id":"same","trigger":"replay","state":"completed"},{"audit_run_id":"older","execution_session_id":"same","trigger":"automatic","state":"completed"}]}`
	if err := os.WriteFile(filepath.Join(source, "audit-lifecycle.json"), []byte(lifecycle), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectState, "runs", "delivered", "local-report.json"), []byte(`{"delivery_state":"delivered"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command("bash", filepath.Join(repoRoot(t), "scripts", "recover-development-audits.sh"), "--data-root", dataRoot).CombinedOutput()
	if err != nil {
		t.Fatalf("recovery dry run: %v\n%s", err, output)
	}
	if strings.Contains(string(output), "REPLAY  source  same") {
		t.Fatalf("dry run replayed an already delivered session:\n%s", output)
	}
}

func strconvQuote(value string) string { return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"` }
