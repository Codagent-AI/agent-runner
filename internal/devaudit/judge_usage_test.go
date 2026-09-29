//go:build dev_audit

package devaudit

import (
	"os"
	"path/filepath"
	"testing"

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
