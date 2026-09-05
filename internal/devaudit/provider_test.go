//go:build dev_audit

package devaudit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/cli"
	"github.com/codagent/agent-runner/internal/config"
	"github.com/codagent/agent-runner/internal/loader"
	"github.com/codagent/agent-runner/internal/metrics"
	"github.com/codagent/agent-runner/internal/runner"
	"github.com/codagent/agent-runner/internal/stateio"
	builtinworkflows "github.com/codagent/agent-runner/workflows"
	"github.com/google/go-cmp/cmp"
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
	assets, err := builtinworkflows.ListAssets("audit")
	if err != nil {
		t.Fatalf("list audit workflow assets: %v", err)
	}
	if len(assets) != 0 {
		t.Fatalf("audit workflow assets = %v, want none", assets)
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

func TestSandboxedCrosscheckCanExecuteAndOnlyWriteAuditOutput(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS sandbox-exec integration")
	}
	root := t.TempDir()
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(root, "workspace")
	outputDir := filepath.Join(root, "output")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	allowedPath := filepath.Join(outputDir, "allowed.txt")
	deniedPath := filepath.Join(root, "denied.txt")
	command, err := sandboxedCrosscheckCommand([]string{
		"/bin/sh", "-c",
		`printf allowed > "$1"; if printf denied > "$2"; then exit 42; fi`,
		"audit-sandbox", allowedPath, deniedPath,
	}, workspace, outputDir)
	if err != nil {
		t.Fatal(err)
	}
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("sandboxed crosscheck did not execute with its bounded write access: %v\n%s", err, output)
	}
	if data, err := os.ReadFile(allowedPath); err != nil || string(data) != "allowed" {
		t.Fatalf("allowed audit output = %q, %v", data, err)
	}
	if _, err := os.Stat(deniedPath); !os.IsNotExist(err) {
		t.Fatalf("write outside audit output was not blocked: %v", err)
	}
}

func TestSandboxedCodexGetsDisposableWritableRuntime(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS sandbox-exec integration")
	}
	root := t.TempDir()
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(root, "workspace")
	outputDir := filepath.Join(root, "output")
	sourceCodexHome := filepath.Join(root, "source-codex-home")
	for _, dir := range []string{workspace, sourceCodexHome} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(sourceCodexHome, "auth.json"), []byte("test-auth"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", sourceCodexHome)
	adapter, err := cli.Get("codex")
	if err != nil {
		t.Fatal(err)
	}
	environment, cleanup, err := cliEnvironment(adapter, &Request{Crosscheck: AgentProvenance{CLI: "codex"}}, nil, workspace, outputDir)
	if err != nil {
		t.Fatal(err)
	}
	runtimeHome := envValue(environment, "CODEX_HOME")
	command, err := sandboxedCrosscheckCommand([]string{
		"/bin/sh", "-c",
		`test "$(cat "$CODEX_HOME/auth.json")" = test-auth; touch "$CODEX_HOME/state" "$HOME/home-state" "$TMPDIR/temp-state"; if touch "$1"; then exit 42; fi`,
		"audit-codex-runtime", filepath.Join(root, "denied.txt"),
	}, workspace, outputDir)
	if err != nil {
		t.Fatal(err)
	}
	command.Env = environment
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("sandboxed Codex runtime is unusable: %v\n%s", err, output)
	}
	cleanup()
	if _, err := os.Stat(runtimeHome); !os.IsNotExist(err) {
		t.Fatalf("disposable Codex runtime remains after cleanup: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(sourceCodexHome, "auth.json")); err != nil || string(data) != "test-auth" {
		t.Fatalf("source Codex authentication changed: %q, %v", data, err)
	}
}

func TestCodexStructuredOutputSchemaIsEphemeralAndBindsValueIdentity(t *testing.T) {
	outputDir := t.TempDir()
	pkg := ValuePackage{BatchID: "value-007", Leaves: []LeafEvidence{{Skeleton: ObservationSkeleton{ObservationID: "observation-1"}}}}
	args, responsePath, cleanup, err := withCodexOutputSchema("codex", []string{"codex", "exec", "prompt"}, outputDir, "value", valueOutputSchema(pkg))
	if err != nil {
		t.Fatal(err)
	}
	if len(args) != 7 || args[2] != "--output-schema" || args[4] != "--output-last-message" || args[5] != responsePath || args[6] != "prompt" {
		t.Fatalf("structured Codex args = %v", args)
	}
	schemaPath := args[3]
	data, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	properties := schema["properties"].(map[string]any)
	batch := properties["batch_id"].(map[string]any)
	if got := batch["enum"].([]any)[0]; got != pkg.BatchID {
		t.Fatalf("batch identity enum = %v, want %q", got, pkg.BatchID)
	}
	if _, exists := properties["schema_version"]; exists {
		t.Fatal("model schema permits measured schema_version field")
	}
	cleanup()
	if _, err := os.Stat(schemaPath); !os.IsNotExist(err) {
		t.Fatalf("structured-output schema remains after cleanup: %v", err)
	}
	if _, err := os.Stat(responsePath); !os.IsNotExist(err) {
		t.Fatalf("structured final response remains after cleanup: %v", err)
	}
}

func TestValueOutputSchemaDisallowsCompleteCoverageForOmittedEvidence(t *testing.T) {
	pkg := ValuePackage{BatchID: "value-coverage", Leaves: []LeafEvidence{
		{Skeleton: ObservationSkeleton{ObservationID: "complete-observation"}},
		{Skeleton: ObservationSkeleton{ObservationID: "incomplete-observation"}, OmittedCategories: []string{"validation"}},
	}}

	schema := valueOutputSchema(pkg)
	properties := schema["properties"].(map[string]any)
	observations := properties["observations"].(map[string]any)
	if diff := cmp.Diff([]string{"complete-observation", "incomplete-observation"}, observations["required"].([]string)); diff != "" {
		t.Errorf("required observation IDs mismatch (-want +got):\n%s", diff)
	}
	complete := coverageEnumsForObservation(t, schema, "complete-observation")
	incomplete := coverageEnumsForObservation(t, schema, "incomplete-observation")
	if diff := cmp.Diff([]string{"complete", "partial", "limited"}, complete); diff != "" {
		t.Errorf("complete evidence coverage enum mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"partial", "limited"}, incomplete); diff != "" {
		t.Errorf("incomplete evidence coverage enum mismatch (-want +got):\n%s", diff)
	}
}

func coverageEnumsForObservation(t *testing.T, schema map[string]any, observationID string) []string {
	t.Helper()
	properties := schema["properties"].(map[string]any)
	observations := properties["observations"].(map[string]any)
	observationProperties, ok := observations["properties"].(map[string]any)
	if !ok {
		t.Fatal("value observation schema is not keyed by observation ID")
	}
	judgment, ok := observationProperties[observationID].(map[string]any)
	if !ok {
		t.Fatalf("value observation schema has no property for %q", observationID)
	}
	judgmentProperties := judgment["properties"].(map[string]any)
	return judgmentProperties["evidence_coverage"].(map[string]any)["enum"].([]string)
}

func TestDecodeModelValueBatchNormalizesKeyedObservationsInPackageOrder(t *testing.T) {
	pkg := ValuePackage{BatchID: "value-keyed", Leaves: []LeafEvidence{
		{Skeleton: ObservationSkeleton{ObservationID: "observation-a"}},
		{Skeleton: ObservationSkeleton{ObservationID: "observation-b"}},
	}}
	response := `{"batch_id":"value-keyed","observations":{"observation-b":{"observation_id":"observation-b","overall_value":"medium","change_effect":"intended","unique_contribution":"complementary","downstream_evidence":"supporting","confidence":"medium","evidence_coverage":"partial","note":"b","consultations":[]},"observation-a":{"observation_id":"observation-a","overall_value":"high","change_effect":"intended","unique_contribution":"unique","downstream_evidence":"confirmed","confidence":"high","evidence_coverage":"complete","note":"a","consultations":[]}}}`

	batch, err := decodeModelValueBatch(response, pkg)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{batch.Observations[0].ObservationID, batch.Observations[1].ObservationID}
	if diff := cmp.Diff([]string{"observation-a", "observation-b"}, got); diff != "" {
		t.Errorf("normalized observation order mismatch (-want +got):\n%s", diff)
	}
}

func TestValueAuditPromptRequiresMarginalCostAwareJudgment(t *testing.T) {
	pkg := ValuePackage{BatchID: "value-007", Leaves: []LeafEvidence{{
		Skeleton: ObservationSkeleton{
			ObservationID: "observation-1",
			StepID:        "validate/fix-violations",
			Lineage:       "validate",
			Cost:          CostEvidence{DurationMS: pointerTo(int64(120000)), TotalTokens: pointerTo(int64(500000))},
		},
		Attempts: 3,
	}}}

	prompt, err := valueAuditPrompt(pkg)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"marginal contribution",
		"resource use",
		"Successful execution alone is not evidence of value",
		"Do not equate no repository change with no value",
		"detector and remediation",
		"lineage",
		"assumption review",
		"simplification",
		"quantify findings or resulting corrections",
		`"batch_id":"value-007"`,
	} {
		if !strings.Contains(prompt, required) {
			t.Errorf("prompt does not contain %q", required)
		}
	}
}

func pointerTo[T any](value T) *T {
	return &value
}

func envValue(environment []string, name string) string {
	prefix := name + "="
	for index := len(environment) - 1; index >= 0; index-- {
		if strings.HasPrefix(environment[index], prefix) {
			return strings.TrimPrefix(environment[index], prefix)
		}
	}
	return ""
}

func TestDiscoverEvidenceDoesNotClassifyRunnerSourceAsRunOutput(t *testing.T) {
	root := t.TempDir()
	runnerSource := filepath.Join(root, "runner-source", "internal")
	if err := os.MkdirAll(runnerSource, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runnerSource, "output.go"), []byte("package internal\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	references, err := discoverEvidence(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, reference := range references {
		if reference.Status == "available" && reference.Category == "narrative" {
			t.Fatalf("Runner source was classified as run narrative evidence: %#v", reference)
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

func TestAuditWorkflowStageUsesAuditSessionWhenStepWorkdirIsEmpty(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	temp := t.TempDir()
	snapshot := filepath.Join(temp, "snapshot")
	if err := os.MkdirAll(snapshot, 0o700); err != nil {
		t.Fatal(err)
	}
	artifact := metrics.Artifact{
		SchemaVersion: metrics.SchemaVersion,
		RunID:         "source-run",
		Workflow:      "core:example",
		Sessions:      []metrics.SessionRecord{{ExecutionSessionID: "source-session"}},
		Steps:         []metrics.StepRecord{{Prefix: "[implement]", ID: "implement", Kind: "step", Type: "agent", Outcome: "success", ExecutionSessionID: "source-session"}},
	}
	data, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(snapshot, metrics.FileName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	auditDir := filepath.Join(temp, "audit")
	if err := os.MkdirAll(auditDir, 0o700); err != nil {
		t.Fatal(err)
	}
	request := Request{AuditRunID: "audit-run", AuditSessionDir: auditDir, SnapshotPath: snapshot, SourceRunID: "source-run", ExecutionSessionID: "source-session", SourceWorkflow: "core:example", Trigger: "automatic"}
	requestData, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(auditDir, "request.json"), requestData, 0o600); err != nil {
		t.Fatal(err)
	}
	workflow, err := loader.LoadWorkflow("builtin:audit/run-audit-v1.0.yaml", loader.Options{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = runner.RunWorkflow(&workflow, map[string]string{"audit_request": filepath.Join(auditDir, "request.json")}, &runner.Options{
		SessionDir: auditDir, WorkflowFile: "builtin:audit/run-audit-v1.0.yaml", WorkingDir: auditDir, ProjectRoot: auditDir,
		ProfileStore: &config.Config{}, ProcessRunner: auditProcessRunner{auditSessionDir: auditDir}, GlobExpander: auditGlobExpander{}, Log: &runner.DiscardLogger{},
	})
	if err != nil {
		t.Fatalf("run audit workflow: %v", err)
	}
	if _, err := os.Stat(filepath.Join(auditDir, "evidence-index.json")); err != nil {
		t.Fatalf("prepare-evidence did not use the audit session: %v", err)
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

func TestValidateValueOutputsAcceptsLeafEvidenceConsultation(t *testing.T) {
	temp := t.TempDir()
	if err := os.Mkdir(filepath.Join(temp, "model-output"), 0o700); err != nil {
		t.Fatal(err)
	}
	before, err := fingerprintTree(temp)
	if err != nil {
		t.Fatal(err)
	}
	reference := EvidenceReference{ID: "metrics-step#1", Category: "metrics", Status: "available"}
	leaf := LeafEvidence{Skeleton: ObservationSkeleton{ObservationID: "observation"}, Evidence: []EvidenceReference{reference}}
	prepared := PreparedValueAudit{
		Index:    EvidenceIndex{Fingerprints: Fingerprints{SnapshotBefore: before}, Leaves: []LeafEvidence{leaf}},
		Packages: []ValuePackage{{BatchID: "value-001", Leaves: []LeafEvidence{leaf}}},
	}
	request := Request{AuditSessionDir: temp, SnapshotPath: temp, Crosscheck: AgentProvenance{Model: "fake"}}
	output := ModelValueBatch{BatchID: "value-001", Observations: []ModelValueJudgment{{
		ObservationID: "observation", OverallValue: "medium", ChangeEffect: "intended", UniqueContribution: "unique",
		DownstreamEvidence: "supporting", Confidence: "medium", EvidenceCoverage: "partial", Consultations: []string{reference.ID},
	}}}
	result, err := ValidateValueOutputs(request, prepared, []ModelValueBatch{output})
	if err != nil {
		t.Fatalf("validate leaf-evidence consultation: %v", err)
	}
	ledger := consultationLedger(result.Observations, allEvidenceReferences(&prepared.Index))
	if len(ledger) != 1 || ledger[0].ReferenceID != reference.ID || ledger[0].Category != reference.Category {
		t.Fatalf("consultation ledger = %#v", ledger)
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

// E2E-001: the automatic audit keeps the source-independent local report when
// stage 7 cannot use a configured connection.
func TestE2E001AutomaticAuditCompletesLocallyWhenSheetsIsUnavailable(t *testing.T) {
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
	oldGH, oldDestination, oldReporter := ghRunner, destinationResolver, defaultSheetsReporter
	ghRunner = &recordingGH{}
	destinationResolver = fakeDestination{DestinationState{State: "configured", SpreadsheetID: "sheet", Tab: "audit"}}
	defaultSheetsReporter = SheetsReporter{Store: ConnectionStore{Home: t.TempDir(), allowInsecureTokenURI: true}}
	t.Cleanup(func() { ghRunner, destinationResolver, defaultSheetsReporter = oldGH, oldDestination, oldReporter })
	for _, stage := range []string{"value-audit", "validate-value"} {
		if result, err := runAuditStage(stage, request.AuditSessionDir); err != nil || result.ExitCode != 0 {
			t.Fatalf("%s = %+v, %v", stage, result, err)
		}
	}
	if calls := ghRunner.(*recordingGH).calls; len(calls) != 0 {
		t.Fatalf("value stages invoked publisher: %#v", calls)
	}
	for _, stage := range []string{"correctness-audit", "validate-publish-correctness", "assemble-local-report", "report-value-observations"} {
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
