//go:build dev_audit

package devaudit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/loader"
	"github.com/codagent/agent-runner/internal/metrics"
	builtinworkflows "github.com/codagent/agent-runner/workflows"
)

func TestTaggedProviderInjectsTheSingleHiddenAuditWorkflow(t *testing.T) {
	ref, err := builtinworkflows.Resolve("audit:run-audit")
	if err != nil {
		t.Fatalf("resolve audit workflow: %v", err)
	}
	if ref != "builtin:audit/run-audit-v1.0.yaml" {
		t.Fatalf("audit ref = %q", ref)
	}
	workflow, err := loader.LoadWorkflow(ref, loader.Options{})
	if err != nil {
		t.Fatalf("load audit workflow: %v", err)
	}
	if !workflow.Hidden || len(workflow.Steps) != 7 {
		t.Fatalf("workflow hidden=%v stages=%d", workflow.Hidden, len(workflow.Steps))
	}
	want := []string{"prepare-evidence", "value-audit", "validate-value", "correctness-audit", "validate-publish-correctness", "assemble-local-report", "report-value-observations"}
	for index, id := range want {
		if workflow.Steps[index].ID != id {
			t.Fatalf("stage %d = %q, want %q", index, workflow.Steps[index].ID, id)
		}
	}
}

func TestAuditWorkflowUsesBoundedEvidenceAndValueStageHandlers(t *testing.T) {
	workflow, err := loader.LoadWorkflow("builtin:audit/run-audit-v1.0.yaml", loader.Options{})
	if err != nil {
		t.Fatalf("load audit workflow: %v", err)
	}
	if len(workflow.Steps) != 7 {
		t.Fatalf("audit stages = %d, want 7", len(workflow.Steps))
	}
	for _, step := range workflow.Steps[:3] {
		if strings.Contains(step.Command, "requires the development audit handler") {
			t.Fatalf("%s still uses the unavailable-stage placeholder", step.ID)
		}
		if !strings.HasPrefix(step.Command, "audit-stage ") {
			t.Fatalf("%s command = %q, want audit-stage handler", step.ID, step.Command)
		}
	}
}

func TestPrepareEvidenceStagePersistsBoundedEvidenceArtifacts(t *testing.T) {
	temp := t.TempDir()
	snapshot := filepath.Join(temp, "snapshot")
	if err := os.MkdirAll(snapshot, 0o700); err != nil {
		t.Fatal(err)
	}
	artifact := metrics.Artifact{SchemaVersion: metrics.SchemaVersion, RunID: "source-run", Workflow: "core:example", Sessions: []metrics.SessionRecord{{ExecutionSessionID: "source-session"}}, Steps: []metrics.StepRecord{{Prefix: "[implement]", ID: "implement", Kind: "step", Type: "agent", Outcome: "success", ExecutionSessionID: "source-session"}}}
	data, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(snapshot, metrics.FileName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	request := Request{AuditRunID: "audit-run", AuditSessionDir: filepath.Join(temp, "audit"), SnapshotPath: snapshot, SourceRunID: "source-run", ExecutionSessionID: "source-session", SourceWorkflow: "core:example", Trigger: "automatic"}
	if err := os.MkdirAll(request.AuditSessionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	requestData, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(request.AuditSessionDir, "request.json"), requestData, 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := (auditProcessRunner{}).RunShell("audit-stage prepare-evidence", true, request.AuditSessionDir)
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("prepare stage = %+v, %v", result, err)
	}
	for _, name := range []string{"evidence-index.json", "source-provenance.json", "value-packages.json"} {
		if _, err := os.Stat(filepath.Join(request.AuditSessionDir, name)); err != nil {
			t.Fatalf("%s was not persisted: %v", name, err)
		}
	}
}

func TestValidateValueStageAcceptsOnlyCompleteFixedRubricOutput(t *testing.T) {
	temp := t.TempDir()
	snapshot := filepath.Join(temp, "snapshot")
	if err := os.MkdirAll(snapshot, 0o700); err != nil {
		t.Fatal(err)
	}
	artifact := metrics.Artifact{SchemaVersion: metrics.SchemaVersion, RunID: "source-run", Workflow: "core:example", Sessions: []metrics.SessionRecord{{ExecutionSessionID: "source-session"}}, Steps: []metrics.StepRecord{{Prefix: "[implement]", ID: "implement", Kind: "step", Type: "agent", Outcome: "success", ExecutionSessionID: "source-session"}}}
	data, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(snapshot, metrics.FileName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	request := Request{AuditRunID: "audit-run", AuditSessionDir: filepath.Join(temp, "audit"), SnapshotPath: snapshot, SourceRunID: "source-run", ExecutionSessionID: "source-session", SourceWorkflow: "core:example", Trigger: "automatic", Crosscheck: AgentProvenance{CLI: "fake", Model: "fake-model"}}
	if _, err := PrepareEvidence(request); err != nil {
		t.Fatalf("prepare evidence: %v", err)
	}
	requestData, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(request.AuditSessionDir, "request.json"), requestData, 0o600); err != nil {
		t.Fatal(err)
	}
	packagesData, err := os.ReadFile(filepath.Join(request.AuditSessionDir, "value-packages.json"))
	if err != nil {
		t.Fatal(err)
	}
	var packages []ValuePackage
	if err := json.Unmarshal(packagesData, &packages); err != nil {
		t.Fatal(err)
	}
	modelOutput := map[string]any{"batch_id": packages[0].BatchID, "observations": []map[string]any{{
		"observation_id": packages[0].Leaves[0].Skeleton.ObservationID,
		"overall_value":  "medium", "change_effect": "intended", "unique_contribution": "unique",
		"downstream_evidence": "supporting", "confidence": "medium", "evidence_coverage": "partial",
	}}}
	modelData, err := json.Marshal(modelOutput)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(request.AuditSessionDir, "model-output", packages[0].BatchID+".json"), modelData, 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := runAuditStage("validate-value", request.AuditSessionDir)
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("validate stage = %+v, %v", result, err)
	}
	for _, name := range []string{"value-observations.json", "value-consultations.json"} {
		if _, err := os.Stat(filepath.Join(request.AuditSessionDir, name)); err != nil {
			t.Fatalf("%s was not persisted: %v", name, err)
		}
	}
}

func TestValidateValueStageRejectsFabricatedMeasurementsAndUnsafeNotes(t *testing.T) {
	temp := t.TempDir()
	request := Request{AuditRunID: "audit", AuditSessionDir: temp, SnapshotPath: temp, ExecutionSessionID: "session", Crosscheck: AgentProvenance{Model: "fake"}}
	prepared := PreparedValueAudit{Index: EvidenceIndex{Fingerprints: Fingerprints{}}, Packages: []ValuePackage{{BatchID: "value-001", Leaves: []LeafEvidence{{Skeleton: ObservationSkeleton{ObservationID: "observation"}}}}}}
	before, err := fingerprintTree(temp)
	if err != nil {
		t.Fatal(err)
	}
	prepared.Index.Fingerprints.SnapshotBefore = before
	prepared.Index.Fingerprints.OutputBefore = before
	output := ModelValueBatch{BatchID: "value-001", Observations: []ModelValueJudgment{{ObservationID: "observation", OverallValue: "medium", ChangeEffect: "intended", UniqueContribution: "unique", DownstreamEvidence: "supporting", Confidence: "medium", EvidenceCoverage: "partial", Note: "https://example.test/evidence"}}}
	if _, err := ValidateValueOutputs(request, prepared, []ModelValueBatch{output}); err == nil {
		t.Fatal("unsafe note was accepted")
	}
	if _, err := loadModelValueBatches(temp, []ValuePackage{{BatchID: "value-001"}}); err == nil {
		t.Fatal("missing model output was accepted")
	}
}
