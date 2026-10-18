package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/codagent/agent-runner/internal/audit"

	"github.com/codagent/agent-runner/internal/metrics"
	"github.com/codagent/agent-runner/internal/model"
)

func TestHeadlessContextE2E001(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	work := filepath.Join(root, "work")
	bin := filepath.Join(root, "bin")
	for _, dir := range []string{home, work, bin} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	var err error
	work, err = filepath.EvalSymlinks(work)
	if err != nil {
		t.Fatal(err)
	}
	writeSmokeProfileConfig(t, home)
	writeProjectSmokeConfig(t, work)
	config := filepath.Join(work, ".agent-runner", "config.yaml")
	data, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, []byte("      codex_headless_smoke:\n        default_mode: autonomous\n        cli: codex\n        model: gpt-5.4\n        effort: low\n")...)
	if err = os.WriteFile(config, data, 0o600); err != nil {
		t.Fatal(err)
	}
	runner := filepath.Join(root, "agent-runner")
	buildAgentRunner(t, findRepoRoot(t), runner)
	claude := `#!/bin/sh
set -eu
if [ -f "$HOME/claude-once" ]; then n=90; else n=50; touch "$HOME/claude-once"; fi
printf '%s\n' '{"type":"system","subtype":"init","session_id":"44444444-4444-4444-4444-444444444444","model":"opus"}'
printf '%s\n' '{"type":"assistant","message":{"id":"peak","model":"opus","usage":{"input_tokens":1,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}'
printf '{"type":"assistant","message":{"id":"peak","model":"opus","usage":{"input_tokens":%s,"cache_read_input_tokens":2,"cache_creation_input_tokens":3}}}\n' "$n"
printf '%s\n' '{"type":"assistant","message":{"id":"last","model":"opus","usage":{"input_tokens":30,"cache_read_input_tokens":2,"cache_creation_input_tokens":3}}}'
printf '%s\n' '{"type":"assistant","parent_tool_use_id":"child","message":{"id":"child","model":"haiku","usage":{"input_tokens":999999,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}'
printf '%s\n' '{"type":"result","session_id":"44444444-4444-4444-4444-444444444444","result":"done","usage":{"input_tokens":10,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":1},"modelUsage":{"opus":{"contextWindow":1000000}}}'
`
	codex := `#!/bin/sh
set -eu
dir="$CODEX_HOME/sessions/2026/10/18"
mkdir -p "$dir"
ts=$(date -u +%Y-%m-%dT%H:%M:%S).999999999Z
cat > "$dir/rollout-test-codex-session.jsonl" <<JSON
{"timestamp":"$ts","type":"token_usage_record","payload":{"thread_id":"codex-session","response_id":"a","usage":{"input_tokens":180000}}}
{"timestamp":"$ts","type":"token_usage_record","payload":{"thread_id":"codex-session","response_id":"compact","usage":{"input_tokens":215000}}}
{"timestamp":"$ts","type":"compacted","payload":{"compaction_response_id":"compact"}}
{"timestamp":"$ts","type":"token_usage_record","payload":{"thread_id":"codex-session","response_id":"b","usage":{"input_tokens":18000}}}
{"timestamp":"$ts","type":"event_msg","payload":{"type":"token_count","info":{"model_context_window":258400,"last_token_usage":{"input_tokens":0,"total_tokens":250000}}}}
JSON
printf '%s\n' '{"type":"thread.started","thread_id":"codex-session"}' '{"type":"turn.completed","usage":{"input_tokens":100,"cached_input_tokens":0,"output_tokens":2,"reasoning_output_tokens":0}}'
`
	for name, script := range map[string]string{"claude": claude, "codex": codex} {
		if err = os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	flows := filepath.Join(work, ".agent-runner", "workflows")
	if err = os.MkdirAll(flows, 0o700); err != nil {
		t.Fatal(err)
	}
	writeWorkflowFile(t, flows, "context-e2e-v1.0.yaml", `name: context-e2e
steps:
 - id: first
   agent: claude_headless_smoke
   session: new
   prompt: first
 - id: second
   session: resume
   prompt: second
 - id: codex
   agent: codex_headless_smoke
   session: new
   prompt: third
 - id: shell
   command: "true"
`)
	env := smokeCommandEnv(os.Environ(), "HOME="+home, "CODEX_HOME="+filepath.Join(home, ".codex"), "CLAUDE_CONFIG_DIR="+filepath.Join(home, ".claude"), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "AGENT_RUNNER_NO_TUI=1")
	out := runRepairBlockedCLI(t, runner, work, env, "--headless", "--profile", "smoke_test", "context-e2e")
	if out.exitCode != 0 {
		t.Fatalf("exit %d: %s", out.exitCode, out.text)
	}
	dir := latestWorkflowRunDir(t, home, work, "context-e2e")
	raw, err := os.ReadFile(filepath.Join(dir, "run-metrics.json"))
	if err != nil {
		t.Fatal(err)
	}
	var a metrics.Artifact
	if err = json.Unmarshal(raw, &a); err != nil {
		t.Fatal(err)
	}
	wantPeaks := []int64{55, 95, 180000}
	wantLast := []int64{35, 35, 18000}
	wantWindow := []int64{1000000, 1000000, 258400}
	if len(a.Steps) != 4 {
		t.Fatalf("steps: %s", raw)
	}
	for i := 0; i < 3; i++ {
		c := a.Steps[i].Usage.Context
		if c == nil || c.Status != model.ContextAvailable || c.PeakContext == nil || *c.PeakContext != wantPeaks[i] || c.LastRequestInput == nil || *c.LastRequestInput != wantLast[i] || c.ContextWindowLimit == nil || *c.ContextWindowLimit != wantWindow[i] {
			t.Fatalf("step %d context %+v", i, c)
		}
	}
	if a.Steps[3].Usage != nil && a.Steps[3].Usage.Context != nil {
		t.Fatal("shell has context")
	}
	s := a.AgentSessionContext
	if len(s.Sessions) != 2 || s.Sessions[0].AttemptCount != 2 || s.Sessions[0].Coverage != model.CoverageComplete || s.Sessions[1].Coverage != model.CoverageComplete {
		t.Fatalf("summary %+v", s)
	}
	if s.Sessions[0].HighWaterMark == nil || *s.Sessions[0].HighWaterMark != 95 || s.Sessions[0].MaxLastRequestInput == nil || *s.Sessions[0].MaxLastRequestInput != 35 || s.Sessions[1].HighWaterMark == nil || *s.Sessions[1].HighWaterMark != 180000 {
		t.Fatalf("session gauges %+v", s)
	}
	if a.Totals.TokenTotals == nil || a.Totals.TokenTotals.Total != 124 || a.Totals.Tokens[model.TokenInput] != 120 || a.Totals.Tokens[model.TokenOutput] != 4 {
		t.Fatalf("totals %+v canonical %+v", a.Totals, a.Totals.TokenTotals)
	}
	auditLog, err := os.ReadFile(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	if completed, err := audit.LatestRunCompleted(bytes.NewReader(auditLog)); err != nil || !completed {
		t.Fatalf("missing successful top-level run_end: %v", err)
	}
}
