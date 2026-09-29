package exec

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/cli"
	"github.com/codagent/agent-runner/internal/metrics"
	"github.com/codagent/agent-runner/internal/model"
)

func TestClaudeInvocationSubagentUsageReachesCollector(t *testing.T) {
	root := t.TempDir()
	work := t.TempDir()
	session := "55555555-5555-5555-5555-555555555555"
	project := filepath.Join(root, "projects", "short-project")
	sub := filepath.Join(project, session, "subagents")
	writeUsageFile(t, filepath.Join(project, session+".jsonl"), `{"uuid":"first","type":"assistant","message":{"content":[{"type":"tool_use","name":"Agent","id":"tool"}]}}`+"\n"+`{"uuid":"last","type":"assistant","message":{"content":[]}}`+"\n")
	writeUsageFile(t, filepath.Join(sub, "agent-a.meta.json"), `{"toolUseId":"tool","agentType":"Explore"}`)
	writeUsageFile(t, filepath.Join(sub, "agent-a.jsonl"), `{"type":"assistant","message":{"id":"m","model":"haiku","usage":{"input_tokens":2,"cache_read_input_tokens":3,"cache_creation_input_tokens":4,"output_tokens":5}}}`+"\n")
	stdout := `{"uuid":"first","type":"system","session_id":"` + session + `","model":"opus"}` + "\n" + `{"uuid":"last","type":"assistant","message":{"model":"opus"}}` + "\n" + `{"type":"result","session_id":"` + session + `","usage":{"input_tokens":1,"cache_read_input_tokens":2,"cache_creation_input_tokens":3,"output_tokens":4},"total_cost_usd":5.77}` + "\n"
	runner := &invocationRecordingRunner{options: make(chan AgentProcessOptions, 1), result: ProcessResult{Started: true, Stdout: stdout}}
	got, err := InvokeAgent(&AgentInvocation{Adapter: &cli.ClaudeAdapter{}, Args: []string{"claude"}, CLI: "claude", Workdir: work, Env: []string{"CLAUDE_CONFIG_DIR=" + root}, InvocationContext: cli.ContextAutonomousHeadless}, runner, &mockLogger{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Usage.SubagentCollection != model.CompletenessComplete || len(got.Usage.Allocations) != 2 || got.Usage.TokenTotals.Total != 24 {
		t.Fatalf("usage=%+v", got.Usage)
	}
	dir := t.TempDir()
	collector := metrics.NewCollector(dir, "run", "workflow", time.Now())
	collector.Process(audit.Event{Type: audit.EventStepEnd, Timestamp: time.Now().UTC().Format(time.RFC3339Nano), Data: map[string]any{metrics.DataIdentity: model.ExecutionIdentity{StepID: "step", StepType: "agent", Kind: "step", AgentInvoked: true, CLI: "claude", SessionID: session}, metrics.DataUsage: got.Usage, "outcome": "success"}})
	raw, err := os.ReadFile(filepath.Join(dir, metrics.FileName))
	if err != nil {
		t.Fatal(err)
	}
	var artifact struct {
		Totals struct {
			EstimatedAPICostUSD *float64 `json:"estimated_api_cost_usd"`
			TokenTotals         struct {
				Total int64 `json:"total"`
			} `json:"token_totals"`
		} `json:"totals"`
		NativeMeasurements []struct {
			Version     int `json:"native_measurement_schema_version"`
			Allocations []struct {
				Kind                string  `json:"kind"`
				ObservedIdentityRef *string `json:"observed_identity_ref"`
				AgentType           string  `json:"agent_type"`
				ToolUseID           string  `json:"tool_use_id"`
			} `json:"allocations"`
			UnallocatedUsage any `json:"unallocated_usage"`
		} `json:"native_measurements"`
	}
	if err := json.Unmarshal(raw, &artifact); err != nil {
		t.Fatal(err)
	}
	n := artifact.NativeMeasurements[0]
	if artifact.Totals.EstimatedAPICostUSD == nil || *artifact.Totals.EstimatedAPICostUSD != 5.77 || artifact.Totals.TokenTotals.Total != 24 {
		t.Fatalf("totals=%+v", artifact.Totals)
	}
	if n.Version != 2 || len(n.Allocations) != 2 || n.Allocations[1].AgentType != "Explore" || n.Allocations[1].ToolUseID != "tool" || n.Allocations[1].ObservedIdentityRef == nil || n.UnallocatedUsage != nil {
		t.Fatalf("native=%+v", n)
	}
}
func writeUsageFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
