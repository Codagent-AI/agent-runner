package interactive

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/codagent/agent-runner/internal/cli"
)

func TestDirectRunnerFinalReportWaitINT003(t *testing.T) {
	for _, mode := range []string{"matching", "missing", "later", "disabled", "durability-failed"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			config := filepath.Join(dir, "claude")
			session := "11111111-1111-1111-1111-111111111111"
			uc := cli.UsageContext{Workdir: dir, Env: append(os.Environ(), "CLAUDE_CONFIG_DIR="+config), StateDir: dir}
			collector := &cli.ClaudeAdapter{}
			plan, err := collector.PrepareInteractiveUsage(session, false, uc)
			if err != nil {
				t.Fatal(err)
			}
			transcript := filepath.Join(config, "projects", "short", session+".jsonl")
			t.Setenv("AGENT_RUNNER_USAGE_TEST_TRANSCRIPT", transcript)
			t.Setenv("AGENT_RUNNER_USAGE_TEST_REPORT", plan.ReportPath)
			t.Setenv("AGENT_RUNNER_USAGE_TEST_MODE", mode)
			t.Setenv("AGENT_RUNNER_DIRECT_HELPER", "1")
			if mode == "disabled" {
				plan.ReportEnabled = false
			}
			if mode == "durability-failed" {
				t.Setenv("AGENT_RUNNER_DIRECT_HELPER", "2")
			}
			server := newTestControlServer(t, dir, &recordingEventLogger{})
			defer server.Close()
			options := &DirectOptions{Args: []string{os.Args[0], "-test.run=^TestDirectRunnerHelperProcess$"}, StepID: "step", SessionID: session, CLI: "fake", Control: server, Probe: immediateDurabilityProbe{}, UsageCollector: collector, UsagePlan: plan, FinalReportTimeout: 350 * time.Millisecond, DurabilityTimeout: 100 * time.Millisecond, TerminationGrace: 100 * time.Millisecond}
			if mode == "durability-failed" {
				options.Probe = usageWaitFailureProbe{}
			}
			start := time.Now()
			result, err := NewDirectRunner(options).Run(context.Background())
			elapsed := time.Since(start)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "durability-failed" {
				if !result.DurabilityFailed {
					t.Fatalf("%+v", result)
				}
			} else if !result.Completed {
				t.Fatalf("%+v", result)
			}
			if mode == "matching" {
				raw, err := os.ReadFile(plan.ReportPath)
				if err != nil || len(raw) == 0 || elapsed >= time.Second {
					t.Fatalf("report %s err %v elapsed %v", raw, err, elapsed)
				}
			}
			if (mode == "missing" || mode == "later") && (elapsed < 350*time.Millisecond || elapsed > time.Second) {
				t.Fatalf("wait took %v", elapsed)
			}
			if (mode == "disabled" || mode == "durability-failed") && elapsed > 350*time.Millisecond {
				t.Fatalf("unexpected wait: %v", elapsed)
			}
		})
	}
}

func prepareUsageWaitHelper(t *testing.T) {
	path := os.Getenv("AGENT_RUNNER_USAGE_TEST_TRANSCRIPT")
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	appendUsageWaitMessage(t, path, "final", 8)
}

func appendUsageWaitMessage(t *testing.T, path, id string, output int) {
	t.Helper()
	entry := map[string]any{"type": "assistant", "timestamp": time.Now().UTC(), "message": map[string]any{"id": id, "model": "opus", "content": []any{}, "usage": map[string]int{"input_tokens": 2, "cache_read_input_tokens": 3, "cache_creation_input_tokens": 4, "output_tokens": output}}}
	raw, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.Write(append(raw, '\n'))
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
}

func emitUsageWaitHelperReport(t *testing.T) {
	mode := os.Getenv("AGENT_RUNNER_USAGE_TEST_MODE")
	if mode != "matching" && mode != "later" {
		return
	}
	time.Sleep(200 * time.Millisecond)
	if mode == "later" {
		appendUsageWaitMessage(t, os.Getenv("AGENT_RUNNER_USAGE_TEST_TRANSCRIPT"), "later", 9)
	}
	cost := 0.42
	usage := json.RawMessage(`{"input_tokens":2,"cache_read_input_tokens":3,"cache_creation_input_tokens":4,"output_tokens":8}`)
	r := cli.ClaudeStatusReport{RecordedAt: time.Now().UTC(), SessionID: "11111111-1111-1111-1111-111111111111", PromptID: "prompt", TotalCostUSD: &cost, CurrentUsage: usage}
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(os.Getenv("AGENT_RUNNER_USAGE_TEST_REPORT"), append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

type usageWaitFailureProbe struct{}

func (usageWaitFailureProbe) Checkpoint(context.Context, string) (cli.Checkpoint, error) {
	return cli.Checkpoint{}, nil
}
func (usageWaitFailureProbe) WaitForCommittedTurn(context.Context, string, cli.Checkpoint) error {
	return errors.New("test durability failure")
}

// A collector stalled in filesystem I/O must not extend terminal ownership.
type stalledUsageCollector struct {
	*cli.ClaudeAdapter
	release <-chan struct{}
}

func (s stalledUsageCollector) WaitForFinalReport(context.Context, cli.InteractiveUsagePlan) {
	<-s.release
}

func TestDirectRunnerFinalReportDeadlineBoundsCollector(t *testing.T) {
	server := newTestControlServer(t, t.TempDir(), &recordingEventLogger{})
	defer server.Close()
	t.Setenv("AGENT_RUNNER_DIRECT_HELPER", "1")
	t.Setenv("AGENT_RUNNER_USAGE_TEST_MODE", "disabled")
	release := make(chan struct{})
	defer close(release)
	collector := stalledUsageCollector{ClaudeAdapter: &cli.ClaudeAdapter{}, release: release}
	options := &DirectOptions{Args: []string{os.Args[0], "-test.run=^TestDirectRunnerHelperProcess$"}, StepID: "step", SessionID: "session", CLI: "fake", Control: server, Probe: immediateDurabilityProbe{}, UsageCollector: collector, UsagePlan: cli.InteractiveUsagePlan{ReportEnabled: true}, FinalReportTimeout: 50 * time.Millisecond, DurabilityTimeout: 100 * time.Millisecond, TerminationGrace: 100 * time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	start := time.Now()
	result, err := NewDirectRunner(options).Run(ctx)
	if err != nil || !result.Completed || time.Since(start) > 300*time.Millisecond {
		t.Fatalf("result=%+v err=%v elapsed=%v", result, err, time.Since(start))
	}
}
