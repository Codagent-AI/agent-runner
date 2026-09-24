//go:build dev_audit

package devaudit

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/stateio"
)

func TestPublishCorrectnessRejectsCandidateWithoutSemanticDuplicateResult(t *testing.T) {
	request, prepared := correctnessFixture(t)
	candidate := confirmedCandidate()
	candidate.SemanticDuplicate = Duplicate{}

	result, err := PublishCorrectness(request, prepared, CorrectnessCandidates{Candidates: []CorrectnessCandidate{candidate}}, fakeGH{})
	if err != nil {
		t.Fatalf("publish correctness: %v", err)
	}
	if len(result.Findings) != 1 || result.Findings[0].PublicationState != "rejected" {
		t.Fatalf("findings = %#v, want semantic-duplicate rejection", result.Findings)
	}
}

func TestPublishCorrectnessNormalizesSuppliedDefectKeys(t *testing.T) {
	for _, supplied := range []string{
		"agent_runner.agent_usage_telemetry_unavailable_in_pty_context",
		"agent-runner/claude-pty-usage-telemetry-missing",
		"Runner Retry Loss",
	} {
		request, prepared := correctnessFixture(t)
		candidate := confirmedCandidate()
		candidate.DefectKey = supplied

		result, err := PublishCorrectness(request, prepared, CorrectnessCandidates{Candidates: []CorrectnessCandidate{candidate}}, deadlineGH{testing: t})
		if err != nil {
			t.Fatalf("publish correctness: %v", err)
		}
		if len(result.Findings) != 1 || result.Findings[0].PublicationState != "created" {
			t.Fatalf("defect key %q: findings = %#v, want a published finding", supplied, result.Findings)
		}
		if got, want := result.Findings[0].Candidate.DefectKey, normalizeDefectKey(supplied); got != want {
			t.Fatalf("defect key %q recorded as %q, want %q", supplied, got, want)
		}
	}
}

func TestPublishCorrectnessRejectsUnnormalizableDefectKey(t *testing.T) {
	request, prepared := correctnessFixture(t)
	candidate := confirmedCandidate()
	candidate.DefectKey = "///"

	result, err := PublishCorrectness(request, prepared, CorrectnessCandidates{Candidates: []CorrectnessCandidate{candidate}}, fakeGH{})
	if err != nil {
		t.Fatalf("publish correctness: %v", err)
	}
	if len(result.Findings) != 1 || result.Findings[0].PublicationState != "rejected" {
		t.Fatalf("findings = %#v, want an empty-defect-key rejection", result.Findings)
	}
}

func TestPublishCorrectnessHoldsAmbiguousDuplicateWithoutIssueURL(t *testing.T) {
	request, prepared := correctnessFixture(t)
	candidate := confirmedCandidate()
	candidate.SemanticDuplicate = Duplicate{State: "ambiguous", DefectKey: candidate.DefectKey}

	result, err := PublishCorrectness(request, prepared, CorrectnessCandidates{Candidates: []CorrectnessCandidate{candidate}}, fakeGH{})
	if err != nil {
		t.Fatalf("publish correctness: %v", err)
	}
	// An ambiguous search found no single issue to cite, so demanding one
	// discards the finding instead of routing it to review.
	if len(result.Findings) != 1 || result.Findings[0].PublicationState != "ambiguous" {
		t.Fatalf("findings = %#v, want the finding held for review", result.Findings)
	}
}

func TestPublishCorrectnessStillRequiresIssueURLForDecidedDuplicate(t *testing.T) {
	for _, state := range []string{"open", "closed"} {
		request, prepared := correctnessFixture(t)
		candidate := confirmedCandidate()
		candidate.SemanticDuplicate = Duplicate{State: state, DefectKey: candidate.DefectKey}

		result, err := PublishCorrectness(request, prepared, CorrectnessCandidates{Candidates: []CorrectnessCandidate{candidate}}, fakeGH{})
		if err != nil {
			t.Fatalf("publish correctness: %v", err)
		}
		if len(result.Findings) != 1 || result.Findings[0].PublicationState != "rejected" {
			t.Fatalf("duplicate state %q: findings = %#v, want a missing-URL rejection", state, result.Findings)
		}
	}
}

func rejectedReportFixture(t *testing.T, candidate *CorrectnessCandidate, failure string) string {
	t.Helper()
	request, prepared := correctnessFixture(t)
	dir := t.TempDir()
	finding := findingFor(candidate, normalizeDefectKey(candidate.DefectKey), "rejected", "", failure)
	report := LocalReport{
		SourceRunID:        "change-1",
		ExecutionSessionID: "session-1",
		Evidence:           prepared.Index,
		RunnerSource:       request.RunnerSource,
		Correctness:        CorrectnessResult{SchemaVersion: correctnessSchema, Findings: []Finding{finding}},
	}
	if err := stateio.WriteJSONAtomic(filepath.Join(dir, "local-report.json"), report); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestRepublishRejectedFindingsPublishesNewlyValidCandidates(t *testing.T) {
	candidate := confirmedCandidate()
	candidate.DefectKey = "agent_runner.usage_telemetry_missing"
	dir := rejectedReportFixture(t, &candidate, "defect_key is not normalized")

	published, err := republishRejectedFindings(dir, deadlineGH{testing: t})
	if err != nil {
		t.Fatalf("republish rejected findings: %v", err)
	}
	if len(published) != 1 || published[0].PublicationState != "created" || published[0].IssueURL == "" {
		t.Fatalf("published = %#v, want one created issue", published)
	}
	if got, want := published[0].Candidate.DefectKey, "agent-runner-usage-telemetry-missing"; got != want {
		t.Fatalf("recorded defect key = %q, want %q", got, want)
	}

	data, err := os.ReadFile(filepath.Join(dir, "local-report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var updated LocalReport
	if err := json.Unmarshal(data, &updated); err != nil {
		t.Fatal(err)
	}
	if updated.Correctness.Findings[0].PublicationState != "created" {
		t.Fatalf("local report was not updated: %#v", updated.Correctness.Findings[0])
	}
}

func TestRepublishRejectedFindingsLeavesStillInvalidCandidatesUnpublished(t *testing.T) {
	candidate := confirmedCandidate()
	candidate.EvidenceRefs = []string{"evidence-missing"}
	dir := rejectedReportFixture(t, &candidate, "unknown or unavailable evidence reference \"evidence-missing\"")

	published, err := republishRejectedFindings(dir, refusingGH{testing: t})
	if err != nil {
		t.Fatalf("republish rejected findings: %v", err)
	}
	if len(published) != 0 {
		t.Fatalf("published = %#v, want nothing published", published)
	}
}

type refusingGH struct{ testing *testing.T }

func (r refusingGH) Run(context.Context, string, []string, []byte) (string, error) {
	r.testing.Helper()
	r.testing.Fatal("republish contacted GitHub for a candidate that is still invalid")
	return "", nil
}

func TestRunBoundedOutputRejectsOversizedCrosscheckResponse(t *testing.T) {
	command := exec.Command("sh", "-c", "head -c 1025 /dev/zero")
	if _, err := runBoundedOutput(command, 1024); err == nil || !strings.Contains(err.Error(), "exceeds maximum size") {
		t.Fatalf("oversized response error = %v", err)
	}
}

func TestPublishCorrectnessRejectsPrivateSemanticDuplicateURL(t *testing.T) {
	request, prepared := correctnessFixture(t)
	candidate := confirmedCandidate()
	candidate.SemanticDuplicate = Duplicate{State: "closed", URL: "https://private.example/issues/9"}

	result, err := PublishCorrectness(request, prepared, CorrectnessCandidates{Candidates: []CorrectnessCandidate{candidate}}, fakeGH{})
	if err != nil {
		t.Fatalf("publish correctness: %v", err)
	}
	if len(result.Findings) != 1 || result.Findings[0].PublicationState != "rejected" {
		t.Fatalf("findings = %#v, want private-URL rejection", result.Findings)
	}
}

func TestPublishCorrectnessRejectsUnboundedSymptoms(t *testing.T) {
	request, prepared := correctnessFixture(t)
	candidate := confirmedCandidate()
	candidate.Symptoms = []string{strings.Repeat("command output ", 300)}

	result, err := PublishCorrectness(request, prepared, CorrectnessCandidates{Candidates: []CorrectnessCandidate{candidate}}, fakeGH{})
	if err != nil {
		t.Fatalf("publish correctness: %v", err)
	}
	if len(result.Findings) != 1 || result.Findings[0].PublicationState != "rejected" {
		t.Fatalf("findings = %#v, want unsafe-symptom rejection", result.Findings)
	}
}

func TestPublishCorrectnessRejectsIncompleteRunnerSourceSnapshot(t *testing.T) {
	request, prepared := correctnessFixture(t)
	incomplete := t.TempDir()
	if err := os.WriteFile(filepath.Join(incomplete, "go.mod"), []byte("module github.com/codagent/agent-runner\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	request.RunnerSource = SourceProvenance{Verified: true, SnapshotPath: incomplete}

	result, err := PublishCorrectness(request, prepared, CorrectnessCandidates{Candidates: []CorrectnessCandidate{confirmedCandidate()}}, fakeGH{})
	if err != nil {
		t.Fatalf("publish correctness: %v", err)
	}
	if len(result.Findings) != 1 || result.Findings[0].PublicationState != "rejected" {
		t.Fatalf("findings = %#v, want incomplete-source rejection", result.Findings)
	}
}

func TestLoadCorrectnessCandidatesRejectsIndependentRunnerEvidenceFallback(t *testing.T) {
	request := Request{AuditSessionDir: t.TempDir()}
	outputDir := filepath.Join(request.AuditSessionDir, "model-output")
	if err := os.MkdirAll(outputDir, 0o700); err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"candidates":[{"status":"confirmed","independent_evidence":[{"reference_id":"runner-match","defect_key":"runner-retry-loss","verification":"matching source"}],"semantic_duplicate":{"state":"none"}}]}`)
	if err := os.WriteFile(filepath.Join(outputDir, correctnessOutput), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadCorrectnessCandidates(&request); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("load correctness error = %v, want rejected independent fallback", err)
	}
}

func TestPublishCorrectnessDoesNotModifyAuditedOrRunnerRepositories(t *testing.T) {
	source := initializedGitRepository(t, false)
	runnerSource := initializedGitRepository(t, true)
	beforeSource, beforeRunner := gitState(t, source), gitState(t, runnerSource)
	before, err := fingerprintTree(source)
	if err != nil {
		t.Fatal(err)
	}
	request := Request{AuditSessionDir: t.TempDir(), SnapshotPath: source, RunnerSource: SourceProvenance{Verified: true, Coverage: "complete", SnapshotPath: runnerSource}}
	prepared := PreparedValueAudit{Index: EvidenceIndex{Fingerprints: Fingerprints{SnapshotBefore: before}, References: []EvidenceReference{{ID: "evidence-1", Status: "available"}}}}
	if _, err := PublishCorrectness(request, prepared, CorrectnessCandidates{Candidates: []CorrectnessCandidate{confirmedCandidate()}}, &recordingGH{}); err != nil {
		t.Fatal(err)
	}
	if after := gitState(t, source); after != beforeSource {
		t.Fatalf("audited repository changed: before=%q after=%q", beforeSource, after)
	}
	if after := gitState(t, runnerSource); after != beforeRunner {
		t.Fatalf("Runner repository changed: before=%q after=%q", beforeRunner, after)
	}
}

func initializedGitRepository(t *testing.T, runner bool) string {
	t.Helper()
	root := t.TempDir()
	for _, args := range [][]string{{"init"}, {"config", "user.email", "audit@example.test"}, {"config", "user.name", "Audit Test"}} {
		if output, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if runner {
		if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module github.com/codagent/agent-runner\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		for _, path := range []string{"cmd/agent-runner", "internal/runner", "workflows"} {
			if err := os.MkdirAll(filepath.Join(root, path), 0o700); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, args := range [][]string{{"add", "."}, {"commit", "-m", "test fixture"}} {
		if output, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	return root
}

func gitState(t *testing.T, root string) string {
	t.Helper()
	head, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	status, err := exec.Command("git", "-C", root, "status", "--porcelain").Output()
	if err != nil {
		t.Fatal(err)
	}
	return string(head) + "\x00" + string(status)
}

func correctnessFixture(t *testing.T) (Request, PreparedValueAudit) {
	t.Helper()
	root := t.TempDir()
	runnerSource := filepath.Join(root, "runner-source")
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
	before, err := fingerprintTree(root)
	if err != nil {
		t.Fatal(err)
	}
	request := Request{SnapshotPath: root, RunnerSource: SourceProvenance{Verified: true, Coverage: "complete", SnapshotPath: runnerSource}}
	prepared := PreparedValueAudit{Index: EvidenceIndex{Fingerprints: Fingerprints{SnapshotBefore: before}, References: []EvidenceReference{{ID: "evidence-1", Status: "available"}}}}
	return request, prepared
}

func confirmedCandidate() CorrectnessCandidate {
	return CorrectnessCandidate{Status: "confirmed", DefectKey: "runner-retry-loss", Title: "retry state is lost", Observed: "retry loses state", Expected: "retry preserves state", Verification: "run the retry workflow", AffectedComponent: "internal/runner", EvidenceRefs: []string{"evidence-1"}, Confidence: "high", SemanticDuplicate: Duplicate{State: "none"}}
}

type fakeGH struct{}

func (fakeGH) Run(context.Context, string, []string, []byte) (string, error) { return "[]", nil }

type deadlineGH struct {
	testing *testing.T
}

func (r deadlineGH) Run(ctx context.Context, _ string, args []string, _ []byte) (string, error) {
	r.testing.Helper()
	if _, ok := ctx.Deadline(); !ok {
		r.testing.Fatalf("gh %v was invoked without a deadline", args)
	}
	if len(args) > 1 && args[1] == "create" {
		return "https://github.com/Codagent-AI/agent-runner/issues/42", nil
	}
	return "[]", nil
}

func TestPublishCorrectnessBoundsEveryGitHubCommand(t *testing.T) {
	request, prepared := correctnessFixture(t)
	result, err := PublishCorrectness(request, prepared, CorrectnessCandidates{Candidates: []CorrectnessCandidate{confirmedCandidate()}}, deadlineGH{testing: t})
	if err != nil {
		t.Fatal(err)
	}
	if result.Findings[0].PublicationState != "created" {
		t.Fatalf("finding = %#v", result.Findings[0])
	}
}

func TestPublishCorrectnessCreatesOneFocusedRedactedIssue(t *testing.T) {
	request, prepared := correctnessFixture(t)
	runner := &recordingGH{}
	candidate := confirmedCandidate()
	candidate.Observed = "token=super-secret at /Users/alice/private/project and https://private.example/run"
	candidate.Symptoms = []string{"first retry failed", "second retry failed"}

	result, err := PublishCorrectness(request, prepared, CorrectnessCandidates{Candidates: []CorrectnessCandidate{candidate}}, runner)
	if err != nil {
		t.Fatalf("publish correctness: %v", err)
	}
	if len(result.Findings) != 1 || result.Findings[0].PublicationState != "created" || result.Findings[0].IssueURL != "https://github.com/Codagent-AI/agent-runner/issues/42" {
		t.Fatalf("findings = %#v", result.Findings)
	}
	if got := runner.createCalls(); got != 1 {
		t.Fatalf("issue creates = %d, want 1 (%#v)", got, runner.calls)
	}
	create := runner.calls[len(runner.calls)-2]
	if got := strings.Join(create.args, " "); !strings.Contains(got, "--repo Codagent-AI/agent-runner") || !strings.Contains(got, "--title [auto-audit]") {
		t.Fatalf("create argv = %q", got)
	}
	body := string(create.stdin)
	for _, unsafe := range []string{"super-secret", "/Users/alice", "https://private.example"} {
		if strings.Contains(body, unsafe) {
			t.Fatalf("unsafe issue body = %q", body)
		}
	}
	if !strings.Contains(body, result.Findings[0].Marker) || !strings.Contains(body, "## Expected behavior") || !strings.Contains(body, "## Verification") {
		t.Fatalf("focused issue body = %q", body)
	}
}

func TestPublishCorrectnessSetsBugTypeAfterCreatingIssue(t *testing.T) {
	request, prepared := correctnessFixture(t)
	runner := &recordingGH{}
	result, err := PublishCorrectness(request, prepared, CorrectnessCandidates{Candidates: []CorrectnessCandidate{confirmedCandidate()}}, runner)
	if err != nil {
		t.Fatal(err)
	}
	if result.Findings[0].PublicationState != "created" {
		t.Fatalf("finding = %#v", result.Findings[0])
	}
	if got, want := runner.callArgs(), []string{
		"gh issue create --repo Codagent-AI/agent-runner --title [auto-audit] retry state is lost --body-file -",
		"gh api --method PATCH repos/Codagent-AI/agent-runner/issues/42 -f type=Bug",
	}; !strings.Contains(strings.Join(got, "\n"), strings.Join(want, "\n")) {
		t.Fatalf("GitHub calls = %#v, want create followed by type PATCH", got)
	}
}

func TestPublishCorrectnessRecordsBugTypeFailureWithoutFailingPublication(t *testing.T) {
	request, prepared := correctnessFixture(t)
	runner := &recordingGH{patchErr: errors.New("type update rejected")}
	result, err := PublishCorrectness(request, prepared, CorrectnessCandidates{Candidates: []CorrectnessCandidate{confirmedCandidate()}}, runner)
	if err != nil {
		t.Fatal(err)
	}
	finding := result.Findings[0]
	if finding.PublicationState != "created" || finding.IssueURL != "https://github.com/Codagent-AI/agent-runner/issues/42" {
		t.Fatalf("finding = %#v", finding)
	}
	data, err := json.Marshal(finding)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["warning"] != "type update rejected" {
		t.Fatalf("warning = %#v, want PATCH failure", fields["warning"])
	}
}

func TestPublishCorrectnessLinksOpenDuplicateWithoutMutation(t *testing.T) {
	request, prepared := correctnessFixture(t)
	runner := &recordingGH{semantic: []ghIssue{{URL: "https://github.com/Codagent-AI/agent-runner/issues/7", State: "OPEN", Body: causeMarker("runner-retry-loss")}}}

	result, err := PublishCorrectness(request, prepared, CorrectnessCandidates{Candidates: []CorrectnessCandidate{confirmedCandidate()}}, runner)
	if err != nil {
		t.Fatalf("publish correctness: %v", err)
	}
	if finding := result.Findings[0]; finding.PublicationState != "duplicate" || finding.DuplicateURL != "https://github.com/Codagent-AI/agent-runner/issues/7" {
		t.Fatalf("finding = %#v", finding)
	}
	if got := runner.createCalls(); got != 0 {
		t.Fatalf("duplicate mutated GitHub %d times", got)
	}
	for _, call := range runner.calls {
		if len(call.args) < 3 || call.args[1] != "issue" || call.args[2] != "list" {
			t.Fatalf("duplicate triggered a GitHub mutation: %#v", call.args)
		}
	}
}

func TestPublishCorrectnessDoesNotTrustUnverifiedModelDuplicate(t *testing.T) {
	request, prepared := correctnessFixture(t)
	candidate := confirmedCandidate()
	candidate.SemanticDuplicate = Duplicate{State: "open", URL: "https://github.com/Codagent-AI/agent-runner/issues/9", DefectKey: "runner-retry-loss"}
	runner := &recordingGH{view: &ghIssue{URL: candidate.SemanticDuplicate.URL, State: "CLOSED"}}

	result, err := PublishCorrectness(request, prepared, CorrectnessCandidates{Candidates: []CorrectnessCandidate{candidate}}, runner)
	if err != nil {
		t.Fatalf("publish correctness: %v", err)
	}
	if finding := result.Findings[0]; finding.PublicationState != "ambiguous" {
		t.Fatalf("finding = %#v, want unverified duplicate to remain ambiguous", finding)
	}
	if got := runner.createCalls(); got != 0 {
		t.Fatalf("unverified duplicate created %d issues", got)
	}
}

func TestPublishCorrectnessLinksModelSelectedManualDuplicateAfterGHVerification(t *testing.T) {
	request, prepared := correctnessFixture(t)
	candidate := confirmedCandidate()
	candidate.SemanticDuplicate = Duplicate{State: "open", URL: "https://github.com/Codagent-AI/agent-runner/issues/9", DefectKey: candidate.DefectKey}
	runner := &recordingGH{view: &ghIssue{URL: candidate.SemanticDuplicate.URL, State: "OPEN", Body: "manually filed issue without an audit marker"}}

	result, err := PublishCorrectness(request, prepared, CorrectnessCandidates{Candidates: []CorrectnessCandidate{candidate}}, runner)
	if err != nil {
		t.Fatalf("publish correctness: %v", err)
	}
	if finding := result.Findings[0]; finding.PublicationState != "duplicate" || finding.DuplicateURL != candidate.SemanticDuplicate.URL {
		t.Fatalf("finding = %#v, want verified manual duplicate", finding)
	}
	if got := runner.createCalls(); got != 0 {
		t.Fatalf("verified manual duplicate created %d issues", got)
	}
}

func TestPublishCorrectnessKeepsAmbiguousDuplicateCandidateRetryable(t *testing.T) {
	request, prepared := correctnessFixture(t)
	runner := &recordingGH{}
	candidate := confirmedCandidate()
	candidate.SemanticDuplicate = Duplicate{State: "ambiguous", URL: "https://github.com/Codagent-AI/agent-runner/issues/8", DefectKey: "runner-retry-loss"}

	result, err := PublishCorrectness(request, prepared, CorrectnessCandidates{Candidates: []CorrectnessCandidate{candidate}}, runner)
	if err != nil {
		t.Fatalf("publish correctness: %v", err)
	}
	if finding := result.Findings[0]; finding.PublicationState != "ambiguous" || finding.Failure == "" {
		t.Fatalf("finding = %#v", finding)
	}
	if got := runner.createCalls(); got != 0 {
		t.Fatalf("ambiguous candidate created %d issues", got)
	}
}

func TestPublishCorrectnessVerifiesClosedRecurrenceBeforeCreating(t *testing.T) {
	request, prepared := correctnessFixture(t)
	candidate := confirmedCandidate()
	candidate.SemanticDuplicate = Duplicate{State: "closed", URL: "https://github.com/Codagent-AI/agent-runner/issues/8", DefectKey: candidate.DefectKey}
	runner := &recordingGH{view: &ghIssue{URL: candidate.SemanticDuplicate.URL, State: "CLOSED", Body: causeMarker(candidate.DefectKey)}}

	result, err := PublishCorrectness(request, prepared, CorrectnessCandidates{Candidates: []CorrectnessCandidate{candidate}}, runner)
	if err != nil {
		t.Fatalf("publish correctness: %v", err)
	}
	if finding := result.Findings[0]; finding.PublicationState != "created" || finding.PriorClosedIssue != candidate.SemanticDuplicate.URL {
		t.Fatalf("finding = %#v", finding)
	}
	if runner.createCalls() != 1 {
		t.Fatalf("closed recurrence did not create exactly one issue: %#v", runner.calls)
	}
}

func TestPublishCorrectnessDoesNotLinkSimilarIssueWithDifferentCause(t *testing.T) {
	request, prepared := correctnessFixture(t)
	runner := &recordingGH{semantic: []ghIssue{{URL: "https://github.com/Codagent-AI/agent-runner/issues/7", State: "OPEN", Body: causeMarker("different-cause")}}}

	result, err := PublishCorrectness(request, prepared, CorrectnessCandidates{Candidates: []CorrectnessCandidate{confirmedCandidate()}}, runner)
	if err != nil {
		t.Fatalf("publish correctness: %v", err)
	}
	if finding := result.Findings[0]; finding.PublicationState != "created" {
		t.Fatalf("finding = %#v", finding)
	}
	if runner.createCalls() != 1 {
		t.Fatalf("similar issue suppressed creation: %#v", runner.calls)
	}
}

func TestPublishCorrectnessGroupsSameCauseCandidates(t *testing.T) {
	request, prepared := correctnessFixture(t)
	first, second := confirmedCandidate(), confirmedCandidate()
	first.Symptoms = []string{"first symptom"}
	second.Observed, second.Symptoms = "second symptom", []string{"second symptom"}
	runner := &recordingGH{}

	result, err := PublishCorrectness(request, prepared, CorrectnessCandidates{Candidates: []CorrectnessCandidate{first, second}}, runner)
	if err != nil {
		t.Fatalf("publish correctness: %v", err)
	}
	if len(result.Findings) != 1 || runner.createCalls() != 1 {
		t.Fatalf("findings=%#v calls=%#v", result.Findings, runner.calls)
	}
	if body := string(runner.calls[len(runner.calls)-2].stdin); !strings.Contains(body, "first symptom") || !strings.Contains(body, "second symptom") {
		t.Fatalf("grouped body = %q", body)
	}
}

func TestPublishCorrectnessRetriesFailedCreationThroughExactMarker(t *testing.T) {
	request, prepared := correctnessFixture(t)
	candidate := confirmedCandidate()
	failing := &recordingGH{createErr: errors.New("GitHub unavailable")}
	first, err := PublishCorrectness(request, prepared, CorrectnessCandidates{Candidates: []CorrectnessCandidate{candidate}}, failing)
	if err != nil {
		t.Fatalf("first publish: %v", err)
	}
	if first.Findings[0].PublicationState != "failed" {
		t.Fatalf("first finding = %#v", first.Findings[0])
	}
	retry := &recordingGH{marker: []ghIssue{{URL: "https://github.com/Codagent-AI/agent-runner/issues/42", State: "OPEN", Body: first.Findings[0].Marker}}}
	second, err := PublishCorrectness(request, prepared, CorrectnessCandidates{Candidates: []CorrectnessCandidate{candidate}}, retry)
	if err != nil {
		t.Fatalf("retry publish: %v", err)
	}
	if second.Findings[0].PublicationState != "created" || retry.createCalls() != 0 {
		t.Fatalf("retry finding=%#v calls=%#v", second.Findings[0], retry.calls)
	}
}

func TestExecutableRunnerUsesFixedGHArgumentsAndStdin(t *testing.T) {
	request, prepared := correctnessFixture(t)
	bin := t.TempDir()
	argsPath := filepath.Join(t.TempDir(), "argv")
	stdinPath := filepath.Join(t.TempDir(), "stdin")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >> \"$FAKE_GH_ARGS\"\ncat >> \"$FAKE_GH_STDIN\"\nif [ \"$2\" = list ]; then printf '[]'; else printf 'https://github.com/Codagent-AI/agent-runner/issues/42\\n'; fi\n"
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_GH_ARGS", argsPath)
	t.Setenv("FAKE_GH_STDIN", stdinPath)

	result, err := PublishCorrectness(request, prepared, CorrectnessCandidates{Candidates: []CorrectnessCandidate{confirmedCandidate()}}, executableRunner{})
	if err != nil {
		t.Fatalf("publish through executable: %v", err)
	}
	if finding := result.Findings[0]; finding.IssueURL != "https://github.com/Codagent-AI/agent-runner/issues/42" {
		t.Fatalf("finding = %#v", finding)
	}
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(args); !strings.Contains(got, "--repo\nCodagent-AI/agent-runner\n") || !strings.Contains(got, "issue\ncreate\n") || !strings.Contains(got, "--body-file\n-\n") {
		t.Fatalf("fake gh argv = %q", got)
	}
	stdin, err := os.ReadFile(stdinPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(stdin), "<!-- agent-runner-audit:") {
		t.Fatalf("fake gh stdin = %q", stdin)
	}
}

func TestRepairIssueBodiesRestoresOnlyBlankAutoAuditIssues(t *testing.T) {
	report := LocalReport{
		SourceRunID: "source", ExecutionSessionID: "session",
		Correctness: CorrectnessResult{Findings: []Finding{{
			PublicationState: "created", IssueURL: "https://github.com/Codagent-AI/agent-runner/issues/42",
			Marker: findingMarker("finding-1"), Candidate: confirmedCandidate(),
		}}},
	}
	runner := &recordingGH{view: &ghIssue{URL: "https://github.com/Codagent-AI/agent-runner/issues/42", State: "OPEN", Title: "[auto-audit] retry state is lost", Body: "-"}}
	if repaired, err := repairIssueBodies(&report, runner); err != nil || repaired != 1 {
		t.Fatalf("repair issue bodies = %d, %v", repaired, err)
	}
	edit := runner.calls[len(runner.calls)-2]
	if got, want := strings.Join(edit.args, " "), "gh issue edit 42 --repo Codagent-AI/agent-runner --body-file -"; got != want {
		t.Fatalf("edit args = %q, want %q", got, want)
	}
	if body := string(edit.stdin); !strings.Contains(body, "## Observed behavior") || !strings.Contains(body, findingMarker("finding-1")) || !strings.Contains(body, causeMarker(report.Correctness.Findings[0].Candidate.DefectKey)) {
		t.Fatalf("repaired issue body = %q", body)
	}
}

func TestRepairIssueBodiesSetsBugTypeAfterBodyRepair(t *testing.T) {
	report := LocalReport{Correctness: CorrectnessResult{Findings: []Finding{{
		PublicationState: "created", IssueURL: "https://github.com/Codagent-AI/agent-runner/issues/42",
		Marker: findingMarker("finding-1"), Candidate: confirmedCandidate(),
	}}}}
	runner := &recordingGH{view: &ghIssue{URL: report.Correctness.Findings[0].IssueURL, State: "OPEN", Title: "[auto-audit] retry state is lost", Body: "-"}}
	if repaired, err := repairIssueBodies(&report, runner); err != nil || repaired != 1 {
		t.Fatalf("repair issue bodies = %d, %v", repaired, err)
	}
	if got := runner.callArgs(); len(got) < 3 || got[len(got)-2] != "gh issue edit 42 --repo Codagent-AI/agent-runner --body-file -" || got[len(got)-1] != "gh api --method PATCH repos/Codagent-AI/agent-runner/issues/42 -f type=Bug" {
		t.Fatalf("GitHub calls = %#v, want edit followed by type PATCH", got)
	}
}

func TestRepairIssueBodiesRecordsBugTypeFailure(t *testing.T) {
	report := LocalReport{Correctness: CorrectnessResult{Findings: []Finding{{
		PublicationState: "created", IssueURL: "https://github.com/Codagent-AI/agent-runner/issues/42",
		Marker: findingMarker("finding-1"), Candidate: confirmedCandidate(),
	}}}}
	runner := &recordingGH{view: &ghIssue{URL: report.Correctness.Findings[0].IssueURL, State: "OPEN", Title: "[auto-audit] retry state is lost", Body: "-"}, patchErr: errors.New("type update rejected")}
	if repaired, err := repairIssueBodies(&report, runner); err != nil || repaired != 1 {
		t.Fatalf("repair issue bodies = %d, %v", repaired, err)
	}
	data, err := json.Marshal(report.Correctness.Findings[0])
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["warning"] != "type update rejected" {
		t.Fatalf("warning = %#v, want PATCH failure", fields["warning"])
	}
}

func TestRepairIssueBodiesRejectsIssueOutsideAuditRepository(t *testing.T) {
	report := LocalReport{Correctness: CorrectnessResult{Findings: []Finding{{
		PublicationState: "created", IssueURL: "https://github.com/example/other/issues/42",
		Marker: findingMarker("finding-1"), Candidate: confirmedCandidate(),
	}}}}
	runner := &recordingGH{view: &ghIssue{URL: "https://github.com/example/other/issues/42", State: "OPEN", Title: "[auto-audit] retry state is lost", Body: "-"}}
	if _, err := repairIssueBodies(&report, runner); err == nil {
		t.Fatal("repair issue bodies accepted an issue outside the audit repository")
	}
	if len(runner.calls) != 0 {
		t.Fatalf("GitHub calls = %#v, want none", runner.calls)
	}
}

func TestPublishCorrectnessPersistsPendingStateBeforeCreate(t *testing.T) {
	request, prepared := correctnessFixture(t)
	request.AuditSessionDir = filepath.Join(t.TempDir(), "audit")
	if err := os.MkdirAll(request.AuditSessionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	runner := &recordingGH{beforeCreate: func() {
		data, err := os.ReadFile(filepath.Join(request.AuditSessionDir, "correctness-findings.json"))
		if err != nil {
			t.Fatalf("read pending finding: %v", err)
		}
		var result CorrectnessResult
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatalf("decode pending finding: %v", err)
		}
		if len(result.Findings) != 1 || result.Findings[0].PublicationState != "pending" {
			t.Fatalf("pre-create state = %#v", result.Findings)
		}
	}}

	result, err := PublishCorrectness(request, prepared, CorrectnessCandidates{Candidates: []CorrectnessCandidate{confirmedCandidate()}}, runner)
	if err != nil {
		t.Fatalf("publish correctness: %v", err)
	}
	if result.Findings[0].PublicationState != "created" {
		t.Fatalf("final state = %#v", result.Findings[0])
	}
}

func TestAssembleLocalReportFreezesConfiguredOrUnavailableDestination(t *testing.T) {
	request, prepared := correctnessFixture(t)
	request.AuditSessionDir = t.TempDir()
	request.AuditRunID, request.SourceRunID, request.ExecutionSessionID, request.Trigger = "audit-1", "source-1", "session-1", "replay"
	if err := persistPrepared(&request, &prepared); err != nil {
		t.Fatal(err)
	}
	if err := writePreparedFingerprint(request.AuditSessionDir); err != nil {
		t.Fatal(err)
	}
	if err := stateio.WriteJSONAtomic(filepath.Join(request.AuditSessionDir, "value-consultations.json"), []consultationLedgerEntry{{ObservationID: "observation", ReferenceID: "evidence-1", Category: "output"}}); err != nil {
		t.Fatal(err)
	}
	oldResolver := destinationResolver
	destinationResolver = fakeDestination{DestinationState{State: "configured", SpreadsheetID: "sheet-1", Tab: "audit"}}
	t.Cleanup(func() { destinationResolver = oldResolver })

	if err := assembleLocalReportStage(&request); err != nil {
		t.Fatalf("assemble report: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(request.AuditSessionDir, "local-report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var report LocalReport
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	if report.SchemaVersion != "step_value_v1" || report.Destination.State != "configured" || report.Destination.SpreadsheetID != "sheet-1" || report.Destination.Tab != "audit" {
		t.Fatalf("report identity = %#v", report)
	}
	if len(report.ValueConsultations) != 1 || report.Trigger != "replay" || report.DeliveryState != "pending" {
		t.Fatalf("report state = %#v", report)
	}
}

func TestAssembleLocalReportDoesNotRetainUnavailableDiagnosticsForPresentResults(t *testing.T) {
	request, prepared := correctnessFixture(t)
	request.AuditSessionDir = t.TempDir()
	leafReference := EvidenceReference{ID: "leaf-metrics", Category: "metrics", Status: "available"}
	prepared.Index.Leaves = []LeafEvidence{{Evidence: []EvidenceReference{leafReference}}}
	if err := persistPrepared(&request, &prepared); err != nil {
		t.Fatal(err)
	}
	if err := writePreparedFingerprint(request.AuditSessionDir); err != nil {
		t.Fatal(err)
	}
	if err := stateio.WriteJSONAtomic(filepath.Join(request.AuditSessionDir, "value-observations.json"), ValueValidationResult{SchemaVersion: evidenceSchemaVersion, Observations: []ValueObservation{}}); err != nil {
		t.Fatal(err)
	}
	correctness := CorrectnessResult{SchemaVersion: correctnessSchema, Findings: []Finding{{
		FindingID: "finding-1", Candidate: CorrectnessCandidate{Consultations: []string{leafReference.ID}}, PublicationState: "retained",
	}}}
	if err := stateio.WriteJSONAtomic(filepath.Join(request.AuditSessionDir, "correctness-findings.json"), correctness); err != nil {
		t.Fatal(err)
	}
	if err := assembleLocalReportStage(&request); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(request.AuditSessionDir, "local-report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var report LocalReport
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Values.Diagnostics) != 0 || len(report.Correctness.Diagnostics) != 0 {
		t.Fatalf("present results retained unavailable diagnostics: values=%v correctness=%v", report.Values.Diagnostics, report.Correctness.Diagnostics)
	}
	if len(report.CorrectnessConsultation) != 1 || report.CorrectnessConsultation[0].Category != leafReference.Category {
		t.Fatalf("correctness consultation ledger = %#v", report.CorrectnessConsultation)
	}
}

func TestAssembleLocalReportPreservesUnavailableAndUnusableDestinationState(t *testing.T) {
	for _, destination := range []DestinationState{
		{State: "unavailable", Diagnostic: "not configured"},
		{State: "unusable", Diagnostic: "record is malformed"},
	} {
		t.Run(destination.State, func(t *testing.T) {
			request, prepared := correctnessFixture(t)
			request.AuditSessionDir = t.TempDir()
			if err := persistPrepared(&request, &prepared); err != nil {
				t.Fatal(err)
			}
			if err := writePreparedFingerprint(request.AuditSessionDir); err != nil {
				t.Fatal(err)
			}
			oldResolver := destinationResolver
			destinationResolver = fakeDestination{destination}
			t.Cleanup(func() { destinationResolver = oldResolver })
			if err := assembleLocalReportStage(&request); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(request.AuditSessionDir, "local-report.json"))
			if err != nil {
				t.Fatal(err)
			}
			var report LocalReport
			if err := json.Unmarshal(data, &report); err != nil {
				t.Fatal(err)
			}
			if report.Destination != destination {
				t.Fatalf("destination = %#v, want %#v", report.Destination, destination)
			}
		})
	}
}

type fakeDestination struct{ state DestinationState }

func (resolver fakeDestination) ResolveDestination() DestinationState { return resolver.state }

type recordedGHCall struct {
	args  []string
	stdin []byte
}

type recordingGH struct {
	semantic     []ghIssue
	marker       []ghIssue
	view         *ghIssue
	calls        []recordedGHCall
	beforeCreate func()
	createErr    error
	patchErr     error
}

func (runner *recordingGH) Run(_ context.Context, name string, args []string, stdin []byte) (string, error) {
	runner.calls = append(runner.calls, recordedGHCall{args: append([]string{name}, args...), stdin: append([]byte(nil), stdin...)})
	if len(args) >= 2 && args[0] == "issue" && args[1] == "create" {
		if runner.beforeCreate != nil {
			runner.beforeCreate()
		}
		if runner.createErr != nil {
			return "temporary create failure", runner.createErr
		}
		return "https://github.com/Codagent-AI/agent-runner/issues/42\n", nil
	}
	if len(args) >= 2 && args[0] == "issue" && args[1] == "view" {
		issue := ghIssue{URL: "https://github.com/Codagent-AI/agent-runner/issues/9", State: "OPEN"}
		if runner.view != nil {
			issue = *runner.view
		}
		data, _ := json.Marshal(issue)
		return string(data), nil
	}
	if len(args) >= 2 && args[0] == "issue" && args[1] == "edit" {
		return "", nil
	}
	if len(args) >= 2 && args[0] == "api" && args[1] == "--method" {
		if runner.patchErr != nil {
			return "type update rejected\n", runner.patchErr
		}
		return "{}", nil
	}
	query := args[indexOf(args, "--search")+1]
	issues := runner.semantic
	if strings.Contains(query, "<!-- agent-runner-audit:") {
		issues = runner.marker
	}
	data, _ := json.Marshal(issues)
	return string(data), nil
}

func (runner *recordingGH) callArgs() []string {
	args := make([]string, 0, len(runner.calls))
	for _, call := range runner.calls {
		args = append(args, strings.Join(call.args, " "))
	}
	return args
}

func (runner *recordingGH) createCalls() int {
	count := 0
	for _, call := range runner.calls {
		if len(call.args) >= 3 && call.args[1] == "issue" && call.args[2] == "create" {
			count++
		}
	}
	return count
}

func indexOf(values []string, target string) int {
	for index, value := range values {
		if value == target {
			return index
		}
	}
	return -1
}
