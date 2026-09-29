//go:build dev_audit

package devaudit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/codagent/agent-runner/internal/cli"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/stateio"
)

func TestJudgeAttemptUsage(t *testing.T) {
	claude := `{"type":"result","usage":{"input_tokens":10,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":4},"total_cost_usd":0.25}`
	usage, cost := judgeAttemptUsage(&cli.ClaudeAdapter{}, []byte(claude))
	if usage.Status != model.UsageCollected || usage.TokenTotals == nil || usage.TokenTotals.Total != 14 || cost == nil || *cost != 0.25 {
		t.Fatalf("Claude usage = %+v, cost = %v", usage, cost)
	}
	usage, cost = judgeAttemptUsage(&cli.CodexAdapter{}, []byte(`{"type":"turn.completed","usage":{"input_tokens":10,"cached_input_tokens":0,"output_tokens":4,"reasoning_output_tokens":0}}`))
	if usage.Status != model.UsageCollected || cost != nil {
		t.Fatalf("Codex usage = %+v, cost = %v", usage, cost)
	}
	for _, tc := range []struct {
		raw    []byte
		reason model.UnavailableReason
	}{{nil, model.UnavailableNoUsageEvent}, {[]byte("bad"), model.UnavailableParseFailure}} {
		usage, cost = judgeAttemptUsage(&cli.ClaudeAdapter{}, tc.raw)
		if usage.Status != model.UsageUnavailable || usage.Reason != tc.reason || cost != nil {
			t.Fatalf("unavailable usage = %+v, cost = %v", usage, cost)
		}
	}
}

func TestSummarizeJudgeUsageRecoversAndCountsLegacy(t *testing.T) {
	request := &Request{AuditSessionDir: t.TempDir(), AuditRunID: "audit", Auditor: AgentProvenance{CLI: "claude", Model: "opus", Effort: "high"}}
	if err := stateio.WriteJSONAtomic(filepath.Join(request.AuditSessionDir, "value-packages.json"), []ValuePackage{{BatchID: "batch-a"}, {BatchID: "batch-b"}}); err != nil {
		t.Fatal(err)
	}
	completed := filepath.Join(request.AuditSessionDir, "model-output", "batch-a.json")
	if err := stateio.WriteJSONAtomic(completed, map[string]any{"batch_id": "batch-a"}); err != nil {
		t.Fatal(err)
	}
	if err := stateio.WriteJSONAtomic(filepath.Join(request.AuditSessionDir, "model-output", "batch-b.json"), map[string]any{"batch_id": "batch-b"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(request.AuditSessionDir, "judge-usage", "value", "batch-a", "attempt.json")
	if err := stateio.WriteJSONDurable(path, JudgeAttempt{Outcome: "exited", Usage: model.UsageRecord{Status: model.UsageCollected, TokenTotals: &model.TokenTotals{Total: 12}}}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(request.AuditSessionDir, "judge-usage", "value", "batch-a", "corrupt.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	summary, err := summarizeJudgeUsage(request)
	if err != nil {
		t.Fatal(err)
	}
	if summary.AttemptCount != 3 || summary.TokenCoverage != model.CoveragePartial || summary.TotalTokens == nil || *summary.TotalTokens != 12 || summary.CostCoverage != model.CoverageNone || summary.CostUSD != nil {
		t.Fatalf("summary = %+v", summary)
	}
	var recovered int
	for _, attempt := range summary.Attempts {
		if attempt.Recovered {
			recovered++
			if attempt.Outcome != "succeeded" {
				t.Errorf("recovered outcome = %q, want succeeded", attempt.Outcome)
			}
		}
	}
	if recovered != 1 {
		t.Errorf("recovered attempts = %d, want 1", recovered)
	}
}

func TestSummarizeJudgeUsageMapsOrphanedExitedToUnknown(t *testing.T) {
	request := &Request{AuditSessionDir: t.TempDir(), AuditRunID: "audit", Auditor: AgentProvenance{CLI: "claude", Model: "opus", Effort: "high"}}
	if err := stateio.WriteJSONAtomic(filepath.Join(request.AuditSessionDir, "value-packages.json"), []ValuePackage{{BatchID: "value-002"}, {BatchID: "value-003"}}); err != nil {
		t.Fatal(err)
	}
	if err := stateio.WriteJSONAtomic(filepath.Join(request.AuditSessionDir, "model-output", "value-002.json"), map[string]any{"batch_id": "value-002"}); err != nil {
		t.Fatal(err)
	}
	for _, record := range []struct {
		batch, name, outcome string
	}{
		{"value-002", "attempt-1", "exited"},
		{"value-002", "attempt-2", "succeeded"},
		{"value-003", "attempt-1", "exited"},
	} {
		path := filepath.Join(request.AuditSessionDir, "judge-usage", "value", record.batch, record.name+".json")
		if err := stateio.WriteJSONDurable(path, JudgeAttempt{Outcome: record.outcome}); err != nil {
			t.Fatal(err)
		}
	}
	summary, err := summarizeJudgeUsage(request)
	if err != nil {
		t.Fatal(err)
	}
	var withOutput, withoutOutput []string
	for _, attempt := range summary.Attempts {
		switch attempt.BatchID {
		case "value-002":
			withOutput = append(withOutput, attempt.Outcome)
		case "value-003":
			withoutOutput = append(withoutOutput, attempt.Outcome)
		}
	}
	if summary.AttemptCount != 3 {
		t.Errorf("attempt count = %d, want 3", summary.AttemptCount)
	}
	if diff := cmp.Diff([]string{"unknown", "succeeded"}, withOutput); diff != "" {
		t.Errorf("value-002 outcomes (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"unknown"}, withoutOutput); diff != "" {
		t.Errorf("value-003 outcomes (-want +got):\n%s", diff)
	}
}

func TestJudgeUsageLedgerDoesNotReadOutsideRoot(t *testing.T) {
	request := &Request{AuditSessionDir: t.TempDir(), AuditRunID: "audit"}
	if err := stateio.WriteJSONAtomic(filepath.Join(request.AuditSessionDir, "value-packages.json"), []ValuePackage{{BatchID: "batch"}}); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "forged.json")
	cost := 999.0
	if err := stateio.WriteJSONAtomic(outside, JudgeAttempt{Outcome: "succeeded", CostUSD: &cost, Usage: model.UsageRecord{Status: model.UsageCollected, TokenTotals: &model.TokenTotals{Total: 999}}}); err != nil {
		t.Fatal(err)
	}
	ledger := filepath.Join(request.AuditSessionDir, "judge-usage", "value", "batch")
	if err := os.MkdirAll(ledger, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(ledger, "attempt.json")); err != nil {
		t.Fatal(err)
	}
	summary, err := summarizeJudgeUsage(request)
	if err != nil {
		t.Fatal(err)
	}
	if summary.AttemptCount != 1 || summary.CostUSD != nil || summary.TotalTokens != nil {
		t.Fatalf("external ledger data was trusted: %+v", summary)
	}
}
