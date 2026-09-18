//go:build dev_audit

package devaudit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/metrics"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/stateio"
)

func TestAuditLeafIncludesChildrenWithoutCountingThemAsRetriesOrWallTime(t *testing.T) {
	artifact := metrics.Artifact{SchemaVersion: 4, Steps: []metrics.StepRecord{
		{RecordID: "parent", ID: "validate", Kind: "step", Type: "shell", DurationMS: 100, ExecutionSessionID: "original"},
		{RecordID: "child", ID: "attempt", Prefix: "validate", Kind: "nested-agent", Type: "agent", AgentInvoked: true, DurationMS: 80, ExecutionSessionID: "original", MeasurementKey: "producer/attempt", Usage: &model.UsageRecord{Status: model.UsageCollected, TokenTotals: &model.TokenTotals{Total: 18}}},
	}}
	leaves := buildLeaves(&Request{ExecutionSessionID: "original"}, &artifact, nil, nil)
	if len(leaves) != 1 {
		t.Fatalf("leaves=%d", len(leaves))
	}
	leaf := leaves[0]
	if leaf.Skeleton.Cost.TotalTokens == nil || *leaf.Skeleton.Cost.TotalTokens != 18 {
		t.Fatalf("child tokens lost: %+v", leaf.Skeleton.Cost)
	}
	if leaf.Attempts != 1 || *leaf.Skeleton.Cost.DurationMS != 100 {
		t.Fatal("child became a retry or added overlapping duration")
	}
}
func TestReplayFiltersEveryV4MeasurementByOriginalSession(t *testing.T) {
	// Exercise the public snapshot helper with an artifact, then inspect all v4
	// arrays rather than trusting filtered legacy steps.
	source := t.TempDir()
	snapshot := t.TempDir()
	artifact := metrics.Artifact{SchemaVersion: 4, Sessions: []metrics.SessionRecord{{ExecutionSessionID: "original"}, {ExecutionSessionID: "later"}}, MeasurementHeads: []metrics.MeasurementHead{{Key: "old", Attribution: metrics.Attribution{ExecutionSessionID: "original"}, Record: json.RawMessage(`{}`)}, {Key: "later", Attribution: metrics.Attribution{ExecutionSessionID: "later"}, Record: json.RawMessage(`{}`)}}, ValidatorContexts: []metrics.DeliveryContext{{Attribution: metrics.Attribution{ExecutionSessionID: "later"}, Gaps: []string{"later-gap"}}}}
	writeMetricsFixture(t, source, &artifact)
	dir, err := snapshotReplayEvidenceAt(source, snapshot, "original")
	if err != nil {
		t.Fatal(err)
	}
	result := readMetricsFixture(t, dir)
	if len(result.MeasurementHeads) != 1 || result.MeasurementHeads[0].Key != "old" || len(result.ValidatorContexts) != 0 {
		t.Fatal("replay exposed later-session v4 measurements")
	}
}

func writeMetricsFixture(t *testing.T, dir string, a *metrics.Artifact) {
	t.Helper()
	if err := stateio.WriteJSONAtomic(filepath.Join(dir, metrics.FileName), a); err != nil {
		t.Fatal(err)
	}
}
func readMetricsFixture(t *testing.T, dir string) metrics.Artifact {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, metrics.FileName))
	if err != nil {
		t.Fatal(err)
	}
	var a metrics.Artifact
	if err = json.Unmarshal(b, &a); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestAuditIncludesCommonNativeEvidenceWithoutChildren(t *testing.T) {
	artifact := metrics.Artifact{SchemaVersion: 4, Steps: []metrics.StepRecord{{RecordID: "native", ID: "implement", Kind: "step", Type: "agent", AgentInvoked: true, ExecutionSessionID: "original"}}, NativeMeasurements: []metrics.NativeMeasurement{{Key: "native", Attribution: metrics.Attribution{ExecutionSessionID: "original", StepID: "implement"}, Provenance: "native"}}}
	leaves := buildLeaves(&Request{ExecutionSessionID: "original"}, &artifact, nil, nil)
	for _, ref := range leaves[0].Evidence {
		if strings.Contains(ref.Detail, `"native_measurements"`) {
			return
		}
	}
	t.Fatal("native common evidence was omitted when the leaf had no children")
}
