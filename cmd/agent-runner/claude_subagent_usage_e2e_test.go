package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestClaudeSubagentUsageInheritedSessionE2E(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake CLI")
	}
	root := t.TempDir()
	home := filepath.Join(root, "home")
	work := filepath.Join(root, "project")
	bin := filepath.Join(root, "bin")
	for _, dir := range []string{home, work, bin} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
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
	runner := filepath.Join(root, "agent-runner")
	buildAgentRunner(t, findRepoRoot(t), runner)
	script := `#!/bin/sh
set -eu
session=44444444-4444-4444-4444-444444444444
project="$HOME/.claude/projects/intentionally-shortened"
parent="$project/$session.jsonl"
sub="$project/$session/subagents"
mkdir -p "$sub"
if [ ! -f "$parent" ]; then
  cat >> "$parent" <<'JSON'
{"uuid":"first-1","type":"assistant","message":{"content":[{"type":"tool_use","name":"Agent","id":"tool-1"}]}}
{"uuid":"last-1","type":"assistant","message":{"content":[]}}
JSON
  printf '%s\n' '{"toolUseId":"tool-1","agentType":"Explore"}' > "$sub/agent-1.meta.json"
  printf '%s\n' '{"type":"assistant","message":{"id":"a","model":"haiku","usage":{"input_tokens":2,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":3}}}' > "$sub/agent-1.jsonl"
  printf '%s\n' '{"uuid":"first-1","type":"system","subtype":"init","session_id":"44444444-4444-4444-4444-444444444444","model":"opus"}'
  printf '%s\n' '{"uuid":"last-1","type":"assistant","message":{"model":"opus"}}'
  printf '%s\n' '{"type":"result","session_id":"44444444-4444-4444-4444-444444444444","result":"first done","usage":{"input_tokens":1,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":1},"total_cost_usd":0.1}'
else
  cat >> "$parent" <<'JSON'
{"uuid":"first-2","type":"assistant","message":{"content":[{"type":"tool_use","name":"Agent","id":"tool-2"}]}}
{"uuid":"last-2","type":"assistant","message":{"content":[]}}
JSON
  printf '%s\n' '{"toolUseId":"tool-2","agentType":"Explore"}' > "$sub/agent-2.meta.json"
  printf '%s\n' '{"type":"assistant","message":{"id":"b","model":"sonnet","usage":{"input_tokens":4,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":5}}}' > "$sub/agent-2.jsonl"
  printf '%s\n' '{"uuid":"first-2","type":"system","subtype":"init","session_id":"44444444-4444-4444-4444-444444444444","model":"opus"}'
  printf '%s\n' '{"uuid":"last-2","type":"assistant","message":{"model":"opus"}}'
  printf '%s\n' '{"type":"result","session_id":"44444444-4444-4444-4444-444444444444","result":"second done","usage":{"input_tokens":1,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":1},"total_cost_usd":0.2}'
fi
`
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	flows := filepath.Join(work, ".agent-runner", "workflows")
	if err := os.MkdirAll(flows, 0o755); err != nil {
		t.Fatal(err)
	}
	writeWorkflowFile(t, flows, "subagent-e2e-v1.0.yaml", `name: subagent-e2e
sessions:
  - name: shared
    agent: claude_headless_smoke
steps:
  - id: first
    session: shared
    prompt: "first"
  - id: second
    session: shared
    prompt: "second"
`)
	env := smokeCommandEnv(os.Environ(), "HOME="+home, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "AGENT_RUNNER_NO_TUI=1")
	out := runRepairBlockedCLI(t, runner, work, env, "--headless", "--profile", "smoke_test", "subagent-e2e")
	if out.exitCode != 0 {
		t.Fatalf("exit=%d: %s", out.exitCode, out.text)
	}
	paths, err := filepath.Glob(filepath.Join(home, ".agent-runner", "projects", "*", "runs", "subagent-e2e-*", "run-metrics.json"))
	if err != nil || len(paths) != 1 {
		t.Fatalf("metrics paths=%v err=%v output=%s", paths, err, out.text)
	}
	raw, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	var artifact struct {
		Steps []struct {
			ID    string `json:"id"`
			Usage struct {
				Allocations []struct {
					Kind      string `json:"kind"`
					Model     string `json:"model"`
					ToolUseID string `json:"tool_use_id"`
				} `json:"allocations"`
				SubagentCollection string `json:"subagent_collection"`
			} `json:"usage"`
		} `json:"steps"`
		Totals struct {
			TokenTotals struct {
				Total int64 `json:"total"`
			} `json:"token_totals"`
			UsageCoverage string `json:"usage_coverage"`
		} `json:"totals"`
	}
	if err := json.Unmarshal(raw, &artifact); err != nil {
		t.Fatal(err)
	}
	if len(artifact.Steps) != 2 || artifact.Totals.TokenTotals.Total != 18 || artifact.Totals.UsageCoverage != "complete" {
		t.Fatalf("metrics=%s", raw)
	}
	for i, want := range []string{"tool-1", "tool-2"} {
		u := artifact.Steps[i].Usage
		if u.SubagentCollection != "complete" || len(u.Allocations) != 2 || u.Allocations[1].ToolUseID != want {
			t.Fatalf("step %d metrics=%s", i, raw)
		}
	}
}
