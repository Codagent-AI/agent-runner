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
	"github.com/codagent/agent-runner/internal/stateio"
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

func TestAuditWorkflowUsesBoundedEvidenceAndLocalCorrectnessStageHandlers(t *testing.T) {
	workflow, err := loader.LoadWorkflow("builtin:audit/run-audit-v1.0.yaml", loader.Options{})
	if err != nil {
		t.Fatalf("load audit workflow: %v", err)
	}
	if len(workflow.Steps) != 7 {
		t.Fatalf("audit stages = %d, want 7", len(workflow.Steps))
	}
	for _, step := range workflow.Steps {
		if strings.Contains(step.Command, "requires the development audit handler") {
			t.Fatalf("%s still uses the unavailable-stage placeholder", step.ID)
		}
		if !strings.HasPrefix(step.Command, "audit-stage ") {
			t.Fatalf("%s command = %q, want audit-stage handler", step.ID, step.Command)
		}
	}
}

func TestSandboxExecArgsBindsOutputDirectoryAsParameter(t *testing.T) {
	outputDir := "/audit/output/\"untrusted\"\n(allow file-write*)"
	args := sandboxExecArgs([]string{"crosscheck", "--batch"}, outputDir)

	if got, want := args[0], "-D"; got != want {
		t.Fatalf("first sandbox argument = %q, want %q", got, want)
	}
	if got, want := args[1], "OUTPUT_DIR="+outputDir; got != want {
		t.Fatalf("sandbox parameter = %q, want %q", got, want)
	}
	if strings.Contains(args[3], outputDir) {
		t.Fatalf("sandbox profile must not interpolate output path: %q", args[3])
	}
	if !strings.Contains(args[3], `(param "OUTPUT_DIR")`) {
		t.Fatalf("sandbox profile must bind OUTPUT_DIR parameter: %q", args[3])
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

func TestCorrectnessStageRecordsMissingModelOutputWithoutFailingAudit(t *testing.T) {
	temp := t.TempDir()
	request := Request{AuditRunID: "audit", AuditSessionDir: temp, SnapshotPath: temp, ExecutionSessionID: "session"}
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(temp, "request.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := runAuditStage("correctness-audit", temp)
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("correctness stage = %+v, %v", result, err)
	}
	if _, err := os.Stat(filepath.Join(temp, "correctness-model-diagnostics.json")); err != nil {
		t.Fatalf("missing-output diagnostic was not persisted: %v", err)
	}
}

func TestAuditWorkflowStagesOneThroughSixCommitLocalReportBeforeDelivery(t *testing.T) {
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
	runnerSource := filepath.Join(snapshot, "runner-source")
	if err := os.MkdirAll(runnerSource, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runnerSource, "go.mod"), []byte("module github.com/codagent/agent-runner\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"cmd/agent-runner", "internal/runner", "workflows"} {
		if err := os.MkdirAll(filepath.Join(runnerSource, path), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	request := Request{AuditRunID: "audit-run", AuditSessionDir: filepath.Join(temp, "audit"), SnapshotPath: snapshot, SourceRunID: "source-run", ExecutionSessionID: "source-session", SourceWorkflow: "core:example", Trigger: "automatic", RunnerSource: SourceProvenance{Verified: true, Coverage: "complete", SnapshotPath: runnerSource}}
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
	if result, err := runAuditStage("prepare-evidence", request.AuditSessionDir); err != nil || result.ExitCode != 0 {
		t.Fatalf("prepare = %+v, %v", result, err)
	}
	var packages []ValuePackage
	packageData, err := os.ReadFile(filepath.Join(request.AuditSessionDir, "value-packages.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(packageData, &packages); err != nil {
		t.Fatal(err)
	}
	value := ModelValueBatch{BatchID: packages[0].BatchID, Observations: []ModelValueJudgment{{ObservationID: packages[0].Leaves[0].Skeleton.ObservationID, OverallValue: "medium", ChangeEffect: "intended", UniqueContribution: "unique", DownstreamEvidence: "supporting", Confidence: "medium", EvidenceCoverage: "partial"}}}
	if err := stateio.WriteJSONAtomic(filepath.Join(request.AuditSessionDir, "model-output", packages[0].BatchID+".json"), value); err != nil {
		t.Fatal(err)
	}
	var index EvidenceIndex
	indexData, err := os.ReadFile(filepath.Join(request.AuditSessionDir, "evidence-index.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(indexData, &index); err != nil {
		t.Fatal(err)
	}
	ref := ""
	for _, candidate := range index.References {
		if candidate.Status == "available" {
			ref = candidate.ID
			break
		}
	}
	if ref == "" {
		t.Fatal("prepared fixture has no available evidence reference")
	}
	correctness := CorrectnessCandidates{Candidates: []CorrectnessCandidate{{Status: "confirmed", DefectKey: "runner-retry-loss", Title: "retry state is lost", Observed: "retry loses state", Expected: "retry preserves state", Verification: "run the retry workflow", AffectedComponent: "internal/runner", EvidenceRefs: []string{ref}, Confidence: "high", SemanticDuplicate: Duplicate{State: "none"}}}}
	if err := stateio.WriteJSONAtomic(filepath.Join(request.AuditSessionDir, "model-output", correctnessOutput), correctness); err != nil {
		t.Fatal(err)
	}
	oldGH, oldDestination := ghRunner, destinationResolver
	ghRunner = &recordingGH{}
	destinationResolver = fakeDestination{DestinationState{State: "configured", SpreadsheetID: "sheet", Tab: "audit"}}
	t.Cleanup(func() { ghRunner, destinationResolver = oldGH, oldDestination })
	for _, stage := range []string{"value-audit", "validate-value"} {
		if result, err := runAuditStage(stage, request.AuditSessionDir); err != nil || result.ExitCode != 0 {
			t.Fatalf("%s = %+v, %v", stage, result, err)
		}
	}
	if calls := ghRunner.(*recordingGH).calls; len(calls) != 0 {
		t.Fatalf("value stages invoked publisher: %#v", calls)
	}
	for _, stage := range []string{"correctness-audit", "validate-publish-correctness", "assemble-local-report"} {
		if result, err := runAuditStage(stage, request.AuditSessionDir); err != nil || result.ExitCode != 0 {
			t.Fatalf("%s = %+v, %v", stage, result, err)
		}
	}
	data, err = os.ReadFile(filepath.Join(request.AuditSessionDir, "local-report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var report LocalReport
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	if report.SchemaVersion != valueSchemaVersion || len(report.Correctness.Findings) != 1 || report.Correctness.Findings[0].PublicationState != "created" {
		t.Fatalf("report = %#v", report)
	}
}
