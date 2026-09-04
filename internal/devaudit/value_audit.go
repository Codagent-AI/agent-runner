//go:build dev_audit

package devaudit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/cli"
	"github.com/codagent/agent-runner/internal/metrics"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/stateio"
)

const (
	evidenceSchemaVersion  = 1
	valueSchemaVersion     = "step_value_v1"
	rubricVersion          = "value-rubric-v1"
	defaultPackageBytes    = 256 * 1024
	defaultLeafDetailBytes = 32 * 1024
	auditSandboxProfile    = "(version 1)\n(deny file-write*)\n(allow file-write* (subpath (param \"OUTPUT_DIR\")))\n"
)

var crosscheckCommand = sandboxedCrosscheckCommand

// Fingerprints bind each value result to its frozen intake and separately
// writable model-output boundary.
type Fingerprints struct {
	SnapshotBefore string `json:"snapshot_before"`
	SnapshotAfter  string `json:"snapshot_after,omitempty"`
	OutputBefore   string `json:"output_before"`
	OutputAfter    string `json:"output_after,omitempty"`
}

type EvidenceReference struct {
	ID                       string `json:"id"`
	Category                 string `json:"category"`
	Status                   string `json:"status"`
	ProducerExecutionSession string `json:"producer_execution_session"`
	Lineage                  string `json:"lineage"`
	Detail                   string `json:"detail,omitempty"`
	Truncated                bool   `json:"truncated,omitempty"`
	LocalPath                string `json:"-"`
}

type GitEvidence struct {
	Attribution  string   `json:"attribution"`
	CommitSHAs   []string `json:"commit_shas,omitempty"`
	DeferredSHAs []string `json:"deferred_commit_shas,omitempty"`
	FilesChanged *int64   `json:"files_changed"`
	LinesAdded   *int64   `json:"lines_added"`
	LinesDeleted *int64   `json:"lines_deleted"`
	Reason       string   `json:"reason,omitempty"`
	ChangedPaths []string `json:"changed_paths,omitempty"`
}

type CostEvidence struct {
	DurationMS   *int64   `json:"duration_ms"`
	CostUSD      *float64 `json:"cost_usd"`
	TotalTokens  *int64   `json:"total_tokens"`
	SourceModels []string `json:"source_models"`
}

// ObservationSkeleton contains only deterministic Runner measurements. It is
// never decoded from model output.
type ObservationSkeleton struct {
	SchemaVersion      string       `json:"schema_version"`
	ObservationID      string       `json:"observation_id"`
	ObservedAtUTC      string       `json:"observed_at_utc"`
	Project            string       `json:"project"`
	Workflow           string       `json:"workflow"`
	SourceRunID        string       `json:"source_run_id"`
	ExecutionSessionID string       `json:"execution_session_id"`
	AuditRunID         string       `json:"audit_run_id"`
	Trigger            string       `json:"trigger"`
	SourceOutcome      string       `json:"source_outcome"`
	StepID             string       `json:"step_id"`
	Lineage            string       `json:"lineage"`
	StepOutcome        string       `json:"step_outcome"`
	Cost               CostEvidence `json:"cost"`
	Git                GitEvidence  `json:"git"`
}

type LeafEvidence struct {
	Skeleton          ObservationSkeleton `json:"skeleton"`
	Attempts          int                 `json:"attempts"`
	Iterations        int                 `json:"iterations"`
	Evidence          []EvidenceReference `json:"evidence"`
	OmittedCategories []string            `json:"omitted_categories"`
}

type EvidenceIndex struct {
	SchemaVersion      int                 `json:"schema_version"`
	SourceRunID        string              `json:"source_run_id"`
	ExecutionSessionID string              `json:"execution_session_id"`
	AuditRunID         string              `json:"audit_run_id"`
	SnapshotPath       string              `json:"snapshot_path"`
	Leaves             []LeafEvidence      `json:"leaves"`
	References         []EvidenceReference `json:"references"`
	Fingerprints       Fingerprints        `json:"fingerprints"`
}

type ValuePackage struct {
	SchemaVersion int            `json:"schema_version"`
	BatchID       string         `json:"batch_id"`
	Leaves        []LeafEvidence `json:"leaves"`
}

type PreparedValueAudit struct {
	Index    EvidenceIndex  `json:"index"`
	Packages []ValuePackage `json:"packages"`
}

type preparedArtifactsFingerprint struct {
	Value string `json:"value"`
}

// PrepareEvidence projects only the selected session from a sealed snapshot.
// It does not read the live source run.
func PrepareEvidence(request Request) (PreparedValueAudit, error) { //nolint:gocritic // Public stage boundary keeps the frozen request immutable.
	if request.SnapshotPath == "" || request.AuditSessionDir == "" || request.ExecutionSessionID == "" {
		return PreparedValueAudit{}, fmt.Errorf("prepare evidence: incomplete audit request")
	}
	before, err := fingerprintTree(request.SnapshotPath)
	if err != nil {
		return PreparedValueAudit{}, fmt.Errorf("fingerprint snapshot: %w", err)
	}
	outputDir := filepath.Join(request.AuditSessionDir, "model-output")
	if err := os.MkdirAll(outputDir, 0o700); err != nil {
		return PreparedValueAudit{}, err
	}
	outputBefore, err := fingerprintTree(outputDir)
	if err != nil {
		return PreparedValueAudit{}, err
	}
	artifact, err := readMetrics(filepath.Join(request.SnapshotPath, metrics.FileName))
	if err != nil {
		return PreparedValueAudit{}, err
	}
	references, err := discoverEvidence(request.SnapshotPath)
	if err != nil {
		return PreparedValueAudit{}, err
	}
	if request.Trigger == "replay" {
		for _, category := range []string{"audit_log", "validation", "artifact", "narrative", "native_session"} {
			if hasEvidenceCategoryReference(references, category) {
				continue
			}
			references = append(references, EvidenceReference{
				ID:                       "replay-" + category + "-unavailable",
				Category:                 category,
				Status:                   "unavailable",
				ProducerExecutionSession: request.ExecutionSessionID,
				Lineage:                  "unavailable",
				Detail:                   "durable execution-session ownership is unavailable",
			})
		}
		sort.Slice(references, func(i, j int) bool { return references[i].ID < references[j].ID })
	}
	index := EvidenceIndex{
		SchemaVersion: evidenceSchemaVersion, SourceRunID: request.SourceRunID,
		ExecutionSessionID: request.ExecutionSessionID, AuditRunID: request.AuditRunID,
		SnapshotPath: request.SnapshotPath, References: references,
		Fingerprints: Fingerprints{SnapshotBefore: before, OutputBefore: outputBefore},
	}
	commits := readGitEvidence(filepath.Join(request.SnapshotPath, "git-evidence.json"))
	index.Leaves = buildLeaves(&request, &artifact, references, commits)
	packages, err := buildValuePackages(index.Leaves)
	if err != nil {
		return PreparedValueAudit{}, err
	}
	prepared := PreparedValueAudit{Index: index, Packages: packages}
	if err := persistPrepared(&request, &prepared); err != nil {
		return PreparedValueAudit{}, err
	}
	if err := writePreparedFingerprint(request.AuditSessionDir); err != nil {
		return PreparedValueAudit{}, err
	}
	return prepared, nil
}

func writePreparedFingerprint(auditSessionDir string) error {
	value, err := preparedFingerprint(auditSessionDir)
	if err != nil {
		return err
	}
	return stateio.WriteJSONAtomic(filepath.Join(auditSessionDir, "prepared-fingerprint.json"), preparedArtifactsFingerprint{Value: value})
}

func verifyPreparedFingerprint(auditSessionDir string) error {
	data, err := os.ReadFile(filepath.Join(auditSessionDir, "prepared-fingerprint.json")) // #nosec G304 -- fixed audit artifact.
	if err != nil {
		return err
	}
	var recorded preparedArtifactsFingerprint
	if err := json.Unmarshal(data, &recorded); err != nil {
		return err
	}
	actual, err := preparedFingerprint(auditSessionDir)
	if err != nil {
		return err
	}
	if recorded.Value != actual {
		return fmt.Errorf("prepared audit artifacts changed after preparation")
	}
	return nil
}

func preparedFingerprint(auditSessionDir string) (string, error) {
	hash := sha256.New()
	for _, name := range []string{"evidence-index.json", "value-packages.json", "evidence-reference-manifest.json", "source-provenance.json"} {
		data, err := os.ReadFile(filepath.Join(auditSessionDir, name)) // #nosec G304 -- fixed audit artifact names.
		if err != nil {
			return "", err
		}
		_, _ = hash.Write([]byte(name + "\x00"))
		_, _ = hash.Write(data)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func readMetrics(path string) (metrics.Artifact, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- fixed file below immutable snapshot.
	if err != nil {
		return metrics.Artifact{}, fmt.Errorf("read snapshotted metrics: %w", err)
	}
	var artifact metrics.Artifact
	if err := json.Unmarshal(data, &artifact); err != nil {
		return metrics.Artifact{}, fmt.Errorf("decode snapshotted metrics: %w", err)
	}
	return artifact, nil
}

func buildLeaves(request *Request, artifact *metrics.Artifact, refs []EvidenceReference, commits map[string]snapshottedGitCommit) []LeafEvidence {
	groups := map[string][]metrics.StepRecord{}
	for index := range artifact.Steps {
		record := &artifact.Steps[index]
		if !valueLeaf(record) || record.ExecutionSessionID != request.ExecutionSessionID {
			continue
		}
		key := logicalPath(record.Prefix, record.ID)
		groups[key] = append(groups[key], *record)
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	leaves := make([]LeafEvidence, 0, len(keys))
	for _, key := range keys {
		records := groups[key]
		sort.SliceStable(records, func(i, j int) bool { return records[i].RecordID < records[j].RecordID })
		leaves = append(leaves, leafFromRecords(request, artifact, key, records, refs, commits))
	}
	applyDeferredCommitAttribution(leaves)
	return leaves
}

func valueLeaf(record *metrics.StepRecord) bool {
	if record.Kind != "step" || record.ID == "" {
		return false
	}
	switch record.Type {
	case "group", "loop", "dispatch", "sub-workflow":
		return false
	}
	return true
}

func logicalPath(prefix, id string) string {
	prefix = strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(prefix), "["), "]")
	if prefix == "" {
		return id
	}
	parts := strings.Split(prefix, ",")
	for index := range parts {
		parts[index] = strings.TrimSpace(parts[index])
	}
	if parts[len(parts)-1] == id {
		return strings.Join(parts, "/")
	}
	return strings.Join(append(parts, id), "/")
}

func leafFromRecords(request *Request, artifact *metrics.Artifact, path string, records []metrics.StepRecord, refs []EvidenceReference, commits map[string]snapshottedGitCommit) LeafEvidence {
	var duration, tokens int64
	var durationKnown bool
	tokensKnown, costKnown := true, true
	var cost float64
	models := map[string]struct{}{}
	outcome := "unknown"
	iterations := 0
	for index := range records {
		record := &records[index]
		if record.DurationMS > 0 {
			duration += record.DurationMS
			durationKnown = true
		}
		if record.Iteration != nil {
			iterations++
		}
		if record.Outcome != "" {
			outcome = record.Outcome
		}
		if record.Usage == nil || record.Usage.Status != model.UsageCollected || record.Usage.TokenTotals == nil {
			tokensKnown = false
		} else {
			tokens += record.Usage.TokenTotals.Total
			if record.Usage.Model != "" {
				models[record.Usage.Model] = struct{}{}
			}
		}
		if record.EstimatedAPICostUSD == nil {
			costKnown = false
		} else {
			cost += *record.EstimatedAPICostUSD
		}
	}
	var durationPtr *int64
	if durationKnown {
		durationPtr = &duration
	}
	var tokensPtr *int64
	if tokensKnown {
		tokensPtr = &tokens
	}
	var costPtr *float64
	if costKnown {
		costPtr = &cost
	}
	lineage := "new"
	for index := range artifact.Steps {
		record := &artifact.Steps[index]
		if valueLeaf(record) && record.ExecutionSessionID != request.ExecutionSessionID && logicalPath(record.Prefix, record.ID) == path {
			lineage = "overlap"
			break
		}
	}
	observedAt := ""
	for _, session := range artifact.Sessions {
		if session.ExecutionSessionID == request.ExecutionSessionID {
			observedAt = session.EndedAt
			if observedAt == "" {
				observedAt = session.LastObservedAt
			}
			break
		}
	}
	skeleton := ObservationSkeleton{
		SchemaVersion: valueSchemaVersion, ObservationID: observationID(request.AuditRunID, request.ExecutionSessionID, path), ObservedAtUTC: observedAt,
		Project: sanitizeProject(request.Project), Workflow: request.SourceWorkflow, SourceRunID: request.SourceRunID, ExecutionSessionID: request.ExecutionSessionID, AuditRunID: request.AuditRunID,
		Trigger: request.Trigger, StepID: path, Lineage: lineage, StepOutcome: outcome,
		SourceOutcome: selectedSessionOutcome(artifact, request.ExecutionSessionID),
		Cost:          CostEvidence{DurationMS: durationPtr, CostUSD: costPtr, TotalTokens: tokensPtr, SourceModels: sortedKeys(models)}, Git: aggregateGit(records, commits),
	}
	leafRefs := referencesForLeaf(refs, records, request.ExecutionSessionID)
	gitDetail, _ := json.Marshal(skeleton.Git)
	leafRefs = append(leafRefs, EvidenceReference{ID: "git-" + skeleton.ObservationID, Category: "git", Status: "available", ProducerExecutionSession: request.ExecutionSessionID, Lineage: skeleton.Lineage, Detail: string(gitDetail)})
	if len(skeleton.Git.CommitSHAs) > 0 {
		summaries := []snapshottedGitCommit{}
		for _, sha := range skeleton.Git.CommitSHAs {
			summaries = append(summaries, commits[sha])
		}
		detail, _ := json.Marshal(summaries)
		leafRefs = append(leafRefs, EvidenceReference{ID: "commit-summary-" + skeleton.ObservationID, Category: "commit_summary", Status: "available", ProducerExecutionSession: request.ExecutionSessionID, Lineage: skeleton.Lineage, Detail: string(detail)})
	}
	sort.Slice(leafRefs, func(i, j int) bool {
		return evidencePriority(leafRefs[i].Category) < evidencePriority(leafRefs[j].Category) || (evidencePriority(leafRefs[i].Category) == evidencePriority(leafRefs[j].Category) && leafRefs[i].ID < leafRefs[j].ID)
	})
	omitted := missingEvidenceCategories(leafRefs)
	return LeafEvidence{Skeleton: skeleton, Attempts: len(records), Iterations: iterations, Evidence: leafRefs, OmittedCategories: omitted}
}

func referencesForLeaf(refs []EvidenceReference, records []metrics.StepRecord, executionSessionID string) []EvidenceReference {
	result := []EvidenceReference{}
	metricsDetail, _ := json.Marshal(records)
	result = append(result, EvidenceReference{ID: "metrics-" + records[0].RecordID, Category: "metrics", Status: "available", ProducerExecutionSession: executionSessionID, Lineage: "new", Detail: string(metricsDetail)})
	for _, ref := range refs {
		for index := range records {
			record := &records[index]
			if (ref.Category == "validation" || ref.Category == "artifact" || ref.Category == "narrative") && strings.Contains(strings.ToLower(ref.LocalPath), strings.ToLower(record.ID)) {
				result = append(result, ref)
				break
			}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return evidencePriority(result[i].Category) < evidencePriority(result[j].Category) || (evidencePriority(result[i].Category) == evidencePriority(result[j].Category) && result[i].ID < result[j].ID)
	})
	return result
}

func missingEvidenceCategories(refs []EvidenceReference) []string {
	required := []string{"metrics", "git", "commit_summary", "validation", "artifact", "narrative", "native_session"}
	seen := map[string]bool{}
	for _, ref := range refs {
		if ref.Status == "available" {
			seen[ref.Category] = true
		}
	}
	missing := []string{}
	for _, category := range required {
		if !seen[category] {
			missing = append(missing, category+"_unavailable")
		}
	}
	return missing
}

func selectedSessionOutcome(artifact *metrics.Artifact, executionSessionID string) string {
	seen := false
	for index := range artifact.Steps {
		record := &artifact.Steps[index]
		if record.ExecutionSessionID != executionSessionID {
			continue
		}
		seen = true
		if record.Outcome == "failed" || record.Outcome == "aborted" {
			return record.Outcome
		}
	}
	if seen {
		return "success"
	}
	return "unknown"
}

func sortedKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func observationID(auditID, executionID, path string) string {
	sum := sha256.Sum256([]byte(auditID + "\x00" + executionID + "\x00" + path))
	return hex.EncodeToString(sum[:])
}

func aggregateGit(records []metrics.StepRecord, commits map[string]snapshottedGitCommit) GitEvidence {
	result := GitEvidence{Attribution: "no_change", CommitSHAs: []string{}, DeferredSHAs: []string{}}
	var files, added, deleted int64
	for index := range records {
		record := &records[index]
		if record.GitChanges == nil || !record.GitChanges.Available {
			return GitEvidence{Attribution: "unavailable", CommitSHAs: []string{}, DeferredSHAs: []string{}, Reason: "Git checkpoint evidence unavailable"}
		}
		files += record.GitChanges.FilesChanged
		added += record.GitChanges.LinesAdded
		deleted += record.GitChanges.LinesDeleted
		if record.GitEnd != nil && record.GitStart != nil && record.GitEnd.HEAD != record.GitStart.HEAD {
			if commits == nil {
				return GitEvidence{Attribution: "unavailable", CommitSHAs: []string{}, DeferredSHAs: []string{}, Reason: "Git commit metadata is unavailable"}
			}
			attributed := []string{}
			var commitFiles, commitAdded, commitDeleted int64
			paths := []string{}
			for _, sha := range record.GitEnd.Commits {
				commit, exists := commits[sha]
				if !exists || !strings.HasPrefix(commit.Subject, "["+record.ID+"]") {
					continue
				}
				attributed = append(attributed, sha)
				commitFiles += commit.FilesChanged
				commitAdded += commit.LinesAdded
				commitDeleted += commit.LinesDeleted
				paths = append(paths, commit.Paths...)
			}
			if len(attributed) == 0 {
				return GitEvidence{Attribution: "ambiguous", CommitSHAs: []string{}, DeferredSHAs: []string{}, Reason: "no boundary commit has this step's exact prefix"}
			}
			sort.Strings(attributed)
			sort.Strings(paths)
			return GitEvidence{Attribution: "attributed", CommitSHAs: attributed, DeferredSHAs: []string{}, FilesChanged: &commitFiles, LinesAdded: &commitAdded, LinesDeleted: &commitDeleted, ChangedPaths: paths}
		}
	}
	if files != 0 || added != 0 || deleted != 0 {
		result.Attribution = "working_tree"
		result.FilesChanged, result.LinesAdded, result.LinesDeleted = &files, &added, &deleted
		result.ChangedPaths = dirtyChangedPaths(records)
	} else {
		zero := int64(0)
		result.FilesChanged, result.LinesAdded, result.LinesDeleted = &zero, &zero, &zero
	}
	return result
}

func dirtyChangedPaths(records []metrics.StepRecord) []string {
	paths := map[string]struct{}{}
	for index := range records {
		record := &records[index]
		if record.GitEnd == nil {
			continue
		}
		for _, stats := range [][]audit.GitFileStat{record.GitEnd.Index, record.GitEnd.Worktree, record.GitEnd.Untracked} {
			for _, stat := range stats {
				paths[stat.Path] = struct{}{}
			}
		}
	}
	result := make([]string, 0, len(paths))
	for path := range paths {
		result = append(result, path)
	}
	sort.Strings(result)
	return result
}

func readGitEvidence(path string) map[string]snapshottedGitCommit {
	data, err := os.ReadFile(path) // #nosec G304 -- fixed exported evidence under immutable snapshot.
	if err != nil {
		return nil
	}
	var evidence snapshottedGitEvidence
	if json.Unmarshal(data, &evidence) != nil || !evidence.Available {
		return nil
	}
	commits := make(map[string]snapshottedGitCommit, len(evidence.Commits))
	for _, commit := range evidence.Commits {
		commits[commit.SHA] = commit
	}
	return commits
}

func applyDeferredCommitAttribution(leaves []LeafEvidence) {
	for index := range leaves {
		if leaves[index].Skeleton.Git.Attribution != "attributed" {
			continue
		}
		for prior := 0; prior < index; prior++ {
			if leaves[prior].Skeleton.Git.Attribution != "working_tree" || leaves[prior].Skeleton.Git.FilesChanged == nil || *leaves[prior].Skeleton.Git.FilesChanged == 0 {
				continue
			}
			if !hasPathOverlap(leaves[prior].Skeleton.Git.ChangedPaths, leaves[index].Skeleton.Git.ChangedPaths) {
				continue
			}
			leaves[prior].Skeleton.Git.Attribution = "deferred_commit"
			leaves[prior].Skeleton.Git.DeferredSHAs = append(leaves[prior].Skeleton.Git.DeferredSHAs, leaves[index].Skeleton.Git.CommitSHAs...)
			leaves[index].Skeleton.Git.Attribution = "no_change"
			zero := int64(0)
			leaves[index].Skeleton.Git.FilesChanged, leaves[index].Skeleton.Git.LinesAdded, leaves[index].Skeleton.Git.LinesDeleted = &zero, &zero, &zero
			break
		}
	}
}

func hasPathOverlap(left, right []string) bool {
	for _, a := range left {
		for _, b := range right {
			if a == b {
				return true
			}
		}
	}
	return false
}

func discoverEvidence(root string) ([]EvidenceReference, error) {
	refs := []EvidenceReference{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if rel == "runner-source" {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		category := evidenceCategory(rel)
		if category == "" {
			return nil
		}
		sum := sha256.Sum256([]byte(rel))
		detail, truncated := evidenceDetail(path, category)
		refs = append(refs, EvidenceReference{ID: category + "-" + hex.EncodeToString(sum[:8]), Category: category, Status: "available", ProducerExecutionSession: "unknown", Lineage: "unknown", Detail: detail, Truncated: truncated, LocalPath: rel})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].ID < refs[j].ID })
	if !hasEvidenceCategory(refs, "native_session") {
		refs = append(refs, EvidenceReference{ID: "native_session-unavailable", Category: "native_session", Status: "unavailable", ProducerExecutionSession: "unknown", Lineage: "unavailable"})
	}
	return refs, nil
}

func evidenceDetail(path, category string) (string, bool) {
	if category != "validation" && category != "artifact" && category != "narrative" {
		return "", false
	}
	file, err := os.Open(path) // #nosec G304 -- enumerated immutable snapshot file.
	if err != nil {
		return "", false
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil {
		return "", false
	}
	return string(data[:min(len(data), 4096)]), len(data) > 4096
}

func evidencePriority(category string) int {
	switch category {
	case "metrics":
		return 2
	case "git":
		return 3
	case "commit_summary":
		return 4
	case "validation":
		return 5
	case "artifact":
		return 6
	case "narrative":
		return 7
	default:
		return 8
	}
}

func hasEvidenceCategory(refs []EvidenceReference, category string) bool {
	for _, ref := range refs {
		if ref.Category == category && ref.Status == "available" {
			return true
		}
	}
	return false
}

func hasEvidenceCategoryReference(refs []EvidenceReference, category string) bool {
	for _, ref := range refs {
		if ref.Category == category {
			return true
		}
	}
	return false
}

func evidenceCategory(rel string) string {
	base := strings.ToLower(filepath.Base(rel))
	switch {
	case base == "audit.log":
		return "audit_log"
	case base == metrics.FileName:
		return "metrics"
	case strings.Contains(base, "validation") || strings.Contains(base, "validator"):
		return "validation"
	case strings.Contains(base, "session"):
		return "native_session"
	case strings.Contains(rel, "output") || strings.Contains(base, "output"):
		return "narrative"
	case strings.Contains(rel, "artifact") || strings.Contains(base, "artifact"):
		return "artifact"
	default:
		return ""
	}
}

func buildValuePackages(leaves []LeafEvidence) ([]ValuePackage, error) {
	packages := []ValuePackage{}
	current := ValuePackage{SchemaVersion: evidenceSchemaVersion, BatchID: "value-001", Leaves: []LeafEvidence{}}
	for index := range leaves {
		leaf := &leaves[index]
		bounded := boundLeafDetail(leaf)
		candidate := current
		candidate.Leaves = append(candidate.Leaves, bounded)
		if encodedJSONBytes(candidate) <= defaultPackageBytes {
			current = candidate
			continue
		}
		if len(current.Leaves) == 0 {
			return nil, fmt.Errorf("compact facts for leaf %q exceed %d byte package limit", leaf.Skeleton.StepID, defaultPackageBytes)
		}
		packages = append(packages, current)
		current = ValuePackage{SchemaVersion: evidenceSchemaVersion, BatchID: fmt.Sprintf("value-%03d", len(packages)+1), Leaves: []LeafEvidence{bounded}}
		if encodedJSONBytes(current) > defaultPackageBytes {
			return nil, fmt.Errorf("compact facts for leaf %q exceed %d byte package limit", leaf.Skeleton.StepID, defaultPackageBytes)
		}
	}
	if len(current.Leaves) > 0 {
		packages = append(packages, current)
	}
	return packages, nil
}

func boundLeafDetail(leaf *LeafEvidence) LeafEvidence {
	result := *leaf
	result.Evidence = append([]EvidenceReference(nil), leaf.Evidence...)
	for len(result.Evidence) > 0 && encodedJSONBytes(result) > defaultLeafDetailBytes {
		result.Evidence = result.Evidence[:len(result.Evidence)-1]
		result.OmittedCategories = append(result.OmittedCategories, "truncated_default_evidence")
	}
	sort.Strings(result.OmittedCategories)
	return result
}

func encodedJSONBytes(value any) int {
	data, _ := json.Marshal(value)
	return len(data)
}

func persistPrepared(request *Request, prepared *PreparedValueAudit) error {
	if err := stateio.WriteJSONAtomic(filepath.Join(request.AuditSessionDir, "evidence-index.json"), prepared.Index); err != nil {
		return err
	}
	if err := stateio.WriteJSONAtomic(filepath.Join(request.AuditSessionDir, "value-packages.json"), prepared.Packages); err != nil {
		return err
	}
	manifest := map[string]string{}
	for _, ref := range prepared.Index.References {
		if ref.LocalPath != "" {
			manifest[ref.ID] = filepath.Join(request.SnapshotPath, ref.LocalPath)
		}
	}
	if err := stateio.WriteJSONAtomic(filepath.Join(request.AuditSessionDir, "evidence-reference-manifest.json"), manifest); err != nil {
		return err
	}
	return stateio.WriteJSONAtomic(filepath.Join(request.AuditSessionDir, "source-provenance.json"), request.RunnerSource)
}

func fingerprintTree(root string) (string, error) {
	hash := sha256.New()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if entry.IsDir() {
			_, _ = io.WriteString(hash, "d\x00"+filepath.ToSlash(rel)+"\x00")
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		_, _ = io.WriteString(hash, "f\x00"+filepath.ToSlash(rel)+"\x00")
		file, err := os.Open(path) // #nosec G304 -- path is enumerated below a frozen audit boundary.
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(hash, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

type ModelValueJudgment struct {
	ObservationID      string   `json:"observation_id"`
	OverallValue       string   `json:"overall_value"`
	ChangeEffect       string   `json:"change_effect"`
	UniqueContribution string   `json:"unique_contribution"`
	DownstreamEvidence string   `json:"downstream_evidence"`
	Confidence         string   `json:"confidence"`
	EvidenceCoverage   string   `json:"evidence_coverage"`
	Note               string   `json:"note,omitempty"`
	Consultations      []string `json:"consultations,omitempty"`
}

type ModelValueBatch struct {
	BatchID      string               `json:"batch_id"`
	Observations []ModelValueJudgment `json:"observations"`
	Provenance   BatchProvenance      `json:"-"`
}

type BatchProvenance struct {
	CLI       string `json:"cli"`
	Model     string `json:"model"`
	Effort    string `json:"reasoning_effort"`
	SessionID string `json:"session_id"`
}

type ValueObservation struct {
	ObservationSkeleton
	OverallValue       string   `json:"overall_value"`
	ChangeEffect       string   `json:"change_effect"`
	UniqueContribution string   `json:"unique_contribution"`
	DownstreamEvidence string   `json:"downstream_evidence"`
	Confidence         string   `json:"confidence"`
	EvidenceCoverage   string   `json:"evidence_coverage"`
	Note               string   `json:"note,omitempty"`
	Consultations      []string `json:"consultations,omitempty"`
	JudgeModel         string   `json:"judge_model"`
	JudgeCLI           string   `json:"judge_cli"`
	JudgeEffort        string   `json:"judge_reasoning_effort"`
	JudgeSessionID     string   `json:"judge_session_id"`
	RubricVersion      string   `json:"rubric_version"`
}

type ValueValidationResult struct {
	SchemaVersion int                `json:"schema_version"`
	Fingerprints  Fingerprints       `json:"fingerprints"`
	Observations  []ValueObservation `json:"observations"`
	Diagnostics   []string           `json:"diagnostics,omitempty"`
}

// ValidateValueOutputs accepts exactly the allowlisted qualitative output from
// each batch. Immutable facts are joined from the prepared skeletons.
func ValidateValueOutputs(request Request, prepared PreparedValueAudit, outputs []ModelValueBatch) (ValueValidationResult, error) { //nolint:gocritic // Public validation boundary keeps the frozen request immutable.
	currentSnapshot, err := fingerprintTree(request.SnapshotPath)
	if err != nil {
		return ValueValidationResult{}, err
	}
	if currentSnapshot != prepared.Index.Fingerprints.SnapshotBefore {
		return ValueValidationResult{}, fmt.Errorf("frozen evidence changed after preparation")
	}
	if len(outputs) != len(prepared.Packages) {
		return ValueValidationResult{}, fmt.Errorf("value output batches = %d, want %d", len(outputs), len(prepared.Packages))
	}
	knownRefs := map[string]struct{}{}
	for _, ref := range prepared.Index.References {
		knownRefs[ref.ID] = struct{}{}
	}
	observations := []ValueObservation{}
	seenBatches := map[string]struct{}{}
	for _, pkg := range prepared.Packages {
		batch := findValueBatch(outputs, pkg.BatchID)
		if batch == nil {
			return ValueValidationResult{}, fmt.Errorf("missing model output for %s", pkg.BatchID)
		}
		if _, exists := seenBatches[batch.BatchID]; exists {
			return ValueValidationResult{}, fmt.Errorf("duplicate model output for %s", batch.BatchID)
		}
		seenBatches[batch.BatchID] = struct{}{}
		validated, err := validateValueBatch(&request, pkg, batch, knownRefs)
		if err != nil {
			return ValueValidationResult{}, err
		}
		observations = append(observations, validated...)
	}
	outputAfter, err := fingerprintTree(filepath.Join(request.AuditSessionDir, "model-output"))
	if err != nil {
		return ValueValidationResult{}, err
	}
	return ValueValidationResult{SchemaVersion: evidenceSchemaVersion, Fingerprints: Fingerprints{
		SnapshotBefore: prepared.Index.Fingerprints.SnapshotBefore, SnapshotAfter: currentSnapshot,
		OutputBefore: prepared.Index.Fingerprints.OutputBefore, OutputAfter: outputAfter,
	}, Observations: observations}, nil
}

func findValueBatch(outputs []ModelValueBatch, batchID string) *ModelValueBatch {
	for index := range outputs {
		if outputs[index].BatchID == batchID {
			return &outputs[index]
		}
	}
	return nil
}

func validateValueBatch(request *Request, pkg ValuePackage, batch *ModelValueBatch, knownRefs map[string]struct{}) ([]ValueObservation, error) {
	expected, incomplete := expectedValueObservations(pkg)
	if len(batch.Observations) != len(expected) {
		return nil, fmt.Errorf("%s observations = %d, want %d", pkg.BatchID, len(batch.Observations), len(expected))
	}
	provenance := batch.Provenance
	if provenance.CLI == "" {
		provenance = BatchProvenance{CLI: request.Crosscheck.CLI, Model: request.Crosscheck.Model, Effort: request.Crosscheck.Effort, SessionID: "unknown"}
	}
	observations := make([]ValueObservation, 0, len(batch.Observations))
	for index := range batch.Observations {
		judgment := &batch.Observations[index]
		skeleton, exists := expected[judgment.ObservationID]
		if !exists {
			return nil, fmt.Errorf("%s has unknown observation %q", pkg.BatchID, judgment.ObservationID)
		}
		delete(expected, judgment.ObservationID)
		if err := validateJudgment(judgment, knownRefs); err != nil {
			return nil, fmt.Errorf("%s observation %q: %w", pkg.BatchID, judgment.ObservationID, err)
		}
		if incomplete[judgment.ObservationID] && judgment.EvidenceCoverage == "complete" {
			return nil, fmt.Errorf("%s observation %q claims complete coverage despite omitted evidence", pkg.BatchID, judgment.ObservationID)
		}
		observations = append(observations, valueObservation(&skeleton, judgment, provenance))
	}
	if len(expected) != 0 {
		return nil, fmt.Errorf("%s omitted one or more observations", pkg.BatchID)
	}
	return observations, nil
}

func expectedValueObservations(pkg ValuePackage) (expected map[string]ObservationSkeleton, incomplete map[string]bool) {
	expected = make(map[string]ObservationSkeleton, len(pkg.Leaves))
	incomplete = make(map[string]bool, len(pkg.Leaves))
	for index := range pkg.Leaves {
		leaf := &pkg.Leaves[index]
		expected[leaf.Skeleton.ObservationID] = leaf.Skeleton
		incomplete[leaf.Skeleton.ObservationID] = len(leaf.OmittedCategories) != 0
		for _, ref := range leaf.Evidence {
			if ref.Status != "available" {
				incomplete[leaf.Skeleton.ObservationID] = true
			}
		}
	}
	return expected, incomplete
}

func valueObservation(skeleton *ObservationSkeleton, judgment *ModelValueJudgment, provenance BatchProvenance) ValueObservation {
	return ValueObservation{
		ObservationSkeleton: *skeleton, OverallValue: judgment.OverallValue, ChangeEffect: judgment.ChangeEffect,
		UniqueContribution: judgment.UniqueContribution, DownstreamEvidence: judgment.DownstreamEvidence,
		Confidence: judgment.Confidence, EvidenceCoverage: judgment.EvidenceCoverage, Note: judgment.Note,
		Consultations: append([]string(nil), judgment.Consultations...), JudgeModel: provenance.Model, JudgeCLI: provenance.CLI,
		JudgeEffort: provenance.Effort, JudgeSessionID: provenance.SessionID, RubricVersion: rubricVersion,
	}
}

func validateJudgment(judgment *ModelValueJudgment, knownRefs map[string]struct{}) error {
	if !oneOf(judgment.OverallValue, "high", "medium", "low", "none", "negative", "unknown") {
		return fmt.Errorf("invalid overall_value")
	}
	if !oneOf(judgment.ChangeEffect, "intended", "partial", "no_material_change", "regressive", "not_applicable", "unknown") {
		return fmt.Errorf("invalid change_effect")
	}
	if !oneOf(judgment.UniqueContribution, "unique", "complementary", "duplicative", "not_applicable", "unknown") {
		return fmt.Errorf("invalid unique_contribution")
	}
	if !oneOf(judgment.DownstreamEvidence, "confirmed", "supporting", "none", "contradicted", "unavailable") {
		return fmt.Errorf("invalid downstream_evidence")
	}
	if !oneOf(judgment.Confidence, "high", "medium", "low") {
		return fmt.Errorf("invalid confidence")
	}
	if !oneOf(judgment.EvidenceCoverage, "complete", "partial", "limited") {
		return fmt.Errorf("invalid evidence_coverage")
	}
	if err := safeValueNote(judgment.Note); err != nil {
		return err
	}
	for _, consultation := range judgment.Consultations {
		if _, exists := knownRefs[consultation]; !exists {
			return fmt.Errorf("unknown consultation %q", consultation)
		}
	}
	return nil
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func safeValueNote(note string) error {
	if note == "" {
		return nil
	}
	if utf8.RuneCountInString(note) > 280 || strings.ContainsAny(note, "\r\n") {
		return fmt.Errorf("note is not a bounded single line")
	}
	lower := strings.ToLower(note)
	if strings.Contains(note, "://") || strings.ContainsAny(note, "\\") || strings.Contains(note, "/") ||
		strings.Contains(lower, "ghp_") || strings.Contains(lower, "sk-") || strings.Contains(lower, "token=") {
		return fmt.Errorf("note contains unsafe detailed evidence")
	}
	return nil
}

func loadPreparedValueAudit(auditSessionDir string) (PreparedValueAudit, error) {
	if err := verifyPreparedFingerprint(auditSessionDir); err != nil {
		return PreparedValueAudit{}, err
	}
	indexData, err := os.ReadFile(filepath.Join(auditSessionDir, "evidence-index.json")) // #nosec G304 -- fixed audit artifact.
	if err != nil {
		return PreparedValueAudit{}, err
	}
	packagesData, err := os.ReadFile(filepath.Join(auditSessionDir, "value-packages.json")) // #nosec G304 -- fixed audit artifact.
	if err != nil {
		return PreparedValueAudit{}, err
	}
	var prepared PreparedValueAudit
	if err := json.Unmarshal(indexData, &prepared.Index); err != nil {
		return PreparedValueAudit{}, fmt.Errorf("decode evidence index: %w", err)
	}
	if err := json.Unmarshal(packagesData, &prepared.Packages); err != nil {
		return PreparedValueAudit{}, fmt.Errorf("decode value packages: %w", err)
	}
	return prepared, nil
}

func loadModelValueBatches(auditSessionDir string, packages []ValuePackage) ([]ModelValueBatch, error) {
	outputs := make([]ModelValueBatch, 0, len(packages))
	for _, pkg := range packages {
		path := filepath.Join(auditSessionDir, "model-output", pkg.BatchID+".json")
		file, err := os.Open(path) // #nosec G304 -- deterministic output path within audit-owned directory.
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", pkg.BatchID, err)
		}
		decoder := json.NewDecoder(file)
		decoder.DisallowUnknownFields()
		var output ModelValueBatch
		decodeErr := decoder.Decode(&output)
		var extra any
		if decodeErr == nil && decoder.Decode(&extra) != io.EOF {
			decodeErr = fmt.Errorf("multiple JSON values")
		}
		closeErr := file.Close()
		if decodeErr != nil {
			return nil, fmt.Errorf("decode %s: %w", pkg.BatchID, decodeErr)
		}
		if closeErr != nil {
			return nil, closeErr
		}
		outputs = append(outputs, output)
	}
	provenanceData, err := os.ReadFile(filepath.Join(auditSessionDir, "value-batch-provenance.json")) // #nosec G304 -- fixed audit artifact.
	if err == nil {
		provenance := map[string]BatchProvenance{}
		if err := json.Unmarshal(provenanceData, &provenance); err != nil {
			return nil, fmt.Errorf("decode value batch provenance: %w", err)
		}
		for index := range outputs {
			outputs[index].Provenance = provenance[outputs[index].BatchID]
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	return outputs, nil
}

func validateValueStage(request *Request) error {
	prepared, err := loadPreparedValueAudit(request.AuditSessionDir)
	if err != nil {
		return err
	}
	outputs, err := loadModelValueBatches(request.AuditSessionDir, prepared.Packages)
	if err != nil {
		return err
	}
	result, err := ValidateValueOutputs(*request, prepared, outputs)
	if err != nil {
		return err
	}
	if err := stateio.WriteJSONAtomic(filepath.Join(request.AuditSessionDir, "value-observations.json"), result); err != nil {
		return err
	}
	return stateio.WriteJSONAtomic(filepath.Join(request.AuditSessionDir, "value-consultations.json"), consultationLedger(result.Observations, prepared.Index.References))
}

type consultationLedgerEntry struct {
	ObservationID string `json:"observation_id"`
	ReferenceID   string `json:"reference_id"`
	Category      string `json:"category"`
}

func consultationLedger(observations []ValueObservation, references []EvidenceReference) []consultationLedgerEntry {
	categories := map[string]string{}
	for _, reference := range references {
		categories[reference.ID] = reference.Category
	}
	ledger := []consultationLedgerEntry{}
	for index := range observations {
		observation := &observations[index]
		for _, referenceID := range observation.Consultations {
			ledger = append(ledger, consultationLedgerEntry{ObservationID: observation.ObservationID, ReferenceID: referenceID, Category: categories[referenceID]})
		}
	}
	sort.Slice(ledger, func(i, j int) bool {
		if ledger[i].ObservationID == ledger[j].ObservationID {
			return ledger[i].ReferenceID < ledger[j].ReferenceID
		}
		return ledger[i].ObservationID < ledger[j].ObservationID
	})
	return ledger
}

func ensureValueOutputs(request *Request) error {
	prepared, err := loadPreparedValueAudit(request.AuditSessionDir)
	if err != nil {
		return err
	}
	provenance := map[string]BatchProvenance{}
	for _, pkg := range prepared.Packages {
		outputPath := filepath.Join(request.AuditSessionDir, "model-output", pkg.BatchID+".json")
		if _, err := os.Stat(outputPath); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return err
		}
		output, err := invokeCrosscheckValueBatch(request, pkg)
		if err != nil {
			_ = stateio.WriteJSONAtomic(filepath.Join(request.AuditSessionDir, "value-model-diagnostics.json"), map[string]string{"batch_id": pkg.BatchID, "error": err.Error()})
			return fmt.Errorf("value model session for %s: %w", pkg.BatchID, err)
		}
		if output.BatchID != pkg.BatchID {
			return fmt.Errorf("value model session returned batch %q, want %q", output.BatchID, pkg.BatchID)
		}
		provenance[pkg.BatchID] = output.Provenance
		if err := stateio.WriteJSONAtomic(filepath.Join(request.AuditSessionDir, "model-output", output.BatchID+".json"), output); err != nil {
			return err
		}
	}
	return stateio.WriteJSONAtomic(filepath.Join(request.AuditSessionDir, "value-batch-provenance.json"), provenance)
}

func invokeCrosscheckValueBatch(request *Request, pkg ValuePackage) (ModelValueBatch, error) {
	trustedInputs, err := trustedAuditInputsFingerprint(request)
	if err != nil {
		return ModelValueBatch{}, err
	}
	if request.Crosscheck.CLI == "" {
		return ModelValueBatch{}, fmt.Errorf("frozen crosscheck CLI is unavailable")
	}
	adapter, err := cli.Get(request.Crosscheck.CLI)
	if err != nil {
		return ModelValueBatch{}, err
	}
	input, err := json.Marshal(pkg)
	if err != nil {
		return ModelValueBatch{}, err
	}
	prompt := "You are judging workflow-step value. Return exactly one JSON object matching the supplied batch, with one observation for every skeleton. You may fill only observation_id, overall_value, change_effect, unique_contribution, downstream_evidence, confidence, evidence_coverage, optional note, and consultation references. Do not include measured fields, paths, transcripts, or prose outside JSON. The audit workspace contains read-only snapshotted evidence; record only supplied consultation identifiers.\n\n" + string(input)
	workspace, err := prepareModelWorkspace(request)
	if err != nil {
		return ModelValueBatch{}, err
	}
	args, err := cli.BuildInvocationArgs(adapter, &cli.BuildArgsInput{
		Prompt: prompt, Model: request.Crosscheck.Model, Effort: request.Crosscheck.Effort,
		Context: cli.ContextAutonomousHeadless, Workdir: workspace,
		DisallowedTools: []string{"AskUserQuestion"},
	})
	if err != nil {
		return ModelValueBatch{}, err
	}
	if len(args) == 0 {
		return ModelValueBatch{}, fmt.Errorf("crosscheck adapter produced no command")
	}
	command, err := crosscheckCommand(args, workspace, filepath.Join(request.AuditSessionDir, "model-output"))
	if err != nil {
		return ModelValueBatch{}, err
	}
	env, err := cliEnvironment(adapter, request, input, workspace)
	if err != nil {
		return ModelValueBatch{}, err
	}
	command.Env = env
	result, runErr := command.Output()
	if after, err := trustedAuditInputsFingerprint(request); err != nil {
		return ModelValueBatch{}, err
	} else if after != trustedInputs {
		return ModelValueBatch{}, fmt.Errorf("trusted audit inputs changed during crosscheck")
	}
	if runErr != nil {
		return ModelValueBatch{}, fmt.Errorf("run crosscheck: %w", runErr)
	}
	response := string(result)
	if filter, ok := adapter.(cli.OutputFilter); ok {
		response = filter.FilterOutput(response)
	}
	decoder := json.NewDecoder(strings.NewReader(response))
	decoder.DisallowUnknownFields()
	var output ModelValueBatch
	if err := decoder.Decode(&output); err != nil {
		return ModelValueBatch{}, fmt.Errorf("decode crosscheck result: %w", err)
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return ModelValueBatch{}, fmt.Errorf("crosscheck result contains multiple JSON values")
	}
	output.Provenance = BatchProvenance{CLI: request.Crosscheck.CLI, Model: request.Crosscheck.Model, Effort: request.Crosscheck.Effort, SessionID: adapter.DiscoverSessionID(&cli.DiscoverOptions{SpawnTime: time.Now(), Headless: true, ProcessOutput: response, Workdir: workspace})}
	if output.Provenance.SessionID == "" {
		output.Provenance.SessionID = "unknown"
	}
	return output, nil
}

func trustedAuditInputsFingerprint(request *Request) (string, error) {
	prepared, err := preparedFingerprint(request.AuditSessionDir)
	if err != nil {
		return "", err
	}
	snapshot, err := fingerprintTree(request.SnapshotPath)
	if err != nil {
		return "", err
	}
	requestJSON, err := json.Marshal(request)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(append(append([]byte(prepared+"\x00"+snapshot+"\x00"), requestJSON...), 0))
	return hex.EncodeToString(sum[:]), nil
}

func sandboxedCrosscheckCommand(args []string, workspace, outputDir string) (*exec.Cmd, error) {
	if runtime.GOOS != "darwin" {
		return nil, fmt.Errorf("OS-enforced audit filesystem sandbox is unavailable on %s", runtime.GOOS)
	}
	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		return nil, fmt.Errorf("resolve audit sandbox: %w", err)
	}
	if err := os.MkdirAll(outputDir, 0o700); err != nil {
		return nil, err
	}
	argv := sandboxExecArgs(args, outputDir)
	command := exec.Command("sandbox-exec", argv...) // #nosec G204 -- registered adapter argv is wrapped in an audit-owned OS sandbox.
	command.Dir = workspace
	return command, nil
}

func sandboxExecArgs(args []string, outputDir string) []string {
	argv := []string{"-D", "OUTPUT_DIR=" + outputDir, "-p", auditSandboxProfile, "--"}
	return append(argv, args...)
}

func cliEnvironment(adapter cli.Adapter, request *Request, input []byte, workdir string) ([]string, error) {
	// Adapters may require isolated process-local setup. It is derived from this
	// batch only and never persisted in the source snapshot.
	build := &cli.BuildArgsInput{Prompt: string(input), Model: request.Crosscheck.Model, Effort: request.Crosscheck.Effort, Context: cli.ContextAutonomousHeadless, Workdir: workdir}
	extra, err := cli.SpawnEnvForInvocation(adapter, build)
	if err != nil {
		return nil, fmt.Errorf("prepare crosscheck environment: %w", err)
	}
	return append(os.Environ(), extra...), nil
}

func prepareModelWorkspace(request *Request) (string, error) {
	workspace := filepath.Join(request.AuditSessionDir, "model-workspace")
	if err := os.Chmod(workspace, 0o700); err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if err := os.RemoveAll(workspace); err != nil {
		return "", err
	}
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		return "", err
	}
	if err := os.Symlink(request.SnapshotPath, filepath.Join(workspace, "evidence")); err != nil {
		return "", err
	}
	for _, name := range []string{"evidence-index.json", "value-packages.json", "evidence-reference-manifest.json", "prepared-fingerprint.json"} {
		if err := os.Symlink(filepath.Join(request.AuditSessionDir, name), filepath.Join(workspace, name)); err != nil {
			return "", err
		}
	}
	if err := os.Chmod(workspace, 0o500); err != nil {
		return "", err
	}
	return workspace, nil
}
