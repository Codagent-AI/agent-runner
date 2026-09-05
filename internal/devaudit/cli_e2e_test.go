//go:build dev_audit && (darwin || linux)

package devaudit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/metrics"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/stateio"
)

func TestE2E001TaggedCLIAutomaticAuditCompletesAndRetriesWithoutDuplicate(t *testing.T) {
	fixture := newCLIAuditFixture(t)

	fixture.run(true, "-C", fixture.project, "--headless", "spec-driven:audit-e2e")
	source := fixture.singleSourceRun()
	link := fixture.waitForLinks(source, 1, false)[0]
	if link.StartedAt == "" {
		t.Fatal("source command returned without a durable linked-audit start")
	}
	link = fixture.waitForLinks(source, 1, true)[0]
	auditDir := filepath.Join(filepath.Dir(source), link.AuditRunID)
	report := readE2EReport(t, auditDir)
	if report.DeliveryState != "delivered" || len(report.Values.Observations) != 1 {
		t.Fatalf("local report = %#v, want one delivered observation", report)
	}
	request := readE2ERequest(t, auditDir)
	if request.Crosscheck.CLI != "codex" || request.Crosscheck.Model != "gpt-5.6-sol" {
		t.Fatalf("crosscheck provenance = %#v", request.Crosscheck)
	}
	if _, err := os.Stat(filepath.Join(auditDir, metrics.FileName)); err != nil {
		t.Fatalf("audit metrics unavailable: %v", err)
	}
	if got := fixture.server.rowCount(); got != 1 {
		t.Fatalf("sheet row count = %d, want 1", got)
	}
	calls := fixture.modelCallCount()
	fixture.run(true, "audit", "retry", auditDir)
	if got := fixture.server.rowCount(); got != 1 {
		t.Fatalf("retry sheet row count = %d, want 1", got)
	}
	if got := fixture.modelCallCount(); got != calls {
		t.Fatalf("retry model calls = %d, want unchanged %d", got, calls)
	}
	if _, err := os.Stat(fixture.githubMarker); !os.IsNotExist(err) {
		t.Fatalf("value-only journey invoked GitHub: %v", err)
	}
}

func TestE2E002TaggedCLIFailureResumeReplayAndRetryPreserveLineage(t *testing.T) {
	fixture := newCLIAuditFixture(t)
	resumeMarker := filepath.Join(fixture.project, ".resume-ready")
	fixture.env = append(fixture.env, "AUDIT_E2E_FAIL_UNTIL="+resumeMarker)

	fixture.run(false, "-C", fixture.project, "--headless", "spec-driven:audit-e2e")
	source := fixture.singleSourceRun()
	failedState := readE2EState(t, source)
	if failedState.Completed {
		t.Fatalf("failed source was marked completed: %#v", failedState)
	}
	firstLink := fixture.waitForLinks(source, 1, true)[0]
	firstSession := readE2EMetrics(t, source).Sessions[0].ExecutionSessionID

	if err := os.WriteFile(resumeMarker, []byte("resume\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.run(true, "-C", fixture.project, "--headless", "--resume", filepath.Base(source))
	links := fixture.waitForLinks(source, 2, true)
	resumed := readE2EMetrics(t, source)
	if len(resumed.Sessions) != 2 || resumed.Sessions[0].ExecutionSessionID == resumed.Sessions[1].ExecutionSessionID {
		t.Fatalf("execution sessions = %#v, want two distinct sessions", resumed.Sessions)
	}
	secondReport := readE2EReport(t, filepath.Join(filepath.Dir(source), links[1].AuditRunID))
	if len(secondReport.Values.Observations) != 1 || secondReport.Values.Observations[0].Lineage != "overlap" {
		t.Fatalf("resumed report lineage = %#v, want overlap", secondReport.Values.Observations)
	}

	beforeGit := fixture.gitState()
	fixture.server.failNextAppend()
	fixture.run(true, "audit", "replay", source, "--session", firstSession)
	links = fixture.waitForLinks(source, 3, true)
	replayLink := links[2]
	if replayLink.AuditRunID == firstLink.AuditRunID || replayLink.Trigger != "replay" {
		t.Fatalf("replay link = %#v", replayLink)
	}
	replayDir := filepath.Join(filepath.Dir(source), replayLink.AuditRunID)
	replayReport := readE2EReport(t, replayDir)
	if replayReport.Trigger != "replay" || replayReport.DeliveryState != "pending" {
		t.Fatalf("replay report = %#v, want pending replay", replayReport)
	}
	calls := fixture.modelCallCount()
	fixture.run(true, "audit", "retry", replayDir)
	if got := fixture.modelCallCount(); got != calls {
		t.Fatalf("replay retry model calls = %d, want unchanged %d", got, calls)
	}
	if report := readE2EReport(t, replayDir); report.DeliveryState != "delivered" {
		t.Fatalf("retried replay delivery state = %q", report.DeliveryState)
	}
	if afterGit := fixture.gitState(); afterGit != beforeGit {
		t.Fatalf("replay changed source repository: before=%q after=%q", beforeGit, afterGit)
	}
	finalState := readE2EState(t, source)
	if !finalState.Completed {
		t.Fatalf("replay changed completed source state: %#v", finalState)
	}
}

type cliAuditFixture struct {
	t            *testing.T
	root         string
	home         string
	project      string
	runner       string
	env          []string
	modelCalls   string
	githubMarker string
	server       *auditSheetsServer
}

func newCLIAuditFixture(t *testing.T) *cliAuditFixture {
	t.Helper()
	root := t.TempDir()
	t.Cleanup(func() {
		_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err == nil {
				_ = os.Chmod(path, 0o700)
			}
			return nil
		})
	})
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	fixture := &cliAuditFixture{
		t: t, root: root, home: filepath.Join(root, "home"), project: filepath.Join(root, "project"),
		runner: filepath.Join(root, "agent-runner"), modelCalls: filepath.Join(root, "model-calls"), githubMarker: filepath.Join(root, "github-called"),
	}
	fixture.server = newAuditSheetsServer(t)
	binDir := filepath.Join(root, "bin")
	for _, dir := range []string{fixture.home, fixture.project, binDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	canonicalProject, err := filepath.EvalSymlinks(fixture.project)
	if err != nil {
		t.Fatal(err)
	}
	fixture.project = canonicalProject
	build := exec.Command("go", "build", "-tags", "dev_audit,devaudit_e2e", "-ldflags", "-X github.com/codagent/agent-runner/internal/devaudit.BuildRoot="+repoRoot, "-o", fixture.runner, "./cmd/agent-runner")
	build.Dir = repoRoot
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build tagged E2E runner: %v\n%s", err, output)
	}
	writeE2EFakeCodex(t, filepath.Join(binDir, "codex"))
	writeE2EFakeGitHub(t, filepath.Join(binDir, "gh"), fixture.githubMarker)
	writeE2EProfile(t, fixture.home)
	fixture.initProject()
	fixture.env = replaceE2EEnv(os.Environ(),
		"HOME="+fixture.home,
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"AGENT_RUNNER_NO_TUI=1",
		"AGENT_RUNNER_DEVAUDIT_E2E_SHEETS_URL="+fixture.server.server.URL+"/v4",
		"AUDIT_E2E_MODEL_CALLS="+fixture.modelCalls,
	)
	clientPath, tokenPath := fixture.writeCredentials()
	fixture.run(true, "audit", "setup", "--client", clientPath, "--token", tokenPath, "--spreadsheet", "test-sheet", "--tab", "step_value_v1")
	return fixture
}

func (f *cliAuditFixture) initProject() {
	f.t.Helper()
	for _, args := range [][]string{{"init"}, {"config", "user.email", "audit@example.test"}, {"config", "user.name", "Audit Test"}, {"remote", "add", "origin", "https://github.com/Codagent-AI/fixture.git"}} {
		command := exec.Command("git", append([]string{"-C", f.project}, args...)...)
		if output, err := command.CombinedOutput(); err != nil {
			f.t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}
	if err := os.WriteFile(filepath.Join(f.project, "README.md"), []byte("fixture\n"), 0o600); err != nil {
		f.t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "README.md"}, {"commit", "-m", "chore: initialize fixture"}} {
		command := exec.Command("git", append([]string{"-C", f.project}, args...)...)
		if output, err := command.CombinedOutput(); err != nil {
			f.t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}
}

func (f *cliAuditFixture) writeCredentials() (clientPath, tokenPath string) {
	f.t.Helper()
	clientPath = filepath.Join(f.root, "client.json")
	tokenPath = filepath.Join(f.root, "token.json")
	clientJSON := fmt.Sprintf(`{"installed":{"client_id":"client","client_secret":"secret","token_uri":%q}}`, f.server.server.URL+"/token")
	if err := os.WriteFile(clientPath, []byte(clientJSON), 0o600); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(tokenPath, []byte(`{"client_id":"client","client_secret":"secret","refresh_token":"refresh","scopes":["https://www.googleapis.com/auth/spreadsheets"]}`), 0o600); err != nil {
		f.t.Fatal(err)
	}
	return clientPath, tokenPath
}

func (f *cliAuditFixture) run(wantSuccess bool, args ...string) string {
	f.t.Helper()
	command := exec.Command(f.runner, args...)
	command.Dir = f.project
	command.Env = f.env
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	err := command.Run()
	if wantSuccess && err != nil {
		f.t.Fatalf("agent-runner %v: %v\n%s", args, err, output.String())
	}
	if !wantSuccess && err == nil {
		f.t.Fatalf("agent-runner %v unexpectedly succeeded\n%s", args, output.String())
	}
	return output.String()
}

func (f *cliAuditFixture) singleSourceRun() string {
	f.t.Helper()
	runsDir := filepath.Join(f.home, ".agent-runner", "projects", audit.EncodePath(f.project), "runs")
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		entries, _ := os.ReadDir(runsDir)
		var sources []string
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			path := filepath.Join(runsDir, entry.Name())
			if _, err := os.Stat(filepath.Join(path, lifecycleFileName)); err == nil {
				sources = append(sources, path)
			}
		}
		if len(sources) == 1 {
			return sources[0]
		}
		time.Sleep(25 * time.Millisecond)
	}
	f.t.Fatalf("did not find one source run under %s", runsDir)
	return ""
}

func (f *cliAuditFixture) waitForLinks(source string, count int, completed bool) []Link {
	f.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		lifecycle, err := ReadLifecycle(filepath.Join(source, lifecycleFileName))
		if err == nil && len(lifecycle.Links) >= count {
			ready := true
			if completed {
				for index := range lifecycle.Links[:count] {
					ready = ready && lifecycle.Links[index].State == LaunchCompleted
				}
			}
			if ready {
				return lifecycle.Links
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	lifecycle, lifecycleErr := ReadLifecycle(filepath.Join(source, lifecycleFileName))
	diagnostics := ""
	for index := range lifecycle.Links {
		link := &lifecycle.Links[index]
		for _, name := range []string{"value-model-diagnostics.json", "correctness-model-diagnostics.json", "detached.log"} {
			if data, err := os.ReadFile(filepath.Join(filepath.Dir(source), link.AuditRunID, name)); err == nil {
				diagnostics += name + ": " + string(data) + "\n"
			}
		}
	}
	f.t.Fatalf("links did not reach count=%d completed=%v: lifecycle=%#v err=%v diagnostics=%s", count, completed, lifecycle, lifecycleErr, diagnostics)
	return nil
}

func (f *cliAuditFixture) modelCallCount() int {
	data, err := os.ReadFile(f.modelCalls)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		f.t.Fatal(err)
	}
	return len(strings.Fields(string(data)))
}

func (f *cliAuditFixture) gitState() string {
	command := exec.Command("git", "-C", f.project, "status", "--porcelain=v1")
	output, err := command.Output()
	if err != nil {
		f.t.Fatal(err)
	}
	return string(output)
}

func readE2EState(t *testing.T, dir string) model.RunState {
	t.Helper()
	state, err := stateio.ReadState(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func readE2EMetrics(t *testing.T, dir string) metrics.Artifact {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, metrics.FileName))
	if err != nil {
		t.Fatal(err)
	}
	var artifact metrics.Artifact
	if err := json.Unmarshal(data, &artifact); err != nil {
		t.Fatal(err)
	}
	return artifact
}

func readE2ERequest(t *testing.T, dir string) Request {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "request.json"))
	if err != nil {
		t.Fatal(err)
	}
	var request Request
	if err := json.Unmarshal(data, &request); err != nil {
		t.Fatal(err)
	}
	return request
}

func readE2EReport(t *testing.T, dir string) LocalReport {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "local-report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var report LocalReport
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	return report
}

func writeE2EProfile(t *testing.T, home string) {
	t.Helper()
	path := filepath.Join(home, ".agent-runner", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	config := "profiles:\n  default:\n    agents:\n      crosscheck:\n        default_mode: autonomous\n        cli: codex\n        model: gpt-5.6-sol\n        effort: low\n"
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeE2EFakeCodex(t *testing.T, path string) {
	t.Helper()
	script := `#!/bin/sh
set -eu
prompt=""
output_path=""
capture_output_path=""
for arg in "$@"; do
  prompt=$arg
  if [ "$capture_output_path" = "true" ]; then
    output_path=$arg
    capture_output_path=""
  elif [ "$arg" = "-o" ] || [ "$arg" = "--output-last-message" ]; then
    capture_output_path="true"
  fi
done
sleep "${AUDIT_E2E_DELAY_SECONDS:-0}"
printf 'call\n' >> "$AUDIT_E2E_MODEL_CALLS"
python3 - "$prompt" "$output_path" <<'PY'
import json, sys
prompt = sys.argv[1]
output_path = sys.argv[2]
if prompt.startswith("You are judging workflow-step value"):
    package = json.loads(prompt.rsplit("\n\n", 1)[1])
    result = {"batch_id": package["batch_id"], "observations": []}
    for leaf in package["leaves"]:
        result["observations"].append({
            "observation_id": leaf["skeleton"]["observation_id"],
            "overall_value": "medium",
            "change_effect": "intended",
            "unique_contribution": "unique",
            "downstream_evidence": "supporting",
            "confidence": "high",
            "evidence_coverage": "partial"
        })
else:
    result = {"candidates": []}
text = json.dumps(result, separators=(",", ":"))
if output_path:
    with open(output_path, "w", encoding="utf-8") as stream:
        stream.write(text + "\n")
print(json.dumps({"type":"thread.started","thread_id":"audit-e2e"}))
print(json.dumps({"type":"item.completed","item":{"type":"agent_message","text":text}}))
print(json.dumps({"type":"turn.completed"}))
PY
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeE2EFakeGitHub(t *testing.T, path, marker string) {
	t.Helper()
	script := "#!/bin/sh\nprintf 'called\\n' >> " + fmt.Sprintf("%q", marker) + "\nexit 99\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func replaceE2EEnv(base []string, overrides ...string) []string {
	names := map[string]bool{}
	for _, entry := range overrides {
		name, _, _ := strings.Cut(entry, "=")
		names[name] = true
	}
	result := make([]string, 0, len(base)+len(overrides))
	for _, entry := range base {
		name, _, _ := strings.Cut(entry, "=")
		if !names[name] {
			result = append(result, entry)
		}
	}
	return append(result, overrides...)
}

type auditSheetsServer struct {
	server     *httptest.Server
	mu         sync.Mutex
	rows       [][]string
	failAppend bool
}

func newAuditSheetsServer(t *testing.T) *auditSheetsServer {
	t.Helper()
	fixture := &auditSheetsServer{}
	fixture.server = httptest.NewServer(http.HandlerFunc(fixture.serveHTTP))
	t.Cleanup(fixture.server.Close)
	return fixture
}

func (s *auditSheetsServer) serveHTTP(writer http.ResponseWriter, request *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	writer.Header().Set("Content-Type", "application/json")
	switch {
	case request.URL.Path == "/token":
		_, _ = writer.Write([]byte(`{"access_token":"test-access"}`))
	case request.Method == http.MethodGet && strings.Contains(request.URL.Path, "1:1"):
		_ = json.NewEncoder(writer).Encode(map[string]any{"values": [][]string{stepValueHeader}})
	case request.Method == http.MethodGet && strings.Contains(request.URL.Path, "B:B"):
		values := [][]string{{"observation_id"}}
		for _, row := range s.rows {
			values = append(values, []string{row[1]})
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{"values": values})
	case request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, ":append"):
		if s.failAppend {
			s.failAppend = false
			http.Error(writer, "temporary failure", http.StatusServiceUnavailable)
			return
		}
		var body struct {
			Values [][]string `json:"values"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		s.rows = append(s.rows, body.Values...)
		_, _ = writer.Write([]byte(`{}`))
	default:
		http.NotFound(writer, request)
	}
}

func (s *auditSheetsServer) rowCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.rows)
}

func (s *auditSheetsServer) failNextAppend() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failAppend = true
}
