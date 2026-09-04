//go:build dev_audit

package devaudit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/codagent/agent-runner/internal/cli"
	"github.com/codagent/agent-runner/internal/stateio"
)

const (
	auditIssueRepository = "Codagent-AI/agent-runner"
	correctnessOutput    = "correctness.json"
	correctnessSchema    = "correctness_v1"
	maxCrosscheckOutput  = 256 * 1024
)

// CorrectnessCandidates is the only writable product of the correctness
// model. It contains judgments, never measured facts or repository commands.
type CorrectnessCandidates struct {
	Candidates []CorrectnessCandidate `json:"candidates"`
	Provenance BatchProvenance        `json:"-"`
}

type CorrectnessCandidate struct {
	Status            string    `json:"status"`
	DefectKey         string    `json:"defect_key,omitempty"`
	Title             string    `json:"title,omitempty"`
	Observed          string    `json:"observed,omitempty"`
	Expected          string    `json:"expected,omitempty"`
	Verification      string    `json:"verification,omitempty"`
	AffectedComponent string    `json:"affected_component,omitempty"`
	EvidenceRefs      []string  `json:"evidence_refs,omitempty"`
	Consultations     []string  `json:"consultations,omitempty"`
	Confidence        string    `json:"confidence,omitempty"`
	Symptoms          []string  `json:"symptoms,omitempty"`
	SemanticDuplicate Duplicate `json:"semantic_duplicate"`
}

// Duplicate is model-supplied duplicate research. The publisher independently
// verifies it through gh before it can influence publication.
type Duplicate struct {
	URL       string `json:"url,omitempty"`
	State     string `json:"state,omitempty"`
	DefectKey string `json:"defect_key,omitempty"`
}

type Finding struct {
	FindingID        string               `json:"finding_id"`
	Candidate        CorrectnessCandidate `json:"candidate"`
	Marker           string               `json:"marker"`
	PublicationState string               `json:"publication_state"`
	IssueURL         string               `json:"issue_url,omitempty"`
	DuplicateURL     string               `json:"duplicate_url,omitempty"`
	PriorClosedIssue string               `json:"prior_closed_issue,omitempty"`
	Failure          string               `json:"failure,omitempty"`
	Redacted         bool                 `json:"redacted,omitempty"`
}

type CorrectnessResult struct {
	SchemaVersion  string    `json:"schema_version"`
	Findings       []Finding `json:"findings"`
	Diagnostics    []string  `json:"diagnostics,omitempty"`
	JudgeCLI       string    `json:"judge_cli,omitempty"`
	JudgeModel     string    `json:"judge_model,omitempty"`
	JudgeEffort    string    `json:"judge_reasoning_effort,omitempty"`
	JudgeSessionID string    `json:"judge_session_id,omitempty"`
}

// CommandRunner keeps the authenticated gh executable as the sole GitHub
// boundary. Tests replace it with a stateful in-memory fake.
type CommandRunner interface {
	Run(context.Context, string, []string, []byte) (string, error)
}

type executableRunner struct{}

func (executableRunner) Run(ctx context.Context, name string, args []string, stdin []byte) (string, error) {
	command := exec.CommandContext(ctx, name, args...) // #nosec G204 -- fixed gh command with validated input only.
	command.Stdin = strings.NewReader(string(stdin))
	output, err := command.CombinedOutput()
	return string(output), err
}

var ghRunner CommandRunner = executableRunner{}

func ensureCorrectnessOutput(request *Request) error {
	path := filepath.Join(request.AuditSessionDir, "model-output", correctnessOutput)
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if request.Crosscheck.CLI == "" {
		return writeCorrectnessDiagnostic(request, "frozen crosscheck CLI is unavailable")
	}
	output, err := invokeCrosscheckCorrectness(request)
	if err != nil {
		return writeCorrectnessDiagnostic(request, err.Error())
	}
	return stateio.WriteJSONAtomic(path, output)
}

func writeCorrectnessDiagnostic(request *Request, message string) error {
	return stateio.WriteJSONAtomic(filepath.Join(request.AuditSessionDir, "correctness-model-diagnostics.json"), map[string]string{"error": message})
}

func invokeCrosscheckCorrectness(request *Request) (CorrectnessCandidates, error) {
	trusted, err := trustedAuditInputsFingerprint(request)
	if err != nil {
		return CorrectnessCandidates{}, err
	}
	adapter, err := cli.Get(request.Crosscheck.CLI)
	if err != nil {
		return CorrectnessCandidates{}, err
	}
	prepared, err := loadPreparedValueAudit(request.AuditSessionDir)
	if err != nil {
		return CorrectnessCandidates{}, err
	}
	input, err := json.Marshal(struct {
		Evidence EvidenceIndex    `json:"evidence"`
		Source   SourceProvenance `json:"runner_source"`
	}{prepared.Index, request.RunnerSource})
	if err != nil {
		return CorrectnessCandidates{}, err
	}
	prompt := "Investigate only reproducible Agent Runner defects using this immutable audit evidence and the read-only Runner source under evidence/runner-source. Return exactly one JSON object with candidates. A candidate has status (confirmed, inconclusive, excluded), normalized defect_key, title, observed, expected, verification, affected_component, evidence_refs, consultations, confidence, symptoms, and semantic_duplicate {url,state,defect_key}. Do not edit repositories, run GitHub commands, include transcripts, paths, URLs other than duplicate issue URLs, secrets, or prose outside JSON. Project, user, and external failures are excluded unless evidence verifies an Agent Runner cause.\n\n" + string(input)
	workspace, err := prepareModelWorkspace(request)
	if err != nil {
		return CorrectnessCandidates{}, err
	}
	args, err := cli.BuildInvocationArgs(adapter, &cli.BuildArgsInput{Prompt: prompt, Model: request.Crosscheck.Model, Effort: request.Crosscheck.Effort, Context: cli.ContextAutonomousHeadless, Workdir: workspace, DisallowedTools: []string{"AskUserQuestion"}})
	if err != nil || len(args) == 0 {
		if err == nil {
			err = fmt.Errorf("crosscheck adapter produced no command")
		}
		return CorrectnessCandidates{}, err
	}
	command, err := crosscheckCommand(args, workspace, filepath.Join(request.AuditSessionDir, "model-output"))
	if err != nil {
		return CorrectnessCandidates{}, err
	}
	env, err := cliEnvironment(adapter, request, input, workspace)
	if err != nil {
		return CorrectnessCandidates{}, err
	}
	command.Env = env
	data, runErr := runBoundedOutput(command, maxCrosscheckOutput)
	if after, err := trustedAuditInputsFingerprint(request); err != nil {
		return CorrectnessCandidates{}, err
	} else if after != trusted {
		return CorrectnessCandidates{}, fmt.Errorf("trusted audit inputs changed during crosscheck")
	}
	if runErr != nil {
		return CorrectnessCandidates{}, fmt.Errorf("run crosscheck: %w", runErr)
	}
	response := string(data)
	if filter, ok := adapter.(cli.OutputFilter); ok {
		response = filter.FilterOutput(response)
	}
	decoder := json.NewDecoder(strings.NewReader(response))
	decoder.DisallowUnknownFields()
	var output CorrectnessCandidates
	if err := decoder.Decode(&output); err != nil {
		return CorrectnessCandidates{}, fmt.Errorf("decode crosscheck result: %w", err)
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return CorrectnessCandidates{}, fmt.Errorf("crosscheck result contains multiple JSON values")
	}
	output.Provenance = BatchProvenance{CLI: request.Crosscheck.CLI, Model: request.Crosscheck.Model, Effort: request.Crosscheck.Effort, SessionID: "unknown"}
	return output, nil
}

func runBoundedOutput(command *exec.Cmd, maximum int64) ([]byte, error) {
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := command.Start(); err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(stdout, maximum+1))
	if int64(len(data)) > maximum {
		_ = command.Process.Kill()
		_ = command.Wait()
		return nil, fmt.Errorf("crosscheck output exceeds maximum size of %d bytes", maximum)
	}
	waitErr := command.Wait()
	if readErr != nil {
		return nil, readErr
	}
	if waitErr != nil {
		return nil, waitErr
	}
	return data, nil
}

func validatePublishCorrectnessStage(request *Request) error {
	prepared, err := loadPreparedValueAudit(request.AuditSessionDir)
	if err != nil {
		return err
	}
	output, diagnostics, err := loadCorrectnessCandidates(request)
	if err != nil {
		return err
	}
	result, err := PublishCorrectness(*request, prepared, output, ghRunner)
	if err != nil {
		return err
	}
	result.Diagnostics = append(result.Diagnostics, diagnostics...)
	provenance := output.Provenance
	if provenance.CLI == "" {
		provenance = BatchProvenance{CLI: request.Crosscheck.CLI, Model: request.Crosscheck.Model, Effort: request.Crosscheck.Effort, SessionID: "unknown"}
	}
	result.JudgeCLI, result.JudgeModel, result.JudgeEffort, result.JudgeSessionID = provenance.CLI, provenance.Model, provenance.Effort, provenance.SessionID
	return stateio.WriteJSONAtomic(filepath.Join(request.AuditSessionDir, "correctness-findings.json"), result)
}

func loadCorrectnessCandidates(request *Request) (CorrectnessCandidates, []string, error) {
	path := filepath.Join(request.AuditSessionDir, "model-output", correctnessOutput)
	data, err := os.ReadFile(path) // #nosec G304 -- fixed audit-owned model output.
	if os.IsNotExist(err) {
		diagnostic, _ := os.ReadFile(filepath.Join(request.AuditSessionDir, "correctness-model-diagnostics.json"))
		message := strings.TrimSpace(string(diagnostic))
		if message == "" {
			message = "correctness model output is unavailable"
		}
		return CorrectnessCandidates{}, []string{message}, nil
	}
	if err != nil {
		return CorrectnessCandidates{}, nil, err
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var output CorrectnessCandidates
	if err := decoder.Decode(&output); err != nil {
		return CorrectnessCandidates{}, nil, fmt.Errorf("decode correctness output: %w", err)
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return CorrectnessCandidates{}, nil, fmt.Errorf("correctness output contains multiple JSON values")
	}
	return output, nil, nil
}

// PublishCorrectness validates model claims, then creates at most one issue per
// stable finding. Every outcome is returned for durable local persistence.
func PublishCorrectness(request Request, prepared PreparedValueAudit, output CorrectnessCandidates, runner CommandRunner) (CorrectnessResult, error) { //nolint:gocritic // Value boundary keeps callers from mutating the frozen request.
	current, err := fingerprintTree(request.SnapshotPath)
	if err != nil {
		return CorrectnessResult{}, err
	}
	if current != prepared.Index.Fingerprints.SnapshotBefore {
		return CorrectnessResult{}, fmt.Errorf("frozen evidence changed after preparation")
	}
	known := map[string]EvidenceReference{}
	for _, ref := range prepared.Index.References {
		if ref.Status == "available" {
			known[ref.ID] = ref
		}
	}
	result := CorrectnessResult{SchemaVersion: correctnessSchema, Findings: []Finding{}}
	grouped := map[string]CorrectnessCandidate{}
	for index := range output.Candidates {
		candidate := &output.Candidates[index]
		key := normalizeDefectKey(candidate.DefectKey)
		if !oneOf(candidate.Status, "confirmed", "inconclusive", "excluded") {
			result.Findings = append(result.Findings, findingFor(candidate, key, "rejected", "", "invalid candidate status"))
			continue
		}
		if candidate.Status != "confirmed" {
			result.Findings = append(result.Findings, findingFor(candidate, key, "retained", "", ""))
			continue
		}
		if err := validateCorrectnessCandidate(candidate, key, known, &request.RunnerSource); err != nil {
			result.Findings = append(result.Findings, findingFor(candidate, key, "rejected", "", err.Error()))
			continue
		}
		candidate.DefectKey = key
		if currentCandidate, exists := grouped[key]; exists {
			grouped[key] = mergeSymptoms(&currentCandidate, candidate)
		} else {
			grouped[key] = *candidate
		}
	}
	keys := make([]string, 0, len(grouped))
	for key := range grouped {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		candidate := grouped[key]
		index := len(result.Findings)
		result.Findings = append(result.Findings, findingFor(&candidate, key, "pending", "", ""))
		if err := persistCorrectnessOutcome(&request, &result); err != nil {
			return CorrectnessResult{}, err
		}
		finding, err := publishCandidate(&request, &candidate, runner)
		if err != nil {
			return CorrectnessResult{}, err
		}
		result.Findings[index] = finding
		if err := persistCorrectnessOutcome(&request, &result); err != nil {
			return CorrectnessResult{}, err
		}
	}
	return result, nil
}

func persistCorrectnessOutcome(request *Request, result *CorrectnessResult) error {
	if request.AuditSessionDir == "" {
		return nil
	}
	return stateio.WriteJSONAtomic(filepath.Join(request.AuditSessionDir, "correctness-findings.json"), result)
}

func validateCorrectnessCandidate(candidate *CorrectnessCandidate, key string, known map[string]EvidenceReference, source *SourceProvenance) error {
	if key == "" || candidate.DefectKey != key {
		return fmt.Errorf("defect_key is not normalized")
	}
	if candidate.Title == "" || candidate.Observed == "" || candidate.Expected == "" || candidate.Verification == "" || candidate.AffectedComponent == "" {
		return fmt.Errorf("candidate is incomplete")
	}
	if !oneOf(candidate.Confidence, "high", "medium") {
		return fmt.Errorf("candidate confidence is not publishable")
	}
	if !oneOf(candidate.SemanticDuplicate.State, "none", "open", "closed", "ambiguous") {
		return fmt.Errorf("candidate has no semantic duplicate result")
	}
	if candidate.SemanticDuplicate.State != "none" && candidate.SemanticDuplicate.URL == "" {
		return fmt.Errorf("semantic duplicate result is missing its issue URL")
	}
	if candidate.SemanticDuplicate.URL != "" && !githubIssueURL(candidate.SemanticDuplicate.URL) {
		return fmt.Errorf("semantic duplicate result has an unsafe issue URL")
	}
	if candidate.SemanticDuplicate.State != "none" && candidate.SemanticDuplicate.DefectKey != key {
		return fmt.Errorf("semantic duplicate result does not identify the candidate cause")
	}
	if len(candidate.EvidenceRefs) == 0 {
		return fmt.Errorf("candidate has no evidence references")
	}
	for _, reference := range append(append([]string{}, candidate.EvidenceRefs...), candidate.Consultations...) {
		if _, exists := known[reference]; !exists {
			return fmt.Errorf("unknown or unavailable evidence reference %q", reference)
		}
	}
	sourceComplete := source.Verified && source.Coverage == "complete" && source.SnapshotPath != "" && runnerSnapshotComplete(source.SnapshotPath)
	if !sourceComplete {
		return fmt.Errorf("authoritative Runner source is unavailable or incomplete")
	}
	for _, value := range []string{candidate.Title, candidate.Observed, candidate.Expected, candidate.Verification, candidate.AffectedComponent} {
		if !boundedText(value) {
			return fmt.Errorf("candidate content is unsafe or unbounded")
		}
	}
	if len(candidate.Symptoms) > 10 {
		return fmt.Errorf("candidate has too many related symptoms")
	}
	for _, symptom := range candidate.Symptoms {
		if !safeSymptom(symptom) {
			return fmt.Errorf("candidate symptom is unsafe or unbounded")
		}
	}
	if candidateIssueTextBytes(candidate) > 6*1024 {
		return fmt.Errorf("candidate issue content exceeds the focused-content limit")
	}
	return nil
}

func normalizeDefectKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(value, "-")
	return strings.Trim(value, "-")
}

func boundedText(value string) bool {
	return value != "" && utf8.RuneCountInString(value) <= 1200 && !strings.Contains(value, "\x00")
}

func safeSymptom(value string) bool {
	return value != "" && utf8.RuneCountInString(value) <= 280 && !strings.ContainsAny(value, "\r\n") && !strings.Contains(value, "```") && !strings.Contains(value, "\x00")
}

func candidateIssueTextBytes(candidate *CorrectnessCandidate) int {
	values := append([]string{candidate.Title, candidate.Observed, candidate.Expected, candidate.Verification, candidate.AffectedComponent}, candidate.Symptoms...)
	total := 0
	for _, value := range values {
		total += len(value)
	}
	return total
}

func githubIssueURL(value string) bool {
	return regexp.MustCompile(`^https://github\.com/Codagent-AI/agent-runner/issues/\d+$`).MatchString(value)
}

func findingFor(candidate *CorrectnessCandidate, key, state, url, failure string) Finding {
	id := stableFindingID(key)
	return Finding{FindingID: id, Candidate: *candidate, Marker: findingMarker(id), PublicationState: state, IssueURL: url, Failure: failure}
}

func stableFindingID(key string) string {
	sum := sha256.Sum256([]byte(key))
	return "finding-" + hex.EncodeToString(sum[:8])
}
func findingMarker(id string) string { return "<!-- agent-runner-audit:" + id + " -->" }
func causeMarker(key string) string  { return "<!-- agent-runner-audit-key:" + key + " -->" }

func mergeSymptoms(left, right *CorrectnessCandidate) CorrectnessCandidate {
	merged := *left
	merged.Symptoms = append(merged.Symptoms, right.Symptoms...)
	merged.Symptoms = append(merged.Symptoms, right.Observed)
	merged.Symptoms = uniqueStrings(merged.Symptoms)
	return merged
}
func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

type ghIssue struct {
	URL   string `json:"url"`
	State string `json:"state"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

func publishCandidate(request *Request, candidate *CorrectnessCandidate, runner CommandRunner) (Finding, error) {
	finding := findingFor(candidate, candidate.DefectKey, "pending", "", "")
	semantic, err := searchIssues(runner, candidate.DefectKey, "open")
	if err != nil {
		finding.PublicationState, finding.Failure = "failed", err.Error()
		return finding, nil
	}
	markerMatches, err := searchIssues(runner, finding.Marker, "all")
	if err != nil {
		finding.PublicationState, finding.Failure = "failed", err.Error()
		return finding, nil
	}
	for _, issue := range markerMatches {
		if strings.Contains(issue.Body, finding.Marker) {
			finding.PublicationState, finding.IssueURL = "created", issue.URL
			return finding, nil
		}
	}
	for _, issue := range semantic {
		if (issue.State == "OPEN" || strings.EqualFold(issue.State, "open")) && issueMatchesCause(issue, candidate.DefectKey) {
			finding.PublicationState, finding.DuplicateURL = "duplicate", issue.URL
			return finding, nil
		}
	}
	if candidate.SemanticDuplicate.State == "open" || candidate.SemanticDuplicate.State == "closed" {
		// The model owns semantic same-cause judgment and its duplicate result is
		// already validated against the candidate's normalized defect key. The
		// publisher independently verifies the selected issue's repository, URL,
		// and current state through gh; requiring an audit marker here would make
		// genuine manually filed duplicates impossible to link.
		issue, err := verifySelectedDuplicate(runner, candidate.SemanticDuplicate)
		if err != nil {
			finding.PublicationState, finding.Failure = "ambiguous", err.Error()
			return finding, nil
		}
		if candidate.SemanticDuplicate.State == "open" {
			finding.PublicationState, finding.DuplicateURL = "duplicate", issue.URL
			return finding, nil
		}
		finding.PriorClosedIssue = issue.URL
	}
	if candidate.SemanticDuplicate.State == "ambiguous" {
		finding.PublicationState, finding.Failure = "ambiguous", "semantic duplicate result requires review before issue creation"
		return finding, nil
	}
	body, redacted := issueBody(request, &finding, candidate)
	title := redactText(candidate.Title)
	finding.Redacted = redacted || title != candidate.Title
	url, err := runner.Run(context.Background(), "gh", []string{"issue", "create", "--repo", auditIssueRepository, "--title", "[auto-audit] " + title, "--body", "-"}, []byte(body))
	if err != nil {
		finding.PublicationState, finding.Failure = "failed", strings.TrimSpace(url)
		if finding.Failure == "" {
			finding.Failure = err.Error()
		}
		return finding, nil
	}
	finding.IssueURL = strings.TrimSpace(url)
	if !githubIssueURL(finding.IssueURL) {
		finding.PublicationState, finding.Failure = "failed", "GitHub CLI returned an unexpected issue URL"
		finding.IssueURL = ""
		return finding, nil
	}
	finding.PublicationState = "created"
	return finding, nil
}

func verifySelectedDuplicate(runner CommandRunner, duplicate Duplicate) (ghIssue, error) {
	issue, err := viewIssue(runner, duplicate.URL)
	if err != nil {
		return ghIssue{}, err
	}
	if !strings.EqualFold(issue.State, duplicate.State) {
		return ghIssue{}, fmt.Errorf("semantic duplicate could not be independently verified")
	}
	return issue, nil
}

func searchIssues(runner CommandRunner, query, state string) ([]ghIssue, error) {
	output, err := runner.Run(context.Background(), "gh", []string{"issue", "list", "--repo", auditIssueRepository, "--state", state, "--search", query, "--json", "url,state,title,body", "--limit", "20"}, nil)
	if err != nil {
		return nil, fmt.Errorf("search GitHub issues: %w: %s", err, strings.TrimSpace(output))
	}
	var issues []ghIssue
	if err := json.Unmarshal([]byte(output), &issues); err != nil {
		return nil, fmt.Errorf("decode GitHub issue search: %w", err)
	}
	return issues, nil
}

func viewIssue(runner CommandRunner, issueURL string) (ghIssue, error) {
	match := regexp.MustCompile(`/issues/(\d+)$`).FindStringSubmatch(issueURL)
	if len(match) != 2 {
		return ghIssue{}, fmt.Errorf("semantic duplicate URL is invalid")
	}
	output, err := runner.Run(context.Background(), "gh", []string{"issue", "view", match[1], "--repo", auditIssueRepository, "--json", "url,state,title,body"}, nil)
	if err != nil {
		return ghIssue{}, fmt.Errorf("view GitHub issue: %w: %s", err, strings.TrimSpace(output))
	}
	var issue ghIssue
	if err := json.Unmarshal([]byte(output), &issue); err != nil {
		return ghIssue{}, fmt.Errorf("decode GitHub issue view: %w", err)
	}
	if issue.URL != issueURL || !githubIssueURL(issue.URL) {
		return ghIssue{}, fmt.Errorf("GitHub issue view returned an unexpected URL")
	}
	return issue, nil
}

func issueMatchesCause(issue ghIssue, key string) bool {
	return strings.Contains(issue.Body, causeMarker(key))
}

func issueBody(request *Request, finding *Finding, candidate *CorrectnessCandidate) (string, bool) {
	redacted := false
	redact := func(value string) string {
		output := redactText(value)
		redacted = redacted || output != value
		return output
	}
	parts := []string{
		"## Observed behavior\n" + redact(candidate.Observed),
		"## Expected behavior\n" + redact(candidate.Expected),
		"## Affected component\n" + redact(candidate.AffectedComponent),
		"## Affected run\nSource run: " + redact(request.SourceRunID) + "\nExecution session: " + redact(request.ExecutionSessionID),
		"## Verification\n" + redact(candidate.Verification),
	}
	if len(candidate.EvidenceRefs) > 0 {
		parts = append(parts, "Evidence references: "+strings.Join(candidate.EvidenceRefs, ", "))
	}
	parts = append(parts, causeMarker(candidate.DefectKey))
	if len(candidate.Symptoms) > 0 {
		symptoms := make([]string, len(candidate.Symptoms))
		for index, symptom := range candidate.Symptoms {
			symptoms[index] = redact(symptom)
		}
		parts = append(parts, "## Related symptoms\n- "+strings.Join(symptoms, "\n- "))
	}
	if finding.PriorClosedIssue != "" {
		parts = append(parts, "Prior closed history: "+finding.PriorClosedIssue)
	}
	parts = append(parts, finding.Marker)
	body := strings.Join(parts, "\n\n")
	return body, redacted
}

var (
	privateURLPattern = regexp.MustCompile(`(?i)https?://\S+`)
	pathPattern       = regexp.MustCompile(`(?:^|\s)(?:/Users/|/home/|[A-Za-z]:\\)\S+`)
	secretPattern     = regexp.MustCompile(`(?i)(ghp_[A-Za-z0-9_]+|sk-[A-Za-z0-9_-]+|(?:token|password|secret|api[_-]?key)\s*[=:]\s*[^\s]+)`)
)

func redactText(value string) string {
	value = privateURLPattern.ReplaceAllString(value, "[private URL]")
	value = pathPattern.ReplaceAllString(value, " [local path]")
	return secretPattern.ReplaceAllString(value, "[redacted]")
}

// DestinationResolver exposes only the frozen non-secret Sheets identity.
type DestinationResolver interface{ ResolveDestination() DestinationState }
type DestinationState struct {
	State         string `json:"state"`
	SpreadsheetID string `json:"spreadsheet_id,omitempty"`
	Tab           string `json:"tab,omitempty"`
	Diagnostic    string `json:"diagnostic,omitempty"`
}
type connectionDestinationResolver struct{}

func (connectionDestinationResolver) ResolveDestination() DestinationState {
	connection, err := (ConnectionStore{}).Read()
	if os.IsNotExist(err) {
		return DestinationState{State: "unavailable", Diagnostic: "reporting destination is not configured"}
	}
	if err != nil {
		return DestinationState{State: "unusable", Diagnostic: "reporting destination is unusable"}
	}
	return DestinationState{State: "configured", SpreadsheetID: connection.SpreadsheetID, Tab: connection.Tab}
}

var destinationResolver DestinationResolver = connectionDestinationResolver{}

type LocalReport struct {
	SchemaVersion           string                    `json:"schema_version"`
	AuditRunID              string                    `json:"audit_run_id"`
	SourceRunID             string                    `json:"source_run_id"`
	ExecutionSessionID      string                    `json:"execution_session_id"`
	Trigger                 string                    `json:"trigger"`
	Evidence                EvidenceIndex             `json:"evidence"`
	Values                  ValueValidationResult     `json:"values"`
	ValueConsultations      []consultationLedgerEntry `json:"value_consultations"`
	Correctness             CorrectnessResult         `json:"correctness"`
	CorrectnessConsultation []correctnessConsultation `json:"correctness_consultations"`
	Destination             DestinationState          `json:"destination"`
	DeliveryState           string                    `json:"delivery_state"`
	RunnerSource            SourceProvenance          `json:"runner_source"`
}

type correctnessConsultation struct {
	FindingID   string `json:"finding_id"`
	ReferenceID string `json:"reference_id"`
	Category    string `json:"category"`
}

func assembleLocalReportStage(request *Request) error {
	prepared, err := loadPreparedValueAudit(request.AuditSessionDir)
	if err != nil {
		return err
	}
	values := ValueValidationResult{SchemaVersion: evidenceSchemaVersion, Diagnostics: []string{"value output is unavailable"}}
	if data, err := os.ReadFile(filepath.Join(request.AuditSessionDir, "value-observations.json")); err == nil {
		if err := json.Unmarshal(data, &values); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	correctness := CorrectnessResult{SchemaVersion: correctnessSchema, Diagnostics: []string{"correctness output is unavailable"}}
	if data, err := os.ReadFile(filepath.Join(request.AuditSessionDir, "correctness-findings.json")); err == nil {
		if err := json.Unmarshal(data, &correctness); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	valueConsultations := []consultationLedgerEntry{}
	if data, err := os.ReadFile(filepath.Join(request.AuditSessionDir, "value-consultations.json")); err == nil {
		if err := json.Unmarshal(data, &valueConsultations); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	report := LocalReport{SchemaVersion: valueSchemaVersion, AuditRunID: request.AuditRunID, SourceRunID: request.SourceRunID, ExecutionSessionID: request.ExecutionSessionID, Trigger: request.Trigger, Evidence: prepared.Index, Values: values, ValueConsultations: valueConsultations, Correctness: correctness, CorrectnessConsultation: correctnessConsultationLedger(correctness.Findings, prepared.Index.References), Destination: destinationResolver.ResolveDestination(), DeliveryState: "pending", RunnerSource: request.RunnerSource}
	return stateio.WriteJSONAtomic(filepath.Join(request.AuditSessionDir, "local-report.json"), report)
}

func correctnessConsultationLedger(findings []Finding, references []EvidenceReference) []correctnessConsultation {
	categories := map[string]string{}
	for _, reference := range references {
		categories[reference.ID] = reference.Category
	}
	entries := []correctnessConsultation{}
	for index := range findings {
		finding := &findings[index]
		for _, reference := range finding.Candidate.Consultations {
			entries = append(entries, correctnessConsultation{FindingID: finding.FindingID, ReferenceID: reference, Category: categories[reference]})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].FindingID == entries[j].FindingID {
			return entries[i].ReferenceID < entries[j].ReferenceID
		}
		return entries[i].FindingID < entries[j].FindingID
	})
	return entries
}
