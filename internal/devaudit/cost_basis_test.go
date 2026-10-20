//go:build dev_audit

package devaudit

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/stateio"

	"github.com/codagent/agent-runner/internal/metrics"
	"github.com/codagent/agent-runner/internal/model"
)

func TestCostBasisObservationAndSheetINT005(t *testing.T) {
	cost := .5
	for _, tt := range []struct {
		bases []model.CostBasis
		want  string
	}{{[]model.CostBasis{model.CostBasisReported}, "reported"}, {[]model.CostBasis{model.CostBasisEstimated}, "estimated"}, {[]model.CostBasis{model.CostBasisReported, model.CostBasisEstimated}, "mixed"}, {nil, ""}} {
		var records []metrics.StepRecord
		for _, basis := range tt.bases {
			records = append(records, metrics.StepRecord{ID: "step", Kind: "step", Type: "agent", AgentInvoked: true, EstimatedAPICostUSD: &cost, CostBasis: basis, ExecutionSessionID: "session"})
		}
		if tt.want == "" {
			records = []metrics.StepRecord{{ID: "step", Kind: "step", Type: "agent", AgentInvoked: true, ExecutionSessionID: "session"}}
		}
		root := t.TempDir()
		snapshot := filepath.Join(root, "snapshot")
		if err := os.MkdirAll(snapshot, 0o700); err != nil {
			t.Fatal(err)
		}
		for i := range records {
			records[i].RecordID = fmt.Sprintf("step#%d", i+1)
			records[i].Attempt = i + 1
		}
		artifact := metrics.Artifact{SchemaVersion: 4, RunID: "source", Workflow: "costs", Steps: records}
		if err := stateio.WriteJSONAtomic(filepath.Join(snapshot, metrics.FileName), artifact); err != nil {
			t.Fatal(err)
		}
		prepared, err := PrepareEvidence(Request{SnapshotPath: snapshot, AuditSessionDir: filepath.Join(root, "audit"), ExecutionSessionID: "session", AuditRunID: "audit", SourceRunID: "source"})
		if err != nil {
			t.Fatal(err)
		}
		if len(prepared.Index.Leaves) != 1 {
			t.Fatalf("leaves %v", prepared.Index.Leaves)
		}
		leaf := prepared.Index.Leaves[0]
		basis := leaf.Skeleton.Cost.CostBasis
		if (basis == nil) != (tt.want == "") || (basis != nil && *basis != tt.want) {
			t.Fatalf("basis %v want %s", basis, tt.want)
		}

		observation := ValueObservation{ObservationSkeleton: leaf.Skeleton}
		row, err := projectObservation(&observation, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(row) != 39 || row[38] != tt.want || (tt.want == "" && row[14] != "") {
			t.Fatalf("projected row %v", row)
		}
		population := make([]*metrics.StepRecord, len(records))
		for i := range records {
			population[i] = &records[i]
		}
		_, _, _, _, measurementBasis := leafModelScalars(population, nil)
		if (measurementBasis == nil) != (tt.want == "") || (measurementBasis != nil && *measurementBasis != tt.want) {
			t.Fatalf("measurement basis %v", measurementBasis)
		}
	}
	for _, old := range [][]string{stepValueHeader, stepValueHeaderV2, stepValueHeaderV3} {
		header := append(append([]string{}, old...), "", "")
		puts := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet {
				_ = json.NewEncoder(w).Encode(map[string]any{"values": [][]string{header}})
				return
			}
			if r.Method != http.MethodPut {
				t.Errorf("method %s", r.Method)
			}
			puts++
			var body struct {
				Values [][]string `json:"values"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if !equalStrings(body.Values[0], stepValueHeaderV3[len(old):]) {
				t.Errorf("trailing upgrade %v", body.Values)
			}
			if !strings.Contains(r.URL.Path, sheetColumn(len(old)+1)+"1:") {
				t.Errorf("range %s", r.URL.Path)
			}
			_, _ = w.Write([]byte(`{}`))
		}))
		reporter := SheetsReporter{HTTPClient: server.Client(), SheetsBaseURL: server.URL}
		if err := reporter.validateHeader(context.Background(), "token", DestinationState{SpreadsheetID: "sheet", Tab: "audit"}); err != nil {
			t.Fatal(err)
		}
		if (puts == 0) != (len(old) == len(stepValueHeaderV3)) {
			t.Fatal("wrong header mutation")
		}
		header = append(append([]string{}, old...), "occupied")
		if err := reporter.validateHeader(context.Background(), "token", DestinationState{SpreadsheetID: "sheet", Tab: "audit"}); err == nil {
			t.Fatal("accepted occupied trailing cell")
		}
		server.Close()
	}
}

func TestUnknownCostCannotExportBasis(t *testing.T) {
	basis := "estimated"
	observation := ValueObservation{ObservationSkeleton: ObservationSkeleton{SchemaVersion: valueSchemaVersion, Cost: CostEvidence{CostBasis: &basis}}}
	row, err := projectObservation(&observation, nil)
	if err != nil {
		t.Fatal(err)
	}
	if row[14] != "" || row[38] != "" {
		t.Fatalf("unknown cost exported basis %v", row)
	}
}
