//go:build dev_audit

package devaudit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/stateio"
)

// INT-001 and INT-002 exercise the child-process, ledger, output, and resume boundary.
func TestJudgeUsageProcessFailureAndResume(t *testing.T) {
	request, pkg := crosscheckFixture(t)
	response := `{"type":"result","subtype":"success","is_error":false,"usage":{"input_tokens":10,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":4},"total_cost_usd":0.25,"structured_output":{"batch_id":"` + pkg.BatchID + `","observations":[]}}`
	stubCrosscheck(t, response, "", "1", nil)
	if err := ensureValueOutputs(request); err == nil {
		t.Fatal("nonzero exit passed")
	}
	summary, err := summarizeJudgeUsage(request)
	if err != nil {
		t.Fatal(err)
	}
	if summary.AttemptCount != 1 || summary.Attempts[0].Outcome != "failed" || summary.Attempts[0].FailureCategory != "process_exit" || summary.TokenCoverage != model.CoverageComplete || summary.CostUSD == nil || *summary.CostUSD != 0.25 {
		t.Fatalf("failed attempt: %+v", summary)
	}
	if attempt := summary.Attempts[0]; attempt.AuditRunID != request.AuditRunID || attempt.Stage != "value" || attempt.BatchID != pkg.BatchID || attempt.CLI != "claude" || attempt.Model != "fable" {
		t.Fatalf("attempt identity: %+v", attempt)
	}
	stubCrosscheck(t, response, "", "0", nil)
	if err := ensureValueOutputs(request); err != nil {
		t.Fatal(err)
	}
	summary, err = summarizeJudgeUsage(request)
	if err != nil {
		t.Fatal(err)
	}
	if summary.AttemptCount != 2 || summary.TokenCoverage != model.CoverageComplete || summary.CostCoverage != model.CoverageComplete || *summary.TotalTokens != 28 || *summary.CostUSD != 0.5 {
		t.Fatalf("resumed attempts: %+v", summary)
	}
	if err := ensureValueOutputs(request); err != nil {
		t.Fatal(err)
	}
	again, err := summarizeJudgeUsage(request)
	if err != nil {
		t.Fatal(err)
	}
	if again.AttemptCount != 2 {
		t.Fatalf("completed batch reran: %+v", again)
	}
}

func TestJudgeUsageLaunchFailureCreatesNoAttempt(t *testing.T) {
	request, pkg := crosscheckFixture(t)
	original := crosscheckCommand
	t.Cleanup(func() { crosscheckCommand = original })
	crosscheckCommand = func([]string, string, string) (*exec.Cmd, error) { return exec.Command("/nonexistent-judge-189"), nil }
	if _, err := invokeCrosscheckValueBatch(request, pkg); err == nil {
		t.Fatal("missing executable passed")
	}
	summary, err := summarizeJudgeUsage(request)
	if err != nil {
		t.Fatal(err)
	}
	if summary.AttemptCount != 0 || summary.TokenCoverage != model.CoverageNone {
		t.Fatalf("unlaunched attempt: %+v", summary)
	}
}

// INT-003 checks the HTTP ranges and the append-only v1 header upgrade.
func TestJudgeUsageSheetHeaderUpgradeAndProjection(t *testing.T) {
	header := append([]string{}, stepValueHeader...)
	var puts, appends int
	var failPut bool
	var row []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			_, _ = w.Write([]byte(`{"access_token":"access"}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "1:1"):
			_ = json.NewEncoder(w).Encode(map[string]any{"values": [][]string{header}})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "B:B"):
			_ = json.NewEncoder(w).Encode(map[string]any{"values": [][]string{{"observation_id"}}})
		case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "AF1:AL1"):
			puts++
			if failPut {
				failPut = false
				http.Error(w, "temporary failure", http.StatusServiceUnavailable)
				return
			}
			var body struct {
				Values [][]string `json:"values"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if !equalStrings(body.Values[0], judgeHeader) {
				t.Errorf("upgrade cells: %v", body.Values)
			}
			header = append(header, body.Values[0]...)
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "A:AL:append"):
			appends++
			var body struct {
				Values [][]string `json:"values"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			row = body.Values[0]
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected", 400)
		}
	}))
	defer server.Close()
	store := ConnectionStore{Home: t.TempDir(), allowInsecureTokenURI: true}
	if err := store.Write(&Connection{SpreadsheetID: "sheet", Tab: "audit", ClientID: "client", ClientSecret: "secret", TokenURI: server.URL + "/token", RefreshToken: "refresh"}); err != nil {
		t.Fatal(err)
	}
	total, cost := int64(1200), 4.25
	report := LocalReport{Destination: DestinationState{State: "configured", SpreadsheetID: "sheet", Tab: "audit"}, JudgeUsage: &JudgeUsageSummary{CLI: "claude", Effort: "high", AttemptCount: 4, TotalTokens: &total, TokenCoverage: model.CoverageComplete, CostUSD: &cost, CostCoverage: model.CoverageComplete}, Values: ValueValidationResult{Observations: []ValueObservation{{ObservationSkeleton: ObservationSkeleton{SchemaVersion: valueSchemaVersion, ObservationID: "obs", AuditRunID: "audit"}}}}}
	reporter := SheetsReporter{Store: store, HTTPClient: server.Client(), SheetsBaseURL: server.URL}
	if err := reporter.Deliver(context.Background(), &report); err != nil {
		t.Fatal(err)
	}
	if puts != 1 || appends != 1 || len(row) != 38 || row[0] != sheetRowSchemaVersion || row[31] != "claude" || row[34] != "1200" || row[36] != "4.25" {
		t.Fatalf("puts=%d appends=%d row=%v", puts, appends, row)
	}
	// A trailing populated cell makes a v1 prefix unsafe to upgrade.
	header = append(append([]string{}, stepValueHeader...), "occupied")
	puts, appends = 0, 0
	if err := reporter.Deliver(context.Background(), &report); err == nil {
		t.Fatal("accepted populated trailing header")
	}
	if puts != 0 || appends != 0 {
		t.Fatalf("modified mismatched header: puts=%d appends=%d", puts, appends)
	}
	header = append([]string{}, stepValueHeader...)
	failPut = true
	report.DeliveryState = "pending"
	if err := reporter.Deliver(context.Background(), &report); err == nil {
		t.Fatal("upgrade failure was accepted")
	}
	if puts != 1 || appends != 0 || report.DeliveryState == "delivered" {
		t.Fatalf("failed upgrade changed rows: puts=%d appends=%d", puts, appends)
	}
	if err := reporter.Deliver(context.Background(), &report); err != nil {
		t.Fatal(err)
	}
	if puts != 2 || appends != 1 {
		t.Fatalf("upgrade retry: puts=%d appends=%d", puts, appends)
	}
	report.JudgeUsage.CostUSD = nil
	report.JudgeUsage.CostCoverage = model.CoverageNone
	if err := reporter.Deliver(context.Background(), &report); err != nil {
		t.Fatal(err)
	}
	if row[36] != "" || row[37] != "none" {
		t.Fatalf("unreported cost cells: %v", row[36:])
	}
	report.JudgeUsage = nil
	if err := reporter.Deliver(context.Background(), &report); err != nil {
		t.Fatal(err)
	}
	for _, cell := range row[33:] {
		if cell != "" {
			t.Fatalf("legacy report wrote judge totals: %v", row[31:])
		}
	}
}

func TestJudgeUsageCorruptLedgerAndLegacyOutput(t *testing.T) {
	request, _ := crosscheckFixture(t)
	if err := stateio.WriteJSONAtomic(filepath.Join(request.AuditSessionDir, "value-packages.json"), []ValuePackage{{BatchID: "legacy"}}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(request.AuditSessionDir, "model-output", "legacy.json")
	if err := stateio.WriteJSONAtomic(path, map[string]any{"batch_id": "legacy"}); err != nil {
		t.Fatal(err)
	}
	corrupt := filepath.Join(request.AuditSessionDir, "judge-usage", "value", "legacy", "attempt.json")
	if err := os.MkdirAll(filepath.Dir(corrupt), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(corrupt, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	summary, err := summarizeJudgeUsage(request)
	if err != nil {
		t.Fatal(err)
	}
	if summary.AttemptCount != 1 || summary.Attempts[0].Legacy || summary.TokenCoverage != model.CoverageNone {
		t.Fatalf("corrupt record: %+v", summary)
	}
	forged := filepath.Join(request.AuditSessionDir, "model-output", "forged.json")
	if err := stateio.WriteJSONAtomic(forged, JudgeAttempt{Outcome: "succeeded", CostUSD: floatPtr(999)}); err != nil {
		t.Fatal(err)
	}
	summary, err = summarizeJudgeUsage(request)
	if err != nil {
		t.Fatal(err)
	}
	if summary.AttemptCount != 1 || summary.CostUSD != nil {
		t.Fatalf("judge-controlled file affected usage: %+v", summary)
	}
}

func floatPtr(value float64) *float64 { return &value }

func TestJudgeUsageCorrectnessAndOutputWriteFailure(t *testing.T) {
	request, pkg := crosscheckFixture(t)
	response := `{"type":"result","subtype":"success","is_error":false,"usage":{"input_tokens":10,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":4},"total_cost_usd":0.25,"structured_output":{"batch_id":"` + pkg.BatchID + `","observations":[]}}`
	stubCrosscheck(t, response, "", "0", nil)
	outputPath := filepath.Join(request.AuditSessionDir, "model-output", pkg.BatchID+".json")
	originalWrite := writeJudgeOutput
	t.Cleanup(func() { writeJudgeOutput = originalWrite })
	writeJudgeOutput = func(path string, value any) error {
		files, err := filepath.Glob(filepath.Join(request.AuditSessionDir, "judge-usage", "value", pkg.BatchID, "*.json"))
		if err != nil {
			t.Fatal(err)
		}
		if len(files) != 1 {
			t.Fatalf("exit record missing before output write: %v", files)
		}
		data, err := os.ReadFile(files[0])
		if err != nil {
			t.Fatal(err)
		}
		var attempt JudgeAttempt
		if err := json.Unmarshal(data, &attempt); err != nil {
			t.Fatal(err)
		}
		if attempt.Outcome != "exited" {
			t.Fatalf("record before output: %+v", attempt)
		}
		return os.ErrPermission
	}
	if err := ensureValueOutputs(request); err == nil {
		t.Fatal("output write unexpectedly passed")
	}
	summary, err := summarizeJudgeUsage(request)
	if err != nil {
		t.Fatal(err)
	}
	if summary.AttemptCount != 1 || summary.Attempts[0].FailureCategory != "output_write_failed" || summary.TokenCoverage != model.CoverageComplete {
		t.Fatalf("failed output write: %+v", summary)
	}
	if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
		t.Fatalf("failed write published output: %v", err)
	}
	writeJudgeOutput = originalWrite
	if err := ensureValueOutputs(request); err != nil {
		t.Fatal(err)
	}
	stubCrosscheck(t, `{"type":"result","subtype":"success","is_error":false,"usage":{"input_tokens":10,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":4},"total_cost_usd":0.25,"structured_output":{"candidates":[]}}`, "", "0", nil)
	if err := ensureCorrectnessOutput(request); err != nil {
		t.Fatal(err)
	}
	summary, err = summarizeJudgeUsage(request)
	if err != nil {
		t.Fatal(err)
	}
	if summary.AttemptCount != 3 || summary.CostUSD == nil || *summary.CostUSD != 0.75 {
		t.Fatalf("all attempts: %+v", summary)
	}
}

func TestJudgeUsageMissingSessionIDIsUnknownInRecordAndProvenance(t *testing.T) {
	request, pkg := crosscheckFixture(t)
	stubCrosscheck(t, `{"type":"result","subtype":"success","is_error":false,"structured_output":{"batch_id":"`+pkg.BatchID+`","observations":[]}}`, "", "0", nil)
	output, handle, err := invokeCrosscheckValueBatchWithAttempt(request, pkg)
	if err != nil {
		t.Fatal(err)
	}
	if handle.record.SessionID != "unknown" || output.Provenance.SessionID != "unknown" {
		t.Fatalf("record session=%q provenance session=%q, want unknown", handle.record.SessionID, output.Provenance.SessionID)
	}
}
