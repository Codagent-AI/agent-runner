package runner

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codagent/agent-runner/internal/config"
	"github.com/codagent/agent-runner/internal/exec"
	"github.com/codagent/agent-runner/internal/loader"
	"github.com/codagent/agent-runner/internal/metrics"
	"github.com/codagent/agent-runner/internal/model"
)

type codexRateLimitRunner struct {
	home              string
	calls, shellCalls int
	threads, accounts []string
	resets            []int64
	skipLogOnCall     int
}

func (r *codexRateLimitRunner) RunShell(string, bool, string) (exec.ProcessResult, error) {
	r.shellCalls++
	if r.shellCalls == 1 {
		return exec.ProcessResult{ExitCode: 1}, nil
	}
	return exec.ProcessResult{ExitCode: 0}, nil
}
func (r *codexRateLimitRunner) RunScript(string, []byte, bool, string) (exec.ProcessResult, error) {
	return exec.ProcessResult{ExitCode: 0}, nil
}
func (r *codexRateLimitRunner) RunAgent(*exec.AgentProcessOptions) (exec.ProcessResult, error) {
	r.calls++
	thread, account, reset := "fixture-thread", "fixture-account", int64(9999999999)
	if r.calls <= len(r.threads) {
		thread = r.threads[r.calls-1]
	}
	if r.calls <= len(r.accounts) {
		account = r.accounts[r.calls-1]
	}
	if r.calls <= len(r.resets) {
		reset = r.resets[r.calls-1]
	}
	stdout := fmt.Sprintf(`{"type":"thread.started","thread_id":%q}`, thread) + "\n" + `{"type":"turn.completed","usage":{"input_tokens":10,"cached_input_tokens":0,"output_tokens":2}}` + "\n"
	if r.calls == r.skipLogOnCall {
		return exec.ProcessResult{Started: true, ExitCode: 0, Stdout: stdout}, nil
	}
	at := time.Now().UTC()
	dir := filepath.Join(r.home, "sessions", at.In(time.Local).Format("2006/01/02"))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return exec.ProcessResult{}, err
	}
	path := filepath.Join(dir, "rollout-test-"+thread+".jsonl")
	_, priorErr := os.Stat(path)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return exec.ProcessResult{}, err
	}
	if os.IsNotExist(priorErr) {
		_, _ = fmt.Fprintf(f, "{\"type\":\"session_meta\",\"payload\":{\"id\":%q,\"creator_account_id\":%q}}\n", thread, account)
	}
	_, err = fmt.Fprintf(f, "{\"timestamp\":%q,\"type\":\"event_msg\",\"payload\":{\"type\":\"token_count\",\"rate_limits\":{\"limit_id\":\"codex\",\"plan_type\":\"pro\",\"primary\":{\"used_percent\":%d,\"window_minutes\":10080,\"resets_at\":%d},\"secondary\":null}}}\n", at.Format(time.RFC3339Nano), 40+r.calls, reset)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return exec.ProcessResult{}, err
	}
	return exec.ProcessResult{Started: true, ExitCode: 0, Stdout: stdout}, nil
}

func TestCodexRateLimitWorkflowArtifact(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEX_HOME", home)
	dir := t.TempDir()
	workflowPath := filepath.Join(t.TempDir(), "rate-limits-v1.0.yaml")
	content := "name: rate-limits\nsteps:\n  - id: fresh\n    agent: codex\n    session: new\n    prompt: first\n  - id: gate\n    command: true\n  - id: resume\n    session: resume\n    prompt: second\n"
	if err := os.WriteFile(workflowPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	workflow, err := loader.LoadWorkflow(workflowPath, loader.Options{})
	if err != nil {
		t.Fatal(err)
	}
	profiles := &config.Config{ActiveAgents: map[string]*config.Agent{"codex": {DefaultMode: "autonomous", CLI: "codex", Model: "gpt-5.6-sol"}}}
	runner := &codexRateLimitRunner{home: home}
	result, err := RunWorkflow(&workflow, nil, &Options{WorkflowFile: workflowPath, SessionDir: dir, ProfileStore: profiles, ProcessRunner: runner, GlobExpander: &mockGlob{}, Log: &mockLog{}})
	if err != nil || result != ResultFailed {
		t.Fatalf("first session result: %s %v", result, err)
	}
	result, err = ResumeWorkflow(filepath.Join(dir, "state.json"), &Options{SessionDir: dir, ProfileStore: profiles, ProcessRunner: runner, GlobExpander: &mockGlob{}, Log: &mockLog{}})
	if err != nil || result != ResultSuccess {
		t.Fatalf("workflow failed: %s %v", result, err)
	}
	data, err := os.ReadFile(filepath.Join(dir, metrics.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "fixture-account") {
		t.Fatal("raw account identifier persisted in metrics")
	}
	auditData, err := os.ReadFile(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(auditData), "fixture-account") {
		t.Fatal("raw account identifier persisted in audit")
	}
	var artifact metrics.Artifact
	if err := json.Unmarshal(data, &artifact); err != nil {
		t.Fatal(err)
	}
	if artifact.SchemaVersion != 4 || artifact.CodexRateLimits == nil || artifact.CodexRateLimits.MeasuredAttempts != 2 || len(artifact.Sessions) != 2 {
		t.Fatalf("artifact has schema %d, steps %d, sessions %d, rollup %+v", artifact.SchemaVersion, len(artifact.Steps), len(artifact.Sessions), artifact.CodexRateLimits)
	}
	var fresh, resumed *model.CodexRateLimitEvidence
	for i := range artifact.Steps {
		switch artifact.Steps[i].ID {
		case "fresh":
			fresh = artifact.Steps[i].CodexRateLimits
		case "resume":
			resumed = artifact.Steps[i].CodexRateLimits
		case "gate":
			if artifact.Steps[i].CodexRateLimits != nil {
				t.Fatal("shell step has Codex evidence")
			}
		}
	}
	if fresh == nil || resumed == nil || fresh.Deltas[0].Reason != "no-baseline" || resumed.StartProvenance != "same-thread" {
		t.Fatalf("Codex evidence fresh=%+v resumed=%+v", fresh, resumed)
	}
	if got := resumed.Deltas[0]; got.PercentagePoints == nil || *got.PercentagePoints != 1 {
		t.Fatalf("resume delta: %+v", got)
	}
}

func TestCodexRateLimitAccountSwitchAndResetAcrossResume(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEX_HOME", home)
	dir := t.TempDir()
	workflowPath := filepath.Join(t.TempDir(), "scope-v1.0.yaml")
	content := "name: scope\nsteps:\n  - id: before\n    agent: codex\n    session: new\n    prompt: one\n  - id: gate\n    command: true\n  - id: after\n    agent: codex\n    session: new\n    prompt: two\n  - id: other\n    agent: codex\n    session: new\n    prompt: three\n  - id: reset\n    agent: codex\n    session: new\n    prompt: four\n"
	if err := os.WriteFile(workflowPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	workflow, err := loader.LoadWorkflow(workflowPath, loader.Options{})
	if err != nil {
		t.Fatal(err)
	}
	profiles := &config.Config{ActiveAgents: map[string]*config.Agent{"codex": {DefaultMode: "autonomous", CLI: "codex", Model: "gpt-5.6-sol"}}}
	runner := &codexRateLimitRunner{home: home, threads: []string{"thread-1", "thread-2", "thread-3", "thread-4"}, accounts: []string{"account-1", "account-1", "account-2", "account-2"}, resets: []int64{100, 100, 100, 200}}
	result, err := RunWorkflow(&workflow, nil, &Options{WorkflowFile: workflowPath, SessionDir: dir, ProfileStore: profiles, ProcessRunner: runner, Log: &mockLog{}})
	if err != nil || result != ResultFailed {
		t.Fatalf("first session: %s %v", result, err)
	}
	result, err = ResumeWorkflow(filepath.Join(dir, "state.json"), &Options{SessionDir: dir, ProfileStore: profiles, ProcessRunner: runner, Log: &mockLog{}})
	if err != nil || result != ResultSuccess {
		t.Fatalf("resume: %s %v", result, err)
	}
	data, err := os.ReadFile(filepath.Join(dir, metrics.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "account-1") || strings.Contains(string(data), "account-2") {
		t.Fatal("raw account identifier leaked")
	}
	var artifact metrics.Artifact
	if err := json.Unmarshal(data, &artifact); err != nil {
		t.Fatal(err)
	}
	if artifact.CodexRateLimits == nil || artifact.CodexRateLimits.MeasuredAttempts != 4 {
		t.Fatalf("rollup: %+v", artifact.CodexRateLimits)
	}
	scopes := map[string]string{}
	groups := 0
	for _, step := range artifact.Steps {
		if step.CodexRateLimits == nil {
			continue
		}
		scopes[step.ID] = step.CodexRateLimits.AccountScope
	}
	for _, group := range artifact.CodexRateLimits.Windows {
		if group.Window == "primary" {
			groups++
		}
	}
	if scopes["before"] == "" || scopes["before"] != scopes["after"] || scopes["before"] == scopes["other"] || scopes["other"] != scopes["reset"] || groups != 3 {
		t.Fatalf("account/window partition: scopes=%v groups=%+v", scopes, artifact.CodexRateLimits.Windows)
	}
}

func TestCodexRateLimitMissingLogDoesNotChangeStepUsage(t *testing.T) {
	workflowPath := filepath.Join(t.TempDir(), "missing-log-v1.0.yaml")
	content := "name: missing-log\nsteps:\n  - id: codex\n    agent: codex\n    session: new\n    prompt: hello\n"
	if err := os.WriteFile(workflowPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	workflow, err := loader.LoadWorkflow(workflowPath, loader.Options{})
	if err != nil {
		t.Fatal(err)
	}
	profiles := &config.Config{ActiveAgents: map[string]*config.Agent{"codex": {DefaultMode: "autonomous", CLI: "codex", Model: "gpt-5.6-sol"}}}
	var records []metrics.StepRecord
	for _, skip := range []int{0, 1} {
		home := t.TempDir()
		t.Setenv("HOME", t.TempDir())
		t.Setenv("CODEX_HOME", home)
		dir := t.TempDir()
		runner := &codexRateLimitRunner{home: home, skipLogOnCall: skip}
		result, runErr := RunWorkflow(&workflow, nil, &Options{WorkflowFile: workflowPath, SessionDir: dir, ProfileStore: profiles, ProcessRunner: runner, Log: &mockLog{}})
		if runErr != nil || result != ResultSuccess {
			t.Fatalf("run with skip=%d: %s %v", skip, result, runErr)
		}
		data, readErr := os.ReadFile(filepath.Join(dir, metrics.FileName))
		if readErr != nil {
			t.Fatal(readErr)
		}
		var artifact metrics.Artifact
		if err := json.Unmarshal(data, &artifact); err != nil {
			t.Fatal(err)
		}
		records = append(records, artifact.Steps[0])
	}
	if records[0].Outcome != records[1].Outcome || records[0].Usage.Status != records[1].Usage.Status || records[0].Usage.Tokens[model.TokenInput] != records[1].Usage.Tokens[model.TokenInput] || records[1].CodexRateLimits.Reason != "session-log-unavailable" || records[0].CodexRateLimits.Status != "captured" {
		t.Fatalf("log failure changed step semantics: readable=%+v missing=%+v", records[0], records[1])
	}
}
