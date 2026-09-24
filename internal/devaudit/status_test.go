//go:build dev_audit

package devaudit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/stateio"
)

func TestReadStatusDerivesDeliveryOutcome(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	lifecycle := Lifecycle{Version: 1, SourceRunID: "source", Links: []Link{
		{AuditRunID: "audit-delivered", State: LaunchCompleted},
		{AuditRunID: "audit-pending", State: LaunchCompleted, ReportingWarning: "Sheets reporting pending: token refresh failed"},
		{AuditRunID: "audit-stage-failed", State: LaunchCompleted, Warning: "value-audit failed: argument list too long"},
		{AuditRunID: "audit-no-warning", State: LaunchCompleted},
		{AuditRunID: "audit-launch-failed", State: LaunchFailed, Warning: "resolve auditor profile"},
		{AuditRunID: "audit-running", State: LaunchStarted},
		{AuditRunID: "audit-corrupt", State: LaunchCompleted},
		{AuditRunID: "audit-interrupted", State: LaunchReserved},
	}}
	if err := writeLifecycle(filepath.Join(source, lifecycleFileName), lifecycle); err != nil {
		t.Fatal(err)
	}
	for id, delivery := range map[string]string{"audit-delivered": "delivered", "audit-pending": "pending"} {
		if err := stateio.WriteJSONAtomic(filepath.Join(root, id, "local-report.json"), LocalReport{AuditRunID: id, DeliveryState: delivery}); err != nil {
			t.Fatal(err)
		}
	}
	if err := stateio.WriteState(&model.RunState{RunID: "audit-no-warning", FailureReason: "correctness-audit failed"}, filepath.Join(root, "audit-no-warning")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "audit-corrupt"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "audit-corrupt", "local-report.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	status, err := ReadStatus(source)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][2]string{
		"audit-delivered":     {OutcomeDelivered, ""},
		"audit-pending":       {OutcomePendingDelivery, "Sheets reporting pending: token refresh failed"},
		"audit-stage-failed":  {OutcomeFailed, "value-audit failed: argument list too long"},
		"audit-no-warning":    {OutcomeFailed, "correctness-audit failed"},
		"audit-launch-failed": {OutcomeFailed, "resolve auditor profile"},
		"audit-running":       {OutcomeActive, ""},
		"audit-corrupt":       {OutcomeFailed, "decode local report: unexpected end of JSON input"},
		"audit-interrupted":   {OutcomeFailed, "audit reservation was never launched; run `agent-runner audit reconcile` for its execution session"},
	}
	if len(status.Links) != len(want) {
		t.Fatalf("status has %d links", len(status.Links))
	}
	for _, link := range status.Links {
		expected := want[link.AuditRunID]
		if link.Outcome != expected[0] || link.Reason != expected[1] {
			t.Errorf("%s = %q (%q), want %q (%q)", link.AuditRunID, link.Outcome, link.Reason, expected[0], expected[1])
		}
		if link.AuditSessionDir != filepath.Join(root, link.AuditRunID) {
			t.Errorf("%s audit dir = %q", link.AuditRunID, link.AuditSessionDir)
		}
	}
}
