package main

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/metrics"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/runview"
)

func TestHeadlessCostEstimationE2E001(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	work := filepath.Join(root, "work")
	bin := filepath.Join(root, "bin")
	for _, dir := range []string{home, work, bin} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	work, err := filepath.EvalSymlinks(work)
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
	data = append(data, []byte("      codex_cost_smoke:\n        default_mode: autonomous\n        cli: codex\n        model: gpt-6.1-sol\n        effort: low\n")...)
	if err = os.WriteFile(config, data, 0o600); err != nil {
		t.Fatal(err)
	}
	runner := filepath.Join(root, "agent-runner")
	buildAgentRunner(t, findRepoRoot(t), runner)
	claude := `#!/bin/sh
printf '%s\n' '{"type":"system","subtype":"init","session_id":"44444444-4444-4444-4444-444444444444","model":"opus"}' '{"type":"result","session_id":"44444444-4444-4444-4444-444444444444","result":"done","total_cost_usd":0.2,"usage":{"input_tokens":10,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":1}}'
`
	codex := `#!/bin/sh
set -eu
n=0
if [ -f "$HOME/codex-counter" ]; then n=$(cat "$HOME/codex-counter"); fi
n=$((n+1)); printf '%s' "$n" > "$HOME/codex-counter"
dir="$CODEX_HOME/sessions/2026/10/20"; mkdir -p "$dir"
ts=$(date -u +%Y-%m-%dT%H:%M:%S).999999999Z
input=100000; cached=40000; write=20000; output=1000
if [ "$n" = 2 ]; then input=300000; cached=200000; write=0; output=2000; fi
if [ "$n" != 4 ]; then
cat > "$dir/rollout-test-cost-$n.jsonl" <<JSON
{"timestamp":"$ts","type":"token_usage_record","payload":{"thread_id":"cost-$n","response_id":"r","usage":{"input_tokens":$input,"cached_input_tokens":$cached,"cache_write_input_tokens":$write,"output_tokens":$output}}}
{"timestamp":"$ts","type":"turn_context","payload":{}}
JSON
fi
printf '{"type":"thread.started","thread_id":"cost-%s"}\n' "$n"
printf '{"type":"turn.completed","usage":{"input_tokens":%s,"cached_input_tokens":%s,"output_tokens":%s}}\n' "$input" "$cached" "$output"
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
	writeWorkflowFile(t, flows, "cost-e2e-v1.0.yaml", `name: cost-e2e
steps:
 - id: reported
   agent: claude_headless_smoke
   session: new
   prompt: reported
 - id: short
   agent: codex_cost_smoke
   session: new
   prompt: short
 - id: long
   agent: codex_cost_smoke
   session: new
   prompt: long
 - id: unlisted
   agent: codex_cost_smoke
   model: unlisted
   session: new
   prompt: unlisted
 - id: missing
   agent: codex_cost_smoke
   session: new
   prompt: missing
 - id: shell
   command: "true"
`)
	env := smokeCommandEnv(os.Environ(), "HOME="+home, "CODEX_HOME="+filepath.Join(home, ".codex"), "CLAUDE_CONFIG_DIR="+filepath.Join(home, ".claude"), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "AGENT_RUNNER_NO_TUI=1")
	out := runRepairBlockedCLI(t, runner, work, env, "--headless", "--profile", "smoke_test", "cost-e2e")
	if out.exitCode != 0 {
		t.Fatalf("exit %d: %s", out.exitCode, out.text)
	}
	dir := latestWorkflowRunDir(t, home, work, "cost-e2e")
	raw, err := os.ReadFile(filepath.Join(dir, metrics.FileName))
	if err != nil {
		t.Fatal(err)
	}
	var a metrics.Artifact
	if err = json.Unmarshal(raw, &a); err != nil {
		t.Fatal(err)
	}
	if len(a.Steps) != 6 {
		t.Fatalf("steps %s", raw)
	}
	for i, want := range []float64{.2, .144, .47} {
		s := a.Steps[i]
		basis := model.CostBasisEstimated
		if i == 0 {
			basis = model.CostBasisReported
		}
		if s.EstimatedAPICostUSD == nil || *s.EstimatedAPICostUSD != want || s.CostBasis != basis {
			t.Fatalf("step %s: %+v", s.ID, s)
		}
		if i > 0 && (s.PriceTableVersion == "" || s.CostEstimate == nil) {
			t.Fatal("missing estimate provenance")
		}
	}
	estimatedNative := 0
	for _, n := range a.NativeMeasurements {
		for _, cost := range n.Costs {
			if cost.ID == "estimated-cost" {
				estimatedNative++
				if cost.Coverage != "full" || cost.Amount.Origin == nil || *cost.Amount.Origin != "derived" {
					t.Fatalf("native evidence %+v", cost)
				}
			}
		}
	}
	if estimatedNative != 2 {
		t.Fatalf("estimated native evidence %d", estimatedNative)
	}
	for i, want := range []model.UnavailableReason{model.UnavailableModelUnpriced, model.UnavailableContextBandUnknown} {
		s := a.Steps[i+3]
		if s.EstimatedAPICostUSD != nil || s.CostUnavailableReason != want || s.CostBasis != "" {
			t.Fatalf("unpriced %+v", s)
		}
	}
	if a.Steps[5].EstimatedAPICostUSD != nil || a.Steps[5].CostBasis != "" {
		t.Fatal("shell priced")
	}
	if a.Totals.CostBasis == nil || *a.Totals.CostBasis != "mixed" || a.Totals.CostCoverage != model.CoveragePartial || a.Totals.CostByBasis["reported"] != .2 || a.Totals.CostByBasis["estimated"] != .614 {
		t.Fatalf("totals %+v", a.Totals)
	}
	if a.Totals.EstimatedAPICostUSD == nil || math.Abs(*a.Totals.EstimatedAPICostUSD-.814) > 1e-15 {
		t.Fatal("total cost")
	}
	log, err := os.ReadFile(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	if complete, err := audit.LatestRunCompleted(bytes.NewReader(log)); err != nil || !complete {
		t.Fatalf("run_end %v", err)
	}
	seen := 0
	for _, line := range strings.Split(string(log), "\n") {
		e, err := runview.ParseLine(line)
		if err != nil {
			continue
		}
		if e.Type == "step_end" {
			seen += assertCostAuditStep(t, e, a.Steps)
		}
		if e.Type == "run_end" {
			totals := e.Data["totals"].(map[string]any)
			if totals["cost_basis"] != "mixed" || totals["cost_by_basis"] == nil {
				t.Fatal(totals)
			}
		}
	}
	if seen != 6 {
		t.Fatalf("step events %d", seen)
	}
}

func assertCostAuditStep(t *testing.T, e runview.RawEvent, steps []metrics.StepRecord) int {
	t.Helper()
	id := strings.Trim(e.Prefix, "[]")
	for i := range steps {
		s := &steps[i]
		if s.ID != id {
			continue
		}
		if s.EstimatedAPICostUSD == nil {
			if e.Data["estimated_api_cost_usd"] != nil {
				t.Fatalf("audit cost %s", id)
			}
		} else if e.Data["estimated_api_cost_usd"] != *s.EstimatedAPICostUSD {
			t.Fatalf("audit cost %s", id)
		}
		if got, _ := e.Data["cost_basis"].(string); got != string(s.CostBasis) {
			t.Fatalf("audit basis %s: %v", id, e.Data)
		}
		if got, _ := e.Data["cost_unavailable_reason"].(string); got != string(s.CostUnavailableReason) {
			t.Fatalf("audit reason %s", id)
		}
		return 1
	}
	return 0
}
