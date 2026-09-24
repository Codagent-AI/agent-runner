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
	cmd := exec.Command("bash", filepath.Join(repoRoot(t), "scripts", "recover-development-audits.sh"), "--data-root", dataRoot, "--include-temp-projects")
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
	output, err := exec.Command("bash", filepath.Join(repoRoot(t), "scripts", "recover-development-audits.sh"), "--data-root", dataRoot, "--include-temp-projects").CombinedOutput()
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
	output, err := exec.Command("bash", filepath.Join(repoRoot(t), "scripts", "recover-development-audits.sh"), "--data-root", dataRoot, "--include-temp-projects").CombinedOutput()
	if err != nil {
		t.Fatalf("recovery dry run: %v\n%s", err, output)
	}
	if strings.Contains(string(output), "REPLAY  source  same") {
		t.Fatalf("dry run replayed an already delivered session:\n%s", output)
	}
}

func TestIssueRepairScriptListsOnlyPlaceholderAutoAuditIssues(t *testing.T) {
	root := t.TempDir()
	dataRoot := filepath.Join(root, "state")
	auditDir := filepath.Join(dataRoot, "projects", "project", "runs", "audit")
	if err := os.MkdirAll(auditDir, 0o700); err != nil {
		t.Fatal(err)
	}
	report := `{"correctness":{"findings":[{"publication_state":"created","issue_url":"https://github.com/Codagent-AI/agent-runner/issues/42","candidate":{"title":"repair me"}},{"publication_state":"created","issue_url":"https://github.com/Codagent-AI/agent-runner/issues/43","candidate":{"title":"already repaired"}}]}}`
	if err := os.WriteFile(filepath.Join(auditDir, "local-report.json"), []byte(report), 0o600); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	gh := "#!/bin/sh\nif [ \"$3\" = \"https://github.com/Codagent-AI/agent-runner/issues/42\" ]; then printf '%s' '{\"title\":\"[auto-audit] repair me\",\"body\":\"-\"}'; else printf '%s' '{\"title\":\"[auto-audit] already repaired\",\"body\":\"full body\"}'; fi\n"
	if err := os.WriteFile(filepath.Join(binDir, "gh"), []byte(gh), 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", filepath.Join(repoRoot(t), "scripts", "repair-development-audit-issues.sh"), "--data-root", dataRoot)
	cmd.Env = append(os.Environ(), "PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("issue repair dry run: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "REPAIR  audit  https://github.com/Codagent-AI/agent-runner/issues/42") || strings.Contains(string(output), "REPAIR  audit  https://github.com/Codagent-AI/agent-runner/issues/43") {
		t.Fatalf("unexpected issue repair inventory:\n%s", output)
	}
}

func TestRecoveryScriptStopsWhenReplayedAuditFinishesWithoutDelivery(t *testing.T) {
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
	auditDir := filepath.Join(projectState, "runs", "audit")
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(auditDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectState, "meta.json"), []byte(`{"path":`+strconvQuote(project)+`}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "audit-lifecycle.json"), []byte(`{"links":[{"audit_run_id":"missing","execution_session_id":"session","trigger":"automatic","state":"completed"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "run-metrics.json"), []byte(`{"sessions":[{"execution_session_id":"session"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(auditDir, "state.json"), []byte(`{"completed":true,"failureReason":"model output rejected"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	goFixture := "#!/bin/sh\nout=\nwhile [ \"$#\" -gt 0 ]; do if [ \"$1\" = \"-o\" ]; then out=$2; shift; fi; shift; done\nprintf '#!/bin/sh\\necho audit\\n' > \"$out\"\nchmod +x \"$out\"\n"
	if err := os.WriteFile(filepath.Join(binDir, "go"), []byte(goFixture), 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", filepath.Join(repoRoot(t), "scripts", "recover-development-audits.sh"), "--execute", "--data-root", dataRoot, "--timeout-seconds", "5", "--include-temp-projects")
	cmd.Env = append(os.Environ(), "PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("recovery unexpectedly succeeded:\n%s", output)
	}
	if !strings.Contains(string(output), "FAILED  source  audit  model output rejected") || strings.Contains(string(output), "TIMEOUT") {
		t.Fatalf("recovery did not stop on completed failed audit:\n%s", output)
	}
}

func strconvQuote(value string) string { return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"` }

func TestRecoveryScriptRecoversExplicitSessionOutsideDataRoot(t *testing.T) {
	root := t.TempDir()
	// A factory attempt: --session-dir places the session outside any recorded
	// project's runs directory, and the run never had an audit lifecycle.
	replay := filepath.Join(root, "claim", "attempt-1", "agent-runner-session")
	retry := filepath.Join(root, "claim", "attempt-2", "agent-runner-session")
	delivered := filepath.Join(root, "claim", "attempt-3", "agent-runner-session")
	for _, dir := range []string{replay, retry, delivered, filepath.Join(filepath.Dir(retry), "audit-pending"), filepath.Join(filepath.Dir(delivered), "audit-done")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, dir := range []string{replay, retry, delivered} {
		if err := os.WriteFile(filepath.Join(dir, "run-metrics.json"), []byte(`{"sessions":[{"execution_session_id":"exec"}]}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(retry, "audit-lifecycle.json"), []byte(`{"links":[{"audit_run_id":"audit-pending","execution_session_id":"exec","trigger":"replay","state":"completed"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(retry), "audit-pending", "local-report.json"), []byte(`{"delivery_state":"pending"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(delivered, "audit-lifecycle.json"), []byte(`{"links":[{"audit_run_id":"audit-done","execution_session_id":"exec","trigger":"replay","state":"completed"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(delivered), "audit-done", "local-report.json"), []byte(`{"delivery_state":"delivered"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command("bash", filepath.Join(repoRoot(t), "scripts", "recover-development-audits.sh"),
		"--data-root", filepath.Join(root, "no-such-state"),
		"--session", replay+":exec:"+root,
		"--session", retry+":exec",
		"--session", delivered+":exec",
		"--session", replay+":unknown",
	).CombinedOutput()
	if err != nil {
		t.Fatalf("recovery dry run: %v\n%s", err, output)
	}
	for _, want := range []string{
		"REPLAY  attempt-1/agent-runner-session  exec",
		"RETRY  attempt-2/agent-runner-session  audit-pending",
		"SKIP delivered-session  attempt-3/agent-runner-session  exec",
		"SKIP unavailable  attempt-1/agent-runner-session  unknown",
		"actionable=2",
	} {
		if !strings.Contains(string(output), want) {
			t.Fatalf("dry run missing %q:\n%s", want, output)
		}
	}
}
