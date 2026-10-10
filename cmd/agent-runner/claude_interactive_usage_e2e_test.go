package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codagent/agent-runner/internal/metrics"
	"github.com/codagent/agent-runner/internal/model"
)

func TestInteractiveClaudeUsageE2E001(t *testing.T) {
	root := t.TempDir()
	runner := filepath.Join(root, "agent-runner")
	buildAgentRunner(t, findRepoRoot(t), runner)
	for _, mode := range []string{"final", "missing"} {
		t.Run(mode, func(t *testing.T) {
			tmp := t.TempDir()
			home := filepath.Join(tmp, "home")
			work := filepath.Join(tmp, "work")
			bin := filepath.Join(tmp, "bin")
			for _, dir := range []string{home, work, bin} {
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			var resolveErr error
			work, resolveErr = filepath.EvalSymlinks(work)
			if resolveErr != nil {
				t.Fatal(resolveErr)
			}
			writeSmokeProfileConfig(t, home)
			writeProjectSmokeConfig(t, work)
			writeTestFile(t, filepath.Join(home, ".agent-runner", "settings.yaml"), "theme: dark\nautonomous_backend: interactive-claude\n")
			writeTestFile(t, filepath.Join(work, ".claude", "settings.json"), `{"statusLine":{"type":"command","command":"printf USER-LINE","padding":4}}`)
			writeTestFile(t, filepath.Join(work, ".agent-runner", "workflows", "interactive-usage-v1.0.yaml"), `name: interactive-usage
steps:
  - id: first
    agent: claude_headless_smoke
    session: new
    prompt: "first"
  - id: child
    workflow: interactive-usage-child-v1.0.yaml
`)
			writeTestFile(t, filepath.Join(work, ".agent-runner", "workflows", "interactive-usage-child-v1.0.yaml"), `name: interactive-usage-child
steps:
 - id: second
   session: inherit
   prompt: "second"
`)
			script := fmt.Sprintf("#!/bin/sh\nexec %q -test.run='^TestInteractiveUsageFixtureProcess$' -- \"$@\"\n", currentTestBinary(t))
			if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(runner, "--headless", "--profile", "smoke_test", "interactive-usage")
			cmd.Dir = work
			// This PTY captures bytes but does not emulate terminal query replies.
			// Keep background-color probes from racing the fixture stdin reader.
			cmd.Env = smokeCommandEnv(os.Environ(), "TERM=dumb", "HOME="+home, "CLAUDE_CONFIG_DIR="+filepath.Join(home, ".claude"), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "AGENT_RUNNER_NO_TUI=1", "AGENT_RUNNER_USAGE_FIXTURE="+mode)
			out, err := runCommandInPTY(cmd, 30*time.Second)
			if err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
			if !strings.Contains(out, "USER-LINE") {
				t.Fatalf("missing user status line: %s", out)
			}
			dir := latestWorkflowRunDir(t, home, work, "interactive-usage")
			raw, err := os.ReadFile(filepath.Join(dir, "run-metrics.json"))
			if err != nil {
				t.Fatal(err)
			}
			var artifact struct {
				Steps []struct {
					ID    string            `json:"id"`
					Usage model.UsageRecord `json:"usage"`
					Cost  *float64          `json:"estimated_api_cost_usd"`
				} `json:"steps"`
				Totals             model.RunTotals             `json:"totals"`
				NativeMeasurements []metrics.NativeMeasurement `json:"native_measurements"`
			}
			if err = json.Unmarshal(raw, &artifact); err != nil {
				t.Fatal(err)
			}
			agentSteps := artifact.Steps[:0]
			for _, step := range artifact.Steps {
				if step.Usage.CLI != "" {
					agentSteps = append(agentSteps, step)
				}
			}
			artifact.Steps = agentSteps
			if len(artifact.Steps) != 2 || artifact.Totals.UsageCoverage != model.CoverageComplete {
				t.Fatalf("metrics: %s", raw)
			}
			for i, step := range artifact.Steps {
				var native *metrics.NativeMeasurement
				for j := range artifact.NativeMeasurements {
					candidate := &artifact.NativeMeasurements[j]
					if candidate.Attribution.StepID == step.ID {
						native = candidate
						break
					}
				}
				if native == nil {
					t.Fatalf("missing native measurement for %s: %s", step.ID, raw)
				}
				if mode == "final" {
					want := []float64{0.42, 0.30}[i]
					if len(native.Costs) != 1 {
						t.Fatalf("native costs for %s: %+v", step.ID, native.Costs)
					}
					cost := native.Costs[0]
					if cost.ID != "reported-cost" || cost.Scope != "attempt" || cost.Coverage != "full" || cost.Overlap != "established" || cost.Amount.Value == nil || *cost.Amount.Value != want {
						t.Fatalf("native cost for %s: %+v", step.ID, cost)
					}
				} else if len(native.Costs) != 0 {
					t.Fatalf("expected no native costs for %s: %+v", step.ID, native.Costs)
				}

				if step.Usage.Source != "claude:session-transcript" || len(step.Usage.Allocations) != 2 || step.Usage.Tokens[model.TokenOutput] != 17 {
					t.Fatalf("step %d: %s", i, raw)
				}
				if mode == "final" {
					want := []float64{0.42, 0.30}[i]
					if step.Cost == nil || *step.Cost != want {
						t.Fatalf("step cost %d: %s", i, raw)
					}
				} else if step.Cost != nil {
					t.Fatalf("expected null cost: %s", raw)
				}
			}
			switch mode {
			case "final":
				if artifact.Totals.EstimatedAPICostUSD == nil || *artifact.Totals.EstimatedAPICostUSD != 0.72 || artifact.Totals.CostCoverage != model.CoverageComplete {
					t.Fatalf("%s", raw)
				}
			default:
				if artifact.Totals.CostCoverage != model.CoverageNone {
					t.Fatalf("fallback: %s", raw)
				}
				audit, err := os.ReadFile(filepath.Join(dir, "audit.log"))
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Contains(audit, []byte(`"cost_unavailable_reason":"cost-report-stale"`)) {
					t.Fatalf("audit: %s", audit)
				}
			}
		})
	}
}

func TestInteractiveUsageFixtureProcess(t *testing.T) {
	mode := os.Getenv("AGENT_RUNNER_USAGE_FIXTURE")
	if mode == "" {
		return
	}
	args := argsAfterDoubleDash(os.Args)
	session := valueAfter(args, "--session-id")
	resume := false
	if session == "" {
		session = valueAfter(args, "--resume")
		resume = true
	}
	var settings struct {
		StatusLine struct {
			Command string `json:"command"`
			Padding int    `json:"padding"`
		} `json:"statusLine"`
		Hooks map[string]any `json:"hooks"`
	}
	if err := json.Unmarshal([]byte(valueAfter(args, "--settings")), &settings); err != nil || settings.StatusLine.Padding != 4 || settings.Hooks["Stop"] == nil {
		t.Fatalf("settings: %+v %v", settings, err)
	}
	report := func(prompt string, cost float64, usage any) {
		t.Helper()
		payload, _ := json.Marshal(map[string]any{"session_id": session, "prompt_id": prompt, "cost": map[string]any{"total_cost_usd": cost}, "context_window": map[string]any{"current_usage": usage}})
		cmd := exec.Command("sh", "-c", settings.StatusLine.Command)
		cmd.Stdin = bytes.NewReader(payload)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			t.Fatal(err)
		}
	}
	report("", 0, nil)
	parent := filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "projects", "shortened", session+".jsonl")
	subdir := filepath.Join(filepath.Dir(parent), session, "subagents")
	if err := os.MkdirAll(subdir, 0o700); err != nil {
		t.Fatal(err)
	}
	suffix := "first"
	cost := 0.42
	if resume {
		suffix = "second"
		cost = 0.30
	}
	tool := "tool-" + suffix
	prompt := "prompt-" + suffix
	tuple := func(output int) map[string]int {
		return map[string]int{"input_tokens": 2, "cache_read_input_tokens": 3, "cache_creation_input_tokens": 4, "output_tokens": output}
	}
	assistant := func(id, name string, output int, content []any) map[string]any {
		return map[string]any{"type": "assistant", "timestamp": time.Now().UTC().Format(time.RFC3339Nano), "message": map[string]any{"id": id, "model": name, "usage": tuple(output), "content": content}}
	}
	appendEntry := func(path string, entry any) {
		t.Helper()
		raw, err := json.Marshal(entry)
		if err != nil {
			t.Fatal(err)
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		_, err = file.Write(append(raw, '\n'))
		_ = file.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	appendEntry(parent, map[string]any{"type": "user", "promptId": prompt, "message": map[string]any{"content": "work"}})
	appendEntry(parent, assistant("spawn-"+suffix, "opus", 4, []any{map[string]any{"type": "tool_use", "name": "Task", "id": tool}}))
	appendEntry(filepath.Join(subdir, "agent-"+suffix+".meta.json"), map[string]any{"toolUseId": tool, "agentType": "Explore"})
	appendEntry(filepath.Join(subdir, "agent-"+suffix+".jsonl"), assistant("sub-"+suffix, "haiku", 5, []any{}))
	appendEntry(parent, map[string]any{"type": "queue-operation", "timestamp": time.Now().UTC().Format(time.RFC3339Nano), "content": "<task-notification><tool-use-id>" + tool + "</tool-use-id><status>completed</status></task-notification>"})
	appendEntry(parent, assistant("final-"+suffix, "opus", 2, []any{}))
	appendEntry(parent, assistant("final-"+suffix, "opus", 8, []any{}))
	if err := completeInteractiveFixture(); err != nil {
		t.Fatal(err)
	}
	if mode == "final" {
		time.Sleep(200 * time.Millisecond)
		report(prompt, cost, tuple(8))
	} else {
		report(prompt, 0, tuple(2))
	}
	// The runner owns process termination after its final-report wait.
	if err := waitForFixtureTermination(); err != nil {
		t.Fatal(err)
	}
	os.Exit(0)
}
