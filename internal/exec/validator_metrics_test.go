package exec

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codagent/agent-runner/internal/measurements"
	"github.com/codagent/agent-runner/internal/metrics"
	"github.com/codagent/agent-runner/internal/stateio"

	"github.com/codagent/agent-runner/internal/model"
)

func TestPrepareValidatorMetricsEnvironmentUsesCorrelationProtocol(t *testing.T) {
	stubValidatorCapabilities(t)
	ctx := &model.ExecutionContext{SessionDir: t.TempDir(), ExecutionSessionID: "execution-1"}
	step := &model.Step{ID: "validate", MetricsSource: "agent-validator"}

	capture, environment, err := prepareNestedMetricsEnvironment(step, ctx)
	if err != nil {
		t.Fatalf("prepare metrics environment: %v", err)
	}
	if capture.contextID == "" {
		t.Fatal("context ID is empty")
	}
	joined := strings.Join(environment, "\n")
	if !strings.Contains(joined, "AGENT_RUNNER_METRICS_CONSUMER=agent-runner") {
		t.Fatalf("environment = %q, want metrics consumer", environment)
	}
	if !strings.Contains(joined, "AGENT_RUNNER_METRICS_CONTEXT="+capture.contextID) {
		t.Fatalf("environment = %q, want opaque metrics context", environment)
	}
	if strings.Contains(joined, "AGENT_RUNNER_NESTED_METRICS") {
		t.Fatalf("environment retains retired JSONL transport: %q", environment)
	}
}

func TestValidatorRecordRejectsUnverifiedDigestAndOpenPayload(t *testing.T) {
	raw := json.RawMessage(`{"record_type":"model_attempt","record_id":"attempt","revision":1,"measurement_schema_version":1,"producer":{"name":"agent-validator","version":"test"},"original_consumer_context":{"consumer":"agent-runner","context_id":"ctx"},"payload":{"attempt_id":"attempt","prompt":"private canary"},"digest":{"algorithm":"sha256","canonicalization":"rfc8785","value":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}`)
	if err := validateValidatorRecord(raw, "ctx"); err == nil {
		t.Fatal("accepted unverified digest and unrestricted payload")
	}
}

func TestValidatorDeliveryPersistsMeasurementsBeforeAcknowledging(t *testing.T) {
	dir := t.TempDir()
	contextID := "ctx"
	fixture, err := os.ReadFile("../measurements/testdata/validator-v1/fixtures/partial-token-export.json")
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	_ = json.Unmarshal(fixture, &value)
	value["original_consumer_context"] = map[string]any{"consumer": "agent-runner", "context_id": contextID}
	raw, _ := json.Marshal(value)
	canonical, err := measurements.CanonicalRecord(raw)
	if err != nil {
		t.Fatal(err)
	}
	value["digest"].(map[string]any)["value"] = fmt.Sprintf("%x", sha256.Sum256(canonical))
	raw, _ = json.Marshal(value)
	path := filepath.Join(dir, "validator-metrics", "contexts", contextID+".json")
	launch := validatorMetricsLaunch{Executable: "fixture-validator", Consumer: "agent-runner", ContextID: contextID, ExecutionSessionID: "original", ParentAttemptID: "parent", StepID: "validate", Project: dir}
	if err = stateio.WriteJSONDurable(path, launch); err != nil {
		t.Fatal(err)
	}
	prior := runValidatorMetricsCommand
	t.Cleanup(func() { runValidatorMetricsCommand = prior })
	acknowledged := false
	runValidatorMetricsCommand = func(_ *validatorMetricsLaunch, args ...string) ([]byte, error) {
		if args[1] == "acknowledge" {
			b, err := os.ReadFile(filepath.Join(dir, metrics.FileName))
			if err != nil {
				t.Error("acknowledged before artifact was persisted")
			} else {
				var artifact map[string]any
				_ = json.Unmarshal(b, &artifact)
				if len(artifact["measurement_heads"].([]any)) != 1 {
					t.Error("acknowledged before head incorporated")
				}
			}
			acknowledged = true
			return []byte(`{"ok":true,"operation":"acknowledge","protocol_version":1,"producer":{"name":"agent-validator"},"diagnostics":[],"receipt":"receipt","disposition":"acknowledged"}`), nil
		}
		records := []json.RawMessage{raw}
		receipt := any("receipt")
		state := "pending"
		if acknowledged {
			records = nil
			receipt = nil
			state = "previously_acknowledged"
		}
		b, _ := json.Marshal(map[string]any{"ok": true, "operation": "export", "protocol_version": 1, "producer": map[string]any{"name": "agent-validator"}, "diagnostics": []string{}, "store_id": "store", "consumer_context": map[string]any{"consumer": "agent-runner", "context_id": contextID}, "export_id": "export", "evidence_state": state, "records": records, "receipt": receipt, "batch": map[string]any{"generation": 1, "returned_revision_count": len(records), "remaining_revision_count": 0, "scope_complete": true}, "delivery_gaps": map[string]any{"count": 0, "reasons": []string{}}})
		return b, nil
	}
	collector := metrics.NewCollector(dir, "run", "workflow", time.Now())
	ctx := &model.ExecutionContext{SessionDir: dir, ExecutionSessionID: "recovery", AuditLogger: metrics.NewPipeline(collector, nil)}
	emitNestedMetricCapture(ctx, &model.Step{ID: "validate", MetricsSource: "agent-validator"}, "", &nestedMetricsCapture{path: path, contextID: contextID, parentAttemptID: "parent"})
	if !acknowledged {
		t.Fatal("record never acknowledged")
	}
	b, err := os.ReadFile(filepath.Join(dir, metrics.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"measurement_heads"`) || !strings.Contains(string(b), `"partial"`) {
		t.Fatal("complete measurement evidence missing")
	}
}

func TestValidatorInstrumentationFailureDoesNotBlockValidation(t *testing.T) {
	dir := t.TempDir()
	block := filepath.Join(dir, "blocked")
	if err := os.WriteFile(block, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	capture, environment, err := prepareNestedMetricsEnvironment(&model.Step{ID: "validate", MetricsSource: "agent-validator"}, &model.ExecutionContext{SessionDir: block, WorkingDir: dir})
	if err != nil || capture == nil || len(environment) != 0 {
		t.Fatalf("telemetry failure blocked validation: %v %v %v", capture, environment, err)
	}
}

func validatorAttemptFixture(t *testing.T) json.RawMessage {
	t.Helper()
	raw, err := os.ReadFile("../measurements/testdata/validator-v1/fixtures/partial-token-export.json")
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	_ = json.Unmarshal(raw, &value)
	value["original_consumer_context"] = map[string]any{"consumer": "agent-runner", "context_id": "ctx"}
	raw, _ = json.Marshal(value)
	canonical, err := measurements.CanonicalRecord(raw)
	if err != nil {
		t.Fatal(err)
	}
	value["digest"].(map[string]any)["value"] = fmt.Sprintf("%x", sha256.Sum256(canonical))
	raw, _ = json.Marshal(value)
	return raw
}
func emptyValidatorExport() []byte {
	return []byte(`{"ok":true,"operation":"export","protocol_version":1,"producer":{"name":"agent-validator"},"diagnostics":[],"store_id":"store","consumer_context":{"consumer":"agent-runner","context_id":"ctx"},"export_id":null,"evidence_state":"previously_acknowledged","records":[],"measurement_schema_versions":[],"receipt":null,"batch":{"generation":2,"returned_revision_count":0,"remaining_revision_count":0,"scope_complete":true},"delivery_gaps":{"count":0,"reasons":[]}}`)
}
func TestValidatorLostAcknowledgmentResponseReplaysAndPersistsDisposition(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "validator-metrics", "contexts", "ctx.json")
	launch := validatorMetricsLaunch{Executable: "fixture-validator", Consumer: "agent-runner", ContextID: "ctx", Project: dir, StoreID: "store", ExecutionSessionID: "original", StepID: "validate", Batches: []validatorMetricsBatch{{Records: []json.RawMessage{validatorAttemptFixture(t)}, Receipt: "receipt"}}}
	if err := stateio.WriteJSONDurable(path, launch); err != nil {
		t.Fatal(err)
	}
	prior := runValidatorMetricsCommand
	t.Cleanup(func() { runValidatorMetricsCommand = prior })
	acks := 0
	runValidatorMetricsCommand = func(_ *validatorMetricsLaunch, args ...string) ([]byte, error) {
		if args[1] == "acknowledge" {
			acks++
			if acks == 1 {
				return nil, fmt.Errorf("lost response")
			}
			return []byte(`{"ok":true,"operation":"acknowledge","protocol_version":1,"producer":{"name":"agent-validator"},"diagnostics":[],"receipt":"receipt","disposition":"acknowledged"}`), nil
		}
		return emptyValidatorExport(), nil
	}
	collector := metrics.NewCollector(dir, "run", "wf", time.Now())
	ctx := &model.ExecutionContext{SessionDir: dir, AuditLogger: metrics.NewPipeline(collector, nil)}
	if err := RecoverValidatorMetrics(ctx); err == nil {
		t.Fatal("lost acknowledgment response claimed completion")
	}
	_ = RecoverValidatorMetrics(ctx)
	saved, err := readValidatorMetricsLaunch(path)
	if err != nil {
		t.Fatal(err)
	}
	if acks != 2 || !saved.Batches[0].Acknowledged {
		t.Fatalf("receipt replay not persisted: acknowledgments=%d saved=%+v", acks, saved.Batches[0].Acknowledged)
	}
	var artifact metrics.Artifact
	raw, _ := os.ReadFile(filepath.Join(dir, metrics.FileName))
	_ = json.Unmarshal(raw, &artifact)
	if len(artifact.MeasurementHeads) != 1 || len(artifact.Steps) != 1 {
		t.Fatal("lost response replay duplicated work")
	}
}
func TestValidatorProjectionFailureNeverAcknowledges(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "validator-metrics", "contexts", "ctx.json")
	launch := validatorMetricsLaunch{Executable: "fixture-validator", Consumer: "agent-runner", ContextID: "ctx", Project: dir, StoreID: "store", Batches: []validatorMetricsBatch{{Records: []json.RawMessage{validatorAttemptFixture(t)}, Receipt: "receipt"}}}
	if err := stateio.WriteJSONDurable(path, launch); err != nil {
		t.Fatal(err)
	}
	collector := metrics.NewCollector(dir, "run", "wf", time.Now())
	if err := os.Mkdir(filepath.Join(dir, metrics.FileName), 0o700); err != nil {
		t.Fatal(err)
	}
	prior := runValidatorMetricsCommand
	t.Cleanup(func() { runValidatorMetricsCommand = prior })
	called := false
	runValidatorMetricsCommand = func(_ *validatorMetricsLaunch, args ...string) ([]byte, error) { called = true; return nil, nil }
	if err := RecoverValidatorMetrics(&model.ExecutionContext{SessionDir: dir, AuditLogger: metrics.NewPipeline(collector, nil)}); err == nil {
		t.Fatal("projection failure claimed success")
	}
	if called {
		t.Fatal("producer called before failed local projection was durable")
	}
}

func TestValidatorRecoveryWithMissingInvocationIsUnresolved(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "validator-metrics", "contexts", "ctx.json")
	launch := validatorMetricsLaunch{Executable: "fixture-validator", Consumer: "agent-runner", ContextID: "ctx", Project: dir, StoreID: "store", Batches: []validatorMetricsBatch{{Records: []json.RawMessage{validatorAttemptFixture(t)}, Receipt: "receipt", Acknowledged: true}}}
	if err := stateio.WriteJSONDurable(path, launch); err != nil {
		t.Fatal(err)
	}
	prior := runValidatorMetricsCommand
	t.Cleanup(func() { runValidatorMetricsCommand = prior })
	runValidatorMetricsCommand = func(_ *validatorMetricsLaunch, _ ...string) ([]byte, error) { return emptyValidatorExport(), nil }
	collector := metrics.NewCollector(dir, "run", "wf", time.Now())
	if err := RecoverValidatorMetrics(&model.ExecutionContext{SessionDir: dir, AuditLogger: metrics.NewPipeline(collector, nil)}); err == nil {
		t.Fatal("missing invocation was reported as resolved recovery")
	}
}

func TestValidatorUnsupportedScopeCannotFallBackToOlderHeads(t *testing.T) {
	err := producerError([]byte(`{"ok":false,"error":{"code":"unsupported_version","required_measurement_schema_versions":[1,2]}}`))
	if deliveryErrorCode(err) != "unsupported_measurement_version" {
		t.Fatalf("unsupported current head became an ordinary retry error: %v", err)
	}
}

func stubValidatorCapabilities(t *testing.T) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "validator")
	data := `#!/bin/sh
if [ "$1 $2" != "metrics capabilities" ]; then exit 1; fi
cat <<'JSON'
{"ok":true,"operation":"capabilities","protocol_version":1,"capabilities_version":1,"producer":{"name":"agent-validator"},"protocol_versions":[1],"measurement_schema_versions":[1],"artifact_schema_versions":[1],"operations":["capabilities","pending","export","acknowledge","discard"],"limits":{"default_inventory_count":100,"maximum_inventory_count":500,"default_export_count":100,"maximum_export_count":500,"default_export_bytes":1000000,"maximum_export_bytes":4000000,"maximum_individual_record_bytes":3000000},"diagnostics":[]}
JSON
`
	if err := os.WriteFile(path, []byte(data), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENT_RUNNER_VALIDATOR_EXECUTABLE", path)
}

func TestValidatorExportRetriesOnlyTransientStoreContention(t *testing.T) {
	prior := runValidatorMetricsCommand
	t.Cleanup(func() { runValidatorMetricsCommand = prior })
	calls := 0
	runValidatorMetricsCommand = func(_ *validatorMetricsLaunch, _ ...string) ([]byte, error) {
		calls++
		if calls < 3 {
			return []byte(`{"ok":false,"error":{"code":"store_busy","retryable":true}}`), fmt.Errorf("busy")
		}
		return emptyValidatorExport(), nil
	}
	if _, err := exportValidatorMetrics(&validatorMetricsLaunch{Executable: "fixture-validator", Consumer: "agent-runner", ContextID: "ctx"}); err != nil || calls != 3 {
		t.Fatalf("transient store contention was not retried: calls=%d error=%v", calls, err)
	}
}

func TestValidatorDrainReconcilesInvocationAndAttemptsAcrossBatches(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "validator-metrics", "contexts", "ctx.json")
	launch := validatorMetricsLaunch{Executable: "fixture-validator", Consumer: "agent-runner", ContextID: "ctx", Project: dir, ExecutionSessionID: "original", StepID: "validate"}
	if err := stateio.WriteJSONDurable(path, launch); err != nil {
		t.Fatal(err)
	}
	invocation := map[string]any{"record_type": "invocation", "record_id": "invocation-fixture", "revision": 1, "measurement_schema_version": 1, "producer": map[string]any{"name": "agent-validator", "version": "fixture"}, "original_consumer_context": map[string]any{"consumer": "agent-runner", "context_id": "ctx"}, "payload": map[string]any{"record_type": "invocation", "invocation_id": "invocation-fixture", "revision": 1, "measurement_schema_version": 1, "session_id": "session-fixture", "lifecycle": map[string]any{"state": "completed", "started_at": "2026-09-06T12:00:00Z", "ended_at": "2026-09-06T12:00:01Z"}, "attempt_ids": []string{"attempt-fixture"}, "zero_dispatch": false, "diagnostics": []string{}}}
	raw, _ := json.Marshal(invocation)
	canonical, err := measurements.CanonicalRecord(raw)
	if err != nil {
		t.Fatal(err)
	}
	invocation["digest"] = map[string]any{"algorithm": "sha256", "canonicalization": "rfc8785", "value": fmt.Sprintf("%x", sha256.Sum256(canonical))}
	raw, _ = json.Marshal(invocation)
	batches := []json.RawMessage{raw, validatorAttemptFixture(t)}
	stage := 0
	prior := runValidatorMetricsCommand
	t.Cleanup(func() { runValidatorMetricsCommand = prior })
	runValidatorMetricsCommand = func(_ *validatorMetricsLaunch, args ...string) ([]byte, error) {
		if args[1] == "acknowledge" {
			receipt := args[len(args)-1]
			stage++
			return []byte(fmt.Sprintf(`{"ok":true,"operation":"acknowledge","protocol_version":1,"producer":{"name":"agent-validator"},"diagnostics":[],"receipt":%q,"disposition":"acknowledged"}`, receipt)), nil
		}
		if stage == len(batches) {
			return emptyValidatorExport(), nil
		}
		var response map[string]any
		_ = json.Unmarshal(emptyValidatorExport(), &response)
		response["records"] = []json.RawMessage{batches[stage]}
		response["measurement_schema_versions"] = []int{1}
		response["evidence_state"] = "pending"
		response["receipt"] = fmt.Sprint("batch-", stage)
		response["export_id"] = response["receipt"]
		response["batch"] = map[string]any{"generation": stage, "returned_revision_count": 1, "remaining_revision_count": len(batches) - stage - 1, "scope_complete": stage == len(batches)-1}
		b, _ := json.Marshal(response)
		return b, nil
	}
	c := metrics.NewCollector(dir, "run", "wf", time.Now())
	ctx := &model.ExecutionContext{SessionDir: dir, AuditLogger: metrics.NewPipeline(c, nil)}
	if err := RecoverValidatorMetrics(ctx); err != nil {
		t.Fatal(err)
	}
	if !c.ValidatorContextComplete("ctx") || stage != 2 {
		t.Fatal("split invocation/attempt batches did not reconcile")
	}
	saved, err := readValidatorMetricsLaunch(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Batches) != 2 || !saved.Batches[0].Acknowledged || !saved.Batches[1].Acknowledged {
		t.Fatal("whole receipts not durably acknowledged")
	}
}

func TestValidatorAcknowledgmentRejectsWrongProducer(t *testing.T) {
	prior := runValidatorMetricsCommand
	t.Cleanup(func() { runValidatorMetricsCommand = prior })
	runValidatorMetricsCommand = func(_ *validatorMetricsLaunch, _ ...string) ([]byte, error) {
		return []byte(`{"ok":true,"operation":"acknowledge","protocol_version":1,"producer":{"name":"other"},"diagnostics":[],"receipt":"receipt","disposition":"acknowledged"}`), nil
	}
	if err := acknowledgeValidatorReceipt(&validatorMetricsLaunch{}, "receipt"); err == nil {
		t.Fatal("wrong producer acknowledged receipt")
	}
}
func TestLegacyValidatorJournalWithoutExecutableHasExplicitGap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "validator-metrics", "contexts", "ctx.json")
	if err := stateio.WriteJSONDurable(path, validatorMetricsLaunch{Consumer: "agent-runner", ContextID: "ctx", Project: dir}); err != nil {
		t.Fatal(err)
	}
	prior := runValidatorMetricsCommand
	t.Cleanup(func() { runValidatorMetricsCommand = prior })
	called := false
	runValidatorMetricsCommand = func(_ *validatorMetricsLaunch, _ ...string) ([]byte, error) {
		called = true
		return nil, fmt.Errorf("no executable")
	}
	c := metrics.NewCollector(dir, "run", "wf", time.Now())
	err := RecoverValidatorMetrics(&model.ExecutionContext{SessionDir: dir, AuditLogger: metrics.NewPipeline(c, nil)})
	if err == nil || !strings.Contains(err.Error(), "original_executable_unavailable") || called {
		t.Fatalf("legacy executable selection was guessed or not diagnosed: called=%v err=%v", called, err)
	}
}
