//go:build dev_audit

package devaudit

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/metrics"
	"github.com/codagent/agent-runner/internal/model"
)

func TestPrepareEvidenceProjectsSelectedMetricsSessionFromPipeline(t *testing.T) {
	snapshot := t.TempDir()
	collector := metrics.NewCollector(snapshot, "source-run", "core:example", time.Unix(0, 0).UTC())
	session := "session-selected"
	collector.Process(audit.Event{Timestamp: "2026-09-01T00:00:00Z", Type: audit.EventRunStart, Data: map[string]any{"execution_session_id": session}})
	collector.Process(audit.Event{Timestamp: "2026-09-01T00:00:01Z", Type: audit.EventStepEnd, Data: map[string]any{
		"outcome": "success", "duration_ms": int64(100),
		metrics.DataIdentity: model.ExecutionIdentity{ExecutionSessionID: session, Prefix: "[group, implement]", StepID: "implement", StepType: "agent", Kind: "step"},
	}})
	collector.Process(audit.Event{Timestamp: "2026-09-01T00:00:02Z", Type: audit.EventRunEnd, Data: map[string]any{"outcome": "success"}})
	request := Request{AuditRunID: "audit-run", AuditSessionDir: filepath.Join(t.TempDir(), "audit"), SnapshotPath: snapshot, SourceRunID: "source-run", ExecutionSessionID: session, SourceWorkflow: "core:example", Trigger: "automatic"}
	prepared, err := PrepareEvidence(request)
	if err != nil {
		t.Fatalf("prepare evidence: %v", err)
	}
	if len(prepared.Index.Leaves) != 1 || prepared.Index.Leaves[0].Skeleton.StepID != "group/implement" {
		t.Fatalf("prepared leaves = %#v", prepared.Index.Leaves)
	}
	if len(prepared.Packages) != 1 || encodedJSONBytes(prepared.Packages[0]) > defaultPackageBytes {
		t.Fatalf("bounded packages = %#v", prepared.Packages)
	}
}
